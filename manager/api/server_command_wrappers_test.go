// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	ocpp16types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/sync"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"go.opentelemetry.io/otel/trace/noop"
	clockTest "k8s.io/utils/clock/testing"
)

const (
	// stationOfA201 is an OCPP 2.0.1 charge station of A that booted and is connected.
	stationOfA201 = "aaaa.CP201"
	// offlineStationOfA is an OCPP 1.6 charge station of A that booted and is not
	// connected.
	offlineStationOfA = "aaaa.CP003"
)

// newWrappersFixture is the fixture of the command API with two more charge stations
// of A: an OCPP 2.0.1 station and an OCPP 1.6 station that is not connected.
func newWrappersFixture(t *testing.T) *commandsFixture {
	t.Helper()
	ctx := context.Background()
	f := newCommandsFixture(t)
	for station, version := range map[string]string{stationOfA201: "2.0.1", offlineStationOfA: "1.6"} {
		require.NoError(t, f.engine.SetChargeStationAuth(ctx, station, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgA}))
		require.NoError(t, f.engine.SetChargeStationRuntimeDetails(ctx, station, &store.ChargeStationRuntimeDetails{OcppVersion: version}))
	}
	f.connected.set(stationOfA201, true)
	return f
}

// call sends a request of the caller to the API of the fixture.
func (f *commandsFixture) call(p *tenancy.Principal, method, path, body string) *httptest.ResponseRecorder {
	return serve(f.router, as(p, method, path, body))
}

// sentCalls returns the calls that were sent to the charge stations, without the
// answers of the OCPP router.
func (f *commandsFixture) sentCalls() []*transport.Message {
	var calls []*transport.Message
	for _, msg := range f.calls.sent() {
		if msg.MessageType == transport.MessageTypeCall {
			calls = append(calls, msg)
		}
	}
	return calls
}

// router16 is the OCPP 1.6 router of the manager with the dispatcher of the fixture as
// its observer; it answers the charge stations through the emitter of the fixture.
func (f *commandsFixture) router16() transport.MessageHandler {
	return ocpp16.NewRouter(f.calls, f.clock, f.engine, events.NopEmitter{}, f.scoper.Resolver,
		nil, nil, nil, nil, time.Minute, schemas.OcppSchemas, f.dispatcher, nil, nil, nil, nil)
}

// answer is the CallResult of the charge station to the command.
func answer(cmd *store.Command, response string) *transport.Message {
	return &transport.Message{
		MessageType:     transport.MessageTypeCallResult,
		Action:          cmd.Action,
		MessageId:       cmd.Id,
		RequestPayload:  cmd.Payload,
		ResponsePayload: json.RawMessage(response),
	}
}

// wrapperCase is an operation that sends one command to an OCPP 1.6 charge station.
type wrapperCase struct {
	name   string
	method string
	path   string // %s is the charge station
	body   string
	// code is the status code of the answer, for every charge station
	code int
	// action and payload are the command that an OCPP 1.6 charge station gets
	action  string
	payload string
	// written reads what the operation writes to the store for other charge stations
	written func(t *testing.T, engine store.Engine, csId string) any
	// prepare (may be nil) puts what the operation needs into the store first
	prepare func(t *testing.T, engine store.Engine, csId string)
}

// lookup reads a request of a charge station from a store of requests.
func lookup[T any](read func(store.Engine, context.Context, string) (T, error)) func(*testing.T, store.Engine, string) any {
	return func(t *testing.T, engine store.Engine, csId string) any {
		t.Helper()
		got, err := read(engine, context.Background(), csId)
		require.NoError(t, err)
		return got
	}
}

var wrapperCases = []wrapperCase{
	{"reset", http.MethodPost, "/cs/%s/reset", `{"type":"Hard"}`, http.StatusAccepted,
		"Reset", `{"type":"Hard"}`, lookup(store.Engine.GetResetRequest), nil},
	{"unlock connector", http.MethodPost, "/cs/%s/unlock-connector", `{"connectorId":2}`, http.StatusAccepted,
		"UnlockConnector", `{"connectorId":2}`, lookup(store.Engine.GetUnlockConnectorRequest), nil},
	{"change availability of a connector", http.MethodPost, "/cs/%s/availability", `{"connectorId":1,"evseId":7,"type":"Inoperative"}`, http.StatusAccepted,
		"ChangeAvailability", `{"connectorId":1,"type":"Inoperative"}`, lookup(store.Engine.LookupChargeStationChangeAvailability), nil},
	{"change availability of the charge station", http.MethodPost, "/cs/%s/availability", `{"type":"Operative"}`, http.StatusAccepted,
		"ChangeAvailability", `{"connectorId":0,"type":"Operative"}`, lookup(store.Engine.LookupChargeStationChangeAvailability), nil},
	{"clear cache", http.MethodPost, "/cs/%s/cache/clear", ``, http.StatusAccepted,
		"ClearCache", `{}`, lookup(store.Engine.LookupChargeStationClearCache), nil},
	{"data transfer", http.MethodPost, "/cs/%s/data-transfer", `{"vendorId":"com.example","messageId":"hello","data":"world"}`, http.StatusAccepted,
		"DataTransfer", `{"vendorId":"com.example","messageId":"hello","data":"world"}`, lookup(store.Engine.LookupChargeStationDataTransfer), nil},
	{"data transfer with the vendor only", http.MethodPost, "/cs/%s/data-transfer", `{"vendorId":"com.example"}`, http.StatusAccepted,
		"DataTransfer", `{"vendorId":"com.example"}`, lookup(store.Engine.LookupChargeStationDataTransfer), nil},
	{"remote start", http.MethodPost, "/cs/%s/start-transaction", `{"idTag":"DEADBEEF","connectorId":1}`, http.StatusAccepted,
		"RemoteStartTransaction", `{"idTag":"DEADBEEF","connectorId":1}`, lookup(store.Engine.GetRemoteStartTransactionRequest), nil},
	{"remote start with a charging profile", http.MethodPost, "/cs/%s/start-transaction",
		`{"idTag":"DEADBEEF","chargingProfile":{"chargingProfileId":5,"stackLevel":0,"chargingProfilePurpose":"TxProfile","chargingProfileKind":"Absolute","chargingSchedule":{"chargingRateUnit":"A","chargingSchedulePeriod":[{"startPeriod":0,"limit":16}]}}}`,
		http.StatusAccepted, "RemoteStartTransaction",
		`{"idTag":"DEADBEEF","chargingProfile":{"chargingProfileId":5,"stackLevel":0,"chargingProfilePurpose":"TxProfile","chargingProfileKind":"Absolute","chargingSchedule":{"chargingRateUnit":"A","chargingSchedulePeriod":[{"startPeriod":0,"limit":16}]}}}`,
		lookup(store.Engine.GetRemoteStartTransactionRequest), nil},
	{"remote stop", http.MethodPost, "/cs/%s/stop-transaction", `{"transactionId":"123"}`, http.StatusAccepted,
		"RemoteStopTransaction", `{"transactionId":123}`, lookup(store.Engine.GetRemoteStopTransactionRequest), nil},
	{"trigger", http.MethodPost, "/cs/%s/trigger", `{"trigger":"StatusNotification","connectorId":1}`, http.StatusCreated,
		"TriggerMessage", `{"requestedMessage":"StatusNotification","connectorId":1}`, lookup(store.Engine.LookupChargeStationTriggerMessage), nil},
}

func TestCommandOperationsQueueACommandForAnOcpp16ChargeStation(t *testing.T) {
	for _, tc := range allWrapperCases() {
		t.Run(tc.name, func(t *testing.T) {
			f := newWrappersFixture(t)
			now := f.clock.Now()
			if tc.prepare != nil {
				tc.prepare(t, f.engine, stationOfA)
			}

			rr := f.call(commandCaller(), tc.method, fmt.Sprintf(tc.path, stationOfA), tc.body)
			require.Equal(t, tc.code, rr.Code, rr.Body.String())

			// sent at once to the connected charge station, the command id as message id
			calls := f.sentCalls()
			require.Len(t, calls, 1)
			sent := calls[0]
			assert.Equal(t, tc.action, sent.Action)
			assert.JSONEq(t, tc.payload, string(sent.RequestPayload))
			assert.Equal(t, "/api/v0/commands/"+sent.MessageId, rr.Header().Get("Location"))

			if tc.action == "DataTransfer" {
				// the status is what the charge station answers: not known yet
				assert.JSONEq(t, `{"commandId":"`+sent.MessageId+`"}`, rr.Body.String())
			} else {
				got := decodeCommand(t, rr)
				assert.Equal(t, sent.MessageId, got.Id.String())
				assert.Equal(t, "CP001", got.ChargeStationId)
				assert.Equal(t, tc.action, got.Action)
				assert.JSONEq(t, tc.payload, string(got.Payload))
				assert.Equal(t, api.CommandStatusSent, got.Status)
				assert.Equal(t, now, got.CreatedAt)
			}

			// one command, in the org of the charge station, with who asked for it
			assert.Equal(t, 1, f.storedCommands(t))
			stored := f.stored(t, sent.MessageId)
			assert.Equal(t, orgA, stored.OrgId)
			assert.Equal(t, stationOfA, stored.StationKey)
			assert.Equal(t, "ocpp1.6", stored.OcppVersion)
			assert.Equal(t, store.CommandStatusSent, stored.Status)
			require.NotNil(t, stored.Actor)
			assert.Equal(t, "user:test", *stored.Actor)
			require.NotNil(t, stored.Jti)
			assert.Equal(t, "jti-a", *stored.Jti)

			// the store of the requests of the operation is not written
			assert.Nil(t, tc.written(t, f.engine, stationOfA))
		})
	}
}

func TestCommandOperationsKeepTheirStoresForOtherChargeStations(t *testing.T) {
	for _, tc := range allWrapperCases() {
		for _, run := range []struct {
			name    string
			csId    string
			noQueue bool
		}{
			{name: "OCPP 2.0.1 charge station", csId: stationOfA201},
			{name: "charge station that never booted", csId: stationOfAWithoutBoot},
			{name: "manager without a command queue (Firestore)", csId: stationOfA, noQueue: true},
		} {
			t.Run(tc.name+"/"+run.name, func(t *testing.T) {
				f := newWrappersFixture(t)
				router := f.router
				if run.noQueue {
					router = f.multiTenant.router(t)
				}
				if tc.prepare != nil {
					tc.prepare(t, f.engine, run.csId)
				}
				code := tc.code
				if run.csId == stationOfAWithoutBoot && strings.Contains(tc.path, "transaction") {
					// remote start and stop need a charge station that booted, as before
					code = http.StatusNotFound
				}

				rr := serve(router, as(commandCaller(), tc.method, fmt.Sprintf(tc.path, run.csId), tc.body))
				require.Equal(t, code, rr.Code, rr.Body.String())
				assert.Empty(t, rr.Header().Get("Location"))
				if code != http.StatusNotFound {
					assert.NotNil(t, tc.written(t, f.engine, run.csId), "the request is in the store as before")
				}
				assert.Zero(t, f.storedCommands(t))
				assert.Empty(t, f.sentCalls())
			})
		}
	}
}

func TestCommandOperationsRefuseWhatAnOcpp16ChargeStationCannotGet(t *testing.T) {
	f := newWrappersFixture(t)

	for _, tc := range []struct{ name, path, body string }{
		{"a reset type that OCPP 1.6 does not have", "/cs/%s/reset", `{"type":"Warm"}`},
		{"an availability that OCPP 1.6 does not have", "/cs/%s/availability", `{"type":"Broken"}`},
		{"a vendor id that is too long", "/cs/%s/data-transfer", `{"vendorId":"` + strings.Repeat("v", 256) + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := f.call(commandCaller(), http.MethodPost, fmt.Sprintf(tc.path, stationOfA), tc.body)
			require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
			assert.Contains(t, rr.Body.String(), "invalid_payload: ")
		})
	}
	assert.Zero(t, f.storedCommands(t))
	assert.Empty(t, f.sentCalls())
}

func TestRemoteStopTransactionTakesBothFormsOfTheTransactionId(t *testing.T) {
	f := newWrappersFixture(t)

	for _, tc := range []struct {
		transactionId string
		want          int
	}{
		{"123", 123},
		{ocpp16types.ConvertToUUID(123), 123},
		{ocpp16types.ConvertToUUID(2147483647), 2147483647},
		{"0", 0},
	} {
		rr := f.call(commandCaller(), http.MethodPost, "/cs/"+stationOfA+"/stop-transaction", `{"transactionId":"`+tc.transactionId+`"}`)
		require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
		got := decodeCommand(t, rr)
		assert.JSONEq(t, fmt.Sprintf(`{"transactionId":%d}`, tc.want), string(got.Payload), tc.transactionId)
		// the next command is sent when the charge station answers this one
		f.dispatcher.ObserveCallResult(context.Background(), stationOfA, answer(f.stored(t, got.Id.String()), `{"status":"Accepted"}`))
	}
	commandsBefore := f.storedCommands(t)

	for _, transactionId := range []string{"abc", uuid.NewString(), "12.5", ""} {
		rr := f.call(commandCaller(), http.MethodPost, "/cs/"+stationOfA+"/stop-transaction", `{"transactionId":"`+transactionId+`"}`)
		require.Equal(t, http.StatusBadRequest, rr.Code, transactionId)
		assert.Contains(t, rr.Body.String(), "transactionId", transactionId)
	}
	assert.Equal(t, commandsBefore, f.storedCommands(t), "nothing queued for an id that is neither form")
	stop, err := f.engine.GetRemoteStopTransactionRequest(context.Background(), stationOfA)
	require.NoError(t, err)
	assert.Nil(t, stop)
}

func TestTriggerOfAnOcpp16ChargeStation(t *testing.T) {
	t.Run("an OCPP 1.6 message is a TriggerMessage", func(t *testing.T) {
		f := newWrappersFixture(t)
		rr := f.call(commandCaller(), http.MethodPost, "/cs/"+stationOfA+"/trigger", `{"trigger":"DiagnosticsStatusNotification"}`)
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		got := decodeCommand(t, rr)
		assert.Equal(t, "TriggerMessage", got.Action)
		assert.JSONEq(t, `{"requestedMessage":"DiagnosticsStatusNotification"}`, string(got.Payload))
	})

	t.Run("another message is a Plug & Charge DataTransfer as the trigger sync sends it", func(t *testing.T) {
		for _, tc := range []struct{ body, trigger string }{
			{`{"trigger":"SignV2GCertificate","connectorId":1}`, "SignV2GCertificate"},
			{`{"trigger":"SignChargingStationCertificate"}`, "SignChargingStationCertificate"},
		} {
			f := newWrappersFixture(t)
			var req api.ChargeStationTrigger
			require.NoError(t, json.Unmarshal([]byte(tc.body), &req))

			rr := f.call(commandCaller(), http.MethodPost, "/cs/"+stationOfA+"/trigger", tc.body)
			require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
			got := decodeCommand(t, rr)
			assert.Equal(t, "DataTransfer", got.Action)

			// what the trigger sync sends for the same trigger
			assert.JSONEq(t, string(syncedTrigger(t, f, req)), string(got.Payload), tc.trigger)
			calls := f.sentCalls()
			require.Len(t, calls, 1)
			assert.JSONEq(t, string(syncedTrigger(t, f, req)), string(calls[0].RequestPayload), tc.trigger)
		}
	})
}

// syncedTrigger returns the payload of the call that the trigger sync sends to an OCPP
// 1.6 charge station for a trigger message in the store.
func syncedTrigger(t *testing.T, f *commandsFixture, req api.ChargeStationTrigger) json.RawMessage {
	t.Helper()
	ctx := context.Background()
	engine := inmemory.NewStore(f.clock)
	require.NoError(t, engine.SetChargeStationRuntimeDetails(ctx, "cs", &store.ChargeStationRuntimeDetails{OcppVersion: "1.6"}))
	require.NoError(t, engine.SetChargeStationTriggerMessage(ctx, "cs", &store.ChargeStationTriggerMessage{
		ChargeStationId: "cs",
		TriggerMessage:  store.TriggerMessage(req.Trigger),
		ConnectorId:     req.ConnectorId,
		TriggerStatus:   store.TriggerStatusPending,
		SendAfter:       f.clock.Now().Add(-time.Minute),
	}))
	emitted := &sentCalls{}
	sync.SyncTriggersOnce(ctx, noop.NewTracerProvider().Tracer(""), engine, f.clock,
		ocpp16.NewCallMaker(emitted), ocpp16.NewDataTransferCallMaker(emitted), ocpp16.NewCallMaker(emitted), "", time.Minute)
	sent := emitted.sent()
	require.Len(t, sent, 1)
	require.Equal(t, "DataTransfer", sent[0].Action)
	return sent[0].RequestPayload
}

func TestChangeConfigurationQueuesOneCommandPerKey(t *testing.T) {
	ctx := context.Background()
	f := newWrappersFixture(t)
	router := f.router16()

	rr := f.call(commandCaller(), http.MethodPatch, "/cs/"+stationOfA+"/configuration",
		`{"MeterValueSampleInterval":"120","HeartbeatInterval":"600"}`)
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	assert.Empty(t, rr.Header().Get("Location"), "more than one command")
	var got api.ConfigurationChangeResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))

	// one command per key, in the order of the keys
	require.Len(t, got.Results, 2)
	require.Equal(t, 2, f.storedCommands(t))
	for i, want := range []struct{ key, payload string }{
		{"HeartbeatInterval", `{"key":"HeartbeatInterval","value":"600"}`},
		{"MeterValueSampleInterval", `{"key":"MeterValueSampleInterval","value":"120"}`},
	} {
		result := got.Results[i]
		assert.Equal(t, want.key, result.Key)
		assert.Equal(t, api.ConfigurationChangeResultStatusPending, result.Status)
		require.NotNil(t, result.CommandId)
		cmd := f.stored(t, result.CommandId.String())
		assert.Equal(t, "ChangeConfiguration", cmd.Action)
		assert.JSONEq(t, want.payload, string(cmd.Payload))
		require.NotNil(t, cmd.Actor)
		assert.Equal(t, "user:test", *cmd.Actor)
	}

	// nothing for the settings sync: the change is not sent a second time
	settings, err := f.engine.LookupChargeStationSettings(ctx, stationOfA)
	require.NoError(t, err)
	assert.Nil(t, settings)
	// past the time after which the sync would send a pending setting
	f.clock.SetTime(f.clock.Now().Add(time.Minute))
	synced := &sentCalls{}
	sync.SyncSettingsOnce(ctx, f.engine, f.clock, ocpp16.NewCallMaker(synced), ocpp16.NewCallMaker(synced), "", time.Minute)
	assert.Empty(t, synced.sent())

	// the charge station answers each command when it gets it: one at a time
	first := f.stored(t, got.Results[0].CommandId.String())
	require.Len(t, f.sentCalls(), 1)
	assert.Equal(t, first.Id, f.sentCalls()[0].MessageId)
	router.Handle(ctx, stationOfA, answer(first, `{"status":"Accepted"}`))
	require.Eventually(t, func() bool { return len(f.sentCalls()) == 2 }, time.Second, 10*time.Millisecond)
	second := f.stored(t, got.Results[1].CommandId.String())
	assert.Equal(t, second.Id, f.sentCalls()[1].MessageId)
	router.Handle(ctx, stationOfA, &transport.Message{MessageType: transport.MessageTypeCallError, Action: "ChangeConfiguration",
		MessageId: second.Id, RequestPayload: second.Payload, ErrorCode: transport.ErrorNotSupported, ErrorDescription: "no"})

	assert.Equal(t, store.CommandStatusAccepted, f.stored(t, first.Id).Status)
	assert.Equal(t, store.CommandStatusFailed, f.stored(t, second.Id).Status)
	// the result handler keeps the outcome of each key, and the sync still sends nothing
	settings, err = f.engine.LookupChargeStationSettings(ctx, stationOfA)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, store.ChargeStationSettingStatusAccepted, settings.Settings["HeartbeatInterval"].Status)
	assert.Equal(t, "600", settings.Settings["HeartbeatInterval"].Value)
	assert.Equal(t, store.ChargeStationSettingStatusRejected, settings.Settings["MeterValueSampleInterval"].Status)
	sync.SyncSettingsOnce(ctx, f.engine, f.clock, ocpp16.NewCallMaker(synced), ocpp16.NewCallMaker(synced), "", time.Minute)
	assert.Empty(t, synced.sent())
	assert.Len(t, f.sentCalls(), 2, "each key was sent once")
}

func TestChangeConfigurationQueuesNoKeyWhenOneCannotBeSent(t *testing.T) {
	f := newWrappersFixture(t)

	rr := f.call(commandCaller(), http.MethodPatch, "/cs/"+stationOfA+"/configuration",
		`{"HeartbeatInterval":"600","TooLong":"`+strings.Repeat("x", 501)+`"}`)
	require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), `invalid_payload: key \"TooLong\"`)
	assert.Zero(t, f.storedCommands(t))
	assert.Empty(t, f.sentCalls())
	settings, err := f.engine.LookupChargeStationSettings(context.Background(), stationOfA)
	require.NoError(t, err)
	assert.Nil(t, settings)
}

func TestChangeConfigurationKeepsThePermissionOfTheOperation(t *testing.T) {
	f := newWrappersFixture(t)
	body := `{"HeartbeatInterval":"600"}`

	// the permission of the operation is enough: no command permission is needed
	rr := f.call(withPerms(commandCaller(), tenancy.PermStationWrite), http.MethodPatch, "/cs/"+stationOfA+"/configuration", body)
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	assert.Equal(t, 1, f.storedCommands(t))

	rr = f.call(withPerms(commandCaller(), tenancy.PermConnectorCommand), http.MethodPatch, "/cs/"+stationOfA+"/configuration", body)
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	assert.Equal(t, 1, f.storedCommands(t))
}

func TestChangeConfigurationOfOtherChargeStationsKeepsThePendingSettings(t *testing.T) {
	ctx := context.Background()
	for _, run := range []struct {
		name    string
		csId    string
		noQueue bool
	}{
		{name: "OCPP 2.0.1 charge station", csId: stationOfA201},
		{name: "manager without a command queue (Firestore)", csId: stationOfA, noQueue: true},
	} {
		t.Run(run.name, func(t *testing.T) {
			f := newWrappersFixture(t)
			router := f.router
			if run.noQueue {
				router = f.multiTenant.router(t)
			}

			rr := serve(router, as(commandCaller(), http.MethodPatch, "/cs/"+run.csId+"/configuration", `{"HeartbeatInterval":"600"}`))
			require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
			assert.JSONEq(t, `{"results":[{"key":"HeartbeatInterval","status":"Accepted"}]}`, rr.Body.String())
			settings, err := f.engine.LookupChargeStationSettings(ctx, run.csId)
			require.NoError(t, err)
			require.NotNil(t, settings)
			assert.Equal(t, store.ChargeStationSettingStatusPending, settings.Settings["HeartbeatInterval"].Status)
			assert.Zero(t, f.storedCommands(t))
		})
	}
}

func TestRefreshConfigurationAsksAnOcpp16ChargeStation(t *testing.T) {
	ctx := context.Background()
	f := newWrappersFixture(t)
	router := f.router16()

	rr := f.call(commandCaller(), http.MethodGet, "/cs/"+stationOfA+"/configuration?refresh=true&key=HeartbeatInterval,%20MeterValueSampleInterval", "")
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	got := decodeCommand(t, rr)
	assert.Equal(t, "/api/v0/commands/"+got.Id.String(), rr.Header().Get("Location"))
	assert.Equal(t, "GetConfiguration", got.Action)
	assert.JSONEq(t, `{"key":["HeartbeatInterval","MeterValueSampleInterval"]}`, string(got.Payload))
	assert.Equal(t, api.CommandStatusSent, got.Status)

	// the values that the charge station answers are what the manager knows
	router.Handle(ctx, stationOfA, answer(f.stored(t, got.Id.String()),
		`{"configurationKey":[{"key":"HeartbeatInterval","readonly":false,"value":"300"}],"unknownKey":["MeterValueSampleInterval"]}`))
	assert.Equal(t, store.CommandStatusAccepted, f.stored(t, got.Id.String()).Status)
	rr = f.call(commandCaller(), http.MethodGet, "/cs/"+stationOfA+"/configuration", "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), `"value":"300"`)

	t.Run("all keys", func(t *testing.T) {
		rr := f.call(commandCaller(), http.MethodGet, "/cs/"+stationOfA+"/configuration?refresh=true", "")
		require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{}`, string(decodeCommand(t, rr).Payload))
	})

	t.Run("refreshing needs the command permission", func(t *testing.T) {
		before := f.storedCommands(t)
		rr := f.call(withPerms(commandCaller(), tenancy.PermStationRead), http.MethodGet, "/cs/"+stationOfA+"/configuration?refresh=true", "")
		require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), "missing permission cpms.connector.command")
		assert.Equal(t, before, f.storedCommands(t))

		// reading what the manager knows does not
		rr = f.call(withPerms(commandCaller(), tenancy.PermStationRead), http.MethodGet, "/cs/"+stationOfA+"/configuration", "")
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	})
}

func TestRefreshConfigurationOfOtherChargeStationsAnswersWhatTheManagerKnows(t *testing.T) {
	ctx := context.Background()
	for _, run := range []struct {
		name    string
		csId    string
		noQueue bool
	}{
		{name: "OCPP 2.0.1 charge station", csId: stationOfA201},
		{name: "manager without a command queue (Firestore)", csId: stationOfA, noQueue: true},
	} {
		t.Run(run.name, func(t *testing.T) {
			f := newWrappersFixture(t)
			router := f.router
			if run.noQueue {
				router = f.multiTenant.router(t)
			}
			require.NoError(t, f.engine.UpdateChargeStationSettings(ctx, run.csId, &store.ChargeStationSettings{
				Settings: map[string]*store.ChargeStationSetting{"HeartbeatInterval": {Value: "600", Status: store.ChargeStationSettingStatusAccepted}},
			}))

			rr := serve(router, as(commandCaller(), http.MethodGet, "/cs/"+run.csId+"/configuration?refresh=true", ""))
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			assert.Contains(t, rr.Body.String(), `"value":"600"`)
			assert.Zero(t, f.storedCommands(t))
		})
	}
}

func TestRemoteStartOfAnOfflineChargeStationIsSentWhenItConnects(t *testing.T) {
	f := newWrappersFixture(t)

	rr := f.call(commandCaller(), http.MethodPost, "/cs/"+offlineStationOfA+"/start-transaction", `{"idTag":"DEADBEEF","connectorId":1}`)
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	got := decodeCommand(t, rr)
	assert.Equal(t, api.CommandStatusQueued, got.Status)
	assert.Empty(t, f.sentCalls(), "nothing is sent to a charge station that is not connected")
	// the request is not refused: it waits for the charge station within its expiry
	assert.Equal(t, f.clock.Now().Add(5*time.Minute), got.ExpiresAt)

	f.connected.set(offlineStationOfA, true)
	f.dispatcher.OnStationConnected(context.Background(), offlineStationOfA)
	f.dispatcher.Clock.(*clockTest.FakeClock).Step(time.Second)

	require.Eventually(t, func() bool { return len(f.sentCalls()) == 1 }, time.Second, 10*time.Millisecond)
	sent := f.sentCalls()[0]
	assert.Equal(t, got.Id.String(), sent.MessageId)
	assert.Equal(t, "RemoteStartTransaction", sent.Action)
	assert.JSONEq(t, `{"idTag":"DEADBEEF","connectorId":1}`, string(sent.RequestPayload))
	assert.Equal(t, store.CommandStatusSent, f.stored(t, got.Id.String()).Status)
}

func TestStationMessagesWhileARemoteStartWaitsForItsAnswerAreHandledAsBefore(t *testing.T) {
	ctx := context.Background()
	f := newWrappersFixture(t)
	router := f.router16()

	rr := f.call(commandCaller(), http.MethodPost, "/cs/"+stationOfA+"/start-transaction", `{"idTag":"DEADBEEF","connectorId":1}`)
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	cmd := f.stored(t, decodeCommand(t, rr).Id.String())
	require.Equal(t, store.CommandStatusSent, cmd.Status)

	router.Handle(ctx, stationOfA, &transport.Message{MessageType: transport.MessageTypeCall,
		Action: "Heartbeat", MessageId: "hb-1", RequestPayload: json.RawMessage(`{}`)})
	router.Handle(ctx, stationOfA, &transport.Message{MessageType: transport.MessageTypeCall,
		Action: "StatusNotification", MessageId: "sn-1",
		RequestPayload: json.RawMessage(`{"connectorId":1,"errorCode":"NoError","status":"Preparing"}`)})

	var answers []*transport.Message
	for _, msg := range f.calls.sent() {
		if msg.MessageType != transport.MessageTypeCall {
			answers = append(answers, msg)
		}
	}
	require.Len(t, answers, 2)
	assert.Equal(t, transport.MessageTypeCallResult, answers[0].MessageType)
	assert.Equal(t, "hb-1", answers[0].MessageId)
	assert.Equal(t, transport.MessageTypeCallResult, answers[1].MessageType)
	assert.Equal(t, "sn-1", answers[1].MessageId)
	status, err := f.engine.GetConnectorStatus(ctx, stationOfA, 1)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, store.ConnectorStatusPreparing, status.Status)
	assert.Equal(t, store.CommandStatusSent, f.stored(t, cmd.Id).Status, "the command still waits for its answer")

	router.Handle(ctx, stationOfA, answer(cmd, `{"status":"Accepted"}`))
	assert.Equal(t, store.CommandStatusAccepted, f.stored(t, cmd.Id).Status)
}

// allWrapperCases are all the operations that send one command to an OCPP 1.6 charge
// station and answer the command.
func allWrapperCases() []wrapperCase {
	return append(append([]wrapperCase{}, wrapperCases...), remainingWrapperCases...)
}

// fullChargingProfile is a request of a charging profile with every field of OCPP 1.6;
// the times have offsets and no fractions, so OCPP sends them as they are.
const fullChargingProfile = `{"connectorId":1,"csChargingProfiles":{"chargingProfileId":5,"transactionId":123,` +
	`"stackLevel":2,"chargingProfilePurpose":"TxProfile","chargingProfileKind":"Recurring","recurrencyKind":"Daily",` +
	`"validFrom":"2026-01-01T00:00:00Z","validTo":"2026-02-01T00:00:00+01:00","chargingSchedule":{"duration":3600,` +
	`"startSchedule":"2026-01-02T00:00:00Z","chargingRateUnit":"W","minChargingRate":1.5,"chargingSchedulePeriod":` +
	`[{"startPeriod":0,"limit":11000,"numberPhases":3},{"startPeriod":1800,"limit":7400.5}]}}}`

const minimalChargingProfile = `{"connectorId":0,"csChargingProfiles":{"chargingProfileId":1,"stackLevel":0,` +
	`"chargingProfilePurpose":"ChargePointMaxProfile","chargingProfileKind":"Absolute","chargingSchedule":` +
	`{"chargingRateUnit":"A","chargingSchedulePeriod":[{"startPeriod":0,"limit":32}]}}}`

const signingCertificate = `-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----`

var remainingWrapperCases = []wrapperCase{
	{name: "reserve now", method: http.MethodPost, path: "/cs/%s/reservation",
		body: `{"reservationId":7,"connectorId":1,"idTag":"DEADBEEF","parentIdTag":"GROUP","expiryDate":"2026-01-02T05:00:00.250+01:00"}`,
		code: http.StatusAccepted, action: "ReserveNow",
		// RFC 3339 to the second, in the offset of the request
		payload: `{"connectorId":1,"expiryDate":"2026-01-02T05:00:00+01:00","idTag":"DEADBEEF","parentIdTag":"GROUP","reservationId":7}`,
		written: reservationWith(7, store.ReservationStatusAccepted)},
	{name: "reserve now without parent", method: http.MethodPost, path: "/cs/%s/reservation",
		body: `{"reservationId":7,"connectorId":0,"idTag":"DEADBEEF","expiryDate":"2026-01-02T05:00:00Z"}`,
		code: http.StatusAccepted, action: "ReserveNow",
		payload: `{"connectorId":0,"expiryDate":"2026-01-02T05:00:00Z","idTag":"DEADBEEF","reservationId":7}`,
		written: reservationWith(7, store.ReservationStatusAccepted)},
	{name: "cancel reservation", method: http.MethodDelete, path: "/cs/%s/reservation/8",
		code: http.StatusAccepted, action: "CancelReservation", payload: `{"reservationId":8}`,
		written: reservationWith(8, store.ReservationStatusCancelled), prepare: reserve(8)},
	{name: "send local list", method: http.MethodPost, path: "/cs/%s/local-list",
		body: `{"listVersion":3,"updateType":"Full","localAuthorizationList":[` +
			`{"idTag":"DEADBEEF","idTagInfo":{"status":"Accepted","expiryDate":"2026-12-31T23:59:59.5Z","parentIdTag":"GROUP"}},` +
			`{"idTag":"CAFEBABE","idTagInfo":{"status":"Blocked"}}]}`,
		code: http.StatusAccepted, action: "SendLocalList",
		payload: `{"listVersion":3,"updateType":"Full","localAuthorizationList":[` +
			`{"idTag":"DEADBEEF","idTagInfo":{"status":"Accepted","expiryDate":"2026-12-31T23:59:59Z","parentIdTag":"GROUP"}},` +
			`{"idTag":"CAFEBABE","idTagInfo":{"status":"Blocked"}}]}`,
		written: localList},
	{name: "send an empty differential local list", method: http.MethodPost, path: "/cs/%s/local-list",
		body: `{"listVersion":4,"updateType":"Differential"}`,
		code: http.StatusAccepted, action: "SendLocalList", payload: `{"listVersion":4,"updateType":"Differential"}`,
		written: localList},
	{name: "update firmware", method: http.MethodPost, path: "/cs/%s/firmware/update",
		// installDate and requestId are for signed updates only
		body: `{"location":"https://example.com/fw.bin","retrieveDate":"2026-01-03T00:00:00Z","retries":2,"retryInterval":60,"installDate":"2026-01-04T00:00:00Z","requestId":5}`,
		code: http.StatusAccepted, action: "UpdateFirmware",
		payload: `{"location":"https://example.com/fw.bin","retrieveDate":"2026-01-03T00:00:00Z","retries":2,"retryInterval":60}`,
		written: lookup(store.Engine.GetFirmwareUpdateRequest)},
	{name: "update firmware at once", method: http.MethodPost, path: "/cs/%s/firmware/update",
		body: `{"location":"https://example.com/fw.bin"}`,
		code: http.StatusAccepted, action: "UpdateFirmware",
		// the time of the clock of the fixture
		payload: `{"location":"https://example.com/fw.bin","retrieveDate":"2026-01-02T03:04:05Z"}`,
		written: lookup(store.Engine.GetFirmwareUpdateRequest)},
	{name: "signed firmware update", method: http.MethodPost, path: "/cs/%s/firmware/update",
		body: `{"location":"https://example.com/fw.bin","retrieveDate":"2026-01-03T00:00:00+02:00","installDate":"2026-01-04T00:00:00Z",` +
			`"signature":"c2lnbmF0dXJl","signingCertificate":"` + signingCertificate + `","requestId":42,"retries":1,"retryInterval":30}`,
		code: http.StatusAccepted, action: "SignedUpdateFirmware",
		payload: `{"requestId":42,"firmware":{"location":"https://example.com/fw.bin","retrieveDateTime":"2026-01-03T00:00:00+02:00",` +
			`"installDateTime":"2026-01-04T00:00:00Z","signingCertificate":"` + signingCertificate + `","signature":"c2lnbmF0dXJl"},"retries":1,"retryInterval":30}`,
		written: lookup(store.Engine.GetFirmwareUpdateRequest)},
	{name: "get diagnostics", method: http.MethodPost, path: "/cs/%s/diagnostics",
		body: `{"location":"ftp://example.com/diagnostics","startTime":"2026-01-01T00:00:00Z","stopTime":"2026-01-02T01:00:00+01:00","retries":3,"retryInterval":30}`,
		code: http.StatusAccepted, action: "GetDiagnostics",
		payload: `{"location":"ftp://example.com/diagnostics","startTime":"2026-01-01T00:00:00Z","stopTime":"2026-01-02T01:00:00+01:00","retries":3,"retryInterval":30}`,
		written: lookup(store.Engine.GetDiagnosticsStatus)},
	{name: "get diagnostics of all times", method: http.MethodPost, path: "/cs/%s/diagnostics",
		body: `{"location":"ftp://example.com/diagnostics"}`,
		code: http.StatusAccepted, action: "GetDiagnostics", payload: `{"location":"ftp://example.com/diagnostics"}`,
		written: lookup(store.Engine.GetDiagnosticsStatus)},
	{name: "get log", method: http.MethodPost, path: "/cs/%s/logs",
		body: `{"logType":"SecurityLog","requestId":9,"log":{"remoteLocation":"https://example.com/logs",` +
			`"oldestTimestamp":"2026-01-01T00:00:00Z","latestTimestamp":"2026-01-02T00:00:00Z"},"retries":1,"retryInterval":10}`,
		code: http.StatusAccepted, action: "GetLog",
		payload: `{"logType":"SecurityLog","requestId":9,"log":{"remoteLocation":"https://example.com/logs",` +
			`"oldestTimestamp":"2026-01-01T00:00:00Z","latestTimestamp":"2026-01-02T00:00:00Z"},"retries":1,"retryInterval":10}`,
		written: lookup(store.Engine.GetLogStatus)},
	{name: "get the diagnostics log", method: http.MethodPost, path: "/cs/%s/logs",
		body: `{"logType":"DiagnosticsLog","requestId":10,"log":{"remoteLocation":"https://example.com/logs"}}`,
		code: http.StatusAccepted, action: "GetLog",
		payload: `{"logType":"DiagnosticsLog","requestId":10,"log":{"remoteLocation":"https://example.com/logs"}}`,
		written: lookup(store.Engine.GetLogStatus)},
	{name: "set charging profile", method: http.MethodPost, path: "/cs/%s/charging-profile",
		body: fullChargingProfile, code: http.StatusAccepted, action: "SetChargingProfile", payload: fullChargingProfile,
		written: chargingProfiles},
	{name: "set a charging profile of the charge station", method: http.MethodPost, path: "/cs/%s/charging-profile",
		body: minimalChargingProfile, code: http.StatusAccepted, action: "SetChargingProfile", payload: minimalChargingProfile,
		written: chargingProfiles},
	{name: "clear charging profile", method: http.MethodDelete,
		path: "/cs/%s/charging-profile/5?connectorId=1&chargingProfilePurpose=TxProfile&stackLevel=2",
		code: http.StatusAccepted, action: "ClearChargingProfile",
		payload: `{"id":5,"connectorId":1,"chargingProfilePurpose":"TxProfile","stackLevel":2}`,
		written: chargingProfileCleared(5), prepare: storeChargingProfile(5)},
	{name: "clear charging profile by id", method: http.MethodDelete, path: "/cs/%s/charging-profile/5",
		code: http.StatusAccepted, action: "ClearChargingProfile", payload: `{"id":5}`,
		written: chargingProfileCleared(5), prepare: storeChargingProfile(5)},
}

// reservationWith reads the reservation with the id when the charge station has it with
// the status.
func reservationWith(id int, status store.ReservationStatus) func(*testing.T, store.Engine, string) any {
	return func(t *testing.T, engine store.Engine, csId string) any {
		t.Helper()
		got, err := engine.GetReservation(context.Background(), id)
		require.NoError(t, err)
		if got == nil || got.ChargeStationId != csId || got.Status != status {
			return nil
		}
		return got
	}
}

// reserve gives the charge station an accepted reservation with the id.
func reserve(id int) func(*testing.T, store.Engine, string) {
	return func(t *testing.T, engine store.Engine, csId string) {
		t.Helper()
		require.NoError(t, engine.CreateReservation(context.Background(), &store.Reservation{
			ReservationId:   id,
			ChargeStationId: csId,
			ConnectorId:     1,
			IdTag:           "DEADBEEF",
			ExpiryDate:      time.Date(2026, 1, 2, 5, 0, 0, 0, time.UTC),
			Status:          store.ReservationStatusAccepted,
		}))
	}
}

// localList reads the local authorization list of the charge station, nil when it has
// none.
func localList(t *testing.T, engine store.Engine, csId string) any {
	t.Helper()
	ctx := context.Background()
	entries, err := engine.GetLocalAuthList(ctx, csId)
	require.NoError(t, err)
	version, err := engine.GetLocalListVersion(ctx, csId)
	require.NoError(t, err)
	if len(entries) == 0 && version == 0 {
		return nil
	}
	return version
}

// chargingProfiles reads the charging profiles of the charge station, nil when it has
// none.
func chargingProfiles(t *testing.T, engine store.Engine, csId string) any {
	t.Helper()
	profiles, err := engine.GetChargingProfiles(context.Background(), csId, nil, nil, nil)
	require.NoError(t, err)
	if len(profiles) == 0 {
		return nil
	}
	return profiles
}

// storeChargingProfile gives connector 1 of the charge station a transaction profile
// with the id at stack level 2.
func storeChargingProfile(id int) func(*testing.T, store.Engine, string) {
	return func(t *testing.T, engine store.Engine, csId string) {
		t.Helper()
		require.NoError(t, engine.SetChargingProfile(context.Background(), &store.ChargingProfile{
			ChargeStationId:        csId,
			ConnectorId:            1,
			ChargingProfileId:      id,
			StackLevel:             2,
			ChargingProfilePurpose: store.ChargingProfilePurposeTxProfile,
			ChargingProfileKind:    store.ChargingProfileKindAbsolute,
			ChargingSchedule: store.ChargingSchedule{
				ChargingRateUnit:       store.ChargingRateUnitA,
				ChargingSchedulePeriod: []store.ChargingSchedulePeriod{{StartPeriod: 0, Limit: 16}},
			},
		}))
	}
}

// chargingProfileCleared says (not nil) when the charge station no longer has the
// charging profile with the id: what clearing it writes.
func chargingProfileCleared(id int) func(*testing.T, store.Engine, string) any {
	return func(t *testing.T, engine store.Engine, csId string) any {
		t.Helper()
		profiles, err := engine.GetChargingProfiles(context.Background(), csId, nil, nil, nil)
		require.NoError(t, err)
		for _, profile := range profiles {
			if profile.ChargingProfileId == id {
				return nil
			}
		}
		return "cleared"
	}
}

// queue sends the request of the caller for the OCPP 1.6 charge station of A and
// returns the command that it queued.
func (f *commandsFixture) queue(t *testing.T, method, path, body string) *store.Command {
	t.Helper()
	rr := f.call(commandCaller(), method, path, body)
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	return f.stored(t, decodeCommand(t, rr).Id.String())
}

func TestSignedFirmwareUpdateNeedsTheSignatureAndItsCertificate(t *testing.T) {
	f := newWrappersFixture(t)

	for _, body := range []string{
		`{"location":"https://example.com/fw.bin","signature":"c2lnbmF0dXJl"}`,
		`{"location":"https://example.com/fw.bin","signingCertificate":"` + signingCertificate + `"}`,
	} {
		rr := f.call(commandCaller(), http.MethodPost, "/cs/"+stationOfA+"/firmware/update", body)
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), "needs both signature and signingCertificate")
	}
	assert.Zero(t, f.storedCommands(t))
	assert.Empty(t, f.sentCalls())

	// other charge stations keep the request as before
	rr := f.call(commandCaller(), http.MethodPost, "/cs/"+stationOfA201+"/firmware/update", `{"location":"https://example.com/fw.bin","signature":"c2lnbmF0dXJl"}`)
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	request, err := f.engine.GetFirmwareUpdateRequest(context.Background(), stationOfA201)
	require.NoError(t, err)
	require.NotNil(t, request)
	assert.Equal(t, "c2lnbmF0dXJl", *request.Signature)
}

func TestSignedFirmwareUpdateWithoutRequestIdGetsOne(t *testing.T) {
	f := newWrappersFixture(t)

	cmd := f.queue(t, http.MethodPost, "/cs/"+stationOfA+"/firmware/update",
		`{"location":"https://example.com/fw.bin","signature":"c2lnbmF0dXJl","signingCertificate":"`+signingCertificate+`"}`)
	assert.Equal(t, "SignedUpdateFirmware", cmd.Action)
	var payload ocpp16types.SignedUpdateFirmwareJson
	require.NoError(t, json.Unmarshal(cmd.Payload, &payload))
	assert.Contains(t, string(cmd.Payload), `"requestId":`)
	assert.GreaterOrEqual(t, payload.RequestId, 0)
	assert.LessOrEqual(t, payload.RequestId, math.MaxInt32)
	assert.Equal(t, "2026-01-02T03:04:05Z", payload.Firmware.RetrieveDateTime, "retrieved at once")
	assert.Nil(t, payload.Firmware.InstallDateTime)
}

func TestReservationIdOfAnotherChargeStationIsRefusedBeforeItIsQueued(t *testing.T) {
	f := newWrappersFixture(t)
	reserve(7)(t, f.engine, stationOfA201)

	rr := f.call(commandCaller(), http.MethodPost, "/cs/"+stationOfA+"/reservation",
		`{"reservationId":7,"connectorId":1,"idTag":"DEADBEEF","expiryDate":"2026-01-02T05:00:00Z"}`)
	require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
	assert.Zero(t, f.storedCommands(t))
	assert.Empty(t, f.sentCalls())
	assert.NotNil(t, reservationWith(7, store.ReservationStatusAccepted)(t, f.engine, stationOfA201), "the other reservation is kept")
}

func TestCancellingAnUnknownReservationQueuesNothing(t *testing.T) {
	f := newWrappersFixture(t)
	reserve(8)(t, f.engine, stationOfA201)

	for _, id := range []string{"8", "9"} {
		rr := f.call(commandCaller(), http.MethodDelete, "/cs/"+stationOfA+"/reservation/"+id, "")
		require.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())
	}
	assert.Zero(t, f.storedCommands(t))
}

func TestSendLocalListKeepsThePermissionOfTheOperation(t *testing.T) {
	f := newWrappersFixture(t)
	body := `{"listVersion":1,"updateType":"Full"}`

	// the permission of the operation is enough: no command permission is needed
	rr := f.call(withPerms(commandCaller(), tenancy.PermTokenWrite), http.MethodPost, "/cs/"+stationOfA+"/local-list", body)
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	assert.Equal(t, "SendLocalList", decodeCommand(t, rr).Action)

	rr = f.call(withPerms(commandCaller(), tenancy.PermConnectorCommand), http.MethodPost, "/cs/"+stationOfA+"/local-list", body)
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	assert.Equal(t, 1, f.storedCommands(t))
}

func TestSendLocalListRefusesWhatAnOcpp16ChargeStationCannotGet(t *testing.T) {
	f := newWrappersFixture(t)

	rr := f.call(commandCaller(), http.MethodPost, "/cs/"+stationOfA+"/local-list",
		`{"listVersion":1,"updateType":"Full","localAuthorizationList":[{"idTag":"`+strings.Repeat("x", 21)+`","idTagInfo":{"status":"Accepted"}}]}`)
	require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), "invalid_payload: ")
	assert.Zero(t, f.storedCommands(t))
	assert.Nil(t, localList(t, f.engine, stationOfA))
}

// The stores of reservations, local lists and charging profiles hold what the charge
// station accepted: the result handlers of the OCPP 1.6 router write them when a
// queued command is answered.
func TestStoresHoldWhatTheChargeStationAccepted(t *testing.T) {
	ctx := context.Background()

	t.Run("reservation", func(t *testing.T) {
		f := newWrappersFixture(t)
		router := f.router16()

		accepted := f.queue(t, http.MethodPost, "/cs/"+stationOfA+"/reservation",
			`{"reservationId":7,"connectorId":1,"idTag":"DEADBEEF","parentIdTag":"GROUP","expiryDate":"2026-01-02T05:00:00+01:00"}`)
		assert.Nil(t, reservationWith(7, store.ReservationStatusAccepted)(t, f.engine, stationOfA), "not before the charge station answers")
		router.Handle(ctx, stationOfA, answer(accepted, `{"status":"Accepted"}`))
		assert.Equal(t, store.CommandStatusAccepted, f.stored(t, accepted.Id).Status)
		got, err := f.engine.GetReservation(ctx, 7)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, stationOfA, got.ChargeStationId)
		assert.Equal(t, 1, got.ConnectorId)
		assert.Equal(t, "DEADBEEF", got.IdTag)
		require.NotNil(t, got.ParentIdTag)
		assert.Equal(t, "GROUP", *got.ParentIdTag)
		assert.True(t, got.ExpiryDate.Equal(time.Date(2026, 1, 2, 4, 0, 0, 0, time.UTC)), got.ExpiryDate.String())
		assert.Equal(t, store.ReservationStatusAccepted, got.Status)

		occupied := f.queue(t, http.MethodPost, "/cs/"+stationOfA+"/reservation",
			`{"reservationId":9,"connectorId":2,"idTag":"DEADBEEF","expiryDate":"2026-01-02T05:00:00Z"}`)
		router.Handle(ctx, stationOfA, answer(occupied, `{"status":"Occupied"}`))
		assert.Equal(t, store.CommandStatusRejected, f.stored(t, occupied.Id).Status)
		got, err = f.engine.GetReservation(ctx, 9)
		require.NoError(t, err)
		assert.Nil(t, got, "a reservation that the charge station refused is not stored")

		rejected := f.queue(t, http.MethodDelete, "/cs/"+stationOfA+"/reservation/7", "")
		router.Handle(ctx, stationOfA, answer(rejected, `{"status":"Rejected"}`))
		assert.NotNil(t, reservationWith(7, store.ReservationStatusAccepted)(t, f.engine, stationOfA), "still reserved")
		cancelled := f.queue(t, http.MethodDelete, "/cs/"+stationOfA+"/reservation/7", "")
		router.Handle(ctx, stationOfA, answer(cancelled, `{"status":"Accepted"}`))
		assert.NotNil(t, reservationWith(7, store.ReservationStatusCancelled)(t, f.engine, stationOfA))
	})

	t.Run("charging profile", func(t *testing.T) {
		f := newWrappersFixture(t)
		router := f.router16()
		list := func(csId string) string {
			rr := f.call(commandCaller(), http.MethodGet, "/cs/"+csId+"/charging-profile", "")
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			return rr.Body.String()
		}

		// what an OCPP 2.0.1 charge station gets stored at once
		rr := f.call(commandCaller(), http.MethodPost, "/cs/"+stationOfA201+"/charging-profile", fullChargingProfile)
		require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
		require.Nil(t, chargingProfiles(t, f.engine, stationOfA) /* nothing yet for the 1.6 station */)

		rejected := f.queue(t, http.MethodPost, "/cs/"+stationOfA+"/charging-profile", fullChargingProfile)
		router.Handle(ctx, stationOfA, answer(rejected, `{"status":"Rejected"}`))
		assert.Equal(t, store.CommandStatusRejected, f.stored(t, rejected.Id).Status)
		assert.Nil(t, chargingProfiles(t, f.engine, stationOfA), "a profile that the charge station refused is not stored")

		accepted := f.queue(t, http.MethodPost, "/cs/"+stationOfA+"/charging-profile", fullChargingProfile)
		router.Handle(ctx, stationOfA, answer(accepted, `{"status":"Accepted"}`))
		assert.Equal(t, store.CommandStatusAccepted, f.stored(t, accepted.Id).Status)
		// the same profile as the one stored for the other charge station
		assert.JSONEq(t, list(stationOfA201), list(stationOfA))
		assert.Contains(t, list(stationOfA), `"transactionId":123`)

		unknown := f.queue(t, http.MethodDelete, "/cs/"+stationOfA+"/charging-profile/5", "")
		router.Handle(ctx, stationOfA, answer(unknown, `{"status":"Unknown"}`))
		assert.NotNil(t, chargingProfiles(t, f.engine, stationOfA), "kept when the charge station knows no such profile")
		cleared := f.queue(t, http.MethodDelete, "/cs/"+stationOfA+"/charging-profile/5?connectorId=1", "")
		router.Handle(ctx, stationOfA, answer(cleared, `{"status":"Accepted"}`))
		assert.Nil(t, chargingProfiles(t, f.engine, stationOfA))
		assert.NotNil(t, chargingProfiles(t, f.engine, stationOfA201), "only the profiles of the charge station")
	})

	t.Run("local list", func(t *testing.T) {
		f := newWrappersFixture(t)
		router := f.router16()
		body := `{"listVersion":3,"updateType":"Full","localAuthorizationList":[` +
			`{"idTag":"DEADBEEF","idTagInfo":{"status":"Accepted","expiryDate":"2026-12-31T23:59:59Z","parentIdTag":"GROUP"}},` +
			`{"idTag":"CAFEBABE","idTagInfo":{"status":"Blocked"}}]}`
		list := func(csId string) string {
			rr := f.call(commandCaller(), http.MethodGet, "/cs/"+csId+"/local-list", "")
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			return rr.Body.String()
		}

		rr := f.call(commandCaller(), http.MethodPost, "/cs/"+stationOfA201+"/local-list", body)
		require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())

		mismatch := f.queue(t, http.MethodPost, "/cs/"+stationOfA+"/local-list", body)
		router.Handle(ctx, stationOfA, answer(mismatch, `{"status":"VersionMismatch"}`))
		assert.Equal(t, store.CommandStatusRejected, f.stored(t, mismatch.Id).Status)
		assert.Nil(t, localList(t, f.engine, stationOfA), "a list that the charge station refused is not stored")

		accepted := f.queue(t, http.MethodPost, "/cs/"+stationOfA+"/local-list", body)
		router.Handle(ctx, stationOfA, answer(accepted, `{"status":"Accepted"}`))
		assert.Equal(t, store.CommandStatusAccepted, f.stored(t, accepted.Id).Status)
		// the same list, and version, as the one stored for the other charge station
		assert.JSONEq(t, list(stationOfA201), list(stationOfA))
		assert.Contains(t, list(stationOfA), `"listVersion":3`)
	})

	t.Run("firmware and diagnostics", func(t *testing.T) {
		f := newWrappersFixture(t)
		router := f.router16()

		update := f.queue(t, http.MethodPost, "/cs/"+stationOfA+"/firmware/update", `{"location":"https://example.com/fw.bin"}`)
		status, err := f.engine.GetFirmwareUpdateStatus(ctx, stationOfA)
		require.NoError(t, err)
		require.Nil(t, status)
		router.Handle(ctx, stationOfA, answer(update, `{}`))
		assert.Equal(t, store.CommandStatusAccepted, f.stored(t, update.Id).Status)
		status, err = f.engine.GetFirmwareUpdateStatus(ctx, stationOfA)
		require.NoError(t, err)
		require.NotNil(t, status)
		assert.Equal(t, store.FirmwareUpdateStatusDownloading, status.Status)
		assert.Equal(t, "https://example.com/fw.bin", status.Location)

		diagnostics := f.queue(t, http.MethodPost, "/cs/"+stationOfA+"/diagnostics", `{"location":"ftp://example.com/diagnostics"}`)
		router.Handle(ctx, stationOfA, answer(diagnostics, `{"fileName":"diagnostics.zip"}`))
		assert.Equal(t, store.CommandStatusAccepted, f.stored(t, diagnostics.Id).Status)
		assert.JSONEq(t, `{"fileName":"diagnostics.zip"}`, string(f.stored(t, diagnostics.Id).Result))
		rr := f.call(commandCaller(), http.MethodGet, "/cs/"+stationOfA+"/diagnostics/status", "")
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), `"status":"Uploading"`)

		logs := f.queue(t, http.MethodPost, "/cs/"+stationOfA+"/logs", `{"logType":"SecurityLog","requestId":9,"log":{"remoteLocation":"https://example.com/logs"}}`)
		router.Handle(ctx, stationOfA, answer(logs, `{"status":"Accepted","filename":"security.log"}`))
		assert.Equal(t, store.CommandStatusAccepted, f.stored(t, logs.Id).Status)
		// the log status follows the LogStatusNotifications of the charge station
		router.Handle(ctx, stationOfA, &transport.Message{MessageType: transport.MessageTypeCall, Action: "LogStatusNotification",
			MessageId: "ls-1", RequestPayload: json.RawMessage(`{"status":"Uploading","requestId":9}`)})
		logStatus, err := f.engine.GetLogStatus(ctx, stationOfA)
		require.NoError(t, err)
		require.NotNil(t, logStatus)
		assert.Equal(t, store.LogStatusUploading, logStatus.Status)
		assert.Equal(t, 9, logStatus.RequestId)
	})
}

func TestRefreshLocalListVersionAsksAnOcpp16ChargeStation(t *testing.T) {
	ctx := context.Background()
	f := newWrappersFixture(t)
	router := f.router16()
	require.NoError(t, f.engine.UpdateLocalAuthList(ctx, stationOfA, 2, store.LocalAuthListUpdateTypeFull, nil))

	rr := f.call(commandCaller(), http.MethodGet, "/cs/"+stationOfA+"/local-list/version?refresh=true", "")
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	got := decodeCommand(t, rr)
	assert.Equal(t, "/api/v0/commands/"+got.Id.String(), rr.Header().Get("Location"))
	assert.Equal(t, "GetLocalListVersion", got.Action)
	assert.JSONEq(t, `{}`, string(got.Payload))
	require.Len(t, f.sentCalls(), 1)
	assert.Equal(t, got.Id.String(), f.sentCalls()[0].MessageId)

	// the version is the result of the command
	router.Handle(ctx, stationOfA, answer(f.stored(t, got.Id.String()), `{"listVersion":5}`))
	cmd := f.stored(t, got.Id.String())
	assert.Equal(t, store.CommandStatusAccepted, cmd.Status)
	assert.JSONEq(t, `{"listVersion":5}`, string(cmd.Result))

	// without refresh: what the manager knows
	rr = f.call(commandCaller(), http.MethodGet, "/cs/"+stationOfA+"/local-list/version", "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.JSONEq(t, `{"listVersion":2}`, rr.Body.String())

	t.Run("refreshing needs the command permission", func(t *testing.T) {
		before := f.storedCommands(t)
		rr := f.call(withPerms(commandCaller(), tenancy.PermStationRead), http.MethodGet, "/cs/"+stationOfA+"/local-list/version?refresh=true", "")
		require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), "missing permission cpms.connector.command")
		assert.Equal(t, before, f.storedCommands(t))

		rr = f.call(withPerms(commandCaller(), tenancy.PermStationRead), http.MethodGet, "/cs/"+stationOfA+"/local-list/version", "")
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	})

	t.Run("other charge stations answer what the manager knows", func(t *testing.T) {
		for _, run := range []struct {
			name    string
			csId    string
			noQueue bool
		}{
			{name: "OCPP 2.0.1 charge station", csId: stationOfA201},
			{name: "manager without a command queue (Firestore)", csId: stationOfA, noQueue: true},
		} {
			router := f.router
			if run.noQueue {
				router = f.multiTenant.router(t)
			}
			before := f.storedCommands(t)
			rr := serve(router, as(commandCaller(), http.MethodGet, "/cs/"+run.csId+"/local-list/version?refresh=true", ""))
			require.Equal(t, http.StatusOK, rr.Code, run.name+": "+rr.Body.String())
			assert.Contains(t, rr.Body.String(), `"listVersion"`, run.name)
			assert.Equal(t, before, f.storedCommands(t), run.name)
		}
	})
}

// operationResponse decodes the answer of an operation that answers an OperationResponse.
func operationResponse(t *testing.T, rr *httptest.ResponseRecorder) api.OperationResponse {
	t.Helper()
	var got api.OperationResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got), rr.Body.String())
	require.NotNil(t, got.OperationId, rr.Body.String())
	require.NotNil(t, got.Status, rr.Body.String())
	return got
}

func TestInstalledCertificatesOfAnOcpp16ChargeStation(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct{ certificateType, ocpp16Type string }{
		{"CSMSRootCertificate", "CentralSystemRootCertificate"},
		{"ManufacturerRootCertificate", "ManufacturerRootCertificate"},
	} {
		t.Run(tc.certificateType, func(t *testing.T) {
			f := newWrappersFixture(t)
			router := f.router16()

			// the permission of the operation is enough
			rr := f.call(withPerms(commandCaller(), tenancy.PermStationRead), http.MethodGet, "/cs/"+stationOfA+"/certificates?certificateType="+tc.certificateType, "")
			require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
			got := operationResponse(t, rr)
			assert.Equal(t, api.OperationResponseStatusPending, *got.Status)
			cmd := f.stored(t, *got.OperationId)
			assert.Equal(t, "/api/v0/commands/"+cmd.Id, rr.Header().Get("Location"))
			assert.Equal(t, "GetInstalledCertificateIds", cmd.Action)
			assert.JSONEq(t, `{"certificateType":"`+tc.ocpp16Type+`"}`, string(cmd.Payload))
			require.NotNil(t, cmd.Actor)
			assert.Equal(t, "user:test", *cmd.Actor)
			require.NotNil(t, cmd.Jti)
			assert.Equal(t, "jti-a", *cmd.Jti)
			calls := f.sentCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, cmd.Id, calls[0].MessageId)
			query, err := f.engine.LookupChargeStationCertificateQuery(ctx, stationOfA)
			require.NoError(t, err)
			assert.Nil(t, query, "no certificate query in the store")

			// the certificates are the result of the command
			result := `{"status":"Accepted","certificateHashData":[{"hashAlgorithm":"SHA256","issuerNameHash":"aa","issuerKeyHash":"bb","serialNumber":"01"}]}`
			router.Handle(ctx, stationOfA, answer(cmd, result))
			cmd = f.stored(t, cmd.Id)
			assert.Equal(t, store.CommandStatusAccepted, cmd.Status)
			assert.JSONEq(t, result, string(cmd.Result))
		})
	}

	t.Run("types that OCPP 1.6 does not have", func(t *testing.T) {
		f := newWrappersFixture(t)
		for _, query := range []string{"", "?certificateType=V2GRootCertificate", "?certificateType=MORootCertificate", "?certificateType=V2GCertificateChain"} {
			rr := f.call(commandCaller(), http.MethodGet, "/cs/"+stationOfA+"/certificates"+query, "")
			require.Equal(t, http.StatusBadRequest, rr.Code, query+": "+rr.Body.String())
			assert.Contains(t, rr.Body.String(), "not supported for OCPP 1.6 stations", query)
		}
		assert.Zero(t, f.storedCommands(t))
		assert.Empty(t, f.sentCalls())
	})

	t.Run("other charge stations get a certificate query in the store", func(t *testing.T) {
		for _, run := range []struct {
			name    string
			csId    string
			noQueue bool
		}{
			{name: "OCPP 2.0.1 charge station", csId: stationOfA201},
			{name: "manager without a command queue (Firestore)", csId: stationOfA, noQueue: true},
		} {
			f := newWrappersFixture(t)
			router := f.router
			if run.noQueue {
				router = f.multiTenant.router(t)
			}
			rr := serve(router, as(commandCaller(), http.MethodGet, "/cs/"+run.csId+"/certificates?certificateType=V2GRootCertificate", ""))
			require.Equal(t, http.StatusAccepted, rr.Code, run.name+": "+rr.Body.String())
			assert.Equal(t, run.csId, *operationResponse(t, rr).OperationId, run.name)
			assert.Empty(t, rr.Header().Get("Location"), run.name)
			query, err := f.engine.LookupChargeStationCertificateQuery(ctx, run.csId)
			require.NoError(t, err)
			assert.NotNil(t, query, run.name)
			assert.Zero(t, f.storedCommands(t), run.name)
		}
	})
}

func TestDeleteCertificateOfAnOcpp16ChargeStation(t *testing.T) {
	ctx := context.Background()
	body := `{"certificateHashData":{"hashAlgorithm":"SHA256","issuerNameHash":"aa","issuerKeyHash":"bb","serialNumber":"01"}}`

	t.Run("queued", func(t *testing.T) {
		f := newWrappersFixture(t)
		router := f.router16()

		// the permission of the operation is enough
		rr := f.call(withPerms(commandCaller(), tenancy.PermStationWrite), http.MethodDelete, "/cs/"+stationOfA+"/certificate", body)
		require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
		got := operationResponse(t, rr)
		assert.Equal(t, api.OperationResponseStatusPending, *got.Status)
		cmd := f.stored(t, *got.OperationId)
		assert.Equal(t, "/api/v0/commands/"+cmd.Id, rr.Header().Get("Location"))
		assert.Equal(t, "DeleteCertificate", cmd.Action)
		assert.JSONEq(t, body, string(cmd.Payload))
		require.NotNil(t, cmd.Actor)
		assert.Equal(t, "user:test", *cmd.Actor)
		require.NotNil(t, cmd.Jti)
		assert.Equal(t, "jti-a", *cmd.Jti)
		deletion, err := f.engine.LookupChargeStationCertificateDeletion(ctx, stationOfA)
		require.NoError(t, err)
		assert.Nil(t, deletion, "no certificate deletion in the store")

		router.Handle(ctx, stationOfA, answer(cmd, `{"status":"NotFound"}`))
		cmd = f.stored(t, cmd.Id)
		assert.Equal(t, store.CommandStatusRejected, cmd.Status)
		assert.JSONEq(t, `{"status":"NotFound"}`, string(cmd.Result))

		rr = f.call(withPerms(commandCaller(), tenancy.PermConnectorCommand), http.MethodDelete, "/cs/"+stationOfA+"/certificate", body)
		require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
		assert.Equal(t, 1, f.storedCommands(t))
	})

	t.Run("other charge stations get a certificate deletion in the store", func(t *testing.T) {
		for _, run := range []struct {
			name    string
			csId    string
			noQueue bool
		}{
			{name: "OCPP 2.0.1 charge station", csId: stationOfA201},
			{name: "manager without a command queue (Firestore)", csId: stationOfA, noQueue: true},
		} {
			f := newWrappersFixture(t)
			router := f.router
			if run.noQueue {
				router = f.multiTenant.router(t)
			}
			rr := serve(router, as(commandCaller(), http.MethodDelete, "/cs/"+run.csId+"/certificate", body))
			require.Equal(t, http.StatusAccepted, rr.Code, run.name+": "+rr.Body.String())
			assert.Equal(t, run.csId, *operationResponse(t, rr).OperationId, run.name)
			deletion, err := f.engine.LookupChargeStationCertificateDeletion(ctx, run.csId)
			require.NoError(t, err)
			assert.NotNil(t, deletion, run.name)
			assert.Zero(t, f.storedCommands(t), run.name)
		}
	})
}

// ExtendedTriggerMessage, InstallCertificate and CertificateSigned have no typed
// operation: they are sent through the command API, and their results are recorded
// whether the OCPP 1.6 router has a result handler for them or not.
func TestSecurityExtensionCommandsThroughTheCommandApi(t *testing.T) {
	ctx := context.Background()
	f := newWrappersFixture(t)
	router := f.router16()

	for _, tc := range []struct{ action, payload, result string }{
		{"ExtendedTriggerMessage", `{"requestedMessage":"LogStatusNotification"}`, `{"status":"Accepted"}`},
		{"InstallCertificate", `{"certificateType":"CentralSystemRootCertificate","certificate":"` + signingCertificate + `"}`, `{"status":"Accepted"}`},
		{"CertificateSigned", `{"certificateChain":"` + signingCertificate + `"}`, `{"status":"Accepted"}`},
	} {
		got := f.mustEnqueue(t, stationOfA, `{"action":"`+tc.action+`","payload":`+tc.payload+`}`)
		calls := f.sentCalls()
		require.NotEmpty(t, calls)
		sent := calls[len(calls)-1]
		assert.Equal(t, got.Id.String(), sent.MessageId, tc.action)
		assert.Equal(t, tc.action, sent.Action)
		assert.JSONEq(t, tc.payload, string(sent.RequestPayload), tc.action)

		router.Handle(ctx, stationOfA, answer(f.stored(t, got.Id.String()), tc.result))
		cmd := f.stored(t, got.Id.String())
		assert.Equal(t, store.CommandStatusAccepted, cmd.Status, tc.action)
		assert.JSONEq(t, tc.result, string(cmd.Result), tc.action)
	}
	// only answers: the router sent no CallError for a result without a route
	for _, msg := range f.calls.sent() {
		assert.Equal(t, transport.MessageTypeCall, msg.MessageType)
	}
}

func TestStationMessagesWhileAGetDiagnosticsWaitsForItsAnswerAreHandledAsBefore(t *testing.T) {
	ctx := context.Background()
	f := newWrappersFixture(t)
	router := f.router16()

	cmd := f.queue(t, http.MethodPost, "/cs/"+stationOfA+"/diagnostics", `{"location":"ftp://example.com/diagnostics"}`)
	require.Equal(t, store.CommandStatusSent, cmd.Status)
	status, err := f.engine.GetDiagnosticsStatus(ctx, stationOfA)
	require.NoError(t, err)
	require.Nil(t, status, "no diagnostics status before the charge station answers")

	// the charge station reports before its answer is handled
	for _, msg := range []*transport.Message{
		{MessageType: transport.MessageTypeCall, Action: "DiagnosticsStatusNotification", MessageId: "ds-1", RequestPayload: json.RawMessage(`{"status":"Uploading"}`)},
		{MessageType: transport.MessageTypeCall, Action: "Heartbeat", MessageId: "hb-1", RequestPayload: json.RawMessage(`{}`)},
		{MessageType: transport.MessageTypeCall, Action: "StatusNotification", MessageId: "sn-1", RequestPayload: json.RawMessage(`{"connectorId":1,"errorCode":"NoError","status":"Available"}`)},
	} {
		router.Handle(ctx, stationOfA, msg)
	}

	var answers []*transport.Message
	for _, msg := range f.calls.sent() {
		if msg.MessageType != transport.MessageTypeCall {
			answers = append(answers, msg)
		}
	}
	require.Len(t, answers, 3)
	for i, id := range []string{"ds-1", "hb-1", "sn-1"} {
		assert.Equal(t, transport.MessageTypeCallResult, answers[i].MessageType, id)
		assert.Equal(t, id, answers[i].MessageId)
	}
	status, err = f.engine.GetDiagnosticsStatus(ctx, stationOfA)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, store.DiagnosticsStatusUploading, status.Status)
	assert.Equal(t, store.CommandStatusSent, f.stored(t, cmd.Id).Status, "the command still waits for its answer")

	router.Handle(ctx, stationOfA, answer(cmd, `{}`))
	assert.Equal(t, store.CommandStatusAccepted, f.stored(t, cmd.Id).Status)
}
