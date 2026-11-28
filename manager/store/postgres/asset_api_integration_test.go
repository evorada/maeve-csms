// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// The tests in this file check that the asset operations of the API change the assets
// and store their location.updated events in one PostgreSQL transaction.

const assetApiLocation = `{"party_ref":"p1","ocpi_id":"LOC-1","address":"Calle Mayor 1","city":"Madrid",` +
	`"country":"ESP","coordinates":{"latitude":"40.416775","longitude":"-3.703790"},"time_zone":"Europe/Madrid"}`

// assetApi returns the routes of a single-tenant API server whose events the fixture
// records, called by a caller of the legacy org with every permission.
func assetApi(t *testing.T, fixture *eventstest.Fixture) http.Handler {
	t.Helper()
	srv, err := api.NewServer(fixture.Engine(testStore), clockTest.NewFakePassiveClock(apiEventsTime), nil, fixture.Events)
	require.NoError(t, err)
	routes := api.Handler(srv)
	caller := &tenancy.Principal{Subject: "user:test", OrgId: store.LegacyOrgId, OrgScope: tenancy.OrgScopeSelf, Perms: tenancy.AllPermissions}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := tenancy.WithPrincipal(r.Context(), caller)
		routes.ServeHTTP(w, r.WithContext(events.WithActor(ctx, caller.Subject)))
	})
}

func callAssetApi(handler http.Handler, method, target, body string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestAssetApi_ChangesAndTheirEventsAreOneTransaction(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()
	fixture := eventstest.New(testStore, "", clockTest.NewFakePassiveClock(apiEventsTime))
	handler := assetApi(t, fixture)
	require.NoError(t, testStore.SetChargeStationAuth(ctx, "ASSET-CS1", &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: store.LegacyOrgId}))

	rr := callAssetApi(handler, http.MethodPost, "/locations", assetApiLocation)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	var loc struct {
		Id string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &loc))
	rr = callAssetApi(handler, http.MethodPost, "/locations/"+loc.Id+"/evses",
		`{"cs_id":"ASSET-CS1","ocpp_evse_id":1,"evse_id":"ES*ABC*E1*1","connectors":[`+
			`{"ocpp_connector_id":1,"standard":"IEC_62196_T2","format":"SOCKET","power_type":"AC_3_PHASE","max_voltage":400,"max_amperage":32},`+
			`{"ocpp_connector_id":2,"standard":"IEC_62196_T2","format":"SOCKET","power_type":"AC_3_PHASE","max_voltage":400,"max_amperage":32}]}`)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	var evse struct {
		Uid        string `json:"uid"`
		Connectors []struct {
			Uid string `json:"uid"`
		} `json:"connectors"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &evse))
	cs, err := testStore.LookupChargeStation(ctx, "ASSET-CS1")
	require.NoError(t, err)
	assert.Equal(t, &loc.Id, cs.LocationId, "the charge station is at the location of its first EVSE")

	evs := fixture.Take(t)
	require.Equal(t, []string{events.TypeLocationUpdated, events.TypeLocationUpdated}, eventstest.Types(evs))
	assert.Equal(t, store.LegacyOrgId, evs[1].OrgId)
	assert.Equal(t, "user:test", evs[1].Actor)
	assert.JSONEq(t, `{"location_id":"`+loc.Id+`","ocpi_id":"LOC-1","party_ref":"p1","change":"created","evse_uid":"`+evse.Uid+`","publish":true,"publish_changed":false}`, string(evs[1].Data))
	requireNoOpenTransaction(t)

	t.Run("refused changes store nothing and leave no transaction open", func(t *testing.T) {
		before := outboxRows(t)
		for _, tc := range []struct {
			method, target, body string
			header               []string
			want                 int
		}{
			{http.MethodPost, "/locations", assetApiLocation, nil, http.StatusConflict},
			{http.MethodPut, "/locations/" + loc.Id, assetApiLocation, []string{"If-Match", `"7"`}, http.StatusPreconditionFailed},
			{http.MethodDelete, "/locations/" + loc.Id, "", nil, http.StatusConflict},
			{http.MethodPost, "/locations/" + loc.Id + "/evses", `{"cs_id":"ASSET-CS1","ocpp_evse_id":1,"evse_id":"ES*ABC*E1*2","connectors":[{"ocpp_connector_id":1,"standard":"IEC_62196_T2","format":"SOCKET","power_type":"AC_3_PHASE","max_voltage":400,"max_amperage":32}]}`, nil, http.StatusConflict},
			{http.MethodPost, "/evses/" + evse.Uid + "/connectors", `{"ocpp_connector_id":2,"standard":"IEC_62196_T2","format":"SOCKET","power_type":"AC_3_PHASE","max_voltage":400,"max_amperage":32}`, nil, http.StatusConflict},
			{http.MethodPut, "/evses/" + evse.Uid + "/connectors/" + evse.Connectors[1].Uid, `{"ocpp_connector_id":1,"standard":"IEC_62196_T2","format":"SOCKET","power_type":"AC_3_PHASE","max_voltage":400,"max_amperage":32}`, nil, http.StatusConflict},
		} {
			rr := callAssetApi(handler, tc.method, tc.target, tc.body, tc.header...)
			require.Equal(t, tc.want, rr.Code, "%s %s: %s", tc.method, tc.target, rr.Body.String())
			requireNoOpenTransaction(t)
		}
		assert.Equal(t, before, outboxRows(t))
		assert.Empty(t, fixture.Take(t))
	})

	t.Run("a change whose event cannot be stored is undone", func(t *testing.T) {
		failing := eventstest.New(testStore, "", clockTest.NewFakePassiveClock(apiEventsTime))
		failing.Events.Emitter = &events.OutboxEmitter{Store: failingOutbox{testStore}, Product: events.Product}
		failingHandler := assetApi(t, failing)
		before := outboxRows(t)

		rr := callAssetApi(failingHandler, http.MethodPut, "/locations/"+loc.Id, strings.Replace(assetApiLocation, "Madrid\"", "Toledo\"", 1))
		require.Equal(t, http.StatusInternalServerError, rr.Code, rr.Body.String())
		rr = callAssetApi(failingHandler, http.MethodDelete, "/evses/"+evse.Uid+"/connectors/"+evse.Connectors[1].Uid, "")
		require.Equal(t, http.StatusInternalServerError, rr.Code, rr.Body.String())

		stored, err := testStore.LookupAssetLocation(ctx, loc.Id)
		require.NoError(t, err)
		assert.Equal(t, "Madrid", stored.City)
		assert.Equal(t, int64(1), stored.Version)
		connector, err := testStore.LookupAssetConnector(ctx, evse.Connectors[1].Uid)
		require.NoError(t, err)
		assert.NotNil(t, connector)
		assert.Equal(t, before, outboxRows(t))
		requireNoOpenTransaction(t)
	})

	t.Run("deleting the EVSE and then the location", func(t *testing.T) {
		rr := callAssetApi(handler, http.MethodDelete, "/evses/"+evse.Uid, "")
		require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
		rr = callAssetApi(handler, http.MethodDelete, "/locations/"+loc.Id, "")
		require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
		cs, err := testStore.LookupChargeStation(ctx, "ASSET-CS1")
		require.NoError(t, err)
		assert.Nil(t, cs.LocationId)
		assert.Equal(t, []string{events.TypeLocationUpdated, events.TypeLocationUpdated}, eventstest.Types(fixture.Take(t)))
		requireNoOpenTransaction(t)
	})
}
