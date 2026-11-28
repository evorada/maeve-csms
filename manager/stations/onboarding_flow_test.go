// SPDX-License-Identifier: Apache-2.0

package stations_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/entitlements"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	basehandlers "github.com/thoughtworks/maeve-csms/manager/handlers"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/internalapi"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/onboarding"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/server"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	clockTest "k8s.io/utils/clock/testing"
)

// A station that no operator registered connects to the host of a tenant, is admitted as
// pending, boots Pending and cannot charge; once it is active it boots Accepted and
// charges.
func TestOnboardingOfAStationThatIsNotRegistered(t *testing.T) {
	ctx := context.Background()
	const (
		orgId      = "0193b2c4-0000-7000-8000-0000000000a1"
		stationKey = "aaaa.CP777"
		card       = "MYRFIDCARD"
	)
	clk := clockTest.NewFakePassiveClock(time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC))
	engine := inmemory.NewStore(clk)
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: orgId, Path: store.OrgPathLabel(orgId), Slug: "tenant-a", OrgKey: "aaaa", Hostnames: []string{"a.example"},
	}))
	require.NoError(t, engine.SetToken(ctx, &store.Token{
		OrgId: orgId, CountryCode: "ES", PartyId: "CPO", Type: "RFID", Uid: card, ContractId: "ESCPO012345678V",
		Issuer: "CPO", Valid: true, CacheMode: store.CacheModeAlways,
	}))
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine}
	internal := internalapi.NewHandler(resolver, engine)
	domainEvents := eventstest.New(engine, orgId, clk)
	boot := handlers.BootNotificationHandler{
		Clock: clk, RuntimeDetailsStore: engine, SettingsStore: engine, StatusStore: engine,
		RegistryStore: engine, Tx: domainEvents.Tx, Events: domainEvents.Events, HeartbeatInterval: 300,
		TenantConfig: &tenancy.ConfigProvider{Resolver: resolver},
	}
	authorize := handlers.AuthorizeHandler{TokenStore: engine, RegistryStore: engine, OrgResolver: resolver, Clock: clk}
	start := handlers.StartTransactionHandler{
		Clock: clk, TokenStore: engine, RegistryStore: engine, OrgResolver: resolver, TransactionStore: engine,
		ReservationStore: engine, Tx: domainEvents.Tx, Events: domainEvents.Events,
	}
	query := "?" + url.Values{"host": {"a.example"}, "stationId": {"CP777"}}.Encode()

	// the gateway looks the station up and records its connection
	rr := httptest.NewRecorder()
	internal.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/station-auth"+query, nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var auth internalapi.StationAuthResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &auth))
	assert.Equal(t, stationKey, auth.StationKey)
	require.NotNil(t, auth.Onboarding)
	assert.True(t, auth.Onboarding.OpenSP1)

	rr = httptest.NewRecorder()
	internal.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/station-onboarding"+query,
		strings.NewReader(`{"transport":"ws","credential":"open"}`)))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.JSONEq(t, `{"created":true}`, rr.Body.String())

	bootStation := func() *types.BootNotificationResponseJson {
		resp, err := boot.HandleCall(ctx, stationKey, &types.BootNotificationJson{ChargePointVendor: "Acme", ChargePointModel: "Volt 22"})
		require.NoError(t, err)
		return resp.(*types.BootNotificationResponseJson)
	}
	authorizeCard := func() types.AuthorizeResponseJsonIdTagInfoStatus {
		resp, err := authorize.HandleCall(ctx, stationKey, &types.AuthorizeJson{IdTag: card})
		require.NoError(t, err)
		return resp.(*types.AuthorizeResponseJson).IdTagInfo.Status
	}
	startTransaction := func() *types.StartTransactionResponseJson {
		resp, err := start.HandleCall(ctx, stationKey, &types.StartTransactionJson{
			ConnectorId: 1, IdTag: card, MeterStart: 0, Timestamp: "2026-11-28T09:05:00Z",
		})
		require.NoError(t, err)
		return resp.(*types.StartTransactionResponseJson)
	}

	// pending: it boots Pending, is announced once, and cannot charge
	got := bootStation()
	assert.Equal(t, types.BootNotificationResponseJsonStatusPending, got.Status)
	assert.Equal(t, 60, got.Interval)
	assert.Equal(t, []string{events.TypeStationPending}, eventstest.Types(domainEvents.Take(t)))
	assert.Equal(t, types.AuthorizeResponseJsonIdTagInfoStatusInvalid, authorizeCard())
	refused := startTransaction()
	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusInvalid, refused.IdTagInfo.Status)
	assert.Equal(t, -1, refused.TransactionId)
	_, total, err := engine.ListTransactionsForChargeStation(ctx, stationKey, "", nil, nil, 10, 0, nil)
	require.NoError(t, err)
	assert.Zero(t, total)
	pending, _, err := engine.ListChargeStations(ctx, store.ChargeStationFilter{
		OrgIds: []string{orgId}, States: []store.StationState{store.StationStatePending},
	})
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, stationKey, pending[0].StationKey)

	// active (as an approval leaves it): it boots Accepted and charges
	require.NoError(t, engine.SetChargeStationState(ctx, stationKey, store.StationStateActive, nil))
	got = bootStation()
	assert.Equal(t, types.BootNotificationResponseJsonStatusAccepted, got.Status)
	assert.Equal(t, 300, got.Interval)
	assert.Equal(t, types.AuthorizeResponseJsonIdTagInfoStatusAccepted, authorizeCard())
	started := startTransaction()
	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusAccepted, started.IdTagInfo.Status)
	assert.Positive(t, started.TransactionId)
}

// sentCalls records the calls that the command queue sends to the stations.
type sentCalls struct {
	mu    sync.Mutex
	calls []*transport.Message
}

func (s *sentCalls) Emit(_ context.Context, _ transport.OcppVersion, _ string, msg *transport.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, msg)
	return nil
}

func (s *sentCalls) sent() []*transport.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	var calls []*transport.Message
	for _, msg := range s.calls {
		if msg.MessageType == transport.MessageTypeCall {
			calls = append(calls, msg)
		}
	}
	return calls
}

// alwaysConnected says that every station is connected.
type alwaysConnected struct{}

func (alwaysConnected) IsConnected(context.Context, string) (bool, error) { return true, nil }

// operatorOf is a verifier that takes every request for one of an operator of the org.
type operatorOf string

func (o operatorOf) Verify(context.Context, string) (*tenancy.Principal, error) {
	return &tenancy.Principal{Subject: "user:alice", OrgId: string(o), OrgScope: tenancy.OrgScopeSelf, Perms: tenancy.AllPermissions}, nil
}

// A station that no operator registered connects to the host of a tenant, is admitted as
// pending, boots Pending and cannot charge. An operator approves it through the API: its
// new password is sent to it through the command queue, the station takes it, connects
// with it, is asked to boot, boots Accepted and charges.
func TestAPendingStationIsApprovedAndCharges(t *testing.T) {
	ctx := context.Background()
	const (
		orgId      = "0193b2c4-0000-7000-8000-0000000000a1"
		stationKey = "aaaa.CP777"
		card       = "MYRFIDCARD"
	)
	clk := clockTest.NewFakeClock(time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC))
	engine := inmemory.NewStore(clk)
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: orgId, Path: store.OrgPathLabel(orgId), Slug: "tenant-a", OrgKey: "aaaa", Hostnames: []string{"a.example"},
		Parties: []store.TenantParty{{Id: "cpo-a", Role: "CPO", CountryCode: "ES", PartyId: "CPO"}},
	}))
	require.NoError(t, engine.SetToken(ctx, &store.Token{
		OrgId: orgId, CountryCode: "ES", PartyId: "CPO", Type: "RFID", Uid: card, ContractId: "ESCPO012345678V",
		Issuer: "CPO", Valid: true, CacheMode: store.CacheModeAlways,
	}))
	loc := &store.AssetLocation{OrgId: orgId, PartyRef: "cpo-a", OcpiId: "LOC1", Name: ptr("Plaza"), Address: "Calle Mayor 1",
		City: "Madrid", Country: "ESP", Latitude: "40.416775", Longitude: "-3.703790", TimeZone: "Europe/Madrid", Publish: true}
	require.NoError(t, engine.CreateAssetLocation(ctx, loc))

	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine}
	scoper := &tenancy.Scoper{Mode: tenancy.ModeMulti, Tenants: engine, Resolver: resolver}
	domainEvents := &events.OutboxEmitter{Store: engine, Orgs: resolver, Clock: clk, Product: events.Product}
	calls := &sentCalls{}
	queueConfig := commands.Config{ResponseTimeout: 30 * time.Second, SweepInterval: 5 * time.Second, DefaultTTL: time.Hour,
		MaxTTL: 168 * time.Hour, Retention: 90 * 24 * time.Hour}
	dispatcher := &commands.Dispatcher{Store: engine, Tx: engine, Events: domainEvents,
		Senders:      map[transport.OcppVersion]basehandlers.RawCallMaker{transport.OcppVersion16: handlers.NewCallMaker(calls)},
		Connectivity: alwaysConnected{}, SchemaFS: schemas.OcppSchemas, Clock: clk, Config: queueConfig}
	queue := &commands.Service{Store: engine, Tx: engine, Events: domainEvents, RuntimeDetails: engine, Registry: engine,
		Dispatcher: dispatcher, SchemaFS: schemas.OcppSchemas, Clock: clk, Config: queueConfig}
	dispatcher.AfterCompletion = []commands.CompletionHook{
		&onboarding.Approvals{Store: engine, Tx: engine, Events: domainEvents, Commands: queue},
	}
	internal := internalapi.NewHandler(resolver, engine)
	apiHandler := server.NewApiHandler(server.ApiDeps{Engine: engine, Verifier: operatorOf(orgId), Scoper: scoper,
		Events: domainEvents, Commands: queue, Entitlements: &entitlements.Enforcer{Tenants: engine, Counter: engine, Mode: tenancy.ModeMulti}})
	router := handlers.NewRouter(calls, clk, engine, domainEvents, resolver, &tenancy.ConfigProvider{Resolver: resolver},
		nil, nil, nil, 5*time.Minute, schemas.OcppSchemas, dispatcher, nil, nil, nil, nil)
	query := "?" + url.Values{"host": {"a.example"}, "stationId": {"CP777"}}.Encode()

	stationAuth := func() internalapi.StationAuthResponse {
		t.Helper()
		rr := httptest.NewRecorder()
		internal.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/station-auth"+query, nil))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var auth internalapi.StationAuthResponse
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &auth))
		return auth
	}
	// call has the router handle a call of the station and returns the answer
	call := func(action, messageId, payload string) json.RawMessage {
		t.Helper()
		before := len(calls.calls)
		router.Handle(ctx, stationKey, &transport.Message{MessageType: transport.MessageTypeCall, Action: action,
			MessageId: messageId, RequestPayload: json.RawMessage(payload)})
		calls.mu.Lock()
		defer calls.mu.Unlock()
		for _, msg := range calls.calls[before:] {
			if msg.MessageId == messageId {
				require.Equal(t, transport.MessageTypeCallResult, msg.MessageType, "%s: %s", action, msg.ErrorDescription)
				return msg.ResponsePayload
			}
		}
		require.Failf(t, "no answer", "%s %s", action, messageId)
		return nil
	}
	status := func(answer json.RawMessage, path ...string) any {
		t.Helper()
		var doc map[string]any
		require.NoError(t, json.Unmarshal(answer, &doc))
		var v any = doc
		for _, key := range path {
			v = v.(map[string]any)[key]
		}
		return v
	}
	boot := `{"chargePointVendor":"Acme","chargePointModel":"Volt 22"}`
	authorize := `{"idTag":"` + card + `"}`
	start := `{"connectorId":1,"idTag":"` + card + `","meterStart":0,"timestamp":"2026-11-28T09:05:00Z"}`

	// admitted for its onboarding, it boots Pending and cannot charge
	require.NotNil(t, stationAuth().Onboarding)
	rr := httptest.NewRecorder()
	internal.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/station-onboarding"+query,
		strings.NewReader(`{"transport":"ws","credential":"open"}`)))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, "Pending", status(call("BootNotification", "b1", boot), "status"))
	assert.Equal(t, "Invalid", status(call("Authorize", "a1", authorize), "idTagInfo", "status"))

	// an operator approves it
	req := httptest.NewRequest(http.MethodPost, "/api/v0/cs/"+stationKey+"/approve", strings.NewReader(`{"location_id":"`+loc.Id+
		`","evses":[{"ocpp_evse_id":1,"connectors":[{"ocpp_connector_id":1,"standard":"IEC_62196_T2","format":"SOCKET",`+
		`"power_type":"AC_3_PHASE","max_voltage":400,"max_amperage":32}]}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer token")
	rr = httptest.NewRecorder()
	apiHandler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())

	// it gets its new password, and may connect with it while it takes it
	sent := calls.sent()
	require.Len(t, sent, 1)
	require.Equal(t, "ChangeConfiguration", sent[0].Action)
	var change types.ChangeConfigurationJson
	require.NoError(t, json.Unmarshal(sent[0].RequestPayload, &change))
	require.Equal(t, "AuthorizationKey", change.Key)
	password := change.Value
	auth := stationAuth()
	assert.NotNil(t, auth.Onboarding, "still pending")
	assert.Equal(t, onboarding.PasswordHash(password), auth.PendingBase64SHA256Password)
	assert.Equal(t, "Pending", status(call("BootNotification", "b2", boot), "status"))

	// it takes the password (the gateway hands its answer on with the call it answers)
	router.Handle(ctx, stationKey, &transport.Message{MessageType: transport.MessageTypeCallResult, Action: sent[0].Action,
		MessageId: sent[0].MessageId, RequestPayload: sent[0].RequestPayload, ResponsePayload: json.RawMessage(`{"status":"Accepted"}`)})

	// it reconnects with its password: the gateway finds an active station whose
	// password hash is the hash of the password it sends
	auth = stationAuth()
	assert.Nil(t, auth.Onboarding)
	require.NotNil(t, auth.SecurityProfile)
	assert.Equal(t, int(store.UnsecuredTransportWithBasicAuth), *auth.SecurityProfile)
	assert.Equal(t, onboarding.PasswordHash(password), auth.Base64SHA256Password)
	assert.Empty(t, auth.PendingBase64SHA256Password)

	// it is asked to boot, boots Accepted and charges
	sent = calls.sent()
	require.Len(t, sent, 2)
	assert.Equal(t, "TriggerMessage", sent[1].Action)
	assert.JSONEq(t, `{"requestedMessage":"BootNotification"}`, string(sent[1].RequestPayload))
	assert.Equal(t, "Accepted", status(call("BootNotification", "b3", boot), "status"))
	assert.Equal(t, "Accepted", status(call("Authorize", "a2", authorize), "idTagInfo", "status"))
	started := call("StartTransaction", "s1", start)
	assert.Equal(t, "Accepted", status(started, "idTagInfo", "status"))
	assert.Positive(t, status(started, "transactionId"))
	_, total, err := engine.ListTransactionsForChargeStation(ctx, stationKey, "", nil, nil, 10, 0, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
}

func ptr[T any](v T) *T {
	return &v
}
