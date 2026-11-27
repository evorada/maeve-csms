// SPDX-License-Identifier: Apache-2.0

package tenancy_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
	clockTest "k8s.io/utils/clock/testing"
)

const (
	orgA       = "0193b2c4-0000-7000-8000-0000000000a1"
	defaultOrg = "0193b2c4-0000-7000-8000-0000000000dd"
)

// fixedStationAuth answers every lookup with the same auth details or error.
type fixedStationAuth struct {
	store.ChargeStationAuthStore
	auth *store.ChargeStationAuth
	err  error
}

func (s fixedStationAuth) LookupChargeStationAuth(context.Context, string) (*store.ChargeStationAuth, error) {
	return s.auth, s.err
}

func TestResolverOrgIdForStation(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, engine.SetChargeStationAuth(ctx, "cs-with-org", &store.ChargeStationAuth{OrgId: orgA}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "cs-without-org", &store.ChargeStationAuth{}))

	resolver := &tenancy.Resolver{Stations: engine, DefaultOrgId: defaultOrg}

	tests := map[string]struct {
		chargeStationId string
		want            string
	}{
		"a station with an org belongs to that org":           {chargeStationId: "cs-with-org", want: orgA},
		"a station without an org belongs to the default org": {chargeStationId: "cs-without-org", want: defaultOrg},
		"a station that is not known gets the default org":    {chargeStationId: "cs-unknown", want: defaultOrg},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := resolver.OrgIdForStation(ctx, tt.chargeStationId)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// PostgreSQL stores a station that is registered without an org in the legacy org,
// whose rows go to the default org when the manager starts.
func TestResolverTreatsTheLegacyOrgAsNoOrg(t *testing.T) {
	resolver := &tenancy.Resolver{
		Stations:     fixedStationAuth{auth: &store.ChargeStationAuth{OrgId: store.LegacyOrgId}},
		DefaultOrgId: defaultOrg,
	}

	got, err := resolver.OrgIdForStation(context.Background(), "cs001")
	require.NoError(t, err)
	assert.Equal(t, defaultOrg, got)
}

func TestResolverWithoutDefaultOrgUsesTheLegacyOrg(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, engine.SetChargeStationAuth(ctx, "cs-with-org", &store.ChargeStationAuth{OrgId: orgA}))
	resolver := &tenancy.Resolver{Stations: engine}

	got, err := resolver.OrgIdForStation(ctx, "cs-unknown")
	require.NoError(t, err)
	assert.Equal(t, store.LegacyOrgId, got)

	got, err = resolver.OrgIdForStation(ctx, "cs-with-org")
	require.NoError(t, err)
	assert.Equal(t, orgA, got)
}

func TestResolverReturnsStoreError(t *testing.T) {
	lookupErr := errors.New("store is down")
	resolver := &tenancy.Resolver{Stations: fixedStationAuth{err: lookupErr}, DefaultOrgId: defaultOrg}

	got, err := resolver.OrgIdForStation(context.Background(), "cs001")
	assert.ErrorIs(t, err, lookupErr)
	assert.Empty(t, got)
}

const orgB = "0193b2c4-0000-7000-8000-0000000000b2"

// countingTenants counts the lookups that reach the tenant store.
type countingTenants struct {
	store.TenantStore
	byHost   int
	byOrgKey int
	byOrgId  int
	err      error
}

func (c *countingTenants) GetTenant(ctx context.Context, orgId string) (*store.Tenant, error) {
	c.byOrgId++
	if c.err != nil {
		return nil, c.err
	}
	return c.TenantStore.GetTenant(ctx, orgId)
}

func (c *countingTenants) GetTenantByHost(ctx context.Context, host string) (*store.Tenant, error) {
	c.byHost++
	if c.err != nil {
		return nil, c.err
	}
	return c.TenantStore.GetTenantByHost(ctx, host)
}

func (c *countingTenants) GetTenantByOrgKey(ctx context.Context, orgKey string) (*store.Tenant, error) {
	c.byOrgKey++
	if c.err != nil {
		return nil, c.err
	}
	return c.TenantStore.GetTenantByOrgKey(ctx, orgKey)
}

// newMultiResolver returns a multi mode resolver on an in-memory store with the tenants
// A (org key aaaa, host a.example) and B (org key bbbb, hosts b.example and
// b2.example), with a clock that only moves when the test moves it.
func newMultiResolver(t *testing.T) (*tenancy.Resolver, *countingTenants, *inmemory.Store, *clockTest.FakeClock) {
	t.Helper()
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: orgA, Path: store.OrgPathLabel(orgA), Slug: "tenant-a", OrgKey: "aaaa", Hostnames: []string{"a.example"},
	}))
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: orgB, Path: store.OrgPathLabel(orgB), Slug: "tenant-b", OrgKey: "bbbb", Hostnames: []string{"b.example", "b2.example"},
	}))

	tenants := &countingTenants{TenantStore: engine}
	fakeClock := clockTest.NewFakeClock(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	resolver := &tenancy.Resolver{
		Mode:         tenancy.ModeMulti,
		Tenants:      tenants,
		Stations:     engine,
		DefaultOrgId: defaultOrg,
		Clock:        fakeClock,
		CacheTTL:     30 * time.Second,
	}
	return resolver, tenants, engine, fakeClock
}

func TestResolveStationInSingleModeIgnoresTheHost(t *testing.T) {
	tenants := &countingTenants{}
	for name, mode := range map[string]tenancy.Mode{"single": tenancy.ModeSingle, "not set": ""} {
		t.Run(name, func(t *testing.T) {
			resolver := &tenancy.Resolver{Mode: mode, Tenants: tenants, DefaultOrgId: defaultOrg}

			for _, host := range []string{"a.example", "unknown.example", ""} {
				res, err := resolver.ResolveStation(context.Background(), host, "CP001")
				require.NoError(t, err)
				assert.Equal(t, &tenancy.StationResolution{OrgId: defaultOrg, StationKey: "CP001"}, res)
			}
		})
	}
	assert.Zero(t, tenants.byHost)

	t.Run("without a default org it is the legacy org", func(t *testing.T) {
		res, err := (&tenancy.Resolver{}).ResolveStation(context.Background(), "a.example", "CP001")
		require.NoError(t, err)
		assert.Equal(t, &tenancy.StationResolution{OrgId: store.LegacyOrgId, StationKey: "CP001"}, res)
	})
}

func TestResolveStationInMultiMode(t *testing.T) {
	resolver, _, _, _ := newMultiResolver(t)

	tests := map[string]struct {
		host       string
		wantOrgId  string
		wantOrgKey string
		wantKey    string
	}{
		"host of tenant A":         {host: "a.example", wantOrgId: orgA, wantOrgKey: "aaaa", wantKey: "aaaa.CP001"},
		"host of tenant B":         {host: "b.example", wantOrgId: orgB, wantOrgKey: "bbbb", wantKey: "bbbb.CP001"},
		"second host of tenant B":  {host: "b2.example", wantOrgId: orgB, wantOrgKey: "bbbb", wantKey: "bbbb.CP001"},
		"host with a port":         {host: "a.example:443", wantOrgId: orgA, wantOrgKey: "aaaa", wantKey: "aaaa.CP001"},
		"host in mixed case":       {host: "B.Example", wantOrgId: orgB, wantOrgKey: "bbbb", wantKey: "bbbb.CP001"},
		"mixed case with a port":   {host: "A.EXAMPLE:9310", wantOrgId: orgA, wantOrgKey: "aaaa", wantKey: "aaaa.CP001"},
		"host with a trailing dot": {host: "a.example.", wantOrgId: orgA, wantOrgKey: "aaaa", wantKey: "aaaa.CP001"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			res, err := resolver.ResolveStation(context.Background(), tt.host, "CP001")
			require.NoError(t, err)
			assert.Equal(t, tt.wantOrgId, res.OrgId)
			assert.Equal(t, tt.wantOrgKey, res.OrgKey)
			assert.Equal(t, tt.wantKey, res.StationKey)
			require.NotNil(t, res.Tenant)
			assert.Equal(t, tt.wantOrgId, res.Tenant.OrgId)
		})
	}

	t.Run("a station id with a dot keeps it after the org key", func(t *testing.T) {
		res, err := resolver.ResolveStation(context.Background(), "a.example", "bbbb.CP001")
		require.NoError(t, err)
		assert.Equal(t, "aaaa.bbbb.CP001", res.StationKey)
		assert.Equal(t, orgA, res.OrgId)
	})
}

func TestResolveStationWithUnknownHost(t *testing.T) {
	resolver, tenants, _, _ := newMultiResolver(t)

	for name, host := range map[string]string{
		"host of no tenant":         "c.example",
		"subdomain of a tenant":     "x.a.example",
		"parent domain of a tenant": "example",
		"org key as host":           "aaaa",
		"empty":                     "",
		"only a port":               ":443",
		"longer than a DNS name":    strings.Repeat("a", 254),
	} {
		t.Run(name, func(t *testing.T) {
			res, err := resolver.ResolveStation(context.Background(), host, "CP001")
			assert.ErrorIs(t, err, tenancy.ErrUnknownHost)
			assert.Nil(t, res)
		})
	}

	t.Run("is not remembered: the host resolves as soon as a tenant has it", func(t *testing.T) {
		before := tenants.byHost
		_, err := resolver.ResolveStation(context.Background(), "new.example", "CP001")
		require.ErrorIs(t, err, tenancy.ErrUnknownHost)
		_, err = resolver.ResolveStation(context.Background(), "new.example", "CP001")
		require.ErrorIs(t, err, tenancy.ErrUnknownHost)
		assert.Equal(t, before+2, tenants.byHost)
	})
}

func TestResolveStationFailsWhenTheTenantStoreFails(t *testing.T) {
	resolver, tenants, _, _ := newMultiResolver(t)
	tenants.err = errors.New("store is down")

	res, err := resolver.ResolveStation(context.Background(), "a.example", "CP001")
	require.ErrorIs(t, err, tenants.err)
	assert.NotErrorIs(t, err, tenancy.ErrUnknownHost)
	assert.Nil(t, res)

	t.Run("and without a tenant store", func(t *testing.T) {
		res, err := (&tenancy.Resolver{Mode: tenancy.ModeMulti}).ResolveStation(context.Background(), "a.example", "CP001")
		require.Error(t, err)
		assert.NotErrorIs(t, err, tenancy.ErrUnknownHost)
		assert.Nil(t, res)
	})
}

// fixedTenant answers every lookup by host with the same tenant.
type fixedTenant struct {
	store.TenantStore
	tenant *store.Tenant
}

func (f fixedTenant) GetTenantByHost(context.Context, string) (*store.Tenant, error) {
	return f.tenant, nil
}

// Without an org key the key of a station would be its plain id, which is the key of a
// station from before tenants.
func TestResolveStationRefusesATenantWithoutAProperOrgKey(t *testing.T) {
	for name, orgKey := range map[string]string{"empty": "", "with a dot": "aaaa.bbbb", "too short": "abc"} {
		t.Run(name, func(t *testing.T) {
			resolver := &tenancy.Resolver{
				Mode:    tenancy.ModeMulti,
				Tenants: fixedTenant{tenant: &store.Tenant{OrgId: orgA, OrgKey: orgKey}},
			}
			res, err := resolver.ResolveStation(context.Background(), "a.example", "CP001")
			require.Error(t, err)
			assert.NotErrorIs(t, err, tenancy.ErrUnknownHost)
			assert.Nil(t, res)
		})
	}
}

func TestResolverCachesTenants(t *testing.T) {
	ctx := context.Background()

	t.Run("a cache hit does not read the store again", func(t *testing.T) {
		resolver, tenants, _, _ := newMultiResolver(t)
		for i := 0; i < 3; i++ {
			res, err := resolver.ResolveStation(ctx, "a.example", "CP001")
			require.NoError(t, err)
			assert.Equal(t, "aaaa.CP001", res.StationKey)
		}
		assert.Equal(t, 1, tenants.byHost)

		// the same host in another spelling is the same entry
		_, err := resolver.ResolveStation(ctx, "A.Example:443", "CP002")
		require.NoError(t, err)
		assert.Equal(t, 1, tenants.byHost)
	})

	t.Run("the host is part of the cache key", func(t *testing.T) {
		resolver, tenants, _, _ := newMultiResolver(t)
		resA, err := resolver.ResolveStation(ctx, "a.example", "CP001")
		require.NoError(t, err)
		resB, err := resolver.ResolveStation(ctx, "b.example", "CP001")
		require.NoError(t, err)
		assert.Equal(t, orgA, resA.OrgId)
		assert.Equal(t, orgB, resB.OrgId)
		assert.Equal(t, 2, tenants.byHost)

		// and again from the cache
		resA, err = resolver.ResolveStation(ctx, "a.example", "CP001")
		require.NoError(t, err)
		resB, err = resolver.ResolveStation(ctx, "b.example", "CP001")
		require.NoError(t, err)
		assert.Equal(t, "aaaa.CP001", resA.StationKey)
		assert.Equal(t, "bbbb.CP001", resB.StationKey)
		assert.Equal(t, 2, tenants.byHost)
	})

	t.Run("an entry is read again when its TTL has passed", func(t *testing.T) {
		resolver, tenants, _, fakeClock := newMultiResolver(t)
		_, err := resolver.ResolveStation(ctx, "a.example", "CP001")
		require.NoError(t, err)

		fakeClock.Step(30*time.Second - time.Nanosecond)
		_, err = resolver.ResolveStation(ctx, "a.example", "CP001")
		require.NoError(t, err)
		assert.Equal(t, 1, tenants.byHost)

		fakeClock.Step(time.Nanosecond)
		_, err = resolver.ResolveStation(ctx, "a.example", "CP001")
		require.NoError(t, err)
		assert.Equal(t, 2, tenants.byHost)
	})

	t.Run("without a TTL it is 30 seconds", func(t *testing.T) {
		resolver, tenants, _, fakeClock := newMultiResolver(t)
		resolver.CacheTTL = 0
		_, err := resolver.ResolveStation(ctx, "a.example", "CP001")
		require.NoError(t, err)

		fakeClock.Step(tenancy.DefaultCacheTTL - time.Nanosecond)
		_, err = resolver.ResolveStation(ctx, "a.example", "CP001")
		require.NoError(t, err)
		assert.Equal(t, 1, tenants.byHost)

		fakeClock.Step(time.Nanosecond)
		_, err = resolver.ResolveStation(ctx, "a.example", "CP001")
		require.NoError(t, err)
		assert.Equal(t, 2, tenants.byHost)
	})

	t.Run("a store error is not cached", func(t *testing.T) {
		resolver, tenants, _, _ := newMultiResolver(t)
		tenants.err = errors.New("store is down")
		_, err := resolver.ResolveStation(ctx, "a.example", "CP001")
		require.Error(t, err)

		tenants.err = nil
		res, err := resolver.ResolveStation(ctx, "a.example", "CP001")
		require.NoError(t, err)
		assert.Equal(t, "aaaa.CP001", res.StationKey)
	})
}

func TestResolverInvalidateForcesAReload(t *testing.T) {
	ctx := context.Background()
	resolver, tenants, engine, _ := newMultiResolver(t)

	res, err := resolver.ResolveStation(ctx, "b.example", "CP001")
	require.NoError(t, err)
	assert.Equal(t, store.TenantStatusActive, res.Tenant.Status)
	tenant, err := resolver.TenantForStationKey(ctx, "bbbb.CP001")
	require.NoError(t, err)
	require.NotNil(t, tenant)
	_, err = resolver.ResolveStation(ctx, "a.example", "CP001")
	require.NoError(t, err)
	require.Equal(t, 2, tenants.byHost)
	require.Equal(t, 1, tenants.byOrgKey)

	// tenant B changes: it is suspended and loses the host b.example
	found, err := engine.SetTenantStatus(ctx, orgB, store.TenantStatusSuspended)
	require.NoError(t, err)
	require.True(t, found)
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: orgB, Path: store.OrgPathLabel(orgB), Slug: "tenant-b", OrgKey: "bbbb", Hostnames: []string{"b2.example"},
	}))

	// the cache still answers with what it has
	res, err = resolver.ResolveStation(ctx, "b.example", "CP001")
	require.NoError(t, err)
	assert.Equal(t, store.TenantStatusActive, res.Tenant.Status)

	// the org id is compared whatever its case
	resolver.Invalidate(strings.ToUpper(orgB))

	_, err = resolver.ResolveStation(ctx, "b.example", "CP001")
	assert.ErrorIs(t, err, tenancy.ErrUnknownHost)
	res, err = resolver.ResolveStation(ctx, "b2.example", "CP001")
	require.NoError(t, err)
	assert.Equal(t, store.TenantStatusSuspended, res.Tenant.Status)
	tenant, err = resolver.TenantForStationKey(ctx, "bbbb.CP001")
	require.NoError(t, err)
	require.NotNil(t, tenant)
	assert.Equal(t, store.TenantStatusSuspended, tenant.Status)
	assert.Equal(t, 2, tenants.byOrgKey)

	// the entries of the other tenants stay
	before := tenants.byHost
	_, err = resolver.ResolveStation(ctx, "a.example", "CP001")
	require.NoError(t, err)
	assert.Equal(t, before, tenants.byHost)
}

func TestTenantForStationKey(t *testing.T) {
	ctx := context.Background()
	resolver, tenants, _, _ := newMultiResolver(t)

	t.Run("the tenant that has the org key", func(t *testing.T) {
		for key, wantOrgId := range map[string]string{
			"aaaa.CP001":     orgA,
			"bbbb.CP001":     orgB,
			"bbbb.aaaa.CP01": orgB,
		} {
			tenant, err := resolver.TenantForStationKey(ctx, key)
			require.NoError(t, err)
			require.NotNil(t, tenant, key)
			assert.Equal(t, wantOrgId, tenant.OrgId, key)
		}
	})

	t.Run("is cached by org key", func(t *testing.T) {
		before := tenants.byOrgKey
		_, err := resolver.TenantForStationKey(ctx, "aaaa.CP002")
		require.NoError(t, err)
		_, err = resolver.TenantForStationKey(ctx, "aaaa.CP003")
		require.NoError(t, err)
		assert.Equal(t, before, tenants.byOrgKey)
	})

	t.Run("nil when no tenant has the org key or the key has none", func(t *testing.T) {
		for _, key := range []string{"cccc.CP001", "CP001", ".CP001", "AAAA.CP001", "CP.001", ""} {
			tenant, err := resolver.TenantForStationKey(ctx, key)
			require.NoError(t, err, key)
			assert.Nil(t, tenant, key)
		}
	})

	t.Run("nil in single mode", func(t *testing.T) {
		single := &tenancy.Resolver{Mode: tenancy.ModeSingle, Tenants: tenants}
		before := tenants.byOrgKey
		tenant, err := single.TenantForStationKey(ctx, "aaaa.CP001")
		require.NoError(t, err)
		assert.Nil(t, tenant)
		assert.Equal(t, before, tenants.byOrgKey)
	})

	t.Run("a store error is returned", func(t *testing.T) {
		tenants.err = errors.New("store is down")
		defer func() { tenants.err = nil }()
		tenant, err := resolver.TenantForStationKey(ctx, "dddd.CP001")
		require.ErrorIs(t, err, tenants.err)
		assert.Nil(t, tenant)
	})
}

func TestOrgIdForStationFallsBackToTheTenantOfTheKeyPrefix(t *testing.T) {
	ctx := context.Background()
	resolver, tenants, engine, _ := newMultiResolver(t)
	require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.with-org-b", &store.ChargeStationAuth{OrgId: orgB}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.without-org", &store.ChargeStationAuth{}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "bbbb.legacy-org", &store.ChargeStationAuth{OrgId: store.LegacyOrgId}))

	tests := map[string]struct {
		stationKey string
		want       string
	}{
		"the org of the auth details comes first":               {stationKey: "aaaa.with-org-b", want: orgB},
		"a station without an org has the org of its prefix":    {stationKey: "aaaa.without-org", want: orgA},
		"a station of the legacy org has the org of its prefix": {stationKey: "bbbb.legacy-org", want: orgB},
		"a station that is not known has the org of its prefix": {stationKey: "bbbb.unknown", want: orgB},
		"a prefix of no tenant gives the default org":           {stationKey: "cccc.unknown", want: defaultOrg},
		"a key without a prefix gives the default org":          {stationKey: "unknown", want: defaultOrg},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := resolver.OrgIdForStation(ctx, tt.stationKey)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("fails when the tenant store fails", func(t *testing.T) {
		tenants.err = errors.New("store is down")
		defer func() { tenants.err = nil }()
		got, err := resolver.OrgIdForStation(ctx, "dddd.unknown")
		require.ErrorIs(t, err, tenants.err)
		assert.Empty(t, got)
	})

	t.Run("single mode does not look at the prefix", func(t *testing.T) {
		single := &tenancy.Resolver{Mode: tenancy.ModeSingle, Tenants: tenants, Stations: engine, DefaultOrgId: defaultOrg}
		before := tenants.byOrgKey
		got, err := single.OrgIdForStation(ctx, "aaaa.without-org")
		require.NoError(t, err)
		assert.Equal(t, defaultOrg, got)
		assert.Equal(t, before, tenants.byOrgKey)
	})
}

func TestTenantByOrgId(t *testing.T) {
	ctx := context.Background()

	t.Run("single mode has no tenants", func(t *testing.T) {
		resolver, tenants, _, _ := newMultiResolver(t)
		resolver.Mode = tenancy.ModeSingle

		tenant, err := resolver.TenantByOrgId(ctx, orgA)
		require.NoError(t, err)
		assert.Nil(t, tenant)
		assert.Zero(t, tenants.byOrgId)
	})

	t.Run("the tenant is kept until the cache expires", func(t *testing.T) {
		resolver, tenants, engine, fakeClock := newMultiResolver(t)

		tenant, err := resolver.TenantByOrgId(ctx, orgB)
		require.NoError(t, err)
		require.NotNil(t, tenant)
		assert.Equal(t, orgB, tenant.OrgId)
		assert.Equal(t, store.TenantStatusActive, tenant.Status)

		found, err := engine.SetTenantStatus(ctx, orgB, store.TenantStatusSuspended)
		require.NoError(t, err)
		require.True(t, found)

		// the org id is looked up whatever its case
		tenant, err = resolver.TenantByOrgId(ctx, strings.ToUpper(orgB))
		require.NoError(t, err)
		require.NotNil(t, tenant)
		assert.Equal(t, store.TenantStatusActive, tenant.Status)
		assert.Equal(t, 1, tenants.byOrgId)

		fakeClock.Step(30 * time.Second)
		tenant, err = resolver.TenantByOrgId(ctx, orgB)
		require.NoError(t, err)
		require.NotNil(t, tenant)
		assert.Equal(t, store.TenantStatusSuspended, tenant.Status)
		assert.Equal(t, 2, tenants.byOrgId)
	})

	t.Run("Invalidate drops the tenant", func(t *testing.T) {
		resolver, tenants, engine, _ := newMultiResolver(t)

		_, err := resolver.TenantByOrgId(ctx, orgA)
		require.NoError(t, err)
		_, err = resolver.TenantByOrgId(ctx, orgB)
		require.NoError(t, err)
		require.Equal(t, 2, tenants.byOrgId)

		found, err := engine.SetTenantStatus(ctx, orgB, store.TenantStatusSuspended)
		require.NoError(t, err)
		require.True(t, found)
		resolver.Invalidate(orgB)

		tenant, err := resolver.TenantByOrgId(ctx, orgB)
		require.NoError(t, err)
		require.NotNil(t, tenant)
		assert.Equal(t, store.TenantStatusSuspended, tenant.Status)
		assert.Equal(t, 3, tenants.byOrgId)

		// the entries of the other tenants stay
		_, err = resolver.TenantByOrgId(ctx, orgA)
		require.NoError(t, err)
		assert.Equal(t, 3, tenants.byOrgId)
	})

	t.Run("an org that is not a tenant", func(t *testing.T) {
		resolver, tenants, engine, _ := newMultiResolver(t)

		for _, orgId := range []string{defaultOrg, store.LegacyOrgId, ""} {
			tenant, err := resolver.TenantByOrgId(ctx, orgId)
			require.NoError(t, err, orgId)
			assert.Nil(t, tenant, orgId)
		}
		// the store is not asked about no org at all
		assert.Equal(t, 2, tenants.byOrgId)

		// that there is no tenant is not kept: the tenant is found once it exists
		require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
			OrgId: defaultOrg, Path: store.OrgPathLabel(defaultOrg), Slug: "tenant-d", OrgKey: "dddd",
		}))
		tenant, err := resolver.TenantByOrgId(ctx, defaultOrg)
		require.NoError(t, err)
		require.NotNil(t, tenant)
		assert.Equal(t, defaultOrg, tenant.OrgId)
	})

	t.Run("a failure of the tenant store is returned and not kept", func(t *testing.T) {
		resolver, tenants, _, _ := newMultiResolver(t)
		tenants.err = errors.New("database is down")

		tenant, err := resolver.TenantByOrgId(ctx, orgA)
		require.ErrorIs(t, err, tenants.err)
		assert.Nil(t, tenant)

		tenants.err = nil
		tenant, err = resolver.TenantByOrgId(ctx, orgA)
		require.NoError(t, err)
		require.NotNil(t, tenant)
	})
}
