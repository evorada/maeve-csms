// SPDX-License-Identifier: Apache-2.0

package tenancy_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
)

const (
	// orgPartner is the parent of the tenants A and B. It is not a tenant itself.
	orgPartner = "0193b2c4-0000-7000-8000-0000000000f0"
	// orgC is a tenant that is not below the partner.
	orgC = "0193b2c4-0000-7000-8000-0000000000c3"
)

// subtreeCounter counts the subtree lookups that reach the tenant store.
type subtreeCounter struct {
	store.TenantStore
	calls int
	err   error
}

func (c *subtreeCounter) ListSubtreeOrgIds(ctx context.Context, orgId string) ([]string, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	return c.TenantStore.ListSubtreeOrgIds(ctx, orgId)
}

// newMultiScoper returns a multi mode scoper on an in-memory store with the tenants A
// (org key aaaa) and B (org key bbbb) below a partner that is not a tenant, and the
// tenant C (org key cccc) on its own.
func newMultiScoper(t *testing.T) (*tenancy.Scoper, *subtreeCounter, *inmemory.Store) {
	t.Helper()
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	partner := orgPartner
	for _, tenant := range []*store.Tenant{
		{OrgId: orgA, ParentId: &partner, Path: store.OrgPathLabel(orgPartner) + "." + store.OrgPathLabel(orgA), Slug: "tenant-a", OrgKey: "aaaa"},
		{OrgId: orgB, ParentId: &partner, Path: store.OrgPathLabel(orgPartner) + "." + store.OrgPathLabel(orgB), Slug: "tenant-b", OrgKey: "bbbb"},
		{OrgId: orgC, Path: store.OrgPathLabel(orgC), Slug: "tenant-c", OrgKey: "cccc"},
	} {
		require.NoError(t, engine.PutTenant(ctx, tenant))
	}

	tenants := &subtreeCounter{TenantStore: engine}
	return &tenancy.Scoper{
		Mode:         tenancy.ModeMulti,
		Tenants:      tenants,
		Resolver:     &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine, DefaultOrgId: defaultOrg},
		DefaultOrgId: defaultOrg,
	}, tenants, engine
}

func caller(orgId string, scope tenancy.OrgScope) *tenancy.Principal {
	return &tenancy.Principal{Subject: "user:test", OrgId: orgId, OrgScope: scope}
}

func TestScoperOrgIdsInSingleMode(t *testing.T) {
	ctx := context.Background()

	for name, mode := range map[string]tenancy.Mode{"single": tenancy.ModeSingle, "not set": ""} {
		t.Run(name, func(t *testing.T) {
			// the tenant store is not asked in single mode
			scoper := &tenancy.Scoper{Mode: mode, DefaultOrgId: defaultOrg}

			for _, scope := range []tenancy.OrgScope{tenancy.OrgScopeSelf, tenancy.OrgScopeSubtree} {
				got, err := scoper.OrgIds(ctx, caller(defaultOrg, scope))
				require.NoError(t, err)
				assert.Equal(t, []string{defaultOrg}, got, "the default org, scope %s", scope)

				got, err = scoper.OrgIds(ctx, caller(orgA, scope))
				require.NoError(t, err)
				assert.Empty(t, got, "another org, scope %s", scope)
			}
		})
	}

	t.Run("without a default org the legacy org is the only org", func(t *testing.T) {
		scoper := &tenancy.Scoper{}

		got, err := scoper.OrgIds(ctx, caller(store.LegacyOrgId, tenancy.OrgScopeSelf))
		require.NoError(t, err)
		assert.Equal(t, []string{store.LegacyOrgId}, got)

		got, err = scoper.OrgIds(ctx, caller(orgA, tenancy.OrgScopeSubtree))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("org ids are compared without regard to case", func(t *testing.T) {
		scoper := &tenancy.Scoper{DefaultOrgId: strings.ToUpper(defaultOrg)}

		inScope, err := scoper.InScope(ctx, caller(defaultOrg, tenancy.OrgScopeSelf), strings.ToUpper(defaultOrg))
		require.NoError(t, err)
		assert.True(t, inScope)
	})
}

func TestScoperOrgIdsInMultiMode(t *testing.T) {
	ctx := context.Background()
	scoper, _, _ := newMultiScoper(t)

	tests := map[string]struct {
		caller *tenancy.Principal
		want   []string
	}{
		"self is the org of the caller":                        {caller(orgA, tenancy.OrgScopeSelf), []string{orgA}},
		"self does not reach the orgs below":                   {caller(orgPartner, tenancy.OrgScopeSelf), []string{orgPartner}},
		"subtree of a partner that is not a tenant":            {caller(orgPartner, tenancy.OrgScopeSubtree), []string{orgA, orgB}},
		"subtree of a tenant without tenants below":            {caller(orgA, tenancy.OrgScopeSubtree), []string{orgA}},
		"subtree of an org that is not known":                  {caller(defaultOrg, tenancy.OrgScopeSubtree), nil},
		"a scope that is not known reaches nothing":            {caller(orgA, tenancy.OrgScope("all")), nil},
		"no scope reaches nothing":                             {caller(orgA, ""), nil},
		"a caller without an org reaches nothing":              {caller("", tenancy.OrgScopeSubtree), nil},
		"no caller reaches nothing":                            {nil, nil},
		"the org of the caller is compared in lower case":      {caller(strings.ToUpper(orgA), tenancy.OrgScopeSelf), []string{orgA}},
		"the subtree is looked up for the org in any spelling": {caller(strings.ToUpper(orgPartner), tenancy.OrgScopeSubtree), []string{orgA, orgB}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := scoper.OrgIds(ctx, tt.caller)
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.want, got)

			// InScope agrees with OrgIds for every org of the test
			for _, orgId := range []string{orgA, orgB, orgC, orgPartner, defaultOrg, store.LegacyOrgId} {
				inScope, err := scoper.InScope(ctx, tt.caller, orgId)
				require.NoError(t, err)
				assert.Equal(t, contains(tt.want, orgId), inScope, "org %s", orgId)
			}
			// nobody reaches the empty org
			inScope, err := scoper.InScope(ctx, tt.caller, "")
			require.NoError(t, err)
			assert.False(t, inScope)
		})
	}
}

func contains(orgIds []string, orgId string) bool {
	for _, id := range orgIds {
		if id == orgId {
			return true
		}
	}
	return false
}

func TestScoperRefusesWhenTheTenantStoreFails(t *testing.T) {
	ctx := context.Background()
	scoper, tenants, _ := newMultiScoper(t)
	tenants.err = errors.New("database is down")
	p := caller(orgPartner, tenancy.OrgScopeSubtree)

	orgIds, err := scoper.OrgIds(ctx, p)
	require.Error(t, err)
	assert.Empty(t, orgIds)

	inScope, err := scoper.InScope(ctx, p, orgA)
	require.Error(t, err)
	assert.False(t, inScope)

	orgId, err := scoper.TargetOrg(ctx, p, orgA)
	require.Error(t, err)
	assert.NotErrorIs(t, err, tenancy.ErrOutOfScope)
	assert.Empty(t, orgId)

	// a failure is not kept for the rest of the request
	ctx = tenancy.WithScopeCache(ctx)
	_, err = scoper.OrgIds(ctx, p)
	require.Error(t, err)
	tenants.err = nil
	orgIds, err = scoper.OrgIds(ctx, p)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{orgA, orgB}, orgIds)
}

func TestScopeCacheAsksTheTenantStoreOncePerRequest(t *testing.T) {
	scoper, tenants, _ := newMultiScoper(t)
	p := caller(orgPartner, tenancy.OrgScopeSubtree)

	// without a cache every question reaches the store
	for i := 0; i < 3; i++ {
		_, err := scoper.InScope(context.Background(), p, orgA)
		require.NoError(t, err)
	}
	assert.Equal(t, 3, tenants.calls)

	tenants.calls = 0
	ctx := tenancy.WithScopeCache(context.Background())
	// a second cache on the same context is the first one
	ctx = tenancy.WithScopeCache(ctx)
	for i := 0; i < 3; i++ {
		inScope, err := scoper.InScope(ctx, p, orgA)
		require.NoError(t, err)
		assert.True(t, inScope)
	}
	orgIds, err := scoper.OrgIds(ctx, p)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{orgA, orgB}, orgIds)
	assert.Equal(t, 1, tenants.calls)

	// what is kept for one caller is not the answer for another one
	orgIds, err = scoper.OrgIds(ctx, caller(orgC, tenancy.OrgScopeSubtree))
	require.NoError(t, err)
	assert.Equal(t, []string{orgC}, orgIds)
}

func TestScoperStationOrg(t *testing.T) {
	ctx := context.Background()

	t.Run("multi mode", func(t *testing.T) {
		scoper, _, engine := newMultiScoper(t)
		require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.CP001", &store.ChargeStationAuth{OrgId: orgA}))
		// the key says tenant A, the charge station was moved to tenant B
		require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.MOVED", &store.ChargeStationAuth{OrgId: strings.ToUpper(orgB)}))
		require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.NOORG", &store.ChargeStationAuth{}))
		require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.LEGACY", &store.ChargeStationAuth{OrgId: store.LegacyOrgId}))
		require.NoError(t, engine.SetChargeStationAuth(ctx, "plain", &store.ChargeStationAuth{OrgId: orgC}))

		tests := map[string]struct {
			stationKey string
			wantOrg    string
			wantExists bool
		}{
			"a charge station belongs to its org":                            {"aaaa.CP001", orgA, true},
			"the org of a charge station wins over its key":                  {"aaaa.MOVED", orgB, true},
			"a charge station without an org belongs to the default org":     {"aaaa.NOORG", defaultOrg, true},
			"a charge station of the legacy org belongs to the default org":  {"aaaa.LEGACY", defaultOrg, true},
			"a charge station with a key that names no tenant":               {"plain", orgC, true},
			"a new charge station belongs to the tenant of its key":          {"bbbb.NEW", orgB, false},
			"a new charge station of a tenant that does not exist":           {"zzzz.NEW", "", false},
			"a new charge station with a key that does not name a tenant":    {"NEW", "", false},
			"a new charge station whose key starts with a slug, not the key": {"tenant-a.NEW", "", false},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				orgId, exists, err := scoper.StationOrg(ctx, tt.stationKey)
				require.NoError(t, err)
				assert.Equal(t, tt.wantOrg, orgId)
				assert.Equal(t, tt.wantExists, exists)
			})
		}
	})

	t.Run("single mode", func(t *testing.T) {
		engine := inmemory.NewStore(clock.RealClock{})
		require.NoError(t, engine.SetChargeStationAuth(ctx, "CP001", &store.ChargeStationAuth{}))
		require.NoError(t, engine.SetChargeStationAuth(ctx, "CP002", &store.ChargeStationAuth{OrgId: orgA}))
		scoper := &tenancy.Scoper{Resolver: &tenancy.Resolver{Stations: engine, DefaultOrgId: defaultOrg}, DefaultOrgId: defaultOrg}

		orgId, exists, err := scoper.StationOrg(ctx, "CP001")
		require.NoError(t, err)
		assert.Equal(t, defaultOrg, orgId)
		assert.True(t, exists)

		orgId, exists, err = scoper.StationOrg(ctx, "CP002")
		require.NoError(t, err)
		assert.Equal(t, orgA, orgId)
		assert.True(t, exists)

		orgId, exists, err = scoper.StationOrg(ctx, "aaaa.NEW")
		require.NoError(t, err)
		assert.Equal(t, defaultOrg, orgId)
		assert.False(t, exists)
	})

	t.Run("a lookup that fails is an error", func(t *testing.T) {
		failure := errors.New("database is down")
		scoper := &tenancy.Scoper{Resolver: &tenancy.Resolver{Stations: fixedStationAuth{err: failure}}}

		orgId, exists, err := scoper.StationOrg(ctx, "CP001")
		require.ErrorIs(t, err, failure)
		assert.Empty(t, orgId)
		assert.False(t, exists)

		_, _, err = (&tenancy.Scoper{}).StationOrg(ctx, "CP001")
		require.Error(t, err)
	})
}

func TestScoperTargetOrg(t *testing.T) {
	ctx := context.Background()
	multi, _, _ := newMultiScoper(t)
	single := &tenancy.Scoper{DefaultOrgId: defaultOrg}

	tests := map[string]struct {
		scoper    *tenancy.Scoper
		caller    *tenancy.Principal
		requested string
		want      string
		wantErr   error
	}{
		"the org of the caller when none is requested":           {multi, caller(orgA, tenancy.OrgScopeSelf), "", orgA, nil},
		"the requested org when it is the org of the caller":     {multi, caller(orgA, tenancy.OrgScopeSelf), strings.ToUpper(orgA), orgA, nil},
		"another org is out of scope":                            {multi, caller(orgA, tenancy.OrgScopeSelf), orgB, "", tenancy.ErrOutOfScope},
		"an org below the caller":                                {multi, caller(orgPartner, tenancy.OrgScopeSubtree), orgB, orgB, nil},
		"an org that is not below the caller":                    {multi, caller(orgPartner, tenancy.OrgScopeSubtree), orgC, "", tenancy.ErrOutOfScope},
		"a partner that is not a tenant must name the org":       {multi, caller(orgPartner, tenancy.OrgScopeSubtree), "", "", tenancy.ErrOrgRequired},
		"a partner that is not a tenant cannot name itself":      {multi, caller(orgPartner, tenancy.OrgScopeSubtree), orgPartner, "", tenancy.ErrOutOfScope},
		"a tenant with the scope subtree creates in its own org": {multi, caller(orgA, tenancy.OrgScopeSubtree), "", orgA, nil},
		"a scope that is not known creates nothing":              {multi, caller(orgA, ""), "", "", tenancy.ErrOutOfScope},
		"no caller creates nothing":                              {multi, nil, orgA, "", tenancy.ErrOutOfScope},
		"single mode: the default org":                           {single, caller(defaultOrg, tenancy.OrgScopeSelf), "", defaultOrg, nil},
		"single mode: the default org when it is requested":      {single, caller(defaultOrg, tenancy.OrgScopeSubtree), defaultOrg, defaultOrg, nil},
		"single mode: a caller of another org":                   {single, caller(orgA, tenancy.OrgScopeSubtree), "", "", tenancy.ErrOutOfScope},
		"single mode: another org is requested":                  {single, caller(defaultOrg, tenancy.OrgScopeSubtree), orgA, "", tenancy.ErrOutOfScope},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := tt.scoper.TargetOrg(ctx, tt.caller, tt.requested)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStationOrgInContext(t *testing.T) {
	_, ok := tenancy.StationOrgFromContext(context.Background())
	assert.False(t, ok)

	_, ok = tenancy.StationOrgFromContext(tenancy.WithStationOrg(context.Background(), ""))
	assert.False(t, ok)

	orgId, ok := tenancy.StationOrgFromContext(tenancy.WithStationOrg(context.Background(), orgA))
	assert.True(t, ok)
	assert.Equal(t, orgA, orgId)
}
