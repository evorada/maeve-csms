// SPDX-License-Identifier: Apache-2.0

package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/server"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
)

// The registry of the charge stations and the tokens through the handlers of the API and
// the internal listener, with the tokens of the platform: a tenant lists, changes and
// decommissions its own charge stations only, a decommissioned charge station can no
// longer connect through the gateway, and a token is deleted in the org of the caller.
func TestStationsAndTokensThroughTheApiHandler(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	for orgId, orgKey := range map[string]string{orgA: "aaaa", orgB: "bbbb"} {
		require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
			OrgId: orgId, Path: store.OrgPathLabel(orgId), Slug: "tenant-" + orgKey, OrgKey: orgKey, Hostnames: []string{orgKey + ".example.com"},
		}))
		for _, stationId := range []string{"CP001", "CP002"} {
			require.NoError(t, engine.SetChargeStationAuth(ctx, orgKey+"."+stationId, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgId}))
		}
		require.NoError(t, engine.SetToken(ctx, &store.Token{
			OrgId: orgId, CountryCode: "GB", PartyId: "TWK", Type: "RFID", Uid: "SAMEUID", ContractId: "GBTWK012345678V",
			Issuer: "TWK", Valid: true, CacheMode: store.CacheModeAlways, LastUpdated: "2026-01-02T03:04:05Z",
		}))
	}
	platform, verifier := newPlatformVerifier(t)
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine, DefaultOrgId: store.LegacyOrgId}
	handler := server.NewApiHandler(server.ApiDeps{
		Engine:   engine,
		Verifier: verifier,
		Scoper:   scoperOf(resolver),
		Events:   eventsOf(engine, resolver),
	})
	internal := server.NewInternalHandler(server.InternalDeps{
		Resolver: resolver,
		Engine:   engine,
		Verifier: verifier,
		Scoper:   scoperOf(resolver),
	})
	tokenOfA := tokenOf(platform, orgA, tenancy.PermStationRead, tenancy.PermStationWrite, tenancy.PermTokenWrite)
	tokenOfB := tokenOf(platform, orgB, tenancy.AllPermissions...)
	listed := func(target, token string) []string {
		t.Helper()
		rr := get(handler, target, token)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var page struct {
			Items []struct {
				CsId  string `json:"cs_id"`
				OrgId string `json:"org_id"`
			} `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &page))
		var keys []string
		for _, item := range page.Items {
			keys = append(keys, item.CsId)
		}
		return keys
	}

	t.Run("each tenant lists its own charge stations", func(t *testing.T) {
		assert.Equal(t, []string{"aaaa.CP001", "aaaa.CP002"}, listed("/api/v0/cs", tokenOfA))
		assert.Equal(t, []string{"bbbb.CP001", "bbbb.CP002"}, listed("/api/v0/cs?limit=200&state=active", tokenOfB))
		assert.Empty(t, listed("/api/v0/cs?q=aaaa", tokenOfB))
	})

	t.Run("the charge station of another tenant", func(t *testing.T) {
		for _, tc := range []struct{ method, path, body string }{
			{http.MethodGet, "", ""},
			{http.MethodPatch, "", `{"display_name":"Mine"}`},
			{http.MethodPost, "/decommission", ""},
		} {
			other := request(handler, tc.method, "/api/v0/cs/aaaa.CP002"+tc.path, tokenOfB, tc.body)
			unknown := request(handler, tc.method, "/api/v0/cs/bbbb.UNKNOWN"+tc.path, tokenOfB, tc.body)
			require.Equal(t, http.StatusNotFound, other.Code, "%s %s: %s", tc.method, tc.path, other.Body.String())
			assert.Equal(t, unknown.Body.String(), other.Body.String())
		}
	})

	t.Run("a read-only token reads but does not change", func(t *testing.T) {
		reader := tokenOf(platform, orgA, tenancy.PermStationRead, tenancy.PermTokenRead)
		rr := get(handler, "/api/v0/cs/aaaa.CP001", reader)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		for _, tc := range []struct{ method, target, permission string }{
			{http.MethodPatch, "/api/v0/cs/aaaa.CP001", tenancy.PermStationWrite},
			{http.MethodPost, "/api/v0/cs/aaaa.CP001/decommission", tenancy.PermStationWrite},
			{http.MethodDelete, "/api/v0/token/SAMEUID", tenancy.PermTokenWrite},
		} {
			rr := request(handler, tc.method, tc.target, reader, `{}`)
			require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
			assert.JSONEq(t, `{"status":"Forbidden","error":"missing permission `+tc.permission+`"}`, rr.Body.String())
		}
	})

	t.Run("a charge station is changed and decommissioned", func(t *testing.T) {
		rr := request(handler, http.MethodPatch, "/api/v0/cs/aaaa.CP001", tokenOfA, `{"display_name":"Plaza 1","allow_any_token":true}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		auth := get(internal, "/_internal/v1/station-auth?host=aaaa.example.com&stationId=CP001", "")
		require.Equal(t, http.StatusOK, auth.Code, auth.Body.String())

		rr = request(handler, http.MethodPost, "/api/v0/cs/aaaa.CP001/decommission", tokenOfA, `{"reason":"replaced"}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var record struct {
			State       string `json:"state"`
			StateReason string `json:"state_reason"`
			DisplayName string `json:"display_name"`
		}
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &record))
		assert.Equal(t, "decommissioned", record.State)
		assert.Equal(t, "replaced", record.StateReason)
		assert.Equal(t, "Plaza 1", record.DisplayName)

		auth = get(internal, "/_internal/v1/station-auth?host=aaaa.example.com&stationId=CP001", "")
		require.Equal(t, http.StatusNotFound, auth.Code, auth.Body.String())
		assert.JSONEq(t, `{"status":"Not Found","error":"unknown_station"}`, auth.Body.String())
		auth = get(internal, "/_internal/v1/station-auth?host=bbbb.example.com&stationId=CP001", "")
		require.Equal(t, http.StatusOK, auth.Code, "the station of B with the same id")

		rr = request(handler, http.MethodPost, "/api/v0/cs/aaaa.CP001", tokenOfA, `{"securityProfile":1}`)
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.Equal(t, []string{"aaaa.CP001"}, listed("/api/v0/cs?state=decommissioned", tokenOfA))
	})

	t.Run("a token is deleted in the org of the caller", func(t *testing.T) {
		rr := request(handler, http.MethodDelete, "/api/v0/token/SAMEUID", tokenOfA, "")
		require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
		rr = request(handler, http.MethodDelete, "/api/v0/token/SAMEUID", tokenOfA, "")
		require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
		rr = request(handler, http.MethodDelete, "/api/v0/token/SAMEUID?orgId="+orgB, tokenOfA, "")
		require.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())

		ofA, err := engine.LookupTokenForOrg(ctx, orgA, "SAMEUID")
		require.NoError(t, err)
		assert.Nil(t, ofA)
		ofB, err := engine.LookupTokenForOrg(ctx, orgB, "SAMEUID")
		require.NoError(t, err)
		assert.NotNil(t, ofB, "the token of the other tenant stays")
	})

	t.Run("the decommissioning is an event of the org of the charge station", func(t *testing.T) {
		var relayed []*store.OutboxEvent
		_, err := engine.RelayOutboxEvents(ctx, 100, func(_ context.Context, evs []*store.OutboxEvent) (int, error) {
			relayed = append(relayed, evs...)
			return len(evs), nil
		})
		require.NoError(t, err)
		require.Len(t, relayed, 1, "no EVSE, no location: only the charge station")
		ev := relayed[0]
		assert.Equal(t, events.TypeStationDecommissioned, ev.Type)
		assert.Equal(t, orgA, ev.OrgId)
		assert.Equal(t, "user:alice", ev.Actor)
		assert.JSONEq(t, `{"station_key":"aaaa.CP001","station_id":"CP001","location_id":null,"reason":"replaced","actor":"user:alice"}`, string(ev.Data))
	})
	t.Run("a rejected charge station can no longer connect", func(t *testing.T) {
		_, err := engine.RecordOnboardingConnection(ctx, "aaaa.NEW", orgA, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 50, time.Now())
		require.NoError(t, err)
		auth := get(internal, "/_internal/v1/station-auth?host=aaaa.example.com&stationId=NEW", "")
		require.Equal(t, http.StatusOK, auth.Code, auth.Body.String())
		assert.Contains(t, auth.Body.String(), `"onboarding"`)

		rr := request(handler, http.MethodPost, "/api/v0/cs/aaaa.NEW/reject", tokenOfB, `{"reason":"not ours"}`)
		require.Equal(t, http.StatusNotFound, rr.Code, "the charge station of another tenant: %s", rr.Body.String())
		rr = request(handler, http.MethodPost, "/api/v0/cs/aaaa.NEW/reject", tokenOfA, `{"reason":"not ours"}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		auth = get(internal, "/_internal/v1/station-auth?host=aaaa.example.com&stationId=NEW", "")
		require.Equal(t, http.StatusNotFound, auth.Code, auth.Body.String())
		assert.JSONEq(t, `{"status":"Not Found","error":"unknown_station"}`, auth.Body.String())
		onboarding := request(internal, http.MethodPut, "/_internal/v1/station-onboarding?host=aaaa.example.com&stationId=NEW", "",
			`{"transport":"ws","credential":"open"}`)
		assert.Equal(t, http.StatusConflict, onboarding.Code, "it is not made pending again: %s", onboarding.Body.String())

		var relayed []*store.OutboxEvent
		_, err = engine.RelayOutboxEvents(ctx, 100, func(_ context.Context, evs []*store.OutboxEvent) (int, error) {
			relayed = append(relayed, evs...)
			return len(evs), nil
		})
		require.NoError(t, err)
		require.Len(t, relayed, 1)
		assert.Equal(t, events.TypeStationRejected, relayed[0].Type)
		assert.Equal(t, orgA, relayed[0].OrgId)
		assert.Equal(t, "user:alice", relayed[0].Actor)
	})

}
