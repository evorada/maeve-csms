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

// The sessions of a charge station stay with the tenant that owned the charge station
// when they started. After a transfer during a running session the old tenant gets the
// events of the rest of its session, and the new tenant does not find the session in
// the history of the charge station.
func TestTheSessionsOfATransferredChargeStationStayWithItsOldTenant(t *testing.T) {
	ctx := context.Background()
	const stationKey = "aaaa.CP001"
	engine := inmemory.NewStore(clock.RealClock{})
	for orgId, orgKey := range map[string]string{orgA: "aaaa", orgB: "bbbb"} {
		require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
			OrgId: orgId, Path: store.OrgPathLabel(orgId), Slug: "tenant-" + orgKey, OrgKey: orgKey, Hostnames: []string{orgKey + ".example.com"},
		}))
	}
	require.NoError(t, engine.SetChargeStationAuth(ctx, stationKey, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgA}))

	platform, verifier := newPlatformVerifier(t)
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine, DefaultOrgId: store.LegacyOrgId}
	domainEvents := eventsOf(engine, resolver)
	api := server.NewApiHandler(server.ApiDeps{Engine: engine, Verifier: verifier, Scoper: scoperOf(resolver), Events: domainEvents})
	internal := server.NewInternalHandler(server.InternalDeps{
		Resolver: resolver, Engine: engine, Verifier: verifier, Scoper: scoperOf(resolver), Events: domainEvents,
	})

	// the messages of the charge station
	start := handlers.StartTransactionHandler{
		Clock: clock.RealClock{}, TokenStore: engine, OrgResolver: resolver, TransactionStore: engine, ReservationStore: engine,
		Tx: engine, Events: domainEvents,
	}
	meterValues := handlers.MeterValuesHandler{TransactionStore: engine, MeterValuesStore: engine, Tx: engine, Events: domainEvents}
	stop := handlers.StopTransactionHandler{
		Clock: clock.RealClock{}, TokenStore: engine, OrgResolver: resolver, TransactionStore: engine, Tx: engine, Events: domainEvents,
	}
	startSession := func(t *testing.T, timestamp string) (int, string) {
		t.Helper()
		got, err := start.HandleCall(ctx, stationKey, &types.StartTransactionJson{ConnectorId: 1, IdTag: "MYRFIDTAG", MeterStart: 1000, Timestamp: timestamp})
		require.NoError(t, err)
		transactionId := got.(*types.StartTransactionResponseJson).TransactionId
		return transactionId, handlers.ConvertToUUID(transactionId)
	}
	finishSession := func(t *testing.T, transactionId int, sampledAt, stoppedAt string) {
		t.Helper()
		_, err := meterValues.HandleCall(ctx, stationKey, &types.MeterValuesJson{
			ConnectorId: 1, TransactionId: &transactionId,
			MeterValue: []types.MeterValuesJsonMeterValueElem{{
				Timestamp:    sampledAt,
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{{Value: "1800"}},
			}},
		})
		require.NoError(t, err)
		_, err = stop.HandleCall(ctx, stationKey, &types.StopTransactionJson{TransactionId: transactionId, MeterStop: 2500, Timestamp: stoppedAt})
		require.NoError(t, err)
	}

	// a session of the old tenant runs while the charge station is transferred
	ofOldTenant, sessionOfOldTenant := startSession(t, "2026-11-28T10:00:00Z")
	mover := serviceToken(platform, "svc:mover", tenancy.PermTenantManage, tenancy.PermStationWrite)
	rr := request(internal, http.MethodPost, transferTarget("urn:cpms:"+orgA+":station/"+stationKey), mover, `{"to_org_id":"`+orgB+`"}`)
	require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
	rr = request(internal, http.MethodPost, transferTarget("urn:cpms:"+orgA+":station/"+stationKey), mover, `{"to_org_id":"`+orgB+`","force":true}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	finishSession(t, ofOldTenant, "2026-11-28T10:15:00Z", "2026-11-28T10:30:00Z")
	// and the new tenant has a session of its own
	ofNewTenant, sessionOfNewTenant := startSession(t, "2026-11-28T11:00:00Z")
	finishSession(t, ofNewTenant, "2026-11-28T11:15:00Z", "2026-11-28T11:30:00Z")

	tokenOfA := tokenOf(platform, orgA, tenancy.PermStationRead, tenancy.PermSessionRead)
	tokenOfB := tokenOf(platform, orgB, tenancy.PermStationRead, tenancy.PermSessionRead)

	t.Run("each tenant gets the events of its own session", func(t *testing.T) {
		_, err := engine.RelayOutboxEvents(ctx, store.MaxOutboxEventListLimit, func(_ context.Context, evs []*store.OutboxEvent) (int, error) {
			return len(evs), nil
		})
		require.NoError(t, err)
		for orgId, session := range map[string]string{orgA: sessionOfOldTenant, orgB: sessionOfNewTenant} {
			claims := platform.Claims(time.Now())
			claims["sub"] = "svc:events"
			claims["org"] = orgId
			claims["org_scope"] = "self"
			claims["perms"] = []string{tenancy.PermTenantManage}
			rr := get(internal, "/_control/v1/events?type=session.started&type=session.updated&type=session.completed", platform.Sign(claims))
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			var page struct {
				Events []struct {
					Type    string `json:"type"`
					Subject string `json:"subject"`
				} `json:"events"`
			}
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &page))
			var got []string
			for _, ev := range page.Events {
				got = append(got, ev.Type)
				assert.Equal(t, "urn:cpms:"+orgId+":session/"+session, ev.Subject)
			}
			assert.Equal(t, []string{"cpms.session.started", "cpms.session.updated", "cpms.session.completed"}, got, orgId)
		}
	})

	t.Run("the new tenant reads its own session in the history of the charge station", func(t *testing.T) {
		rr := get(api, "/api/v0/cs/"+stationKey+"/transactions", tokenOfB)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var list struct {
			Total        int `json:"total"`
			Transactions []struct {
				TransactionId string `json:"transactionId"`
			} `json:"transactions"`
		}
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &list))
		assert.Equal(t, 1, list.Total)
		require.Len(t, list.Transactions, 1)
		assert.Equal(t, sessionOfNewTenant, list.Transactions[0].TransactionId)

		rr = get(api, "/api/v0/cs/"+stationKey+"/transaction/"+sessionOfNewTenant, tokenOfB)
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		unknown := get(api, "/api/v0/cs/"+stationKey+"/transaction/"+handlers.ConvertToUUID(999999), tokenOfB)
		require.Equal(t, http.StatusNotFound, unknown.Code, unknown.Body.String())
		rr = get(api, "/api/v0/cs/"+stationKey+"/transaction/"+sessionOfOldTenant, tokenOfB)
		assert.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())
		assert.Equal(t, unknown.Body.String(), rr.Body.String(), "like a transaction that does not exist")

		rr = get(api, "/api/v0/cs/"+stationKey+"/meter-values", tokenOfB)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var meterValues struct {
			Total       int `json:"total"`
			MeterValues []struct {
				TransactionId string `json:"transactionId"`
			} `json:"meterValues"`
		}
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &meterValues))
		assert.Equal(t, 1, meterValues.Total)
		require.Len(t, meterValues.MeterValues, 1)
		assert.Equal(t, sessionOfNewTenant, meterValues.MeterValues[0].TransactionId)
	})

	t.Run("the old tenant does not reach the charge station any more", func(t *testing.T) {
		for _, target := range []string{"/transactions", "/transaction/" + sessionOfOldTenant, "/meter-values"} {
			rr := get(api, "/api/v0/cs/"+stationKey+target, tokenOfA)
			assert.Equal(t, http.StatusNotFound, rr.Code, target)
		}
	})
}
