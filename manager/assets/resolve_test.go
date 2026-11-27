// SPDX-License-Identifier: Apache-2.0

package assets_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/assets"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"k8s.io/utils/clock"
)

const (
	testOrg       = "0193b2c4-0000-7000-8000-0000000000a1"
	station16     = "k7f2.CP16"
	station201    = "k7f2.CP201"
	stationNoEvse = "k7f2.CP000"
)

// assetStore holds a location with a 1.6 station (EVSEs 1 and 2, each with connector 1)
// and a 2.0.1 station (EVSE 2 with connectors 1 and 2).
func assetStore(t *testing.T) *inmemory.Store {
	t.Helper()
	ctx := context.Background()
	s := inmemory.NewStore(clock.RealClock{})
	for _, stationKey := range []string{station16, station201, stationNoEvse} {
		require.NoError(t, s.SetChargeStationAuth(ctx, stationKey, &store.ChargeStationAuth{OrgId: testOrg}))
	}
	loc := validLocation()
	require.NoError(t, s.CreateAssetLocation(ctx, loc))
	for _, evse := range []*store.AssetEvse{
		{OrgId: testOrg, LocationId: loc.Id, StationKey: station16, OcppEvseId: 1, Connectors: []*store.AssetConnector{validConnector(1)}},
		{OrgId: testOrg, LocationId: loc.Id, StationKey: station16, OcppEvseId: 2, Connectors: []*store.AssetConnector{validConnector(1)}},
		{OrgId: testOrg, LocationId: loc.Id, StationKey: station201, OcppEvseId: 2, Connectors: []*store.AssetConnector{validConnector(1), validConnector(2)}},
	} {
		require.NoError(t, s.CreateAssetEvse(ctx, evse))
	}
	return s
}

func TestResolveConnector(t *testing.T) {
	ctx := context.Background()
	s := assetStore(t)

	for _, tc := range []struct {
		name                    string
		stationKey, ocppVersion string
		evseId, connectorId     int
		wantEvse, wantConnector int // OCPP ids, 0 = nil
	}{
		{"1.6 connector 2 is EVSE 2, connector 1", station16, "ocpp1.6", 0, 2, 2, 1},
		{"1.6 ignores the EVSE id", station16, "ocpp1.6", 7, 1, 1, 1},
		{"1.6 connector 0 is the station", station16, "ocpp1.6", 0, 0, 0, 0},
		{"1.6 connector without EVSE", station16, "ocpp1.6", 0, 3, 0, 0},
		{"2.0.1 EVSE 2, connector 1", station201, "ocpp2.0.1", 2, 1, 2, 1},
		{"2.0.1 EVSE 2, connector 2", station201, "ocpp2.0.1", 2, 2, 2, 2},
		{"2.0.1 EVSE 2 without connector 3", station201, "ocpp2.0.1", 2, 3, 2, 0},
		{"2.0.1 EVSE 0 is the station", station201, "ocpp2.0.1", 0, 1, 0, 0},
		{"2.0.1 EVSE without asset", station201, "ocpp2.0.1", 1, 1, 0, 0},
		{"station without assets", stationNoEvse, "ocpp1.6", 0, 1, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			connector, evse, err := assets.ResolveConnector(ctx, s, tc.stationKey, tc.ocppVersion, tc.evseId, tc.connectorId)
			require.NoError(t, err)
			if tc.wantEvse == 0 {
				assert.Nil(t, evse)
			} else if assert.NotNil(t, evse) {
				assert.Equal(t, tc.stationKey, evse.StationKey)
				assert.Equal(t, tc.wantEvse, evse.OcppEvseId)
			}
			if tc.wantConnector == 0 {
				assert.Nil(t, connector)
			} else if assert.NotNil(t, connector) {
				assert.Equal(t, tc.wantConnector, connector.OcppConnectorId)
				assert.Equal(t, evse.Id, connector.EvseUid)
			}
		})
	}

	t.Run("unknown OCPP version", func(t *testing.T) {
		_, _, err := assets.ResolveConnector(ctx, s, station16, "ocpp2.1", 1, 1)
		assert.ErrorContains(t, err, "unknown OCPP version")
	})

	t.Run("store error", func(t *testing.T) {
		_, _, err := assets.ResolveConnector(ctx, failingAssetStore{}, station16, "ocpp1.6", 0, 1)
		assert.ErrorIs(t, err, store.ErrNotImplemented)
	})
}

// failingAssetStore is an asset store without the asset model, like Firestore.
type failingAssetStore struct {
	store.AssetStore
}

func (failingAssetStore) LookupAssetEvseByOcpp(context.Context, string, int) (*store.AssetEvse, error) {
	return nil, fmt.Errorf("LookupAssetEvseByOcpp: %w", store.ErrNotImplemented)
}
