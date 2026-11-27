// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
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
		nil, nil, nil, nil, time.Minute, schemas.OcppSchemas, f.dispatcher, nil)
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
		"Reset", `{"type":"Hard"}`, lookup(store.Engine.GetResetRequest)},
	{"unlock connector", http.MethodPost, "/cs/%s/unlock-connector", `{"connectorId":2}`, http.StatusAccepted,
		"UnlockConnector", `{"connectorId":2}`, lookup(store.Engine.GetUnlockConnectorRequest)},
	{"change availability of a connector", http.MethodPost, "/cs/%s/availability", `{"connectorId":1,"evseId":7,"type":"Inoperative"}`, http.StatusAccepted,
		"ChangeAvailability", `{"connectorId":1,"type":"Inoperative"}`, lookup(store.Engine.LookupChargeStationChangeAvailability)},
	{"change availability of the charge station", http.MethodPost, "/cs/%s/availability", `{"type":"Operative"}`, http.StatusAccepted,
		"ChangeAvailability", `{"connectorId":0,"type":"Operative"}`, lookup(store.Engine.LookupChargeStationChangeAvailability)},
	{"clear cache", http.MethodPost, "/cs/%s/cache/clear", ``, http.StatusAccepted,
		"ClearCache", `{}`, lookup(store.Engine.LookupChargeStationClearCache)},
	{"data transfer", http.MethodPost, "/cs/%s/data-transfer", `{"vendorId":"com.example","messageId":"hello","data":"world"}`, http.StatusAccepted,
		"DataTransfer", `{"vendorId":"com.example","messageId":"hello","data":"world"}`, lookup(store.Engine.LookupChargeStationDataTransfer)},
	{"data transfer with the vendor only", http.MethodPost, "/cs/%s/data-transfer", `{"vendorId":"com.example"}`, http.StatusAccepted,
		"DataTransfer", `{"vendorId":"com.example"}`, lookup(store.Engine.LookupChargeStationDataTransfer)},
	{"remote start", http.MethodPost, "/cs/%s/start-transaction", `{"idTag":"DEADBEEF","connectorId":1}`, http.StatusAccepted,
		"RemoteStartTransaction", `{"idTag":"DEADBEEF","connectorId":1}`, lookup(store.Engine.GetRemoteStartTransactionRequest)},
	{"remote start with a charging profile", http.MethodPost, "/cs/%s/start-transaction",
		`{"idTag":"DEADBEEF","chargingProfile":{"chargingProfileId":5,"stackLevel":0,"chargingProfilePurpose":"TxProfile","chargingProfileKind":"Absolute","chargingSchedule":{"chargingRateUnit":"A","chargingSchedulePeriod":[{"startPeriod":0,"limit":16}]}}}`,
		http.StatusAccepted, "RemoteStartTransaction",
		`{"idTag":"DEADBEEF","chargingProfile":{"chargingProfileId":5,"stackLevel":0,"chargingProfilePurpose":"TxProfile","chargingProfileKind":"Absolute","chargingSchedule":{"chargingRateUnit":"A","chargingSchedulePeriod":[{"startPeriod":0,"limit":16}]}}}`,
		lookup(store.Engine.GetRemoteStartTransactionRequest)},
	{"remote stop", http.MethodPost, "/cs/%s/stop-transaction", `{"transactionId":"123"}`, http.StatusAccepted,
		"RemoteStopTransaction", `{"transactionId":123}`, lookup(store.Engine.GetRemoteStopTransactionRequest)},
	{"trigger", http.MethodPost, "/cs/%s/trigger", `{"trigger":"StatusNotification","connectorId":1}`, http.StatusCreated,
		"TriggerMessage", `{"requestedMessage":"StatusNotification","connectorId":1}`, lookup(store.Engine.LookupChargeStationTriggerMessage)},
}

func TestCommandOperationsQueueACommandForAnOcpp16ChargeStation(t *testing.T) {
	for _, tc := range wrapperCases {
		t.Run(tc.name, func(t *testing.T) {
			f := newWrappersFixture(t)
			now := f.clock.Now()

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
	for _, tc := range wrapperCases {
		// each run writes to the store of another charge station
		f := newWrappersFixture(t)
		withoutQueue := f.multiTenant.router(t)
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
				router := f.router
				if run.noQueue {
					router = withoutQueue
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
