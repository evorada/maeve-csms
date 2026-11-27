// SPDX-License-Identifier: Apache-2.0

package tenancy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
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
