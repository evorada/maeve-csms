// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	stdslog "log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/entitlements"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/onboarding"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"golang.org/x/exp/slog"
	clockTest "k8s.io/utils/clock/testing"
)

// unbootedStationOfA is a pending charge station of A that connected but never booted.
const unbootedStationOfA = "aaaa.NOBOOT"

// onboardingPasswordOfA is the hash of the onboarding password of tenant A, when it has
// one.
const onboardingPasswordOfA = "QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVowMTIzNDU="

var hexPassword = regexp.MustCompile(`^[0-9a-f]{40}$`)

// onboardingFixture is the multi mode manager of newMultiTenant with a command queue
// whose completions the approvals follow, the connector limits of the tenants, three
// pending charge stations of A (an OCPP 1.6 one that connected without a password, an
// OCPP 2.0.1 one that connected with the onboarding password and one that never booted),
// a location of A and a location of B.
type onboardingFixture struct {
	*multiTenant
	calls       *sentCalls
	connected   *connectedStations
	dispatcher  *commands.Dispatcher
	service     *commands.Service
	router      *chi.Mux
	location    *store.AssetLocation
	locationOfB *store.AssetLocation
}

func newOnboardingFixture(t *testing.T) *onboardingFixture {
	t.Helper()
	ctx := context.Background()
	m := newMultiTenant(t)
	domainEvents := &events.OutboxEmitter{Store: m.engine, Orgs: m.scoper.Resolver, Clock: m.clock, Product: events.Product}
	config := commands.Config{ResponseTimeout: 30 * time.Second, SweepInterval: 5 * time.Second, DefaultTTL: time.Hour,
		MaxTTL: 168 * time.Hour, Retention: 90 * 24 * time.Hour}
	calls := &sentCalls{}
	connected := &connectedStations{connected: map[string]bool{}}
	dispatcher := &commands.Dispatcher{
		Store:  m.engine,
		Tx:     m.engine,
		Events: domainEvents,
		Senders: map[transport.OcppVersion]handlers.RawCallMaker{
			transport.OcppVersion16:  ocpp16.NewCallMaker(calls),
			transport.OcppVersion201: ocpp201.NewCallMaker(calls),
		},
		Connectivity: connected,
		SchemaFS:     schemas.OcppSchemas,
		Clock:        clockTest.NewFakeClock(m.clock.Now()),
		Config:       config,
	}
	service := &commands.Service{Store: m.engine, Tx: m.engine, Events: domainEvents, RuntimeDetails: m.engine, Registry: m.engine,
		Dispatcher: dispatcher, SchemaFS: schemas.OcppSchemas, Clock: m.clock, Config: config}
	dispatcher.AfterCompletion = []commands.CompletionHook{
		&onboarding.Approvals{Store: m.engine, Tx: m.engine, Events: domainEvents, Commands: service},
	}
	enforcer := &entitlements.Enforcer{Tenants: m.engine, Counter: m.engine, Mode: tenancy.ModeMulti}
	srv, err := api.NewServer(m.engine, m.clock, nil, domainEvents, api.WithScoper(m.scoper), api.WithCommands(service),
		api.WithEntitlements(enforcer))
	require.NoError(t, err)
	router := routerOf(t, srv, m.scoper, func(r *http.Request) *tenancy.Principal {
		p, _ := tenancy.FromContext(r.Context())
		return p
	})

	for _, station := range []struct {
		key, transport, credential, version string
	}{
		{pendingStationOfA, store.OnboardingTransportWs, store.OnboardingCredentialOpen, "1.6"},
		{pending201StationOfA, store.OnboardingTransportWss, store.OnboardingCredentialOnboardingPassword, "2.0.1"},
		{unbootedStationOfA, store.OnboardingTransportWs, store.OnboardingCredentialOpen, ""},
	} {
		_, err := m.engine.RecordOnboardingConnection(ctx, station.key, orgA, station.transport, station.credential, 50, m.clock.Now())
		require.NoError(t, err)
		if station.version != "" {
			require.NoError(t, m.engine.SetChargeStationRuntimeDetails(ctx, station.key, &store.ChargeStationRuntimeDetails{OcppVersion: station.version}))
			connected.set(station.key, true)
		}
	}
	locations := map[string]*store.AssetLocation{}
	for orgId, party := range map[string]string{orgA: partyOfA, orgB: partyOfB} {
		loc := &store.AssetLocation{OrgId: orgId, PartyRef: party, OcpiId: "LOC1", Name: ptr("Plaza"), Address: "Calle Mayor 1",
			City: "Madrid", Country: "ESP", Latitude: "40.416775", Longitude: "-3.703790", TimeZone: "Europe/Madrid", Publish: true}
		require.NoError(t, m.engine.CreateAssetLocation(ctx, loc))
		locations[orgId] = loc
	}
	return &onboardingFixture{multiTenant: m, calls: calls, connected: connected, dispatcher: dispatcher, service: service,
		router: router, location: locations[orgA], locationOfB: locations[orgB]}
}

// approvalBody is an approval at the location with one EVSE of two connectors, and the
// extra fields (JSON members with a leading comma).
func approvalBody(locationId, extra string) string {
	return `{"location_id":"` + locationId + `","display_name":"Plaza 1","evses":[{"ocpp_evse_id":1,"connectors":[` +
		`{"ocpp_connector_id":1,"standard":"IEC_62196_T2","format":"SOCKET","power_type":"AC_3_PHASE","max_voltage":400,"max_amperage":32},` +
		`{"ocpp_connector_id":2,"standard":"IEC_62196_T2_COMBO","format":"CABLE","power_type":"DC","max_voltage":800,"max_amperage":200}]}]` +
		extra + `}`
}

// approve approves the charge station through the API as a person of A.
func (f *onboardingFixture) approve(csId, body string) *httptest.ResponseRecorder {
	return serve(f.router, as(commandCaller(), http.MethodPost, "/cs/"+csId+"/approve", body))
}

// approvalResult reads the answer to an approval.
func approvalResult(t *testing.T, rr *httptest.ResponseRecorder) api.ApprovalResult {
	t.Helper()
	var result api.ApprovalResult
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &result), rr.Body.String())
	return result
}

func (f *onboardingFixture) station(t *testing.T, csId string) *store.ChargeStation {
	t.Helper()
	cs, err := f.engine.LookupChargeStation(context.Background(), csId)
	require.NoError(t, err)
	require.NotNil(t, cs)
	return cs
}

func (f *onboardingFixture) auth(t *testing.T, csId string) *store.ChargeStationAuth {
	t.Helper()
	auth, err := f.engine.LookupChargeStationAuth(context.Background(), csId)
	require.NoError(t, err)
	require.NotNil(t, auth)
	return auth
}

// takeEvents returns the events that reached the outbox since the last call, in order,
// and fails when one of them has the secret.
func (f *onboardingFixture) takeEvents(t *testing.T, secret string) []*store.OutboxEvent {
	t.Helper()
	var taken []*store.OutboxEvent
	_, err := f.engine.RelayOutboxEvents(context.Background(), store.MaxOutboxEventListLimit,
		func(_ context.Context, evs []*store.OutboxEvent) (int, error) {
			taken = append(taken, evs...)
			return len(evs), nil
		})
	require.NoError(t, err)
	for _, ev := range taken {
		if secret != "" {
			assert.NotContains(t, string(ev.Data), secret, ev.Type)
		}
	}
	return taken
}

// outbox returns the types of the events that reached the outbox since the last call,
// and fails when one of them has the secret.
func (f *onboardingFixture) outbox(t *testing.T, secret string) []string {
	t.Helper()
	var types []string
	for _, ev := range f.takeEvents(t, secret) {
		types = append(types, ev.Type)
	}
	return types
}

// sentCalls returns the calls that were sent to the charge stations.
func (f *onboardingFixture) sentCalls() []*transport.Message {
	var calls []*transport.Message
	for _, msg := range f.calls.sent() {
		if msg.MessageType == transport.MessageTypeCall {
			calls = append(calls, msg)
		}
	}
	return calls
}

// captureLogs sends what the manager logs to a buffer until the test ends.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous, previousStd := slog.Default(), stdslog.Default()
	previousOutput, previousFlags := log.Writer(), log.Flags()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	stdslog.SetDefault(stdslog.New(stdslog.NewTextHandler(&logs, &stdslog.HandlerOptions{Level: stdslog.LevelDebug})))
	log.SetOutput(&logs)
	t.Cleanup(func() {
		slog.SetDefault(previous)
		stdslog.SetDefault(previousStd)
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
	})
	return &logs
}

func TestApproveSendsANewPasswordToTheStation(t *testing.T) {
	ctx := context.Background()
	f := newOnboardingFixture(t)
	logs := captureLogs(t)

	rr := f.approve(pendingStationOfA, approvalBody(f.location.Id, ""))
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	assert.NotContains(t, rr.Body.String(), `"password"`, "the password is never shown")
	result := approvalResult(t, rr)
	assert.Nil(t, result.Password)
	assert.Equal(t, api.ChargeStationState("pending"), result.Station.State)
	require.NotNil(t, result.Station.Onboarding)
	assert.Equal(t, api.Approving, result.Station.Onboarding.Status)
	require.NotNil(t, result.Station.Onboarding.CommandId)
	commandId := *result.Station.Onboarding.CommandId

	// the station gets its new password
	sent := f.sentCalls()
	require.Len(t, sent, 1)
	assert.Equal(t, "ChangeConfiguration", sent[0].Action)
	assert.Equal(t, commandId, sent[0].MessageId)
	var change struct{ Key, Value string }
	require.NoError(t, json.Unmarshal(sent[0].RequestPayload, &change))
	assert.Equal(t, "AuthorizationKey", change.Key)
	password := change.Value
	assert.Regexp(t, hexPassword, password)
	assert.Equal(t, onboarding.PasswordHash(password), f.auth(t, pendingStationOfA).PendingBase64SHA256Password,
		"the station may connect with its new password")
	assert.Empty(t, f.auth(t, pendingStationOfA).Base64SHA256Password)
	evses, err := f.engine.ListAssetEvsesForStation(ctx, pendingStationOfA)
	require.NoError(t, err)
	assert.Empty(t, evses, "the EVSEs are created once the station has its password")

	// the command keeps no password once it is sent, and the API never shows it
	stored, err := f.engine.GetCommand(ctx, commandId)
	require.NoError(t, err)
	assert.JSONEq(t, `{"key":"AuthorizationKey","value":"***"}`, string(stored.Payload))
	assert.Equal(t, "user:test", *stored.Actor)
	for _, target := range []string{"/commands/" + commandId, "/cs/" + pendingStationOfA + "/commands"} {
		rr := serve(f.router, as(commandCaller(), http.MethodGet, target, ""))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.NotContains(t, rr.Body.String(), password, target)
		assert.Contains(t, rr.Body.String(), `"value":"***"`, target)
	}
	rr = serve(f.router, as(commandCaller(), http.MethodPost, "/cs/"+pendingStationOfA+"/approve", approvalBody(f.location.Id, "")))
	require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), "approval_in_progress")

	// the station takes it (through the OCPP 1.6 router, as the gateway hands it on)
	router := ocpp16.NewRouter(f.calls, f.clock, f.engine, events.NopEmitter{}, f.scoper.Resolver,
		nil, nil, nil, nil, time.Minute, schemas.OcppSchemas, f.dispatcher, nil, nil)
	router.Handle(ctx, pendingStationOfA, &transport.Message{MessageType: transport.MessageTypeCallResult, Action: sent[0].Action,
		MessageId: sent[0].MessageId, RequestPayload: sent[0].RequestPayload, ResponsePayload: json.RawMessage(`{"status":"Accepted"}`)})

	cs := f.station(t, pendingStationOfA)
	assert.Equal(t, store.StationStateActive, cs.State)
	assert.Equal(t, &store.ChargeStationAuth{SecurityProfile: store.UnsecuredTransportWithBasicAuth,
		Base64SHA256Password: onboarding.PasswordHash(password), InvalidUsernameAllowed: true, OrgId: orgA}, f.auth(t, pendingStationOfA))
	settings, err := f.engine.LookupChargeStationSettings(ctx, pendingStationOfA)
	require.NoError(t, err)
	assert.Equal(t, "***", settings.Settings["AuthorizationKey"].Value, "the settings keep no password")

	rr = serve(f.router, as(commandCaller(), http.MethodGet, "/cs/"+pendingStationOfA, ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var record api.ChargeStationRecord
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &record))
	assert.Equal(t, api.ChargeStationState("active"), record.State)
	assert.Nil(t, record.Onboarding)
	assert.Equal(t, &f.location.Id, record.LocationId)
	assert.Equal(t, ptr("Plaza 1"), record.DisplayName)
	require.NotNil(t, record.Evses)
	require.Len(t, *record.Evses, 1)
	assert.Equal(t, ptr("BE*AAA*ENEW16*1"), (*record.Evses)[0].EvseId, "made from the party of the location")
	assert.Len(t, (*record.Evses)[0].Connectors, 2)

	// and is asked for a new BootNotification
	sent = f.sentCalls()
	require.Len(t, sent, 2)
	assert.Equal(t, "TriggerMessage", sent[1].Action)

	assert.Equal(t, []string{events.TypeCommandCompleted, events.TypeLocationUpdated, events.TypeStationApproved},
		f.outbox(t, password))
	assert.Contains(t, logs.String(), "charge station approved", "the logs are captured")
	assert.NotContains(t, logs.String(), password, "the password is never logged")
}

func TestApproveSendsTheNewPasswordOfAnOcpp201Station(t *testing.T) {
	ctx := context.Background()
	f := newOnboardingFixture(t)
	f.connected.set(pending201StationOfA, false)

	rr := f.approve(pending201StationOfA, approvalBody(f.location.Id, ""))
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	commandId := *approvalResult(t, rr).Station.Onboarding.CommandId

	// queued until the station connects: stored with the password, never shown with it
	stored, err := f.engine.GetCommand(ctx, commandId)
	require.NoError(t, err)
	assert.Equal(t, store.CommandStatusQueued, stored.Status)
	var queued struct {
		SetVariableData []struct {
			AttributeValue string `json:"attributeValue"`
		} `json:"setVariableData"`
	}
	require.NoError(t, json.Unmarshal(stored.Payload, &queued))
	password := queued.SetVariableData[0].AttributeValue
	assert.Regexp(t, hexPassword, password)
	rr = serve(f.router, as(commandCaller(), http.MethodGet, "/commands/"+commandId, ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.NotContains(t, rr.Body.String(), password)
	assert.JSONEq(t, `{"setVariableData":[{"attributeValue":"***","component":{"name":"SecurityCtrlr"},"variable":{"name":"BasicAuthPassword"}}]}`,
		string(decodeCommand(t, rr).Payload))

	f.connected.set(pending201StationOfA, true)
	require.NoError(t, f.dispatcher.Kick(ctx, pending201StationOfA))
	sent := f.sentCalls()
	require.Len(t, sent, 1)
	assert.Equal(t, "SetVariables", sent[0].Action)
	assert.JSONEq(t, `{"setVariableData":[{"attributeValue":"`+password+`","component":{"name":"SecurityCtrlr"},"variable":{"name":"BasicAuthPassword"}}]}`,
		string(sent[0].RequestPayload))
	assert.Equal(t, onboarding.PasswordHash(password), f.auth(t, pending201StationOfA).PendingBase64SHA256Password)
	assert.Equal(t, store.TLSWithBasicAuth, f.station(t, pending201StationOfA).Onboarding.Approval.SecurityProfile,
		"the profile of the transport it connected with")
}

func TestApproveWithAPasswordSetByHand(t *testing.T) {
	ctx := context.Background()
	f := newOnboardingFixture(t)
	logs := captureLogs(t)

	rr := f.approve(unbootedStationOfA, approvalBody(f.location.Id, `,"password_mode":"manual","security_profile":"SP2","allow_any_token":true,"max_capacity_kw":22`))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	result := approvalResult(t, rr)
	require.NotNil(t, result.Password, "shown once")
	password := *result.Password
	assert.Regexp(t, hexPassword, password)
	assert.Equal(t, api.ChargeStationState("active"), result.Station.State)
	assert.Nil(t, result.Station.Onboarding)
	assert.True(t, result.Station.AllowAnyToken)
	assert.Equal(t, ptr(22.0), result.Station.MaxCapacityKw)

	assert.Equal(t, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, Base64SHA256Password: onboarding.PasswordHash(password),
		InvalidUsernameAllowed: true, OrgId: orgA}, f.auth(t, unbootedStationOfA), "only its hash is stored")
	assert.False(t, f.station(t, unbootedStationOfA).SharedCredentials)
	evses, err := f.engine.ListAssetEvsesForStation(ctx, unbootedStationOfA)
	require.NoError(t, err)
	assert.Len(t, evses, 1)
	assert.Empty(t, f.sentCalls(), "nothing is sent")

	rr = serve(f.router, as(commandCaller(), http.MethodGet, "/cs/"+unbootedStationOfA, ""))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.NotContains(t, rr.Body.String(), password)
	assert.NotContains(t, logs.String(), password)

	stored := f.takeEvents(t, password)
	require.Len(t, stored, 2)
	assert.Equal(t, []string{events.TypeLocationUpdated, events.TypeStationApproved}, []string{stored[0].Type, stored[1].Type})
	assert.JSONEq(t, `{"station_key":"`+unbootedStationOfA+`","station_id":"NOBOOT","location_id":"`+f.location.Id+`","evse_count":1,`+
		`"connector_count":2,"password_mode":"manual","shared_credentials":false,"actor":"user:test"}`, string(stored[1].Data))
	assert.Equal(t, "user:test", stored[1].Actor)
}

func TestApproveWithTheOnboardingPassword(t *testing.T) {
	keep := `,"password_mode":"keep_onboarding"`

	t.Run("refused without the onboarding password", func(t *testing.T) {
		f := newOnboardingFixture(t)
		// connected without a password
		f.setTenantConfig(t, orgA, `{"version":1,"onboarding_password_sha256":"`+onboardingPasswordOfA+`"}`)
		rr := f.approve(pendingStationOfA, approvalBody(f.location.Id, keep))
		require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), `"field":"password_mode"`)

		// the tenant has no onboarding password (any more)
		f.setTenantConfig(t, orgA, `{"version":2}`)
		rr = f.approve(pending201StationOfA, approvalBody(f.location.Id, keep))
		require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
		assert.Equal(t, store.StationStatePending, f.station(t, pending201StationOfA).State)
	})

	t.Run("approved with the shared password", func(t *testing.T) {
		f := newOnboardingFixture(t)
		f.setTenantConfig(t, orgA, `{"version":1,"onboarding_password_sha256":"`+onboardingPasswordOfA+`"}`)
		rr := f.approve(pending201StationOfA, approvalBody(f.location.Id, keep))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Nil(t, approvalResult(t, rr).Password)

		cs := f.station(t, pending201StationOfA)
		assert.Equal(t, store.StationStateActive, cs.State)
		assert.True(t, cs.SharedCredentials)
		assert.Equal(t, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, Base64SHA256Password: onboardingPasswordOfA,
			InvalidUsernameAllowed: true, OrgId: orgA}, f.auth(t, pending201StationOfA))
		assert.Empty(t, f.sentCalls())
		stored := f.takeEvents(t, "")
		require.Len(t, stored, 2)
		assert.Contains(t, string(stored[1].Data), `"password_mode":"keep_onboarding","shared_credentials":true`)
	})
}

// assertNothingApproved checks that the pending charge station is as it was.
func (f *onboardingFixture) assertNothingApproved(t *testing.T, csId string) {
	t.Helper()
	cs := f.station(t, csId)
	assert.Equal(t, store.StationStatePending, cs.State)
	assert.Equal(t, store.OnboardingAwaitingApproval, cs.Onboarding.Status)
	assert.Nil(t, cs.Onboarding.Approval)
	assert.Empty(t, f.auth(t, csId).PendingBase64SHA256Password)
	assert.Empty(t, f.auth(t, csId).Base64SHA256Password)
	evses, err := f.engine.ListAssetEvsesForStation(context.Background(), csId)
	require.NoError(t, err)
	assert.Empty(t, evses)
	_, total, err := f.engine.ListCommands(context.Background(), store.CommandFilter{StationKey: csId})
	require.NoError(t, err)
	assert.Zero(t, total)
}

func TestApproveRefusals(t *testing.T) {
	ctx := context.Background()

	t.Run("fields that break a rule", func(t *testing.T) {
		f := newOnboardingFixture(t)
		connectors := `"connectors":[{"ocpp_connector_id":1,"standard":"IEC_62196_T2","format":"SOCKET","power_type":"AC_3_PHASE","max_voltage":400,"max_amperage":32}]`
		for name, tc := range map[string]struct{ body, field string }{
			"location not a UUID":       {`{"location_id":"plaza","evses":[{"ocpp_evse_id":1,` + connectors + `}]}`, "location_id"},
			"unknown location":          {approvalBody("0193b2c5-0000-7000-8000-00000000ffff", ""), "location_id"},
			"location of another org":   {approvalBody(f.locationOfB.Id, ""), "location_id"},
			"no EVSE":                   {`{"location_id":"` + f.location.Id + `","evses":[]}`, "evses"},
			"an EVSE without connector": {`{"location_id":"` + f.location.Id + `","evses":[{"ocpp_evse_id":1,"connectors":[]}]}`, "evses[0].connectors"},
			"two EVSEs with one id":     {`{"location_id":"` + f.location.Id + `","evses":[{"ocpp_evse_id":1,` + connectors + `},{"ocpp_evse_id":1,` + connectors + `}]}`, "evses[1].ocpp_evse_id"},
			"an EVSE id of 0":           {`{"location_id":"` + f.location.Id + `","evses":[{"ocpp_evse_id":0,` + connectors + `}]}`, "evses[0].ocpp_evse_id"},
			"a bad eMI3 id":             {`{"location_id":"` + f.location.Id + `","evses":[{"ocpp_evse_id":1,"evse_id":"nope",` + connectors + `}]}`, "evses[0].evse_id"},
			"a bad connector":           {`{"location_id":"` + f.location.Id + `","evses":[{"ocpp_evse_id":1,"connectors":[{"ocpp_connector_id":1,"standard":"X","format":"SOCKET","power_type":"AC_3_PHASE","max_voltage":400,"max_amperage":32}]}]}`, "evses[0].connectors[0].standard"},
			"a bad tariff":              {`{"location_id":"` + f.location.Id + `","evses":[{"ocpp_evse_id":1,"connectors":[{"ocpp_connector_id":1,"standard":"IEC_62196_T2","format":"SOCKET","power_type":"AC_3_PHASE","max_voltage":400,"max_amperage":32,"tariff_ref":"t1"}]}]}`, "evses[0].connectors[0].tariff_ref"},
			"security profile 3":        {approvalBody(f.location.Id, `,"security_profile":"SP3"`), "security_profile"},
			"unknown password mode":     {approvalBody(f.location.Id, `,"password_mode":"auto"`), "password_mode"},
			"blank display name":        {strings.Replace(approvalBody(f.location.Id, ""), `"Plaza 1"`, `" "`, 1), "display_name"},
			"no capacity":               {approvalBody(f.location.Id, `,"max_capacity_kw":0`), "max_capacity_kw"},
		} {
			t.Run(name, func(t *testing.T) {
				rr := f.approve(pendingStationOfA, tc.body)
				require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
				assert.Contains(t, rr.Body.String(), `"field":"`+tc.field+`"`)
				f.assertNothingApproved(t, pendingStationOfA)
			})
		}

		rr := f.approve(pendingStationOfA, `{"location_id":"`+f.location.Id+`","evses":[{"ocpp_evse_id":1,"connectors":[]},{"ocpp_evse_id":1,`+connectors+`}]}`)
		require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"invalid","errors":[`+
			`{"field":"evses[0].connectors","reason":"must have at least one connector"},`+
			`{"field":"evses[1].ocpp_evse_id","reason":"1 is used by another EVSE of the charge station"}]}`, rr.Body.String())

		rr = f.approve(pendingStationOfA, `{"location_id":`)
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
	})

	t.Run("a tariff that is not a tariff of the org", func(t *testing.T) {
		f := newOnboardingFixture(t)
		deleted := f.createTariff(t, orgA, "")
		require.NoError(t, f.engine.DeleteTariff(context.Background(), deleted))
		body := func(tariffRef string) string {
			return `{"location_id":"` + f.location.Id + `","password_mode":"manual","evses":[{"ocpp_evse_id":1,"connectors":[` +
				`{"ocpp_connector_id":1,"standard":"IEC_62196_T2","format":"SOCKET","power_type":"AC_3_PHASE","max_voltage":400,"max_amperage":32},` +
				`{"ocpp_connector_id":2,"standard":"IEC_62196_T2","format":"SOCKET","power_type":"AC_3_PHASE","max_voltage":400,"max_amperage":32,` +
				`"adhoc_tariff_ref":"` + tariffRef + `"}]}]}`
		}
		for name, ref := range map[string]string{
			"a tariff that does not exist": "0193b2c6-0000-7000-8000-00000000ffff",
			"a tariff of another org":      f.createTariff(t, orgB, ""),
			"a deleted tariff":             deleted,
		} {
			rr := f.approve(pendingStationOfA, body(ref))
			require.Equal(t, http.StatusUnprocessableEntity, rr.Code, "%s: %s", name, rr.Body.String())
			assert.JSONEq(t, `{"status":"invalid","errors":[{"field":"evses[0].connectors[1].adhoc_tariff_ref","reason":"is not a tariff of the org"}]}`,
				rr.Body.String(), name)
			f.assertNothingApproved(t, pendingStationOfA)
		}

		tariff := f.createTariff(t, orgA, "")
		rr := f.approve(pendingStationOfA, body(tariff))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		evses, err := f.engine.ListAssetEvsesForStation(context.Background(), pendingStationOfA)
		require.NoError(t, err)
		require.Len(t, evses, 1)
		require.Len(t, evses[0].Connectors, 2)
		assert.Equal(t, &tariff, evses[0].Connectors[1].AdhocTariffRef)
	})

	t.Run("a charge station that is not pending", func(t *testing.T) {
		f := newOnboardingFixture(t)
		require.NoError(t, f.engine.SetChargeStationAuth(ctx, retiredStationOfA, &store.ChargeStationAuth{OrgId: orgA}))
		require.NoError(t, f.engine.DecommissionChargeStation(ctx, retiredStationOfA, nil))
		for _, csId := range []string{stationOfA, retiredStationOfA} {
			rr := f.approve(csId, approvalBody(f.location.Id, `,"password_mode":"manual"`))
			require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
			assert.Contains(t, rr.Body.String(), "invalid_state")
		}
		assert.Equal(t, store.StationStateActive, f.station(t, stationOfA).State)
	})

	t.Run("a charge station that the caller does not reach", func(t *testing.T) {
		f := newOnboardingFixture(t)
		for _, csId := range []string{stationOfB, "aaaa.UNKNOWN"} {
			rr := f.approve(csId, approvalBody(f.location.Id, ""))
			require.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())
			assert.JSONEq(t, notFoundBody, rr.Body.String())
		}
		rr := serve(f.router, as(callerOf(orgB, tenancy.OrgScopeSelf), http.MethodPost, "/cs/"+pendingStationOfA+"/approve", approvalBody(f.locationOfB.Id, "")))
		require.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())
		f.assertNothingApproved(t, pendingStationOfA)

		reader := callerOf(orgA, tenancy.OrgScopeSelf)
		reader.Perms = []string{tenancy.PermStationRead, tenancy.PermConnectorCommand}
		rr = serve(f.router, as(reader, http.MethodPost, "/cs/"+pendingStationOfA+"/approve", approvalBody(f.location.Id, "")))
		require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	})

	t.Run("the connector limit of the tenant", func(t *testing.T) {
		f := newOnboardingFixture(t)
		f.setEntitlements(t, orgA, connectorLimit(3, "hard"))
		f.reportConnectors(t, stationOfA, 0, 1, 2)
		// the connectors of a pending charge station do not count
		f.reportConnectors(t, pending201StationOfA, 1, 2, 3)

		rr := f.approve(pendingStationOfA, approvalBody(f.location.Id, ""))
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Conflict","error":"entitlement_exceeded: entitlement_limit_exceeded: cpms.connectors.max (limit 3, current 2, requested 2)"}`,
			rr.Body.String())
		f.assertNothingApproved(t, pendingStationOfA)

		f.setEntitlements(t, orgA, connectorLimit(4, "hard"))
		rr = f.approve(pendingStationOfA, approvalBody(f.location.Id, `,"password_mode":"manual"`))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	})

	t.Run("a new password for a charge station that never booted", func(t *testing.T) {
		f := newOnboardingFixture(t)
		rr := f.approve(unbootedStationOfA, approvalBody(f.location.Id, ""))
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), "station_version_unknown")
		f.assertNothingApproved(t, unbootedStationOfA)
	})

	t.Run("an EVSE id that is taken", func(t *testing.T) {
		f := newOnboardingFixture(t)
		other := &store.AssetEvse{OrgId: orgA, LocationId: f.location.Id, StationKey: stationOfA, OcppEvseId: 1, EvseId: ptr("BE*AAA*ENEW16*1")}
		require.NoError(t, f.engine.CreateAssetEvse(ctx, other))
		rr := f.approve(pendingStationOfA, approvalBody(f.location.Id, `,"password_mode":"manual"`))
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), "already_exists")
		f.assertNothingApproved(t, pendingStationOfA)
	})

	t.Run("a new password that cannot be queued", func(t *testing.T) {
		f := newOnboardingFixture(t)
		f.service.Store = failingCommandStore{f.engine}
		rr := f.approve(pendingStationOfA, approvalBody(f.location.Id, ""))
		require.Equal(t, http.StatusInternalServerError, rr.Code, rr.Body.String())
		f.assertNothingApproved(t, pendingStationOfA)
		assert.Empty(t, f.sentCalls())
	})
}

// failingCommandStore is a command store that cannot store a command.
type failingCommandStore struct {
	store.CommandStore
}

func (failingCommandStore) CreateCommand(context.Context, *store.Command) error {
	return errors.New("the command store is down")
}

func TestApproveAfterAFailedApproval(t *testing.T) {
	ctx := context.Background()
	f := newOnboardingFixture(t)
	rr := f.approve(pendingStationOfA, approvalBody(f.location.Id, ""))
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	sent := f.sentCalls()
	require.Len(t, sent, 1)
	require.True(t, f.dispatcher.ObserveCallResult(ctx, pendingStationOfA, answer(&store.Command{Id: sent[0].MessageId,
		Action: sent[0].Action, Payload: sent[0].RequestPayload}, `{"status":"NotSupported"}`)))

	rr = serve(f.router, as(commandCaller(), http.MethodGet, "/cs/"+pendingStationOfA, ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var record api.ChargeStationRecord
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &record))
	require.NotNil(t, record.Onboarding)
	assert.Equal(t, api.CredentialsFailed, record.Onboarding.Status)
	assert.Equal(t, ptr("NotSupported"), record.Onboarding.Error)

	// the operator sets the password by hand instead
	rr = f.approve(pendingStationOfA, approvalBody(f.location.Id, `,"password_mode":"manual"`))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	password := *approvalResult(t, rr).Password
	assert.Equal(t, &store.ChargeStationAuth{SecurityProfile: store.UnsecuredTransportWithBasicAuth, Base64SHA256Password: onboarding.PasswordHash(password),
		InvalidUsernameAllowed: true, OrgId: orgA}, f.auth(t, pendingStationOfA), "the pending password is gone")
	assert.Equal(t, []string{events.TypeCommandCompleted, events.TypeStationApprovalFailed, events.TypeLocationUpdated, events.TypeStationApproved},
		f.outbox(t, password))
}

func TestPendingStationsShowTheirOnboarding(t *testing.T) {
	f := newOnboardingFixture(t)
	rr := serve(f.router, as(commandCaller(), http.MethodGet, "/cs?state=pending", ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var page api.ChargeStationPage
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &page))
	require.Len(t, page.Items, 3)
	byKey := map[string]api.ChargeStationRecord{}
	for _, item := range page.Items {
		byKey[item.CsId] = item
	}
	assert.Equal(t, &api.StationOnboarding{Status: api.AwaitingApproval, FirstSeenAt: f.clock.Now().UTC(), LastSeenAt: f.clock.Now().UTC(),
		Transport: api.Wss, Credential: api.OnboardingPassword}, byKey[pending201StationOfA].Onboarding)
	assert.Equal(t, api.Ws, byKey[pendingStationOfA].Onboarding.Transport)

	rr = serve(f.router, as(commandCaller(), http.MethodGet, "/cs/"+stationOfA, ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.NotContains(t, rr.Body.String(), `"onboarding"`, "only pending charge stations have one")
}

func TestRejectChargeStation(t *testing.T) {
	ctx := context.Background()
	reject := func(f *onboardingFixture, csId, body string) *httptest.ResponseRecorder {
		return serve(f.router, as(commandCaller(), http.MethodPost, "/cs/"+csId+"/reject", body))
	}

	t.Run("a pending charge station", func(t *testing.T) {
		f := newOnboardingFixture(t)
		rr := reject(f, pendingStationOfA, `{"reason":"not one of ours"}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var record api.ChargeStationRecord
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &record))
		assert.Equal(t, api.ChargeStationState("decommissioned"), record.State)
		assert.Equal(t, ptr("rejected: not one of ours"), record.StateReason)
		assert.Nil(t, record.Onboarding)

		stored := f.takeEvents(t, "")
		require.Len(t, stored, 1)
		assert.Equal(t, events.TypeStationRejected, stored[0].Type)
		assert.Equal(t, orgA, stored[0].OrgId)
		assert.Equal(t, "user:test", stored[0].Actor)
		assert.JSONEq(t, `{"station_key":"`+pendingStationOfA+`","station_id":"NEW16","reason":"not one of ours","actor":"user:test"}`, string(stored[0].Data))

		// it is gone: approved, rejected and registered again it is not
		rr = reject(f, pendingStationOfA, "")
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), "invalid_state")
		rr = f.approve(pendingStationOfA, approvalBody(f.location.Id, `,"password_mode":"manual"`))
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		rr = serve(f.router, as(commandCaller(), http.MethodPost, "/cs/"+pendingStationOfA, `{"securityProfile":1,"base64SHA256Password":"DEADBEEF"}`))
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), "station_decommissioned")

		rr = reject(f, pending201StationOfA, "")
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, ptr("rejected"), f.station(t, pending201StationOfA).StateReason)
	})

	t.Run("while its new password waits to be sent", func(t *testing.T) {
		f := newOnboardingFixture(t)
		f.connected.set(pendingStationOfA, false)
		rr := f.approve(pendingStationOfA, approvalBody(f.location.Id, ""))
		require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
		commandId := *approvalResult(t, rr).Station.Onboarding.CommandId

		rr = reject(f, pendingStationOfA, "")
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		cmd, err := f.engine.GetCommand(ctx, commandId)
		require.NoError(t, err)
		assert.Equal(t, store.CommandStatusCancelled, cmd.Status, "the password is not sent")
		assert.Empty(t, f.auth(t, pendingStationOfA).PendingBase64SHA256Password, "nor accepted")
		assert.Equal(t, store.StationStateDecommissioned, f.station(t, pendingStationOfA).State)
	})

	t.Run("refusals", func(t *testing.T) {
		f := newOnboardingFixture(t)
		rr := reject(f, stationOfA, "")
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.Equal(t, store.StationStateActive, f.station(t, stationOfA).State)
		for _, csId := range []string{stationOfB, "aaaa.UNKNOWN"} {
			rr = reject(f, csId, "")
			require.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())
		}
		rr = reject(f, pendingStationOfA, `{"reason":"`+strings.Repeat("x", 256)+`"}`)
		require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
		rr = reject(f, pendingStationOfA, `{"reason":`)
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
		assert.Equal(t, store.StationStatePending, f.station(t, pendingStationOfA).State)
		assert.Empty(t, f.takeEvents(t, ""))
	})
}
