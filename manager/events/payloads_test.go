// SPDX-License-Identifier: Apache-2.0

package events_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	clocktest "k8s.io/utils/clock/testing"
)

func ptr[T any](v T) *T {
	return &v
}

var payloadTime = time.Date(2026, 11, 27, 10, 30, 0, 0, time.UTC)

// The field names of the payloads are the contract with the consumers.
func TestPayloadFieldNames(t *testing.T) {
	for name, tc := range map[string]struct {
		payload any
		want    string
	}{
		"station.registered": {
			payload: events.StationRegisteredData{StationKey: "k7f2.CP001", StationId: "CP001", SecurityProfile: 2},
			want:    `{"station_key":"k7f2.CP001","station_id":"CP001","security_profile":2}`,
		},
		"station.connected": {
			payload: events.StationConnectedData{StationKey: "k7f2.CP001", StationId: "CP001", OcppVersion: "ocpp1.6",
				ConnectionId: "c1", ConnectedAt: payloadTime},
			want: `{"station_key":"k7f2.CP001","station_id":"CP001","ocpp_version":"ocpp1.6","connection_id":"c1",` +
				`"connected_at":"2026-11-27T10:30:00Z"}`,
		},
		"station.disconnected": {
			payload: events.StationDisconnectedData{StationKey: "k7f2.CP001", StationId: "CP001", ConnectionId: "c1",
				DisconnectedAt: payloadTime, Reason: "ping_timeout", CloseCode: 1008},
			want: `{"station_key":"k7f2.CP001","station_id":"CP001","connection_id":"c1",` +
				`"disconnected_at":"2026-11-27T10:30:00Z","reason":"ping_timeout","close_code":1008}`,
		},
		"station.disconnected without connection": {
			payload: events.StationDisconnectedData{StationKey: "k7f2.CP001", StationId: "CP001", DisconnectedAt: payloadTime, Reason: "stale"},
			want:    `{"station_key":"k7f2.CP001","station_id":"CP001","disconnected_at":"2026-11-27T10:30:00Z","reason":"stale"}`,
		},
		"connector.status_changed": {
			payload: events.ConnectorStatusChangedData{StationKey: "k7f2.CP001", StationId: "CP001", EvseId: 1, ConnectorId: 1,
				Status: "Charging", PreviousStatus: "Preparing", ErrorCode: "NoError", Info: ptr("i"), VendorId: ptr("v"),
				VendorErrorCode: ptr("e"), Timestamp: payloadTime},
			want: `{"station_key":"k7f2.CP001","station_id":"CP001","evse_id":1,"connector_id":1,"status":"Charging",` +
				`"previous_status":"Preparing","error_code":"NoError","info":"i","vendor_id":"v","vendor_error_code":"e",` +
				`"timestamp":"2026-11-27T10:30:00Z"}`,
		},
		"connector.status_changed first status": {
			payload: events.ConnectorStatusChangedData{StationKey: "k7f2.CP001", StationId: "CP001",
				Status: "Available", ErrorCode: "NoError", Timestamp: payloadTime},
			want: `{"station_key":"k7f2.CP001","station_id":"CP001","evse_id":0,"connector_id":0,"status":"Available",` +
				`"error_code":"NoError","timestamp":"2026-11-27T10:30:00Z"}`,
		},
		"session.started": {
			payload: events.SessionStartedData{StationKey: "k7f2.CP001", StationId: "CP001", TransactionId: "tx",
				OcppTransactionId: "17", OcppVersion: "ocpp1.6", EvseId: ptr(1), ConnectorId: ptr(1), IdToken: "TAG",
				TokenType: "ISO14443", IdTokenGroup: ptr("G"), AuthStatus: "Accepted", StartedAt: payloadTime,
				MeterStartWh: ptr(int64(100)), ReservationId: ptr(7)},
			want: `{"station_key":"k7f2.CP001","station_id":"CP001","transaction_id":"tx","ocpp_transaction_id":"17",` +
				`"ocpp_version":"ocpp1.6","evse_id":1,"connector_id":1,"id_token":"TAG","token_type":"ISO14443",` +
				`"id_token_group":"G","auth_status":"Accepted","started_at":"2026-11-27T10:30:00Z","meter_start_wh":100,` +
				`"reservation_id":7}`,
		},
		"session.started minimal": {
			payload: events.SessionStartedData{StationKey: "CP001", StationId: "CP001", TransactionId: "tx",
				OcppTransactionId: "17", OcppVersion: "ocpp1.6", IdToken: "TAG", TokenType: "ISO14443", AuthStatus: "Invalid",
				StartedAt: payloadTime},
			want: `{"station_key":"CP001","station_id":"CP001","transaction_id":"tx","ocpp_transaction_id":"17",` +
				`"ocpp_version":"ocpp1.6","id_token":"TAG","token_type":"ISO14443","auth_status":"Invalid",` +
				`"started_at":"2026-11-27T10:30:00Z"}`,
		},
		"session.updated": {
			payload: events.SessionUpdatedData{StationKey: "k7f2.CP001", StationId: "CP001", TransactionId: "tx",
				OcppTransactionId: "17", EvseId: ptr(1), ConnectorId: ptr(1), SampledAt: payloadTime,
				MeterWh: ptr(int64(1800)), EnergyWh: ptr(int64(800)), PowerW: ptr(7400.0), SocPercent: ptr(63.5)},
			want: `{"station_key":"k7f2.CP001","station_id":"CP001","transaction_id":"tx","ocpp_transaction_id":"17",` +
				`"evse_id":1,"connector_id":1,"sampled_at":"2026-11-27T10:30:00Z","meter_wh":1800,"energy_wh":800,` +
				`"power_w":7400,"soc_percent":63.5}`,
		},
		"session.updated without readings": {
			payload: events.SessionUpdatedData{StationKey: "k7f2.CP001", StationId: "CP001", TransactionId: "tx",
				OcppTransactionId: "17", SampledAt: payloadTime},
			want: `{"station_key":"k7f2.CP001","station_id":"CP001","transaction_id":"tx","ocpp_transaction_id":"17",` +
				`"sampled_at":"2026-11-27T10:30:00Z"}`,
		},
		"session.completed": {
			payload: events.SessionCompletedData{StationKey: "k7f2.CP001", StationId: "CP001", TransactionId: "tx",
				OcppTransactionId: "17", EvseId: ptr(1), ConnectorId: ptr(1), IdToken: "TAG", TokenType: "ISO14443",
				IdTokenGroup: ptr("G"), StartedAt: ptr(payloadTime.Add(-time.Hour)), StoppedAt: payloadTime,
				DurationS: ptr(int64(3600)), MeterStartWh: ptr(int64(100)), MeterStopWh: ptr(int64(2100)),
				EnergyWh: ptr(int64(2000)), StopReason: "EVDisconnected"},
			want: `{"station_key":"k7f2.CP001","station_id":"CP001","transaction_id":"tx","ocpp_transaction_id":"17",` +
				`"evse_id":1,"connector_id":1,"id_token":"TAG","token_type":"ISO14443","id_token_group":"G",` +
				`"started_at":"2026-11-27T09:30:00Z","stopped_at":"2026-11-27T10:30:00Z","duration_s":3600,` +
				`"meter_start_wh":100,"meter_stop_wh":2100,"energy_wh":2000,"stop_reason":"EVDisconnected"}`,
		},
		"session.completed minimal": {
			payload: events.SessionCompletedData{StationKey: "k7f2.CP001", StationId: "CP001", TransactionId: "tx",
				OcppTransactionId: "17", StoppedAt: payloadTime, StopReason: "Local"},
			want: `{"station_key":"k7f2.CP001","station_id":"CP001","transaction_id":"tx","ocpp_transaction_id":"17",` +
				`"id_token":"","token_type":"","stopped_at":"2026-11-27T10:30:00Z","stop_reason":"Local"}`,
		},
		"command.completed": {
			payload: events.CommandCompletedData{CommandId: "cmd", StationKey: "k7f2.CP001", StationId: "CP001",
				Action: "Reset", Status: "Failed", ErrorCode: "Timeout", ErrorDescription: "no answer",
				Result: json.RawMessage(`{"status":"Rejected"}`), CompletedAt: payloadTime},
			want: `{"command_id":"cmd","station_key":"k7f2.CP001","station_id":"CP001","action":"Reset","status":"Failed",` +
				`"error_code":"Timeout","error_description":"no answer","result":{"status":"Rejected"},` +
				`"completed_at":"2026-11-27T10:30:00Z"}`,
		},
		"command.completed minimal": {
			payload: events.CommandCompletedData{CommandId: "cmd", StationKey: "k7f2.CP001", StationId: "CP001",
				Action: "Reset", Status: "Accepted", CompletedAt: payloadTime},
			want: `{"command_id":"cmd","station_key":"k7f2.CP001","station_id":"CP001","action":"Reset","status":"Accepted",` +
				`"completed_at":"2026-11-27T10:30:00Z"}`,
		},
	} {
		got, err := json.Marshal(tc.payload)
		require.NoError(t, err, name)
		assert.Equal(t, tc.want, string(got), name)
	}
}

func TestStationId(t *testing.T) {
	assert.Equal(t, "CP001", events.StationId("k7f2.CP001"))
	assert.Equal(t, "CP.001", events.StationId("k7f2.CP.001"), "a station id may have a dot")
	assert.Equal(t, "CP001", events.StationId("CP001"), "a key without an org key")
}

func TestStationActorAndConnectorResourceId(t *testing.T) {
	assert.Equal(t, "station:k7f2.CP001", events.StationActor("k7f2.CP001"))
	assert.Equal(t, "k7f2.CP001#0", events.ConnectorResourceId("k7f2.CP001", 0))
	assert.Equal(t, "k7f2.CP001#12", events.ConnectorResourceId("k7f2.CP001", 12))
}

// Every type of the catalogue is one that the emitter accepts.
func TestAllTypesCanBeEmitted(t *testing.T) {
	assert.Len(t, events.AllTypes, 8)
	assert.Equal(t, []string{
		"station.registered", "station.connected", "station.disconnected", "connector.status_changed",
		"session.started", "session.updated", "session.completed", "command.completed",
	}, events.AllTypes)

	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	emitter := newEmitter(s)
	for _, eventType := range events.AllTypes {
		ev := sessionEvent()
		ev.Type = eventType
		require.NoError(t, emitter.Emit(context.Background(), ev), eventType)
	}
	assert.Len(t, outbox(t, s), len(events.AllTypes))
}
