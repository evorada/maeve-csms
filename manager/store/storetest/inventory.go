// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// listInventory returns the whole inventory of the type of the org, page by page of the
// given size, after checking that every page but the last one is full and that the ids
// are in byte order.
func listInventory(t *testing.T, s store.Engine, orgId, typ string, includeInactive bool, limit int) []store.InventoryItem {
	t.Helper()
	ctx := context.Background()
	var items []store.InventoryItem
	cursor := ""
	for pages := 0; ; pages++ {
		require.Less(t, pages, 100, "the pages do not end")
		page, next, err := s.ListInventory(ctx, orgId, typ, includeInactive, store.PageRequest{Limit: limit, Cursor: cursor})
		require.NoError(t, err)
		require.NotNil(t, page)
		items = append(items, page...)
		if next == "" {
			break
		}
		require.Len(t, page, store.InventoryPageLimit(store.PageRequest{Limit: limit}), "a page before the last one is full")
		cursor = next
	}
	ids := inventoryIds(items)
	require.True(t, slices.IsSorted(ids), "byte order: %v", ids)
	require.Len(t, slices.Compact(slices.Clone(ids)), len(ids), "no id twice: %v", ids)
	return items
}

func inventoryIds(items []store.InventoryItem) []string {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.Id
	}
	return ids
}

// inventoryOf returns the ids of the whole inventory of the type of the org, after
// checking that CountInventory counts them.
func inventoryOf(t *testing.T, s store.Engine, orgId, typ string, includeInactive bool) []string {
	t.Helper()
	ids := inventoryIds(listInventory(t, s, orgId, typ, includeInactive, 2))
	count, err := s.CountInventory(context.Background(), orgId, typ, includeInactive)
	require.NoError(t, err)
	require.Equal(t, len(ids), count, "the count of %s of %s", typ, orgId)
	return ids
}

// inventoryAttrs returns the attributes of the items of the whole inventory, by id.
func inventoryAttrs(t *testing.T, s store.Engine, orgId, typ string, includeInactive bool) map[string]map[string]string {
	t.Helper()
	attrs := map[string]map[string]string{}
	for _, item := range listInventory(t, s, orgId, typ, includeInactive, 0) {
		attrs[item.Id] = item.Attrs
	}
	return attrs
}

// inventoryFixture is what the orgs A and B own:
//   - A: the active station aaaa.CP001 at the location plaza, with EVSE 1 (one connector),
//     EVSE 2 (two connectors) and EVSE 3 (REMOVED, one connector); the pending station
//     aaaa.CP002 with EVSE 1 (one connector); the decommissioned station aaaa.CP003; the
//     valid token GB*AAA*TAG1 and the token GB*AAA*TAG2 that is not valid.
//   - B: the active station bbbb.CP001 at its location depot with EVSE 1 (one
//     connector), and a valid token GB*BBB*TAG1.
type inventoryFixture struct {
	plaza, depot      *store.AssetLocation
	first, second     *store.AssetEvse
	removed, pending  *store.AssetEvse
	otherOrgsStation  string
	decommissionedKey string
}

func newInventoryFixture(t *testing.T, s store.Engine) *inventoryFixture {
	t.Helper()
	ctx := context.Background()
	f := &inventoryFixture{otherOrgsStation: "bbbb.CP001", decommissionedKey: "aaaa.CP003"}
	for _, stationKey := range []string{"aaaa.CP001", "aaaa.CP002", "aaaa.CP003"} {
		registerStation(t, s, stationKey, orgA)
	}
	registerStation(t, s, f.otherOrgsStation, orgB)
	require.NoError(t, s.SetChargeStationState(ctx, "aaaa.CP002", store.StationStatePending, nil))
	require.NoError(t, s.SetChargeStationState(ctx, f.decommissionedKey, store.StationStateDecommissioned, ptr("rejected")))

	f.plaza = createAssetLocation(t, s, testAssetLocation("Plaza"))
	depot := testAssetLocation("Depot")
	depot.OrgId, depot.PartyRef, depot.OcpiId, depot.Publish = orgB, assetPartyB, "DEPOT", false
	f.depot = createAssetLocation(t, s, depot)

	f.first = createAssetEvse(t, s, testAssetEvse(f.plaza.Id, "aaaa.CP001", 1, 1))
	f.second = createAssetEvse(t, s, testAssetEvse(f.plaza.Id, "aaaa.CP001", 2, 1, 2))
	removed := testAssetEvse(f.plaza.Id, "aaaa.CP001", 3, 1)
	removed.Status = store.AssetEvseStatusRemoved
	f.removed = createAssetEvse(t, s, removed)
	f.pending = createAssetEvse(t, s, testAssetEvse(f.plaza.Id, "aaaa.CP002", 1, 1))
	require.NoError(t, s.MoveChargeStationToLocation(ctx, "aaaa.CP001", f.plaza.Id))
	otherOrgsEvse := testAssetEvse(f.depot.Id, f.otherOrgsStation, 1, 1)
	otherOrgsEvse.OrgId = orgB
	createAssetEvse(t, s, otherOrgsEvse)
	require.NoError(t, s.MoveChargeStationToLocation(ctx, f.otherOrgsStation, f.depot.Id))

	require.NoError(t, s.SetToken(ctx, testToken(orgA, "AAA", "TAG1")))
	invalid := testToken(orgA, "AAA", "TAG2")
	invalid.Valid = false
	require.NoError(t, s.SetToken(ctx, invalid))
	require.NoError(t, s.SetToken(ctx, testToken(orgB, "BBB", "TAG1")))
	return f
}

// TestInventoryStore is the contract of store.InventoryStore. newStore returns an empty
// store.
func TestInventoryStore(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()

	t.Run("stations", func(t *testing.T) {
		s := newStore(t)
		f := newInventoryFixture(t, s)

		assert.Equal(t, []string{"aaaa.CP001", "aaaa.CP002"}, inventoryOf(t, s, orgA, store.InventoryStation, false))
		assert.Equal(t, []string{"aaaa.CP001", "aaaa.CP002", "aaaa.CP003"}, inventoryOf(t, s, orgA, store.InventoryStation, true),
			"a decommissioned station is inactive")
		assert.Equal(t, map[string]map[string]string{
			"aaaa.CP001": {"state": "active", "location_id": f.plaza.Id},
			"aaaa.CP002": {"state": "pending"},
			"aaaa.CP003": {"state": "decommissioned"},
		}, inventoryAttrs(t, s, orgA, store.InventoryStation, true))
		assert.Equal(t, []string{f.otherOrgsStation}, inventoryOf(t, s, orgB, store.InventoryStation, true))
	})

	t.Run("connectors", func(t *testing.T) {
		s := newStore(t)
		f := newInventoryFixture(t, s)

		assert.Equal(t, []string{"aaaa.CP001#1", "aaaa.CP001#2.1", "aaaa.CP001#2.2"}, inventoryOf(t, s, orgA, store.InventoryConnector, false),
			"neither the connectors of a REMOVED EVSE nor those of a station that is not active")
		all := inventoryAttrs(t, s, orgA, store.InventoryConnector, true)
		assert.Equal(t, map[string]map[string]string{
			"aaaa.CP001#1":   {"station_key": "aaaa.CP001", "evse_uid": f.first.Id, "connector_uid": f.first.Connectors[0].Id},
			"aaaa.CP001#2.1": {"station_key": "aaaa.CP001", "evse_uid": f.second.Id, "connector_uid": f.second.Connectors[0].Id},
			"aaaa.CP001#2.2": {"station_key": "aaaa.CP001", "evse_uid": f.second.Id, "connector_uid": f.second.Connectors[1].Id},
			"aaaa.CP001#3":   {"station_key": "aaaa.CP001", "evse_uid": f.removed.Id, "connector_uid": f.removed.Connectors[0].Id},
			"aaaa.CP002#1":   {"station_key": "aaaa.CP002", "evse_uid": f.pending.Id, "connector_uid": f.pending.Connectors[0].Id},
		}, all)
		// the id is the subject key of the usage facts
		for id, attrs := range all {
			evse := lookupAssetEvse(t, s, attrs["evse_uid"])
			connector := lookupAssetConnector(t, s, attrs["connector_uid"])
			assert.Equal(t, store.ConnectorSubjectKey(evse.StationKey, evse.OcppEvseId, connector.OcppConnectorId, len(evse.Connectors)), id)
		}
		assert.Equal(t, []string{"bbbb.CP001#1"}, inventoryOf(t, s, orgB, store.InventoryConnector, true))

		// a station that is decommissioned takes its connectors out of the inventory
		require.NoError(t, s.DecommissionChargeStation(ctx, "aaaa.CP001", nil))
		assert.Empty(t, inventoryOf(t, s, orgA, store.InventoryConnector, false))
		assert.Len(t, inventoryOf(t, s, orgA, store.InventoryConnector, true), 5)
	})

	t.Run("locations", func(t *testing.T) {
		s := newStore(t)
		f := newInventoryFixture(t, s)

		for _, includeInactive := range []bool{false, true} {
			assert.Equal(t, []string{f.plaza.Id}, inventoryOf(t, s, orgA, store.InventoryLocation, includeInactive))
			assert.Equal(t, []string{f.depot.Id}, inventoryOf(t, s, orgB, store.InventoryLocation, includeInactive),
				"a location that is not published is in the inventory")
		}
		assert.Equal(t, map[string]map[string]string{
			f.plaza.Id: {"ocpi_id": f.plaza.OcpiId, "party_ref": assetPartyA, "publish": "true"},
		}, inventoryAttrs(t, s, orgA, store.InventoryLocation, false))
		assert.Equal(t, map[string]map[string]string{
			f.depot.Id: {"ocpi_id": "DEPOT", "party_ref": assetPartyB, "publish": "false"},
		}, inventoryAttrs(t, s, orgB, store.InventoryLocation, false))
	})

	t.Run("tokens", func(t *testing.T) {
		s := newStore(t)
		newInventoryFixture(t, s)

		assert.Equal(t, []string{"GB*AAA*TAG1"}, inventoryOf(t, s, orgA, store.InventoryToken, false))
		assert.Equal(t, []string{"GB*AAA*TAG1", "GB*AAA*TAG2"}, inventoryOf(t, s, orgA, store.InventoryToken, true),
			"a token that is not valid is inactive")
		assert.Equal(t, map[string]map[string]string{
			"GB*AAA*TAG1": {"type": "RFID", "valid": "true"},
			"GB*AAA*TAG2": {"type": "RFID", "valid": "false"},
		}, inventoryAttrs(t, s, orgA, store.InventoryToken, true))
		assert.Equal(t, []string{"GB*BBB*TAG1"}, inventoryOf(t, s, orgB, store.InventoryToken, true),
			"the token of another org with the same UID")
	})

	t.Run("an org without resources has an empty inventory", func(t *testing.T) {
		s := newStore(t)
		newInventoryFixture(t, s)
		for _, typ := range store.InventoryTypes {
			for _, includeInactive := range []bool{false, true} {
				assert.Empty(t, inventoryOf(t, s, orgC, typ, includeInactive), typ)
			}
		}
	})

	t.Run("the orgs below an org are not part of its inventory", func(t *testing.T) {
		s := newStore(t)
		parent := orgB
		require.NoError(t, s.PutTenant(ctx, &store.Tenant{OrgId: orgB, Path: tenantPath(orgB), Slug: "parent", OrgKey: "bbbb"}))
		require.NoError(t, s.PutTenant(ctx, &store.Tenant{OrgId: orgA, ParentId: &parent, Path: tenantPath(orgB, orgA), Slug: "child", OrgKey: "aaaa"}))
		newInventoryFixture(t, s)

		assert.Equal(t, []string{"bbbb.CP001"}, inventoryOf(t, s, orgB, store.InventoryStation, true))
		assert.Equal(t, []string{"bbbb.CP001#1"}, inventoryOf(t, s, orgB, store.InventoryConnector, true))
		assert.Len(t, inventoryOf(t, s, orgB, store.InventoryLocation, true), 1)
		assert.Equal(t, []string{"GB*BBB*TAG1"}, inventoryOf(t, s, orgB, store.InventoryToken, true))
	})

	t.Run("a transferred station is in the inventory of its new org only", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, "aaaa.CP001", orgA)
		plaza := createAssetLocation(t, s, testAssetLocation("Plaza"))
		evse := createAssetEvse(t, s, testAssetEvse(plaza.Id, "aaaa.CP001", 1, 1, 2))
		require.NoError(t, s.MoveChargeStationToLocation(ctx, "aaaa.CP001", plaza.Id))
		_, err := s.TransferChargeStation(ctx, store.TransferParams{
			StationKey: "aaaa.CP001", FromOrgId: orgA, ToOrgId: orgB, MoveLocation: true, TargetPartyRef: ptr(assetPartyB),
		})
		require.NoError(t, err)

		for _, typ := range []string{store.InventoryStation, store.InventoryConnector, store.InventoryLocation} {
			assert.Empty(t, inventoryOf(t, s, orgA, typ, true), typ)
		}
		// the station key keeps the org key of the old org
		assert.Equal(t, []string{"aaaa.CP001"}, inventoryOf(t, s, orgB, store.InventoryStation, false))
		assert.Equal(t, map[string]map[string]string{
			"aaaa.CP001#1.1": {"station_key": "aaaa.CP001", "evse_uid": evse.Id, "connector_uid": evse.Connectors[0].Id},
			"aaaa.CP001#1.2": {"station_key": "aaaa.CP001", "evse_uid": evse.Id, "connector_uid": evse.Connectors[1].Id},
		}, inventoryAttrs(t, s, orgB, store.InventoryConnector, false))
		assert.Equal(t, []string{plaza.Id}, inventoryOf(t, s, orgB, store.InventoryLocation, false))
	})

	t.Run("pages of 2,500 connectors", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, "aaaa.CP001", orgA)
		plaza := createAssetLocation(t, s, testAssetLocation("Plaza"))
		// 240 EVSEs with ten connectors and 100 EVSEs with one
		want := make([]string, 0, 2500)
		for ocppEvseId := 1; ocppEvseId <= 340; ocppEvseId++ {
			connectors := []int{1}
			if ocppEvseId <= 240 {
				connectors = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
			}
			createAssetEvse(t, s, testAssetEvse(plaza.Id, "aaaa.CP001", ocppEvseId, connectors...))
			for _, connectorId := range connectors {
				want = append(want, store.ConnectorSubjectKey("aaaa.CP001", ocppEvseId, connectorId, len(connectors)))
			}
		}
		slices.Sort(want)
		require.Len(t, want, 2500)

		var sizes []int
		var got []string
		cursor := ""
		for {
			page, next, err := s.ListInventory(ctx, orgA, store.InventoryConnector, false, store.PageRequest{Limit: 1000, Cursor: cursor})
			require.NoError(t, err)
			sizes = append(sizes, len(page))
			got = append(got, inventoryIds(page)...)
			if next == "" {
				break
			}
			require.Less(t, len(sizes), 10, "the pages do not end")
			cursor = next
		}
		assert.Equal(t, []int{1000, 1000, 500}, sizes)
		assert.Equal(t, want, got, "every connector once, in byte order")
		count, err := s.CountInventory(ctx, orgA, store.InventoryConnector, false)
		require.NoError(t, err)
		assert.Equal(t, 2500, count, "the count is the whole inventory, not a page")

		page, _, err := s.ListInventory(ctx, orgA, store.InventoryConnector, false, store.PageRequest{})
		require.NoError(t, err)
		assert.Len(t, page, store.DefaultInventoryPageLimit, "a page without a limit")
		page, next, err := s.ListInventory(ctx, orgA, store.InventoryConnector, false, store.PageRequest{Limit: store.MaxInventoryPageLimit + 1})
		require.NoError(t, err)
		assert.Len(t, page, 2500)
		assert.Empty(t, next)
	})

	t.Run("ids are in byte order", func(t *testing.T) {
		s := newStore(t)
		// an order that a collation of a language would change
		keys := []string{"aaaa.cp-2", "aaaa.CP_1", "aaaa.Cp1", "aaaa.CP#1", "aaaa.cp1", "aaaa.CP-1", "aaaa.CP1", "aaaa.CP.1"}
		for _, key := range keys {
			registerStation(t, s, key, orgA)
		}
		slices.Sort(keys)
		assert.Equal(t, keys, inventoryIds(listInventory(t, s, orgA, store.InventoryStation, false, 1)), "one station per page")
	})

	t.Run("refusals", func(t *testing.T) {
		s := newStore(t)
		_, _, err := s.ListInventory(ctx, orgA, store.InventoryStation, false, store.PageRequest{Cursor: "%%%"})
		assert.ErrorIs(t, err, store.ErrInvalidCursor)
		for name, call := range map[string]func(orgId, typ string) error{
			"list": func(orgId, typ string) error {
				_, _, err := s.ListInventory(ctx, orgId, typ, false, store.PageRequest{})
				return err
			},
			"count": func(orgId, typ string) error {
				_, err := s.CountInventory(ctx, orgId, typ, false)
				return err
			},
		} {
			assert.Error(t, call(orgA, "session"), "%s: a type that the inventory does not have", name)
			assert.Error(t, call("not-a-uuid", store.InventoryStation), "%s: an org id that is not a UUID", name)
			assert.NoError(t, call(strings.ToUpper(orgA), store.InventoryStation), "%s: an org id in upper case", name)
		}
	})
}
