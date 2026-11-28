// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// registerStation registers a charge station of the org and returns its record.
func registerStation(t *testing.T, s store.Engine, stationKey, orgId string) *store.ChargeStation {
	t.Helper()
	require.NoError(t, s.SetChargeStationAuth(context.Background(), stationKey,
		&store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgId}))
	return lookupStation(t, s, stationKey)
}

func lookupStation(t *testing.T, s store.Engine, stationKey string) *store.ChargeStation {
	t.Helper()
	cs, err := s.LookupChargeStation(context.Background(), stationKey)
	require.NoError(t, err)
	require.NotNil(t, cs, stationKey)
	return cs
}

func stationKeys(stations []*store.ChargeStation) []string {
	keys := make([]string, len(stations))
	for i, cs := range stations {
		keys[i] = cs.StationKey
	}
	return keys
}

// listStationKeys lists the keys of the matching stations (one page).
func listStationKeys(t *testing.T, s store.Engine, filter store.ChargeStationFilter) []string {
	t.Helper()
	stations, _, err := s.ListChargeStations(context.Background(), filter)
	require.NoError(t, err)
	return stationKeys(stations)
}

// testBootTime is a boot time that every backend stores exactly (whole microseconds, UTC).
var testBootTime = time.Date(2026, 11, 28, 9, 30, 15, 123456000, time.UTC)

// TestChargeStationRegistryStore is the contract of store.ChargeStationRegistryStore.
// newStore returns an empty store.
func TestChargeStationRegistryStore(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()

	t.Run("registered stations are active", func(t *testing.T) {
		s := newStore(t)
		cs := registerStation(t, s, "k7f2.CP001", orgA)

		assert.Equal(t, "k7f2.CP001", cs.StationKey)
		assert.Equal(t, orgA, cs.OrgId)
		assert.Equal(t, store.StationStateActive, cs.State)
		assert.False(t, cs.AllowAnyToken, "only the tokens of the token store")
		assert.Nil(t, cs.LocationId)
		assert.Nil(t, cs.DisplayName)
		assert.Nil(t, cs.MaxCapacityKw)
		assert.Nil(t, cs.StateReason)
		assert.Nil(t, cs.Runtime, "the station has not booted")
		assert.False(t, cs.StateChangedAt.IsZero())
		assert.False(t, cs.CreatedAt.IsZero())
		assert.False(t, cs.UpdatedAt.IsZero())

		// a station registered without an org is in the legacy org
		require.NoError(t, s.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{}))
		legacy := lookupStation(t, s, "cs001")
		assert.Equal(t, store.LegacyOrgId, legacy.OrgId)
		assert.Equal(t, store.StationStateActive, legacy.State)

		got, err := s.LookupChargeStation(ctx, "k7f2.unknown")
		require.NoError(t, err)
		assert.Nil(t, got, "a station that is not registered has no record")
	})

	t.Run("registering again keeps the record", func(t *testing.T) {
		s := newStore(t)
		cs := registerStation(t, s, "k7f2.CP001", orgA)
		cs.DisplayName = ptr("Plaza")
		cs.AllowAnyToken = true
		require.NoError(t, s.UpdateChargeStation(ctx, cs))
		require.NoError(t, s.SetChargeStationState(ctx, "k7f2.CP001", store.StationStateDecommissioned, ptr("broken")))
		before := lookupStation(t, s, "k7f2.CP001")

		got := registerStation(t, s, "k7f2.CP001", "")

		assert.Equal(t, orgA, got.OrgId)
		assert.Equal(t, ptr("Plaza"), got.DisplayName)
		assert.True(t, got.AllowAnyToken)
		assert.Equal(t, store.StationStateDecommissioned, got.State)
		assert.Equal(t, ptr("broken"), got.StateReason)
		assert.Equal(t, before.StateChangedAt, got.StateChangedAt)
		assert.Equal(t, before.CreatedAt, got.CreatedAt)
	})

	t.Run("lookup joins the runtime details", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, "k7f2.CP001", orgA)
		details := &store.ChargeStationRuntimeDetails{
			OcppVersion:       "1.6",
			FirmwareVersion:   ptr("1.2.3"),
			Model:             ptr("Volt 22"),
			Vendor:            ptr("Acme"),
			SerialNumber:      ptr("SN-1001"),
			Iccid:             ptr("89340711000000000019"),
			Imsi:              ptr("214070000000001"),
			MeterSerialNumber: ptr("MTR-77"),
			MeterType:         ptr("DZG DVH4013"),
			LastBootAt:        ptr(testBootTime),
		}
		require.NoError(t, s.SetChargeStationRuntimeDetails(ctx, "k7f2.CP001", details))

		runtime, err := s.LookupChargeStationRuntimeDetails(ctx, "k7f2.CP001")
		require.NoError(t, err)
		assert.Equal(t, details, runtime)
		assert.Equal(t, details, lookupStation(t, s, "k7f2.CP001").Runtime)

		// a later boot replaces what the station reported
		next := &store.ChargeStationRuntimeDetails{OcppVersion: "2.0.1", Vendor: ptr("Acme"), Model: ptr("Volt 50")}
		require.NoError(t, s.SetChargeStationRuntimeDetails(ctx, "k7f2.CP001", next))
		assert.Equal(t, next, lookupStation(t, s, "k7f2.CP001").Runtime)
	})

	t.Run("update", func(t *testing.T) {
		s := newStore(t)
		loc := createAssetLocation(t, s, testAssetLocation("Plaza"))
		cs := registerStation(t, s, "k7f2.CP001", orgA)
		before := *cs
		time.Sleep(5 * time.Millisecond)

		cs.LocationId = &loc.Id
		cs.DisplayName = ptr("Plaza Mayor 1")
		cs.MaxCapacityKw = ptr(150.125)
		cs.AllowAnyToken = true
		cs.State = store.StationStateDecommissioned // not written by an update
		cs.OrgId = orgB                             // not written by an update
		require.NoError(t, s.UpdateChargeStation(ctx, cs))
		assert.True(t, cs.UpdatedAt.After(before.UpdatedAt), "the new UpdatedAt is written back")

		got := lookupStation(t, s, "k7f2.CP001")
		assert.Equal(t, &loc.Id, got.LocationId)
		assert.Equal(t, ptr("Plaza Mayor 1"), got.DisplayName)
		assert.Equal(t, ptr(150.125), got.MaxCapacityKw)
		assert.True(t, got.AllowAnyToken)
		assert.Equal(t, store.StationStateActive, got.State)
		assert.Equal(t, orgA, got.OrgId)
		assert.Equal(t, before.StateChangedAt, got.StateChangedAt)
		assert.Equal(t, before.CreatedAt, got.CreatedAt)
		assert.Equal(t, cs.UpdatedAt, got.UpdatedAt)

		// the capacity has three decimals
		got.MaxCapacityKw = ptr(7.4567)
		require.NoError(t, s.UpdateChargeStation(ctx, got))
		assert.Equal(t, ptr(7.457), lookupStation(t, s, "k7f2.CP001").MaxCapacityKw)

		// nil clears a field
		got.LocationId = nil
		got.DisplayName = nil
		got.MaxCapacityKw = nil
		got.AllowAnyToken = false
		require.NoError(t, s.UpdateChargeStation(ctx, got))
		cleared := lookupStation(t, s, "k7f2.CP001")
		assert.Nil(t, cleared.LocationId)
		assert.Nil(t, cleared.DisplayName)
		assert.Nil(t, cleared.MaxCapacityKw)
		assert.False(t, cleared.AllowAnyToken)
	})

	t.Run("update needs the station and the location", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, "k7f2.CP001", orgA)

		err := s.UpdateChargeStation(ctx, &store.ChargeStation{StationKey: "k7f2.unknown"})
		assert.ErrorIs(t, err, store.ErrNotFound, "station that is not registered")

		for _, locationId := range []string{testAssetId(99), "not-a-uuid"} {
			err := s.UpdateChargeStation(ctx, &store.ChargeStation{StationKey: "k7f2.CP001", LocationId: ptr(locationId), DisplayName: ptr("x")})
			assert.ErrorIs(t, err, store.ErrNotFound, locationId)
		}
		assert.Nil(t, lookupStation(t, s, "k7f2.CP001").DisplayName, "nothing is written")
	})

	t.Run("a deleted location is taken from its stations", func(t *testing.T) {
		s := newStore(t)
		gone := createAssetLocation(t, s, testAssetLocation("Gone"))
		kept := createAssetLocation(t, s, testAssetLocation("Kept"))
		for key, loc := range map[string]*store.AssetLocation{"k7f2.CP001": gone, "k7f2.CP002": kept} {
			cs := registerStation(t, s, key, orgA)
			cs.LocationId = &loc.Id
			cs.DisplayName = ptr("Station " + key)
			require.NoError(t, s.UpdateChargeStation(ctx, cs))
		}

		require.NoError(t, s.DeleteAssetLocation(ctx, gone.Id))

		cleared := lookupStation(t, s, "k7f2.CP001")
		assert.Nil(t, cleared.LocationId)
		assert.Equal(t, ptr("Station k7f2.CP001"), cleared.DisplayName, "the rest of the record stays")
		assert.Equal(t, &kept.Id, lookupStation(t, s, "k7f2.CP002").LocationId)
	})

	t.Run("state change", func(t *testing.T) {
		s := newStore(t)
		cs := registerStation(t, s, "k7f2.CP001", orgA)
		cs.DisplayName = ptr("Plaza")
		require.NoError(t, s.UpdateChargeStation(ctx, cs))
		before := lookupStation(t, s, "k7f2.CP001")
		time.Sleep(5 * time.Millisecond)

		require.NoError(t, s.SetChargeStationState(ctx, "k7f2.CP001", store.StationStateDecommissioned, ptr("replaced")))

		decommissioned := lookupStation(t, s, "k7f2.CP001")
		assert.Equal(t, store.StationStateDecommissioned, decommissioned.State)
		assert.Equal(t, ptr("replaced"), decommissioned.StateReason)
		assert.True(t, decommissioned.StateChangedAt.After(before.StateChangedAt), "the time of the change is set")
		assert.Equal(t, ptr("Plaza"), decommissioned.DisplayName, "the other fields are kept")
		time.Sleep(5 * time.Millisecond)

		require.NoError(t, s.SetChargeStationState(ctx, "k7f2.CP001", store.StationStateActive, nil))
		active := lookupStation(t, s, "k7f2.CP001")
		assert.Equal(t, store.StationStateActive, active.State)
		assert.Nil(t, active.StateReason, "no reason clears the reason")
		assert.True(t, active.StateChangedAt.After(decommissioned.StateChangedAt))

		err := s.SetChargeStationState(ctx, "k7f2.unknown", store.StationStatePending, nil)
		assert.ErrorIs(t, err, store.ErrNotFound)
		err = s.SetChargeStationState(ctx, "k7f2.CP001", store.StationState("retired"), nil)
		assert.Error(t, err, "unknown state")
		assert.Equal(t, store.StationStateActive, lookupStation(t, s, "k7f2.CP001").State)
	})

	t.Run("list filters", func(t *testing.T) {
		s := newStore(t)
		plaza := createAssetLocation(t, s, testAssetLocation("Plaza"))
		depot := createAssetLocation(t, s, testAssetLocation("Depot"))

		// k7f2.CP001: org A at the plaza, active, connected, charging on connector 1
		cp1 := registerStation(t, s, "k7f2.CP001", orgA)
		cp1.LocationId = &plaza.Id
		cp1.DisplayName = ptr("Plaza Mayor")
		require.NoError(t, s.UpdateChargeStation(ctx, cp1))
		require.NoError(t, s.SetChargeStationRuntimeDetails(ctx, "k7f2.CP001", &store.ChargeStationRuntimeDetails{
			OcppVersion: "1.6", Vendor: ptr("Acme"), Model: ptr("Volt 22"), SerialNumber: ptr("SN-1001"), Iccid: ptr("89340711000000000019"),
		}))
		require.NoError(t, s.SetChargeStationStatus(ctx, "k7f2.CP001", &store.ChargeStationStatus{ChargeStationId: "k7f2.CP001", Connected: true}))
		require.NoError(t, s.SetConnectorStatus(ctx, "k7f2.CP001", 1, &store.ConnectorStatus{
			ChargeStationId: "k7f2.CP001", ConnectorId: 1, Status: store.ConnectorStatusCharging, ErrorCode: store.ConnectorErrorCodeNoError,
		}))

		// k7f2.CP002: org A without location, pending, disconnected, the station faulted
		registerStation(t, s, "k7f2.CP002", orgA)
		require.NoError(t, s.SetChargeStationState(ctx, "k7f2.CP002", store.StationStatePending, nil))
		require.NoError(t, s.SetChargeStationRuntimeDetails(ctx, "k7f2.CP002", &store.ChargeStationRuntimeDetails{
			OcppVersion: "1.6", Vendor: ptr("acme"), Model: ptr("Volt 50"), SerialNumber: ptr("SN_2002"),
		}))
		require.NoError(t, s.SetChargeStationStatus(ctx, "k7f2.CP002", &store.ChargeStationStatus{ChargeStationId: "k7f2.CP002", Connected: false}))
		for connectorId, status := range map[int]store.ConnectorStatusType{0: store.ConnectorStatusFaulted, 1: store.ConnectorStatusAvailable} {
			require.NoError(t, s.SetConnectorStatus(ctx, "k7f2.CP002", connectorId, &store.ConnectorStatus{
				ChargeStationId: "k7f2.CP002", ConnectorId: connectorId, Status: status, ErrorCode: store.ConnectorErrorCodeNoError,
			}))
		}

		// k7f2.CP003: org A at the depot, decommissioned, never booted nor connected
		cp3 := registerStation(t, s, "k7f2.CP003", orgA)
		cp3.LocationId = &depot.Id
		cp3.DisplayName = ptr("100% Solar")
		require.NoError(t, s.UpdateChargeStation(ctx, cp3))
		require.NoError(t, s.SetChargeStationState(ctx, "k7f2.CP003", store.StationStateDecommissioned, nil))

		// x9q1.CP001: org B, like k7f2.CP001
		registerStation(t, s, "x9q1.CP001", orgB)
		require.NoError(t, s.SetChargeStationRuntimeDetails(ctx, "x9q1.CP001", &store.ChargeStationRuntimeDetails{
			OcppVersion: "1.6", Vendor: ptr("Acme"), Model: ptr("Volt 22"),
		}))
		require.NoError(t, s.SetChargeStationStatus(ctx, "x9q1.CP001", &store.ChargeStationStatus{ChargeStationId: "x9q1.CP001", Connected: true}))

		filter := func(change func(f *store.ChargeStationFilter)) store.ChargeStationFilter {
			f := store.ChargeStationFilter{OrgIds: []string{orgA}}
			if change != nil {
				change(&f)
			}
			return f
		}
		tests := map[string]struct {
			filter store.ChargeStationFilter
			want   []string
		}{
			"org":               {filter(nil), []string{"k7f2.CP001", "k7f2.CP002", "k7f2.CP003"}},
			"two orgs":          {filter(func(f *store.ChargeStationFilter) { f.OrgIds = []string{orgB, orgA} }), []string{"k7f2.CP001", "k7f2.CP002", "k7f2.CP003", "x9q1.CP001"}},
			"no org":            {filter(func(f *store.ChargeStationFilter) { f.OrgIds = nil }), []string{}},
			"location":          {filter(func(f *store.ChargeStationFilter) { f.LocationId = &plaza.Id }), []string{"k7f2.CP001"}},
			"unknown location":  {filter(func(f *store.ChargeStationFilter) { f.LocationId = ptr(testAssetId(99)) }), []string{}},
			"location not UUID": {filter(func(f *store.ChargeStationFilter) { f.LocationId = ptr("plaza") }), []string{}},
			"pending":           {filter(func(f *store.ChargeStationFilter) { f.States = []store.StationState{store.StationStatePending} }), []string{"k7f2.CP002"}},
			"two states": {filter(func(f *store.ChargeStationFilter) {
				f.States = []store.StationState{store.StationStateActive, store.StationStateDecommissioned}
			}), []string{"k7f2.CP001", "k7f2.CP003"}},
			"vendor ignores case": {filter(func(f *store.ChargeStationFilter) { f.Vendor = ptr("ACME") }), []string{"k7f2.CP001", "k7f2.CP002"}},
			"vendor is exact":     {filter(func(f *store.ChargeStationFilter) { f.Vendor = ptr("Acm") }), []string{}},
			"model":               {filter(func(f *store.ChargeStationFilter) { f.Model = ptr("volt 22") }), []string{"k7f2.CP001"}},
			"connected":           {filter(func(f *store.ChargeStationFilter) { f.Connected = ptr(true) }), []string{"k7f2.CP001"}},
			"not connected":       {filter(func(f *store.ChargeStationFilter) { f.Connected = ptr(false) }), []string{"k7f2.CP002", "k7f2.CP003"}},
			"connector status": {filter(func(f *store.ChargeStationFilter) {
				f.StatusAnyOf = []store.ConnectorStatusType{store.ConnectorStatusCharging}
			}), []string{"k7f2.CP001"}},
			"status of the station": {filter(func(f *store.ChargeStationFilter) {
				f.StatusAnyOf = []store.ConnectorStatusType{store.ConnectorStatusFaulted}
			}), []string{"k7f2.CP002"}},
			"one of two statuses": {filter(func(f *store.ChargeStationFilter) {
				f.StatusAnyOf = []store.ConnectorStatusType{store.ConnectorStatusCharging, store.ConnectorStatusAvailable}
			}), []string{"k7f2.CP001", "k7f2.CP002"}},
			"status nobody has": {filter(func(f *store.ChargeStationFilter) {
				f.StatusAnyOf = []store.ConnectorStatusType{store.ConnectorStatusReserved}
			}), []string{}},
			"query on display name":  {filter(func(f *store.ChargeStationFilter) { f.Query = ptr("plaza") }), []string{"k7f2.CP001"}},
			"query on station key":   {filter(func(f *store.ChargeStationFilter) { f.Query = ptr("cp003") }), []string{"k7f2.CP003"}},
			"query on serial number": {filter(func(f *store.ChargeStationFilter) { f.Query = ptr("sn-1") }), []string{"k7f2.CP001"}},
			"query on ICCID":         {filter(func(f *store.ChargeStationFilter) { f.Query = ptr("89340711") }), []string{"k7f2.CP001"}},
			"query with _":           {filter(func(f *store.ChargeStationFilter) { f.Query = ptr("sn_") }), []string{"k7f2.CP002"}},
			"query with %":           {filter(func(f *store.ChargeStationFilter) { f.Query = ptr("100%") }), []string{"k7f2.CP003"}},
			"every filter": {filter(func(f *store.ChargeStationFilter) {
				f.OrgIds = []string{orgA, orgB}
				f.States = []store.StationState{store.StationStateActive}
				f.Vendor = ptr("acme")
				f.Connected = ptr(true)
				f.Query = ptr("CP001")
			}), []string{"k7f2.CP001", "x9q1.CP001"}},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				assert.Equal(t, tt.want, listStationKeys(t, s, tt.filter))
			})
		}

		// a list returns the records that a lookup returns
		stations, _, err := s.ListChargeStations(ctx, filter(nil))
		require.NoError(t, err)
		for _, cs := range stations {
			assert.Equal(t, lookupStation(t, s, cs.StationKey), cs)
		}

		_, _, err = s.ListChargeStations(ctx, filter(func(f *store.ChargeStationFilter) { f.OrgIds = []string{"not-a-uuid"} }))
		assert.Error(t, err, "org id that is not a UUID")
	})

	t.Run("list pages", func(t *testing.T) {
		s := newStore(t)
		for i := 120; i >= 1; i-- {
			registerStation(t, s, fmt.Sprintf("k7f2.CP%03d", i), orgA)
		}
		registerStation(t, s, "x9q1.CP001", orgB)

		var keys []string
		page := store.PageRequest{Limit: 50}
		var sizes []int
		for {
			items, next, err := s.ListChargeStations(ctx, store.ChargeStationFilter{OrgIds: []string{orgA}, Page: page})
			require.NoError(t, err)
			sizes = append(sizes, len(items))
			keys = append(keys, stationKeys(items)...)
			if next == "" {
				break
			}
			require.Less(t, len(sizes), 4, "too many pages")
			page.Cursor = next
		}
		assert.Equal(t, []int{50, 50, 20}, sizes)
		require.Len(t, keys, 120)
		for i, key := range keys {
			assert.Equal(t, fmt.Sprintf("k7f2.CP%03d", i+1), key, "ordered by station key without duplicates")
		}

		items, next, err := s.ListChargeStations(ctx, store.ChargeStationFilter{OrgIds: []string{orgA}})
		require.NoError(t, err)
		assert.Len(t, items, store.DefaultPageLimit)
		assert.NotEmpty(t, next)

		items, next, err = s.ListChargeStations(ctx, store.ChargeStationFilter{OrgIds: []string{orgA}, Page: store.PageRequest{Limit: 120}})
		require.NoError(t, err)
		assert.Len(t, items, 120)
		assert.Empty(t, next, "the exact last page has no next page")

		_, _, err = s.ListChargeStations(ctx, store.ChargeStationFilter{OrgIds: []string{orgA}, Page: store.PageRequest{Cursor: "not a cursor!"}})
		assert.ErrorIs(t, err, store.ErrInvalidCursor)
	})

	t.Run("count", func(t *testing.T) {
		s := newStore(t)
		for _, key := range []string{"k7f2.CP001", "k7f2.CP002", "k7f2.CP003"} {
			registerStation(t, s, key, orgA)
		}
		registerStation(t, s, "x9q1.CP001", orgB)
		require.NoError(t, s.SetChargeStationState(ctx, "k7f2.CP002", store.StationStatePending, nil))
		require.NoError(t, s.SetChargeStationState(ctx, "k7f2.CP003", store.StationStateDecommissioned, nil))

		for name, tt := range map[string]struct {
			orgId  string
			states []store.StationState
			want   int
		}{
			"every state":          {orgA, nil, 3},
			"active":               {orgA, []store.StationState{store.StationStateActive}, 1},
			"two states":           {orgA, []store.StationState{store.StationStatePending, store.StationStateDecommissioned}, 2},
			"other org":            {orgB, nil, 1},
			"other org, pending":   {orgB, []store.StationState{store.StationStatePending}, 0},
			"org without stations": {"0193b2c4-0000-7000-8000-0000000000c3", nil, 0},
		} {
			t.Run(name, func(t *testing.T) {
				got, err := s.CountChargeStations(ctx, tt.orgId, tt.states...)
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			})
		}

		_, err := s.CountChargeStations(ctx, "not-a-uuid")
		assert.Error(t, err)
	})
}
