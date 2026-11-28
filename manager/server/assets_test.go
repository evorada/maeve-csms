// SPDX-License-Identifier: Apache-2.0

package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/server"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
)

// The asset operations through the handler of the API listener, with the tokens of the
// platform: what one tenant creates, another tenant cannot read or change.
func TestAssetsThroughTheApiHandler(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	for orgId, orgKey := range map[string]string{orgA: "aaaa", orgB: "bbbb"} {
		require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
			OrgId: orgId, Path: store.OrgPathLabel(orgId), Slug: "tenant-" + orgKey, OrgKey: orgKey,
			Parties: []store.TenantParty{{Id: "cpo-" + orgKey, Role: "CPO", CountryCode: "BE", PartyId: "TWK"}},
		}))
		require.NoError(t, engine.SetChargeStationAuth(ctx, orgKey+".CP001", &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgId}))
	}
	platform, verifier := newPlatformVerifier(t)
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine, DefaultOrgId: store.LegacyOrgId}
	handler := server.NewApiHandler(server.ApiDeps{
		Engine:   engine,
		Verifier: verifier,
		Scoper:   scoperOf(resolver),
		Events:   eventsOf(engine, resolver),
	})
	tokenOfA := tokenOf(platform, orgA, tenancy.PermStationRead, tenancy.PermStationWrite)
	tokenOfB := tokenOf(platform, orgB, tenancy.AllPermissions...)

	const locationBody = `{"party_ref":"cpo-aaaa","name":"Gent","address":"F. Rooseveltlaan 3A","city":"Gent",` +
		`"country":"BEL","coordinates":{"latitude":"51.047599","longitude":"3.729944"},"time_zone":"Europe/Brussels"}`
	rr := request(handler, http.MethodPost, "/api/v0/locations", tokenOfA, locationBody)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	var location struct {
		Id    string `json:"id"`
		OrgId string `json:"org_id"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &location))
	assert.Equal(t, orgA, location.OrgId)
	locationPath := rr.Header().Get("Location")
	require.Equal(t, "/api/v0/locations/"+location.Id, locationPath)

	rr = get(handler, locationPath, tokenOfA)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, `"1"`, rr.Header().Get("ETag"))

	rr = request(handler, http.MethodPost, locationPath+"/evses", tokenOfA,
		`{"cs_id":"aaaa.CP001","ocpp_evse_id":1,"connectors":[{"ocpp_connector_id":1,"standard":"IEC_62196_T2","format":"SOCKET","power_type":"AC_3_PHASE","max_voltage":400,"max_amperage":32},`+
			`{"ocpp_connector_id":2,"standard":"IEC_62196_T2","format":"CABLE","power_type":"AC_3_PHASE","max_voltage":400,"max_amperage":32}]}`)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	var evse struct {
		Uid        string `json:"uid"`
		EvseId     string `json:"evse_id"`
		Connectors []struct {
			Uid string `json:"uid"`
		} `json:"connectors"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &evse))
	assert.Equal(t, "BE*TWK*ECP001*1", evse.EvseId)
	evsePath := rr.Header().Get("Location")
	require.Equal(t, "/api/v0/evses/"+evse.Uid, evsePath)
	connectorPath := evsePath + "/connectors/" + evse.Connectors[1].Uid

	t.Run("another tenant", func(t *testing.T) {
		const unknown = "0193b2c5-ffff-7000-8000-000000000000"
		for _, tc := range []struct{ method, target, unknown, body string }{
			{http.MethodGet, locationPath, "/api/v0/locations/" + unknown, ""},
			{http.MethodPut, locationPath, "/api/v0/locations/" + unknown, locationBody},
			{http.MethodDelete, locationPath, "/api/v0/locations/" + unknown, ""},
			{http.MethodGet, locationPath + "/ocpi", "/api/v0/locations/" + unknown + "/ocpi", ""},
			{http.MethodGet, locationPath + "/evses", "/api/v0/locations/" + unknown + "/evses", ""},
			{http.MethodGet, evsePath, "/api/v0/evses/" + unknown, ""},
			{http.MethodPut, evsePath, "/api/v0/evses/" + unknown, `{"cs_id":"aaaa.CP001","ocpp_evse_id":2}`},
			{http.MethodDelete, evsePath, "/api/v0/evses/" + unknown, ""},
			{http.MethodPost, evsePath + "/connectors", "/api/v0/evses/" + unknown + "/connectors", `{"ocpp_connector_id":3,"standard":"IEC_62196_T2","format":"SOCKET","power_type":"AC_3_PHASE","max_voltage":400,"max_amperage":32}`},
			{http.MethodPut, connectorPath, "/api/v0/evses/" + unknown + "/connectors/" + unknown, `{"ocpp_connector_id":2,"standard":"IEC_62196_T2","format":"SOCKET","power_type":"AC_3_PHASE","max_voltage":400,"max_amperage":32}`},
			{http.MethodDelete, connectorPath, "/api/v0/evses/" + unknown + "/connectors/" + unknown, ""},
		} {
			other := request(handler, tc.method, tc.target, tokenOfB, tc.body)
			none := request(handler, tc.method, tc.unknown, tokenOfB, tc.body)
			require.Equal(t, http.StatusNotFound, other.Code, "%s %s: %s", tc.method, tc.target, other.Body.String())
			assert.JSONEq(t, `{"status":"Not Found"}`, other.Body.String())
			assert.Equal(t, none.Code, other.Code)
			assert.Equal(t, none.Body.String(), other.Body.String())
		}

		rr := get(handler, "/api/v0/locations", tokenOfB)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"items":[],"next_cursor":null}`, rr.Body.String())
	})

	t.Run("a change against an older version", func(t *testing.T) {
		rr := request(handler, http.MethodPut, locationPath, tokenOfA, locationBody)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, `"2"`, rr.Header().Get("ETag"))

		req := httptest.NewRequest(http.MethodPut, locationPath, strings.NewReader(locationBody))
		req.Header.Set("Authorization", "Bearer "+tokenOfA)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", `"1"`)
		rr = httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		require.Equal(t, http.StatusPreconditionFailed, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Precondition Failed","error":"version_conflict: the location has version 2"}`, rr.Body.String())
	})

	t.Run("a location in use", func(t *testing.T) {
		rr := request(handler, http.MethodDelete, locationPath, tokenOfA, "")
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Conflict","error":"in_use: EVSEs are at the location"}`, rr.Body.String())
	})

	t.Run("reading is not writing", func(t *testing.T) {
		reader := tokenOf(platform, orgA, tenancy.PermStationRead)
		rr := get(handler, evsePath, reader)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		for _, tc := range []struct{ method, target string }{
			{http.MethodPost, "/api/v0/locations"},
			{http.MethodDelete, evsePath},
			{http.MethodDelete, connectorPath},
		} {
			rr := request(handler, tc.method, tc.target, reader, locationBody)
			require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
			assert.JSONEq(t, `{"status":"Forbidden","error":"missing permission cpms.station.write"}`, rr.Body.String())
		}
	})

	t.Run("the changes are events of the org of the location", func(t *testing.T) {
		var relayed []*store.OutboxEvent
		_, err := engine.RelayOutboxEvents(ctx, 100, func(_ context.Context, evs []*store.OutboxEvent) (int, error) {
			relayed = append(relayed, evs...)
			return len(evs), nil
		})
		require.NoError(t, err)
		require.Len(t, relayed, 3, "location created, EVSE created, location updated")
		for _, ev := range relayed {
			assert.Equal(t, events.TypeLocationUpdated, ev.Type)
			assert.Equal(t, orgA, ev.OrgId)
			assert.Equal(t, "user:alice", ev.Actor)
		}
	})
}
