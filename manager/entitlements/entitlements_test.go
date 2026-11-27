// SPDX-License-Identifier: Apache-2.0

package entitlements_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/entitlements"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
)

// The orgs of the tests: the tenants C1 and C2 are below the partner P, the tenant D is
// on its own.
const (
	orgP  = "0193b2c4-0000-7000-8000-0000000000f0"
	orgC1 = "0193b2c4-0000-7000-8000-0000000000c1"
	orgC2 = "0193b2c4-0000-7000-8000-0000000000c2"
	orgD  = "0193b2c4-0000-7000-8000-0000000000d4"
)

// fakeTenants is a tenant store that only knows the two lookups of the enforcer. Any
// other method panics.
type fakeTenants struct {
	store.TenantStore
	tenants    map[string]*store.Tenant
	subtrees   map[string][]string
	getErr     error
	subtreeErr error
	// the orgs that a tenant, and the orgs below, were asked for
	gets   []string
	listed []string
}

func (f *fakeTenants) GetTenant(_ context.Context, orgId string) (*store.Tenant, error) {
	f.gets = append(f.gets, orgId)
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.tenants[orgId], nil
}

func (f *fakeTenants) ListSubtreeOrgIds(_ context.Context, orgId string) ([]string, error) {
	f.listed = append(f.listed, orgId)
	if f.subtreeErr != nil {
		return nil, f.subtreeErr
	}
	return f.subtrees[orgId], nil
}

// fakeCounter answers every count with the same number.
type fakeCounter struct {
	count int
	err   error
	// the orgs of every count that was asked for
	calls [][]string
}

func (f *fakeCounter) CountConnectorsForOrgs(_ context.Context, orgIds []string) (int, error) {
	f.calls = append(f.calls, orgIds)
	return f.count, f.err
}

// limitDocument is an entitlement document with a limit on the connectors.
func limitDocument(limit string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"version":1,"status":"active","limits":{"cpms.connectors.max":%s}}`, limit))
}

// newEnforcer creates a multi mode enforcer for the tenant C1 with the given entitlement
// document, with current connectors in whatever orgs are counted. The partner P has the
// tenants C1 and C2 below it and is a tenant itself.
func newEnforcer(document json.RawMessage, current int) (*entitlements.Enforcer, *fakeTenants, *fakeCounter) {
	tenants := &fakeTenants{
		tenants: map[string]*store.Tenant{
			orgC1: {OrgId: orgC1, EntitlementsVersion: 1, Entitlements: document},
		},
		subtrees: map[string][]string{
			orgP:  {orgC1, orgC2, orgP},
			orgC1: {orgC1},
		},
	}
	counter := &fakeCounter{count: current}
	return &entitlements.Enforcer{Tenants: tenants, Counter: counter, Mode: tenancy.ModeMulti}, tenants, counter
}

func TestCheckConnectorsAllowsWhatHasNoHardLimit(t *testing.T) {
	ctx := context.Background()

	for name, document := range map[string]json.RawMessage{
		"entitlements that were never sent": json.RawMessage(`{}`),
		"no document":                       nil,
		"no limits":                         json.RawMessage(`{"version":3,"status":"active","features":{"cpms":true}}`),
		"another limit":                     json.RawMessage(`{"version":3,"limits":{"cpms.locations.max":{"value":0,"enforcement":"hard","scope":"org"}}}`),
		"a soft limit that is used up":      limitDocument(`{"value":10,"enforcement":"soft","scope":"org"}`),
		"a soft limit that is exceeded":     limitDocument(`{"value":5,"enforcement":"soft","scope":"subtree"}`),
		"no enforcement":                    limitDocument(`{"value":10,"scope":"org"}`),
		"an enforcement that is not known":  limitDocument(`{"value":10,"enforcement":"HARD","scope":"org"}`),
	} {
		t.Run(name, func(t *testing.T) {
			enforcer, tenants, counter := newEnforcer(document, 10)

			require.NoError(t, enforcer.CheckConnectors(ctx, orgC1, 1))
			assert.Equal(t, []string{orgC1}, tenants.gets)
			assert.Empty(t, counter.calls, "nothing has to be counted without a hard limit")
		})
	}

	t.Run("an org that is not a tenant", func(t *testing.T) {
		enforcer, tenants, counter := newEnforcer(limitDocument(`{"value":0,"enforcement":"hard","scope":"org"}`), 10)

		require.NoError(t, enforcer.CheckConnectors(ctx, orgD, 1))
		assert.Equal(t, []string{orgD}, tenants.gets)
		assert.Empty(t, counter.calls)
	})
}

func TestCheckConnectorsEnforcesAHardLimitOfTheOrg(t *testing.T) {
	ctx := context.Background()
	document := limitDocument(`{"value":10,"enforcement":"hard","scope":"org"}`)

	cases := []struct {
		name       string
		current    int
		additional int
		want       *entitlements.LimitExceededError
	}{
		{name: "room for one more", current: 9, additional: 1},
		{name: "used up", current: 10, additional: 1,
			want: &entitlements.LimitExceededError{Key: "cpms.connectors.max", Limit: 10, Current: 10, Requested: 1}},
		{name: "already above the limit", current: 14, additional: 1,
			want: &entitlements.LimitExceededError{Key: "cpms.connectors.max", Limit: 10, Current: 14, Requested: 1}},
		{name: "nothing yet", current: 0, additional: 1},
		{name: "several that fit exactly", current: 7, additional: 3},
		{name: "several that do not fit", current: 8, additional: 3,
			want: &entitlements.LimitExceededError{Key: "cpms.connectors.max", Limit: 10, Current: 8, Requested: 3}},
		{name: "more than the limit at once", current: 0, additional: 11,
			want: &entitlements.LimitExceededError{Key: "cpms.connectors.max", Limit: 10, Current: 0, Requested: 11}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enforcer, tenants, counter := newEnforcer(document, tc.current)

			err := enforcer.CheckConnectors(ctx, orgC1, tc.additional)

			if tc.want == nil {
				require.NoError(t, err)
			} else {
				var exceeded *entitlements.LimitExceededError
				require.ErrorAs(t, err, &exceeded)
				assert.Equal(t, tc.want, exceeded)
			}
			// the org alone is counted, and the orgs below it are not looked up
			assert.Equal(t, [][]string{{orgC1}}, counter.calls)
			assert.Empty(t, tenants.listed)
		})
	}

	t.Run("a limit of zero allows nothing", func(t *testing.T) {
		enforcer, _, _ := newEnforcer(limitDocument(`{"value":0,"enforcement":"hard","scope":"org"}`), 0)

		var exceeded *entitlements.LimitExceededError
		require.ErrorAs(t, enforcer.CheckConnectors(ctx, orgC1, 1), &exceeded)
		assert.Equal(t, &entitlements.LimitExceededError{Key: "cpms.connectors.max", Limit: 0, Current: 0, Requested: 1}, exceeded)
	})
}

func TestLimitExceededErrorNamesTheLimit(t *testing.T) {
	err := &entitlements.LimitExceededError{Key: entitlements.LimitConnectorsMax, Limit: 10, Current: 10, Requested: 1}

	assert.EqualError(t, err, "entitlement_limit_exceeded: cpms.connectors.max (limit 10, current 10, requested 1)")
}

func TestCheckConnectorsCountsThePoolOfASubtreeLimit(t *testing.T) {
	ctx := context.Background()

	t.Run("the subtree of the org that holds the subscription", func(t *testing.T) {
		document := limitDocument(`{"value":10,"enforcement":"hard","scope":"subtree","pool_org_id":"` + orgP + `"}`)
		enforcer, tenants, counter := newEnforcer(document, 10)

		var exceeded *entitlements.LimitExceededError
		require.ErrorAs(t, enforcer.CheckConnectors(ctx, orgC1, 1), &exceeded)
		assert.Equal(t, &entitlements.LimitExceededError{Key: "cpms.connectors.max", Limit: 10, Current: 10, Requested: 1}, exceeded)
		assert.Equal(t, []string{orgP}, tenants.listed)
		assert.Equal(t, [][]string{{orgC1, orgC2, orgP}}, counter.calls)

		counter.count, counter.calls = 9, nil
		require.NoError(t, enforcer.CheckConnectors(ctx, orgC1, 1))
		assert.Equal(t, [][]string{{orgC1, orgC2, orgP}}, counter.calls)
	})

	t.Run("the subtree of the org itself without a pool", func(t *testing.T) {
		document := limitDocument(`{"value":10,"enforcement":"hard","scope":"subtree"}`)
		enforcer, tenants, counter := newEnforcer(document, 10)

		var exceeded *entitlements.LimitExceededError
		require.ErrorAs(t, enforcer.CheckConnectors(ctx, orgC1, 1), &exceeded)
		assert.Equal(t, []string{orgC1}, tenants.listed)
		assert.Equal(t, [][]string{{orgC1}}, counter.calls)
	})

	t.Run("a pool is ignored by a limit of the org alone", func(t *testing.T) {
		document := limitDocument(`{"value":10,"enforcement":"hard","scope":"org","pool_org_id":"` + orgP + `"}`)
		enforcer, tenants, counter := newEnforcer(document, 9)

		require.NoError(t, enforcer.CheckConnectors(ctx, orgC1, 1))
		assert.Empty(t, tenants.listed)
		assert.Equal(t, [][]string{{orgC1}}, counter.calls)
	})

	t.Run("a scope that is not known counts the org alone", func(t *testing.T) {
		for _, scope := range []string{`"scope":"tree",`, `"scope":"",`, ``} {
			document := limitDocument(`{` + scope + `"value":10,"enforcement":"hard","pool_org_id":"` + orgP + `"}`)
			enforcer, tenants, counter := newEnforcer(document, 10)

			var exceeded *entitlements.LimitExceededError
			require.ErrorAs(t, enforcer.CheckConnectors(ctx, orgC1, 1), &exceeded, scope)
			assert.Empty(t, tenants.listed, scope)
			assert.Equal(t, [][]string{{orgC1}}, counter.calls, scope)
		}
	})
}

func TestCheckConnectorsCountsTheConnectorsOfTheStore(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	partner := orgP
	pooled := limitDocument(`{"value":3,"enforcement":"hard","scope":"subtree","pool_org_id":"` + orgP + `"}`)
	for _, tenant := range []*store.Tenant{
		{OrgId: orgC1, ParentId: &partner, Path: store.OrgPathLabel(orgP) + "." + store.OrgPathLabel(orgC1), Slug: "c1", OrgKey: "cccc1",
			EntitlementsVersion: 1, Entitlements: pooled},
		{OrgId: orgC2, ParentId: &partner, Path: store.OrgPathLabel(orgP) + "." + store.OrgPathLabel(orgC2), Slug: "c2", OrgKey: "cccc2",
			EntitlementsVersion: 1, Entitlements: pooled},
		{OrgId: orgD, Path: store.OrgPathLabel(orgD), Slug: "d", OrgKey: "dddd",
			EntitlementsVersion: 1, Entitlements: limitDocument(`{"value":2,"enforcement":"hard","scope":"org"}`)},
	} {
		require.NoError(t, engine.PutTenant(ctx, tenant))
	}
	report := func(csId, orgId string, connectorIds ...int) {
		t.Helper()
		require.NoError(t, engine.SetChargeStationAuth(ctx, csId, &store.ChargeStationAuth{OrgId: orgId}))
		for _, connectorId := range connectorIds {
			require.NoError(t, engine.SetConnectorStatus(ctx, csId, connectorId, &store.ConnectorStatus{
				ChargeStationId: csId, ConnectorId: connectorId, Status: store.ConnectorStatusAvailable,
			}))
		}
	}
	enforcer := &entitlements.Enforcer{Tenants: engine, Counter: engine, Mode: tenancy.ModeMulti}

	// the partner is not a tenant here: its pool is what its tenants have
	report("cccc1.CP001", orgC1, 0, 1)
	report("cccc2.CP001", orgC2, 0, 1)
	report("dddd.CP001", orgD, 0, 1)
	require.NoError(t, enforcer.CheckConnectors(ctx, orgC1, 1), "2 of 3 in the pool")
	require.NoError(t, enforcer.CheckConnectors(ctx, orgC2, 1), "2 of 3 in the pool")
	require.NoError(t, enforcer.CheckConnectors(ctx, orgD, 1), "1 of 2")

	// a connector of one tenant uses up the pool for the other one too
	report("cccc2.CP001", orgC2, 2)
	for _, orgId := range []string{orgC1, orgC2} {
		var exceeded *entitlements.LimitExceededError
		require.ErrorAs(t, enforcer.CheckConnectors(ctx, orgId, 1), &exceeded, orgId)
		assert.Equal(t, &entitlements.LimitExceededError{Key: "cpms.connectors.max", Limit: 3, Current: 3, Requested: 1}, exceeded)
	}
	// and nothing of a tenant outside the pool
	require.NoError(t, enforcer.CheckConnectors(ctx, orgD, 1))

	report("dddd.CP001", orgD, 2)
	var exceeded *entitlements.LimitExceededError
	require.ErrorAs(t, enforcer.CheckConnectors(ctx, orgD, 1), &exceeded)
	assert.Equal(t, &entitlements.LimitExceededError{Key: "cpms.connectors.max", Limit: 2, Current: 2, Requested: 1}, exceeded)
}

func TestCheckConnectorsAllowsWhenTheLimitCannotBeApplied(t *testing.T) {
	ctx := context.Background()

	for name, document := range map[string]json.RawMessage{
		"a document that is not JSON":      json.RawMessage(`{"limits":`),
		"a limit that is a number":         json.RawMessage(`{"version":3,"limits":{"cpms.connectors.max":10}}`),
		"a value that is a text":           limitDocument(`{"value":"10","enforcement":"hard","scope":"org"}`),
		"a negative limit":                 limitDocument(`{"value":-1,"enforcement":"hard","scope":"org"}`),
		"a pool that is not an org id":     limitDocument(`{"value":10,"enforcement":"hard","scope":"subtree","pool_org_id":"partner"}`),
		"a pool that is an lquery":         limitDocument(`{"value":10,"enforcement":"hard","scope":"subtree","pool_org_id":"*"}`),
		"a feature that cannot be read":    json.RawMessage(`{"features":{"cpms":1},"limits":{"cpms.connectors.max":{"value":10,"enforcement":"hard","scope":"org"}}}`),
		"limits that are not an object":    json.RawMessage(`{"limits":"cpms.connectors.max"}`),
		"a document that is not an object": json.RawMessage(`[{"value":10,"enforcement":"hard","scope":"org"}]`),
	} {
		t.Run(name, func(t *testing.T) {
			enforcer, tenants, counter := newEnforcer(document, 10)

			require.NoError(t, enforcer.CheckConnectors(ctx, orgC1, 1))
			assert.Empty(t, tenants.listed)
			assert.Empty(t, counter.calls)
		})
	}
}

func TestCheckConnectorsAllowsOnAStorageBackendWithoutTenantsOrCounts(t *testing.T) {
	ctx := context.Background()
	document := limitDocument(`{"value":10,"enforcement":"hard","scope":"subtree"}`)
	notImplemented := fmt.Errorf("CountConnectorsForOrgs: %w", store.ErrNotImplemented)

	t.Run("the connectors cannot be counted", func(t *testing.T) {
		enforcer, _, counter := newEnforcer(document, 10)
		counter.err = notImplemented

		require.NoError(t, enforcer.CheckConnectors(ctx, orgC1, 1))
		assert.Len(t, counter.calls, 1)
	})

	t.Run("there are no tenants", func(t *testing.T) {
		enforcer, tenants, counter := newEnforcer(document, 10)
		tenants.getErr = notImplemented

		require.NoError(t, enforcer.CheckConnectors(ctx, orgC1, 1))
		assert.Empty(t, counter.calls)
	})

	t.Run("the orgs below an org cannot be listed", func(t *testing.T) {
		enforcer, tenants, counter := newEnforcer(document, 10)
		tenants.subtreeErr = notImplemented

		require.NoError(t, enforcer.CheckConnectors(ctx, orgC1, 1))
		assert.Empty(t, counter.calls)
	})
}

func TestCheckConnectorsFailsWhenTheStoreFails(t *testing.T) {
	ctx := context.Background()
	document := limitDocument(`{"value":10,"enforcement":"hard","scope":"subtree"}`)
	down := errors.New("database is down")

	for name, breakIt := range map[string]func(*fakeTenants, *fakeCounter){
		"the tenant cannot be read":             func(tenants *fakeTenants, _ *fakeCounter) { tenants.getErr = down },
		"the orgs below it cannot be listed":    func(tenants *fakeTenants, _ *fakeCounter) { tenants.subtreeErr = down },
		"the connectors cannot be counted":      func(_ *fakeTenants, counter *fakeCounter) { counter.err = down },
		"the count fails with room in the pool": func(_ *fakeTenants, counter *fakeCounter) { counter.count, counter.err = 0, down },
	} {
		t.Run(name, func(t *testing.T) {
			enforcer, tenants, counter := newEnforcer(document, 10)
			breakIt(tenants, counter)

			err := enforcer.CheckConnectors(ctx, orgC1, 1)

			require.ErrorIs(t, err, down)
			var exceeded *entitlements.LimitExceededError
			assert.False(t, errors.As(err, &exceeded), "a check that failed is not a limit that is exceeded")
		})
	}

	t.Run("an enforcer without a store", func(t *testing.T) {
		for _, enforcer := range []*entitlements.Enforcer{
			{Mode: tenancy.ModeMulti, Counter: &fakeCounter{}},
			{Mode: tenancy.ModeMulti, Tenants: &fakeTenants{}},
		} {
			assert.Error(t, enforcer.CheckConnectors(ctx, orgC1, 1))
		}
	})
}

func TestCheckConnectorsLimitsNothingInSingleMode(t *testing.T) {
	ctx := context.Background()
	document := limitDocument(`{"value":0,"enforcement":"hard","scope":"org"}`)

	for _, mode := range []tenancy.Mode{tenancy.ModeSingle, ""} {
		enforcer, tenants, counter := newEnforcer(document, 10)
		enforcer.Mode = mode

		require.NoError(t, enforcer.CheckConnectors(ctx, orgC1, 1))
		assert.Empty(t, tenants.gets, "the stores are not touched in single mode")
		assert.Empty(t, counter.calls)
	}

	t.Run("without stores", func(t *testing.T) {
		require.NoError(t, (&entitlements.Enforcer{}).CheckConnectors(ctx, orgC1, 1))
	})
}

func TestCheckConnectorsWithNothingToAddOrNoEnforcer(t *testing.T) {
	ctx := context.Background()
	document := limitDocument(`{"value":10,"enforcement":"hard","scope":"org"}`)

	for _, additional := range []int{0, -1} {
		enforcer, tenants, counter := newEnforcer(document, 14)

		require.NoError(t, enforcer.CheckConnectors(ctx, orgC1, additional))
		assert.Empty(t, tenants.gets)
		assert.Empty(t, counter.calls)
	}

	var none *entitlements.Enforcer
	require.NoError(t, none.CheckConnectors(ctx, orgC1, 1))
}
