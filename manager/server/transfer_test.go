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
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/server"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
)

// A charge station that the platform transfers through the control plane keeps
// connecting to the host of its old tenant with its key and password, gets the config of
// its new tenant, and moves from the API of the old tenant to the one of the new tenant.
func TestATransferredChargeStationConnectsAsBeforeAndBelongsToItsNewTenant(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	for _, tenant := range []*store.Tenant{
		{OrgId: orgA, Path: store.OrgPathLabel(orgA), Slug: "tenant-a", OrgKey: "aaaa", Hostnames: []string{"a.example.com"},
			ConfigVersion: 1, Config: json.RawMessage(`{"version":1,"heartbeat_interval_s":120}`),
			Parties: []store.TenantParty{{Id: "cpo-a", Role: "CPO", CountryCode: "ES", PartyId: "AAA"}}},
		{OrgId: orgB, Path: store.OrgPathLabel(orgB), Slug: "tenant-b", OrgKey: "bbbb", Hostnames: []string{"b.example.com"},
			ConfigVersion: 1, Config: json.RawMessage(`{"version":1,"heartbeat_interval_s":600}`),
			Parties: []store.TenantParty{{Id: "cpo-b", Role: "CPO", CountryCode: "ES", PartyId: "BBB"}}},
	} {
		require.NoError(t, engine.PutTenant(ctx, tenant))
	}
	require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.CP001", &store.ChargeStationAuth{
		SecurityProfile: store.TLSWithBasicAuth, Base64SHA256Password: "c2VjcmV0", OrgId: orgA,
	}))

	platform, verifier := newPlatformVerifier(t)
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine, DefaultOrgId: store.LegacyOrgId}
	domainEvents := eventsOf(engine, resolver)
	api := server.NewApiHandler(server.ApiDeps{Engine: engine, Verifier: verifier, Scoper: scoperOf(resolver), Events: domainEvents})
	internal := server.NewInternalHandler(server.InternalDeps{
		Resolver: resolver, Engine: engine, Verifier: verifier, Scoper: scoperOf(resolver), Events: domainEvents,
	})
	tokenOfA := tokenOf(platform, orgA, tenancy.PermStationRead, tenancy.PermStationWrite)
	tokenOfB := tokenOf(platform, orgB, tenancy.PermStationRead, tenancy.PermStationWrite)
	boot := handlers.BootNotificationHandler{
		Clock: clock.RealClock{}, RuntimeDetailsStore: engine, SettingsStore: engine, StatusStore: engine,
		HeartbeatInterval: 300, TenantConfig: &tenancy.ConfigProvider{Resolver: resolver},
	}
	stationAuth := func(t *testing.T) map[string]any {
		t.Helper()
		rr := get(internal, "/_internal/v1/station-auth?host=a.example.com&stationId=CP001", "")
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var answer map[string]any
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &answer))
		return answer
	}
	heartbeat := func(t *testing.T) int {
		t.Helper()
		got, err := boot.HandleCall(ctx, "aaaa.CP001", &types.BootNotificationJson{ChargePointVendor: "Acme", ChargePointModel: "Volt"})
		require.NoError(t, err)
		response := got.(*types.BootNotificationResponseJson)
		require.Equal(t, types.BootNotificationResponseJsonStatusAccepted, response.Status)
		return response.Interval
	}

	before := stationAuth(t)
	assert.Equal(t, orgA, before["orgId"])
	assert.Equal(t, 120, heartbeat(t))

	// a platform service that may manage tenants and write charge stations
	mover := serviceToken(platform, "svc:mover", tenancy.PermTenantManage, tenancy.PermStationWrite)
	rr := request(internal, http.MethodPost, transferTarget("urn:cpms:"+orgA+":station/aaaa.CP001"), mover, `{"to_org_id":"`+orgB+`"}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	t.Run("the station connects to the same host with the same key and password", func(t *testing.T) {
		after := stationAuth(t)
		assert.Equal(t, orgB, after["orgId"])
		assert.Equal(t, "aaaa", after["orgKey"], "the key of the old tenant stays")
		assert.Equal(t, "aaaa.CP001", after["stationKey"])
		assert.Equal(t, before["securityProfile"], after["securityProfile"])
		assert.EqualValues(t, store.TLSWithBasicAuth, after["securityProfile"])
		assert.Equal(t, "c2VjcmV0", after["base64SHA256Password"])
		assert.Equal(t, before["base64SHA256Password"], after["base64SHA256Password"])
	})

	t.Run("and gets the config of its new tenant", func(t *testing.T) {
		assert.Equal(t, 600, heartbeat(t))
	})

	t.Run("the old tenant no longer sees it, the new one does", func(t *testing.T) {
		rr := get(api, "/api/v0/cs/aaaa.CP001", tokenOfA)
		assert.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())
		rr = get(api, "/api/v0/cs/aaaa.CP001", tokenOfB)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var record struct {
			CsId  string `json:"cs_id"`
			OrgId string `json:"org_id"`
		}
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &record))
		assert.Equal(t, "aaaa.CP001", record.CsId)
		assert.Equal(t, orgB, record.OrgId)

		var page struct {
			Items []struct {
				CsId string `json:"cs_id"`
			} `json:"items"`
		}
		rr = get(api, "/api/v0/cs", tokenOfA)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &page))
		assert.Empty(t, page.Items)
		rr = get(api, "/api/v0/cs", tokenOfB)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &page))
		require.Len(t, page.Items, 1)
		assert.Equal(t, "aaaa.CP001", page.Items[0].CsId)
	})

	t.Run("both tenants get the event", func(t *testing.T) {
		_, err := engine.RelayOutboxEvents(ctx, store.MaxOutboxEventListLimit, func(_ context.Context, evs []*store.OutboxEvent) (int, error) {
			return len(evs), nil
		})
		require.NoError(t, err)
		for _, orgId := range []string{orgA, orgB} {
			claims := platform.Claims(time.Now())
			claims["sub"] = "svc:events"
			claims["org"] = orgId
			claims["org_scope"] = "self"
			claims["perms"] = []string{tenancy.PermTenantManage}
			rr := get(internal, "/_control/v1/events?type=station.transferred", platform.Sign(claims))
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			var page struct {
				Events []struct {
					Type    string `json:"type"`
					Subject string `json:"subject"`
					Actor   string `json:"actor"`
				} `json:"events"`
			}
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &page))
			require.Len(t, page.Events, 1, orgId)
			assert.Equal(t, events.Product+"."+events.TypeStationTransferred, page.Events[0].Type)
			assert.Equal(t, "urn:cpms:"+orgId+":station/aaaa.CP001", page.Events[0].Subject)
			assert.Equal(t, "svc:mover", page.Events[0].Actor)
		}
	})
}
