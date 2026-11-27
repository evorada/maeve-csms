// SPDX-License-Identifier: Apache-2.0

// Package storetest holds the test cases that every storage backend that implements a
// store must pass. A backend runs them from its own tests.
package storetest

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

const (
	tenantOrgRoot      = "0193b2c4-0000-7000-8000-00000000000a" // never provisioned
	tenantOrgPartner   = "0193b2c4-0000-7000-8000-00000000000b"
	tenantOrgChild1    = "0193b2c4-0000-7000-8000-00000000000c"
	tenantOrgChild2    = "0193b2c4-0000-7000-8000-00000000000d"
	tenantOrgUnrelated = "0193b2c4-0000-7000-8000-00000000000e"
	tenantOrgUnknown   = "0193b2c4-0000-7000-8000-0000000000ff" // never provisioned
)

// tenantPath builds the path of an org from the ids of its ancestors and its own id.
func tenantPath(orgIds ...string) string {
	labels := make([]string, 0, len(orgIds))
	for _, orgId := range orgIds {
		labels = append(labels, store.OrgPathLabel(orgId))
	}
	return strings.Join(labels, ".")
}

// minimalTenant is a tenant directly under the root with nothing but its identity.
func minimalTenant(orgId, slug, orgKey string) *store.Tenant {
	parentId := tenantOrgRoot
	return &store.Tenant{
		OrgId:    orgId,
		ParentId: &parentId,
		Path:     tenantPath(tenantOrgRoot, orgId),
		Slug:     slug,
		OrgKey:   orgKey,
	}
}

// partnerTenant is a tenant with every field set.
func partnerTenant() *store.Tenant {
	tenant := minimalTenant(tenantOrgPartner, "chargeco", "chrg01")
	tenant.Hostnames = []string{"ChargeCo.OCPP.example.com:443", "api.chargeco.example"}
	tenant.Parties = []store.TenantParty{{Id: "party-1", Role: "CPO", CountryCode: "NL", PartyId: "CHC"}}
	tenant.EntitlementsVersion = 3
	tenant.Entitlements = json.RawMessage(`{"version":3,"limits":{"cpms.connectors.max":10}}`)
	tenant.ConfigVersion = 2
	tenant.Config = json.RawMessage(`{"version":2,"heartbeat_interval":300}`)
	return tenant
}

func getTenant(t *testing.T, s store.TenantStore, orgId string) *store.Tenant {
	t.Helper()
	tenant, err := s.GetTenant(context.Background(), orgId)
	require.NoError(t, err)
	require.NotNil(t, tenant, "tenant %s", orgId)
	return tenant
}

// assertSameTenant compares two tenants as they are read from the store. The documents
// are compared as JSON, because a backend may store them in another layout.
func assertSameTenant(t *testing.T, want, got *store.Tenant) {
	t.Helper()
	assert.JSONEq(t, string(want.Entitlements), string(got.Entitlements))
	assert.JSONEq(t, string(want.Config), string(got.Config))
	assert.True(t, want.CreatedAt.Equal(got.CreatedAt), "created at: want %s, got %s", want.CreatedAt, got.CreatedAt)
	assert.True(t, want.UpdatedAt.Equal(got.UpdatedAt), "updated at: want %s, got %s", want.UpdatedAt, got.UpdatedAt)
	w, g := *want, *got
	w.Entitlements, w.Config, g.Entitlements, g.Config = nil, nil, nil, nil
	w.CreatedAt, w.UpdatedAt, g.CreatedAt, g.UpdatedAt = got.CreatedAt, got.UpdatedAt, got.CreatedAt, got.UpdatedAt
	assert.Equal(t, w, g)
}

// TestTenantStore runs the cases that every implementation of store.TenantStore must
// pass. newStore returns an empty store; it is called once per case.
func TestTenantStore(t *testing.T, newStore func(t *testing.T) store.TenantStore) {
	ctx := context.Background()

	t.Run("put and get by org id, host and org key", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.PutTenant(ctx, partnerTenant()))

		got := getTenant(t, s, tenantOrgPartner)
		assert.Equal(t, tenantOrgPartner, got.OrgId)
		require.NotNil(t, got.ParentId)
		assert.Equal(t, tenantOrgRoot, *got.ParentId)
		assert.Equal(t, tenantPath(tenantOrgRoot, tenantOrgPartner), got.Path)
		assert.Equal(t, "chargeco", got.Slug)
		assert.Equal(t, "chrg01", got.OrgKey)
		assert.Equal(t, []string{"api.chargeco.example", "chargeco.ocpp.example.com"}, got.Hostnames)
		assert.Equal(t, []store.TenantParty{{Id: "party-1", Role: "CPO", CountryCode: "NL", PartyId: "CHC"}}, got.Parties)
		assert.Equal(t, int64(3), got.EntitlementsVersion)
		assert.JSONEq(t, `{"version":3,"limits":{"cpms.connectors.max":10}}`, string(got.Entitlements))
		assert.Equal(t, int64(2), got.ConfigVersion)
		assert.JSONEq(t, `{"version":2,"heartbeat_interval":300}`, string(got.Config))
		assert.Equal(t, store.TenantStatusActive, got.Status)
		assert.False(t, got.CreatedAt.IsZero())
		assert.False(t, got.UpdatedAt.IsZero())

		// an org id is a UUID in any case
		byUpperCaseId, err := s.GetTenant(ctx, strings.ToUpper(tenantOrgPartner))
		require.NoError(t, err)
		require.NotNil(t, byUpperCaseId)
		assertSameTenant(t, got, byUpperCaseId)

		byHost, err := s.GetTenantByHost(ctx, "CHARGECO.ocpp.Example.com:8443")
		require.NoError(t, err)
		require.NotNil(t, byHost)
		assertSameTenant(t, got, byHost)

		byOrgKey, err := s.GetTenantByOrgKey(ctx, "chrg01")
		require.NoError(t, err)
		require.NotNil(t, byOrgKey)
		assertSameTenant(t, got, byOrgKey)

		// what the caller does with a tenant it read does not change the stored tenant
		byOrgKey.Hostnames[0] = "changed.example"
		byOrgKey.Parties[0].Role = "EMSP"
		byOrgKey.Entitlements[2] = 'X'
		*byOrgKey.ParentId = tenantOrgUnknown
		assertSameTenant(t, got, getTenant(t, s, tenantOrgPartner))
	})

	t.Run("missing tenant is nil without an error", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.PutTenant(ctx, partnerTenant()))

		got, err := s.GetTenant(ctx, tenantOrgUnknown)
		require.NoError(t, err)
		assert.Nil(t, got)

		got, err = s.GetTenantByHost(ctx, "unknown.ocpp.example.com")
		require.NoError(t, err)
		assert.Nil(t, got)

		got, err = s.GetTenantByOrgKey(ctx, "nokey1")
		require.NoError(t, err)
		assert.Nil(t, got)

		_, err = s.GetTenant(ctx, "not-a-uuid")
		assert.Error(t, err)
	})

	t.Run("tenant with only its identity", func(t *testing.T) {
		s := newStore(t)
		tenant := minimalTenant(tenantOrgPartner, "chargeco", "chrg01")
		tenant.ParentId = nil
		tenant.Path = tenantPath(tenantOrgPartner)
		require.NoError(t, s.PutTenant(ctx, tenant))

		got := getTenant(t, s, tenantOrgPartner)
		assert.Nil(t, got.ParentId)
		assert.Empty(t, got.Hostnames)
		assert.Empty(t, got.Parties)
		assert.Equal(t, int64(0), got.EntitlementsVersion)
		assert.JSONEq(t, `{}`, string(got.Entitlements))
		assert.Equal(t, int64(0), got.ConfigVersion)
		assert.JSONEq(t, `{}`, string(got.Config))
		assert.Equal(t, store.TenantStatusActive, got.Status)
	})

	t.Run("older entitlements and config are ignored, newer ones replace", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.PutTenant(ctx, partnerTenant()))
		created := getTenant(t, s, tenantOrgPartner)

		// both documents are older: they are kept, the rest of the tenant is updated
		older := partnerTenant()
		older.Parties = []store.TenantParty{{Id: "party-2", Role: "EMSP", CountryCode: "XX", PartyId: "CH2"}}
		older.EntitlementsVersion = 2
		older.Entitlements = json.RawMessage(`{"version":2}`)
		older.ConfigVersion = 1
		older.Config = json.RawMessage(`{"version":1}`)
		require.NoError(t, s.PutTenant(ctx, older))

		got := getTenant(t, s, tenantOrgPartner)
		assert.Equal(t, int64(3), got.EntitlementsVersion)
		assert.JSONEq(t, `{"version":3,"limits":{"cpms.connectors.max":10}}`, string(got.Entitlements))
		assert.Equal(t, int64(2), got.ConfigVersion)
		assert.JSONEq(t, `{"version":2,"heartbeat_interval":300}`, string(got.Config))
		assert.Equal(t, older.Parties, got.Parties)
		assert.True(t, created.CreatedAt.Equal(got.CreatedAt))
		assert.False(t, got.UpdatedAt.Before(created.UpdatedAt))

		// each document has its own version: newer entitlements with an older config
		mixed := partnerTenant()
		mixed.EntitlementsVersion = 4
		mixed.Entitlements = json.RawMessage(`{"version":4}`)
		mixed.ConfigVersion = 1
		mixed.Config = json.RawMessage(`{"version":1}`)
		require.NoError(t, s.PutTenant(ctx, mixed))

		got = getTenant(t, s, tenantOrgPartner)
		assert.Equal(t, int64(4), got.EntitlementsVersion)
		assert.JSONEq(t, `{"version":4}`, string(got.Entitlements))
		assert.Equal(t, int64(2), got.ConfigVersion)
		assert.JSONEq(t, `{"version":2,"heartbeat_interval":300}`, string(got.Config))

		// a document of the stored version replaces the stored one
		same := partnerTenant()
		same.EntitlementsVersion = 4
		same.Entitlements = json.RawMessage(`{"version":4}`)
		same.ConfigVersion = 2
		same.Config = json.RawMessage(`{"version":2,"heartbeat_interval":60}`)
		require.NoError(t, s.PutTenant(ctx, same))

		got = getTenant(t, s, tenantOrgPartner)
		assert.Equal(t, int64(2), got.ConfigVersion)
		assert.JSONEq(t, `{"version":2,"heartbeat_interval":60}`, string(got.Config))
	})

	t.Run("slug and org key cannot change", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.PutTenant(ctx, partnerTenant()))
		before := getTenant(t, s, tenantOrgPartner)

		newSlug := partnerTenant()
		newSlug.Slug = "chargeco2"
		newSlug.Hostnames = []string{"other.example"}
		assert.ErrorIs(t, s.PutTenant(ctx, newSlug), store.ErrTenantImmutable)

		newOrgKey := partnerTenant()
		newOrgKey.OrgKey = "chrg02"
		assert.ErrorIs(t, s.PutTenant(ctx, newOrgKey), store.ErrTenantImmutable)

		assertSameTenant(t, before, getTenant(t, s, tenantOrgPartner))
	})

	t.Run("hostname, slug and org key belong to one tenant", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.PutTenant(ctx, partnerTenant()))
		first := getTenant(t, s, tenantOrgPartner)

		sameHostname := minimalTenant(tenantOrgUnrelated, "voltify", "volt01")
		sameHostname.Hostnames = []string{"voltify.example", "chargeco.ocpp.example.com"}
		assert.ErrorIs(t, s.PutTenant(ctx, sameHostname), store.ErrTenantConflict)

		sameSlug := minimalTenant(tenantOrgUnrelated, "chargeco", "volt01")
		assert.ErrorIs(t, s.PutTenant(ctx, sameSlug), store.ErrTenantConflict)

		sameOrgKey := minimalTenant(tenantOrgUnrelated, "voltify", "chrg01")
		assert.ErrorIs(t, s.PutTenant(ctx, sameOrgKey), store.ErrTenantConflict)

		// nothing of the refused tenant is stored
		refused, err := s.GetTenant(ctx, tenantOrgUnrelated)
		require.NoError(t, err)
		assert.Nil(t, refused)
		byHost, err := s.GetTenantByHost(ctx, "voltify.example")
		require.NoError(t, err)
		assert.Nil(t, byHost)

		// an existing tenant that claims the hostname of another one keeps what it had
		second := minimalTenant(tenantOrgUnrelated, "voltify", "volt01")
		second.Hostnames = []string{"voltify.example"}
		require.NoError(t, s.PutTenant(ctx, second))
		claim := minimalTenant(tenantOrgUnrelated, "voltify", "volt01")
		claim.Hostnames = []string{"api.chargeco.example", "voltify2.example"}
		claim.EntitlementsVersion = 1
		claim.Entitlements = json.RawMessage(`{"version":1}`)
		assert.ErrorIs(t, s.PutTenant(ctx, claim), store.ErrTenantConflict)

		got := getTenant(t, s, tenantOrgUnrelated)
		assert.Equal(t, []string{"voltify.example"}, got.Hostnames)
		assert.Equal(t, int64(0), got.EntitlementsVersion)
		byHost, err = s.GetTenantByHost(ctx, "voltify2.example")
		require.NoError(t, err)
		assert.Nil(t, byHost)

		assertSameTenant(t, first, getTenant(t, s, tenantOrgPartner))
		byHost, err = s.GetTenantByHost(ctx, "api.chargeco.example")
		require.NoError(t, err)
		require.NotNil(t, byHost)
		assert.Equal(t, tenantOrgPartner, byHost.OrgId)
	})

	t.Run("hostnames are replaced as a set", func(t *testing.T) {
		s := newStore(t)
		tenant := minimalTenant(tenantOrgPartner, "chargeco", "chrg01")
		tenant.Hostnames = []string{"one.example", "two.example"}
		require.NoError(t, s.PutTenant(ctx, tenant))

		// the same hostname twice, in two spellings, is one hostname
		tenant.Hostnames = []string{"two.example", "Three.Example:443", "three.example."}
		require.NoError(t, s.PutTenant(ctx, tenant))

		assert.Equal(t, []string{"three.example", "two.example"}, getTenant(t, s, tenantOrgPartner).Hostnames)
		removed, err := s.GetTenantByHost(ctx, "one.example")
		require.NoError(t, err)
		assert.Nil(t, removed)
		for _, host := range []string{"two.example", "three.example"} {
			got, err := s.GetTenantByHost(ctx, host)
			require.NoError(t, err)
			require.NotNil(t, got, host)
			assert.Equal(t, tenantOrgPartner, got.OrgId)
		}

		// a removed hostname is free for another tenant
		other := minimalTenant(tenantOrgUnrelated, "voltify", "volt01")
		other.Hostnames = []string{"one.example"}
		require.NoError(t, s.PutTenant(ctx, other))

		tenant.Hostnames = nil
		require.NoError(t, s.PutTenant(ctx, tenant))
		assert.Empty(t, getTenant(t, s, tenantOrgPartner).Hostnames)
		removed, err = s.GetTenantByHost(ctx, "two.example")
		require.NoError(t, err)
		assert.Nil(t, removed)
	})

	t.Run("status is set by SetTenantStatus only", func(t *testing.T) {
		s := newStore(t)
		tenant := partnerTenant()
		tenant.Status = store.TenantStatusCancelled // ignored: a new tenant is active
		require.NoError(t, s.PutTenant(ctx, tenant))
		assert.Equal(t, store.TenantStatusActive, getTenant(t, s, tenantOrgPartner).Status)

		found, err := s.SetTenantStatus(ctx, tenantOrgPartner, store.TenantStatusSuspended)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, store.TenantStatusSuspended, getTenant(t, s, tenantOrgPartner).Status)

		// a later put does not reset the status
		update := partnerTenant()
		update.Status = store.TenantStatusActive
		update.EntitlementsVersion = 4
		update.Entitlements = json.RawMessage(`{"version":4}`)
		require.NoError(t, s.PutTenant(ctx, update))
		got := getTenant(t, s, tenantOrgPartner)
		assert.Equal(t, store.TenantStatusSuspended, got.Status)
		assert.Equal(t, int64(4), got.EntitlementsVersion)

		found, err = s.SetTenantStatus(ctx, tenantOrgPartner, store.TenantStatusActive)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, store.TenantStatusActive, getTenant(t, s, tenantOrgPartner).Status)

		found, err = s.SetTenantStatus(ctx, tenantOrgUnknown, store.TenantStatusSuspended)
		require.NoError(t, err)
		assert.False(t, found)

		_, err = s.SetTenantStatus(ctx, tenantOrgPartner, store.TenantStatus("deleted"))
		assert.Error(t, err)
		assert.Equal(t, store.TenantStatusActive, getTenant(t, s, tenantOrgPartner).Status)
	})

	t.Run("subtree of an org", func(t *testing.T) {
		s := newStore(t)
		// root (not provisioned) > partner > child 1, child 2; root > unrelated
		partner := minimalTenant(tenantOrgPartner, "partner", "part01")
		child1 := minimalTenant(tenantOrgChild1, "child-one", "chld01")
		child1.ParentId = &partner.OrgId
		child1.Path = tenantPath(tenantOrgRoot, tenantOrgPartner, tenantOrgChild1)
		child2 := minimalTenant(tenantOrgChild2, "child-two", "chld02")
		child2.ParentId = &partner.OrgId
		child2.Path = tenantPath(tenantOrgRoot, tenantOrgPartner, tenantOrgChild2)
		unrelated := minimalTenant(tenantOrgUnrelated, "unrelated", "unrl01")
		for _, tenant := range []*store.Tenant{child2, unrelated, partner, child1} {
			require.NoError(t, s.PutTenant(ctx, tenant))
		}

		subtree, err := s.ListSubtreeOrgIds(ctx, tenantOrgPartner)
		require.NoError(t, err)
		assert.Equal(t, []string{tenantOrgPartner, tenantOrgChild1, tenantOrgChild2}, subtree)

		subtree, err = s.ListSubtreeOrgIds(ctx, tenantOrgRoot)
		require.NoError(t, err)
		assert.Equal(t, []string{tenantOrgPartner, tenantOrgChild1, tenantOrgChild2, tenantOrgUnrelated}, subtree)

		subtree, err = s.ListSubtreeOrgIds(ctx, strings.ToUpper(tenantOrgChild1))
		require.NoError(t, err)
		assert.Equal(t, []string{tenantOrgChild1}, subtree)

		subtree, err = s.ListSubtreeOrgIds(ctx, tenantOrgUnknown)
		require.NoError(t, err)
		assert.Empty(t, subtree)

		// only a UUID names an org: a path pattern is not a way to list every tenant
		for _, orgId := range []string{"*", "", store.OrgPathLabel(tenantOrgPartner) + ".*"} {
			_, err = s.ListSubtreeOrgIds(ctx, orgId)
			assert.Error(t, err, "org id %q", orgId)
		}
	})

	t.Run("list tenants in pages", func(t *testing.T) {
		s := newStore(t)
		second := minimalTenant(tenantOrgChild1, "second", "scnd01")
		second.Hostnames = []string{"second.example"}
		for _, tenant := range []*store.Tenant{
			minimalTenant(tenantOrgUnrelated, "third", "thrd01"),
			minimalTenant(tenantOrgPartner, "first", "frst01"),
			second,
		} {
			require.NoError(t, s.PutTenant(ctx, tenant))
		}

		page, err := s.ListTenants(ctx, "", 2)
		require.NoError(t, err)
		require.Len(t, page, 2)
		assert.Equal(t, tenantOrgPartner, page[0].OrgId)
		assert.Equal(t, "first", page[0].Slug)
		assert.Equal(t, tenantOrgChild1, page[1].OrgId)
		assertSameTenant(t, getTenant(t, s, tenantOrgChild1), page[1])

		page, err = s.ListTenants(ctx, page[1].OrgId, 2)
		require.NoError(t, err)
		require.Len(t, page, 1)
		assert.Equal(t, tenantOrgUnrelated, page[0].OrgId)

		page, err = s.ListTenants(ctx, page[0].OrgId, 2)
		require.NoError(t, err)
		assert.Empty(t, page)

		page, err = s.ListTenants(ctx, "", 0)
		require.NoError(t, err)
		assert.Empty(t, page)
	})

	t.Run("first put of one tenant by several callers at once", func(t *testing.T) {
		s := newStore(t)
		const callers = 8
		errs := make([]error, callers)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs[i] = s.PutTenant(ctx, partnerTenant())
			}()
		}
		close(start)
		wg.Wait()

		for i, err := range errs {
			assert.NoError(t, err, "caller %d", i)
		}
		got := getTenant(t, s, tenantOrgPartner)
		assert.Equal(t, []string{"api.chargeco.example", "chargeco.ocpp.example.com"}, got.Hostnames)
		all, err := s.ListTenants(ctx, "", 10)
		require.NoError(t, err)
		assert.Len(t, all, 1)
	})
}
