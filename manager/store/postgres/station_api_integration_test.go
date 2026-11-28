// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	clockTest "k8s.io/utils/clock/testing"
)

// The tests in this file check that the operations on the registry of the charge stations
// change it, with the EVSEs of the charge station and the events, in one PostgreSQL
// transaction.

// stationApi returns the routes of assetApi for operations on a charge station of the
// legacy org, as AuthzMiddleware resolves it.
func stationApi(t *testing.T, fixture *eventstest.Fixture) http.Handler {
	t.Helper()
	routes := assetApi(t, fixture)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routes.ServeHTTP(w, r.WithContext(tenancy.WithStationOrg(r.Context(), store.LegacyOrgId)))
	})
}

func TestStationApi_ChangesAndTheirEventsAreOneTransaction(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()
	fixture := eventstest.New(testStore, "", clockTest.NewFakePassiveClock(apiEventsTime))
	handler := stationApi(t, fixture)
	const stationKey = "STATION-CS1"
	require.NoError(t, testStore.SetChargeStationAuth(ctx, stationKey, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: store.LegacyOrgId}))

	var locations []string
	for _, ocpiId := range []string{"LOC-1", "LOC-2"} {
		loc := &store.AssetLocation{OrgId: store.LegacyOrgId, PartyRef: "p1", OcpiId: ocpiId, Address: "Calle Mayor 1", City: "Madrid",
			Country: "ESP", Latitude: "40.416775", Longitude: "-3.703790", TimeZone: "Europe/Madrid", Publish: true}
		require.NoError(t, testStore.CreateAssetLocation(ctx, loc))
		locations = append(locations, loc.Id)
	}
	evse := &store.AssetEvse{OrgId: store.LegacyOrgId, LocationId: locations[0], StationKey: stationKey, OcppEvseId: 1,
		Connectors: []*store.AssetConnector{{OcppConnectorId: 1, Standard: "IEC_62196_T2", Format: "SOCKET", PowerType: "AC_3_PHASE", MaxVoltage: 400, MaxAmperage: 32}}}
	require.NoError(t, testStore.CreateAssetEvse(ctx, evse))
	require.NoError(t, testStore.UpdateChargeStation(ctx, &store.ChargeStation{StationKey: stationKey, LocationId: &locations[0]}))

	t.Run("a charge station moves with its EVSEs", func(t *testing.T) {
		rr := callAssetApi(handler, http.MethodPatch, "/cs/"+stationKey, `{"location_id":"`+locations[1]+`","max_capacity_kw":22.5}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var record api.ChargeStationRecord
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &record))
		assert.Equal(t, &locations[1], record.LocationId)
		assert.Equal(t, 22.5, *record.MaxCapacityKw)
		moved, err := testStore.LookupAssetEvse(ctx, evse.Id)
		require.NoError(t, err)
		assert.Equal(t, locations[1], moved.LocationId)
		assert.Equal(t, int64(2), moved.Version)
		evs := fixture.Take(t)
		require.Equal(t, []string{events.TypeLocationUpdated, events.TypeLocationUpdated}, eventstest.Types(evs))
		assert.Contains(t, string(evs[0].Data), locations[1])
		assert.Contains(t, string(evs[1].Data), locations[0])
		requireNoOpenTransaction(t)
	})

	t.Run("a refused decommissioning changes nothing and leaves no transaction open", func(t *testing.T) {
		require.NoError(t, testStore.CreateTransaction(ctx, stationKey, "tx-1", "TAG", "ISO14443", nil, 0, false))
		before := outboxRows(t)
		rr := callAssetApi(handler, http.MethodPost, "/cs/"+stationKey+"/decommission", `{"reason":"replaced"}`)
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		requireNoOpenTransaction(t)

		failing := eventstest.New(testStore, "", clockTest.NewFakePassiveClock(apiEventsTime))
		failing.Events.Emitter = &events.OutboxEmitter{Store: failingOutbox{testStore}, Product: events.Product}
		rr = callAssetApi(stationApi(t, failing), http.MethodPost, "/cs/"+stationKey+"/decommission", `{"force":true}`)
		require.Equal(t, http.StatusInternalServerError, rr.Code, rr.Body.String())
		requireNoOpenTransaction(t)

		cs, err := testStore.LookupChargeStation(ctx, stationKey)
		require.NoError(t, err)
		assert.Equal(t, store.StationStateActive, cs.State)
		stored, err := testStore.LookupAssetEvse(ctx, evse.Id)
		require.NoError(t, err)
		assert.Equal(t, store.AssetEvseStatusActive, stored.Status)
		assert.Equal(t, int64(2), stored.Version)
		assert.Equal(t, before, outboxRows(t))
		assert.Empty(t, fixture.Take(t))
	})

	t.Run("a decommissioned charge station", func(t *testing.T) {
		rr := callAssetApi(handler, http.MethodPost, "/cs/"+stationKey+"/decommission", `{"reason":"replaced","force":true}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		requireNoOpenTransaction(t)
		cs, err := testStore.LookupChargeStation(ctx, stationKey)
		require.NoError(t, err)
		assert.Equal(t, store.StationStateDecommissioned, cs.State)
		assert.Equal(t, "replaced", *cs.StateReason)
		removed, err := testStore.LookupAssetEvse(ctx, evse.Id)
		require.NoError(t, err)
		assert.Equal(t, store.AssetEvseStatusRemoved, removed.Status)
		evs := fixture.Take(t)
		require.Equal(t, []string{events.TypeLocationUpdated, events.TypeStationDecommissioned}, eventstest.Types(evs))
		assert.Equal(t, store.LegacyOrgId, evs[1].OrgId)
		assert.JSONEq(t, `{"station_key":"STATION-CS1","station_id":"STATION-CS1","location_id":"`+locations[1]+`","reason":"replaced","actor":"user:test"}`, string(evs[1].Data))

		rr = callAssetApi(handler, http.MethodGet, "/cs?state=decommissioned", "")
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var page api.ChargeStationPage
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &page))
		require.Len(t, page.Items, 1)
		assert.Equal(t, stationKey, page.Items[0].CsId)
		rr = callAssetApi(handler, http.MethodGet, "/cs/"+stationKey, "")
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), `"live_status":"REMOVED"`)

		rr = callAssetApi(handler, http.MethodPost, "/cs/"+stationKey, `{"securityProfile":1}`)
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		requireNoOpenTransaction(t)
	})
}
