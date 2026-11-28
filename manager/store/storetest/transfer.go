// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// Two tariffs of orgA that the connectors of the transfer tests refer to.
var (
	transferTariff      = testAssetId(901)
	transferAdhocTariff = testAssetId(902)
)

// transferFixture is a charge station of orgA with two EVSEs at a location, one of its
// connectors with tariffs, a charging profile and a transaction; and another charge
// station of orgA at another location.
type transferFixture struct {
	plaza, garage *store.AssetLocation
	first, second *store.AssetEvse
	auth          *store.ChargeStationAuth
}

func newTransferFixture(t *testing.T, s store.Engine) *transferFixture {
	t.Helper()
	ctx := context.Background()
	f := &transferFixture{auth: &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, Base64SHA256Password: "c2VjcmV0", OrgId: orgA}}
	require.NoError(t, s.SetChargeStationAuth(ctx, assetStationA, f.auth))
	require.NoError(t, s.SetChargeStationAuth(ctx, assetStationB, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgA}))
	f.plaza = createAssetLocation(t, s, testAssetLocation("Plaza"))
	garage := testAssetLocation("Garage")
	garage.OcpiId = "GARAGE"
	f.garage = createAssetLocation(t, s, garage)

	first := testAssetEvse(f.plaza.Id, assetStationA, 1, 1)
	first.Connectors[0].TariffRef = ptr(transferTariff)
	first.Connectors[0].AdhocTariffRef = ptr(transferAdhocTariff)
	f.first = createAssetEvse(t, s, first)
	second := testAssetEvse(f.plaza.Id, assetStationA, 2, 1, 2)
	second.Connectors[1].TariffRef = ptr(transferTariff)
	f.second = createAssetEvse(t, s, second)
	require.NoError(t, s.MoveChargeStationToLocation(ctx, assetStationA, f.plaza.Id))
	createAssetEvse(t, s, testAssetEvse(f.garage.Id, assetStationB, 1, 1))
	require.NoError(t, s.MoveChargeStationToLocation(ctx, assetStationB, f.garage.Id))

	require.NoError(t, s.SetChargingProfile(ctx, testChargingProfile(assetStationA, 7)))
	require.NoError(t, s.CreateTransaction(ctx, assetStationA, "tx-1", "TAG1", "ISO14443", nil, 0, false))
	return f
}

func chargingProfileOrg(t *testing.T, s store.Engine, stationKey string) string {
	t.Helper()
	profiles, err := s.GetChargingProfiles(context.Background(), stationKey, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, profiles, 1)
	return profiles[0].OrgId
}

// assertNotTransferred checks that the charge station of the fixture is as it was.
func (f *transferFixture) assertNotTransferred(t *testing.T, s store.Engine) {
	t.Helper()
	cs := lookupStation(t, s, assetStationA)
	assert.Equal(t, orgA, cs.OrgId)
	assert.Equal(t, &f.plaza.Id, cs.LocationId)
	assert.Equal(t, f.plaza, lookupAssetLocation(t, s, f.plaza.Id))
	assert.Equal(t, f.first, lookupAssetEvse(t, s, f.first.Id))
	assert.Equal(t, f.second, lookupAssetEvse(t, s, f.second.Id))
	assert.Equal(t, orgA, chargingProfileOrg(t, s, assetStationA))
}

// testStationTransfer is the contract of AssetStore.TransferChargeStation.
func testStationTransfer(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()

	t.Run("a charge station moves to another org with its location", func(t *testing.T) {
		s := newStore(t)
		f := newTransferFixture(t, s)

		result, err := s.TransferChargeStation(ctx, store.TransferParams{
			StationKey: assetStationA, FromOrgId: orgA, ToOrgId: orgB, MoveLocation: true, TargetPartyRef: ptr(assetPartyB),
		})
		require.NoError(t, err)
		assert.Equal(t, &store.TransferResult{
			StationKey:        assetStationA,
			LocationId:        &f.plaza.Id,
			LocationMoved:     true,
			EvsesMoved:        2,
			ConnectorsMoved:   3,
			ClearedTariffRefs: []string{transferTariff, transferAdhocTariff},
		}, result)

		cs := lookupStation(t, s, assetStationA)
		assert.Equal(t, orgB, cs.OrgId)
		assert.Equal(t, &f.plaza.Id, cs.LocationId)
		assert.Equal(t, store.StationStateActive, cs.State)
		plaza := lookupAssetLocation(t, s, f.plaza.Id)
		assert.Equal(t, orgB, plaza.OrgId)
		assert.Equal(t, assetPartyB, plaza.PartyRef)
		assert.Equal(t, f.plaza.OcpiId, plaza.OcpiId)
		assert.Equal(t, int64(2), plaza.Version)
		for _, before := range []*store.AssetEvse{f.first, f.second} {
			evse := lookupAssetEvse(t, s, before.Id)
			assert.Equal(t, orgB, evse.OrgId)
			assert.Equal(t, f.plaza.Id, evse.LocationId)
			assert.Equal(t, int64(2), evse.Version)
			for _, c := range evse.Connectors {
				assert.Equal(t, orgB, c.OrgId, "a connector belongs to the org of its EVSE")
				assert.Nil(t, c.TariffRef, "a tariff of the old org is cleared")
				assert.Nil(t, c.AdhocTariffRef)
				assert.Equal(t, int64(2), c.Version)
			}
		}
		assert.Equal(t, orgB, chargingProfileOrg(t, s, assetStationA))

		// the station connects as before: same key, same credentials, new org
		auth, err := s.LookupChargeStationAuth(ctx, assetStationA)
		require.NoError(t, err)
		require.NotNil(t, auth)
		assert.Equal(t, f.auth.SecurityProfile, auth.SecurityProfile)
		assert.Equal(t, f.auth.Base64SHA256Password, auth.Base64SHA256Password)
		assert.Equal(t, orgB, auth.OrgId)

		// the history keeps the old org
		transaction, err := s.FindTransaction(ctx, assetStationA, "tx-1")
		require.NoError(t, err)
		require.NotNil(t, transaction)
		assert.Equal(t, orgA, transaction.OrgId)

		// the other station is as it was
		other := lookupStation(t, s, assetStationB)
		assert.Equal(t, orgA, other.OrgId)
		assert.Equal(t, orgA, lookupAssetLocation(t, s, f.garage.Id).OrgId)
		listed := listStationKeys(t, s, store.ChargeStationFilter{OrgIds: []string{orgA}})
		assert.Equal(t, []string{assetStationB}, listed)
		listed = listStationKeys(t, s, store.ChargeStationFilter{OrgIds: []string{orgB}})
		assert.Equal(t, []string{assetStationA}, listed)

		// it does not go back from an org that it is not in
		_, err = s.TransferChargeStation(ctx, store.TransferParams{StationKey: assetStationA, FromOrgId: orgA, ToOrgId: orgB, TargetLocationId: &f.garage.Id})
		assert.ErrorIs(t, err, store.ErrInvalidState)
	})

	t.Run("a charge station moves to a location of the other org", func(t *testing.T) {
		s := newStore(t)
		f := newTransferFixture(t, s)
		target := testAssetLocation("Of B")
		target.OrgId = orgB
		target.PartyRef = assetPartyB
		target = createAssetLocation(t, s, target)

		result, err := s.TransferChargeStation(ctx, store.TransferParams{
			StationKey: assetStationA, FromOrgId: orgA, ToOrgId: orgB, TargetLocationId: &target.Id,
		})
		require.NoError(t, err)
		assert.Equal(t, &target.Id, result.LocationId)
		assert.False(t, result.LocationMoved)
		assert.Equal(t, 2, result.EvsesMoved)
		assert.Equal(t, 3, result.ConnectorsMoved)

		cs := lookupStation(t, s, assetStationA)
		assert.Equal(t, orgB, cs.OrgId)
		assert.Equal(t, &target.Id, cs.LocationId)
		assert.Equal(t, f.plaza, lookupAssetLocation(t, s, f.plaza.Id), "the location stays with the old org")
		assert.Empty(t, listAssetEvses(t, s, f.plaza.Id))
		moved := listAssetEvses(t, s, target.Id)
		assert.Equal(t, []string{f.first.Id, f.second.Id}, evseUids(moved))
		for _, evse := range moved {
			assert.Equal(t, orgB, evse.OrgId)
		}
	})

	t.Run("a charge station without a location and without EVSEs", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, assetStationA, orgA)

		result, err := s.TransferChargeStation(ctx, store.TransferParams{StationKey: assetStationA, FromOrgId: orgA, ToOrgId: orgB})
		require.NoError(t, err)
		assert.Equal(t, &store.TransferResult{StationKey: assetStationA, ClearedTariffRefs: []string{}}, result)
		cs := lookupStation(t, s, assetStationA)
		assert.Equal(t, orgB, cs.OrgId)
		assert.Nil(t, cs.LocationId)

		_, err = s.TransferChargeStation(ctx, store.TransferParams{
			StationKey: assetStationA, FromOrgId: orgB, ToOrgId: orgA, MoveLocation: true, TargetPartyRef: ptr(assetPartyA),
		})
		assert.ErrorIs(t, err, store.ErrInvalidState, "no location to move")
	})

	t.Run("a pending charge station", func(t *testing.T) {
		s := newStore(t)
		pendingStation(t, s, assetStationA)

		_, err := s.TransferChargeStation(ctx, store.TransferParams{StationKey: assetStationA, FromOrgId: orgA, ToOrgId: orgB})
		require.NoError(t, err)
		cs := lookupStation(t, s, assetStationA)
		assert.Equal(t, orgB, cs.OrgId)
		assert.Equal(t, store.StationStatePending, cs.State)
		assert.NotNil(t, cs.Onboarding)
	})

	t.Run("a refused transfer changes nothing", func(t *testing.T) {
		s := newStore(t)
		f := newTransferFixture(t, s)
		taken := testAssetLocation("Taken")
		taken.OrgId = orgB
		taken.PartyRef = assetPartyB
		taken.OcpiId = f.plaza.OcpiId
		createAssetLocation(t, s, taken)
		// the station of B at the garage: the garage is shared once station A is there too
		require.NoError(t, s.SetChargeStationAuth(ctx, "k7f2.CP003", &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgA}))
		require.NoError(t, s.MoveChargeStationToLocation(ctx, "k7f2.CP003", f.plaza.Id))

		for name, tc := range map[string]struct {
			params store.TransferParams
			err    error
		}{
			"an unknown station": {
				store.TransferParams{StationKey: "k7f2.UNKNOWN", FromOrgId: orgA, ToOrgId: orgB, TargetLocationId: &f.garage.Id}, store.ErrNotFound,
			},
			"another org": {
				store.TransferParams{StationKey: assetStationA, FromOrgId: orgB, ToOrgId: orgA, TargetLocationId: &f.garage.Id}, store.ErrInvalidState,
			},
			"a location that is shared": {
				store.TransferParams{StationKey: assetStationA, FromOrgId: orgA, ToOrgId: orgB, MoveLocation: true, TargetPartyRef: ptr(assetPartyB)}, store.ErrInvalidState,
			},
			"EVSEs without a location": {
				store.TransferParams{StationKey: assetStationA, FromOrgId: orgA, ToOrgId: orgB}, store.ErrInvalidState,
			},
			// the last writes fail, after the station, its profiles and connectors were written
			"a target location that does not exist": {
				store.TransferParams{StationKey: assetStationA, FromOrgId: orgA, ToOrgId: orgB, TargetLocationId: ptr(testAssetId(99))}, store.ErrNotFound,
			},
			"a target location that is not a UUID": {
				store.TransferParams{StationKey: assetStationA, FromOrgId: orgA, ToOrgId: orgB, TargetLocationId: ptr("not-a-uuid")}, store.ErrNotFound,
			},
		} {
			_, err := s.TransferChargeStation(ctx, tc.params)
			assert.ErrorIs(t, err, tc.err, name)
			f.assertNotTransferred(t, s)
		}

		// the OCPI id of the location is taken in the party of the target
		require.NoError(t, s.MoveChargeStationToLocation(ctx, "k7f2.CP003", f.garage.Id))
		_, err := s.TransferChargeStation(ctx, store.TransferParams{
			StationKey: assetStationA, FromOrgId: orgA, ToOrgId: orgB, MoveLocation: true, TargetPartyRef: ptr(assetPartyB),
		})
		assert.ErrorIs(t, err, store.ErrAlreadyExists)
		f.assertNotTransferred(t, s)

		require.NoError(t, s.DecommissionChargeStation(ctx, assetStationA, nil))
		_, err = s.TransferChargeStation(ctx, store.TransferParams{StationKey: assetStationA, FromOrgId: orgA, ToOrgId: orgB, TargetLocationId: &f.garage.Id})
		assert.ErrorIs(t, err, store.ErrInvalidState, "a decommissioned station")
		assert.Equal(t, orgA, lookupStation(t, s, assetStationA).OrgId)
	})
}
