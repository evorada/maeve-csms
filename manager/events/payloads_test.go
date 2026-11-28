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
				Action: "Reset", Status: "accepted", ErrorCode: "ResponseTimeout", ErrorDescription: "no answer",
				Result: json.RawMessage(`{"status":"Accepted"}`), CompletedAt: payloadTime,
				OrgId: "0193b2c4-0000-7000-8000-0000000000a1", OcppVersion: "ocpp1.6", Actor: ptr("user:alice"),
				Jti: ptr("jti-1"), CreatedAt: payloadTime.Add(-time.Hour), SentAt: ptr(payloadTime.Add(-time.Minute)),
				Late: true},
			want: `{"command_id":"cmd","station_key":"k7f2.CP001","station_id":"CP001","action":"Reset","status":"accepted",` +
				`"error_code":"ResponseTimeout","error_description":"no answer","result":{"status":"Accepted"},` +
				`"completed_at":"2026-11-27T10:30:00Z","org_id":"0193b2c4-0000-7000-8000-0000000000a1",` +
				`"ocpp_version":"ocpp1.6","actor":"user:alice","jti":"jti-1","created_at":"2026-11-27T09:30:00Z",` +
				`"sent_at":"2026-11-27T10:29:00Z","late":true}`,
		},
		"command.completed minimal": {
			payload: events.CommandCompletedData{CommandId: "cmd", StationKey: "k7f2.CP001", StationId: "CP001",
				Action: "Reset", Status: "timeout", CompletedAt: payloadTime, OrgId: "org", OcppVersion: "ocpp1.6",
				CreatedAt: payloadTime},
			want: `{"command_id":"cmd","station_key":"k7f2.CP001","station_id":"CP001","action":"Reset","status":"timeout",` +
				`"completed_at":"2026-11-27T10:30:00Z","org_id":"org","ocpp_version":"ocpp1.6",` +
				`"created_at":"2026-11-27T10:30:00Z","late":false}`,
		},
		"command.progress": {
			payload: events.CommandProgressData{CommandId: "cmd", OrgId: "0193b2c4-0000-7000-8000-0000000000a1",
				StationId: "CP001", StationKey: "k7f2.CP001", Action: "RemoteStartTransaction", Status: "accepted",
				Progress:   json.RawMessage(`{"state":"transaction_started","transactionId":"tx","ocppTransactionId":17,"connectorId":1}`),
				ProgressAt: payloadTime},
			want: `{"command_id":"cmd","org_id":"0193b2c4-0000-7000-8000-0000000000a1","station_id":"CP001",` +
				`"station_key":"k7f2.CP001","action":"RemoteStartTransaction","status":"accepted",` +
				`"progress":{"state":"transaction_started","transactionId":"tx","ocppTransactionId":17,"connectorId":1},` +
				`"progress_at":"2026-11-27T10:30:00Z"}`,
		},
		"location.updated of a connector": {
			payload: events.LocationUpdatedData{LocationId: "0193b2c4-0000-7000-8000-000000000001", OcpiId: "LOC1",
				PartyRef: "cpo-1", Change: events.LocationChangeCreated, EvseUid: "0193b2c4-0000-7000-8000-000000000002",
				ConnectorUid: "0193b2c4-0000-7000-8000-000000000003", Publish: true},
			want: `{"location_id":"0193b2c4-0000-7000-8000-000000000001","ocpi_id":"LOC1","party_ref":"cpo-1",` +
				`"change":"created","evse_uid":"0193b2c4-0000-7000-8000-000000000002",` +
				`"connector_uid":"0193b2c4-0000-7000-8000-000000000003","publish":true,"publish_changed":false}`,
		},
		"station.decommissioned": {
			payload: events.StationDecommissionedData{StationKey: "k7f2.CP001", StationId: "CP001",
				LocationId: ptr("0193b2c4-0000-7000-8000-000000000001"), Reason: ptr("replaced"), Actor: "user:alice"},
			want: `{"station_key":"k7f2.CP001","station_id":"CP001","location_id":"0193b2c4-0000-7000-8000-000000000001",` +
				`"reason":"replaced","actor":"user:alice"}`,
		},
		"station.decommissioned without location and reason": {
			payload: events.StationDecommissionedData{StationKey: "k7f2.CP001", StationId: "CP001", Actor: "svc:maeve"},
			want:    `{"station_key":"k7f2.CP001","station_id":"CP001","location_id":null,"reason":null,"actor":"svc:maeve"}`,
		},
		"station.pending": {
			payload: events.StationPendingData{StationKey: "k7f2.CP001", StationId: "CP001", Vendor: "Acme", Model: "Volt 22",
				SerialNumber: ptr("SN-1"), FirmwareVersion: ptr("1.2.3"), Iccid: ptr("89340711000000000019"), Imsi: ptr("214070000000001"),
				FirstSeenAt: payloadTime, Transport: "ws", Credential: "open"},
			want: `{"station_key":"k7f2.CP001","station_id":"CP001","vendor":"Acme","model":"Volt 22","serial_number":"SN-1",` +
				`"firmware_version":"1.2.3","iccid":"89340711000000000019","imsi":"214070000000001",` +
				`"first_seen_at":"2026-11-27T10:30:00Z","transport":"ws","credential":"open"}`,
		},
		"station.pending without the optional fields": {
			payload: events.StationPendingData{StationKey: "k7f2.CP001", StationId: "CP001", Vendor: "Acme", Model: "Volt 22",
				FirstSeenAt: payloadTime, Transport: "wss", Credential: "onboarding_password"},
			want: `{"station_key":"k7f2.CP001","station_id":"CP001","vendor":"Acme","model":"Volt 22",` +
				`"first_seen_at":"2026-11-27T10:30:00Z","transport":"wss","credential":"onboarding_password"}`,
		},
		"station.approved": {
			payload: events.StationApprovedData{StationKey: "k7f2.CP001", StationId: "CP001", LocationId: "0193b2c4-0000-7000-8000-000000000001",
				EvseCount: 2, ConnectorCount: 3, PasswordMode: events.PasswordModeKeepOnboarding, SharedCredentials: true, Actor: "user:alice"},
			want: `{"station_key":"k7f2.CP001","station_id":"CP001","location_id":"0193b2c4-0000-7000-8000-000000000001",` +
				`"evse_count":2,"connector_count":3,"password_mode":"keep_onboarding","shared_credentials":true,"actor":"user:alice"}`,
		},
		"station.approval_failed": {
			payload: events.StationApprovalFailedData{StationKey: "k7f2.CP001", StationId: "CP001", CommandId: "0193b2c4-0000-7000-8000-00000000c0de",
				Result: "NotSupported"},
			want: `{"station_key":"k7f2.CP001","station_id":"CP001","command_id":"0193b2c4-0000-7000-8000-00000000c0de","result":"NotSupported","error":null}`,
		},
		"station.rejected": {
			payload: events.StationRejectedData{StationKey: "k7f2.CP001", StationId: "CP001", Reason: ptr("not ours"), Actor: "user:alice"},
			want:    `{"station_key":"k7f2.CP001","station_id":"CP001","reason":"not ours","actor":"user:alice"}`,
		},
		"station.transferred": {
			payload: events.StationTransferredData{StationKey: "k7f2.CP001", StationId: "CP001",
				FromOrgId: "0193b2c4-0000-7000-8000-0000000000a1", ToOrgId: "0193b2c4-0000-7000-8000-0000000000b2",
				LocationId: ptr("0193b2c4-0000-7000-8000-000000000001"), LocationMoved: true, EvsesMoved: 2, ConnectorsMoved: 3,
				ClearedTariffRefs: []string{"0193b2c4-0000-7000-8000-00000000ta01"}, Actor: "svc:platform", Reason: ptr("sold")},
			want: `{"station_key":"k7f2.CP001","station_id":"CP001","from_org_id":"0193b2c4-0000-7000-8000-0000000000a1",` +
				`"to_org_id":"0193b2c4-0000-7000-8000-0000000000b2","location_id":"0193b2c4-0000-7000-8000-000000000001",` +
				`"location_moved":true,"evses_moved":2,"connectors_moved":3,"cleared_tariff_refs":["0193b2c4-0000-7000-8000-00000000ta01"],` +
				`"actor":"svc:platform","reason":"sold"}`,
		},
		"station.transferred without a location": {
			payload: events.StationTransferredData{StationKey: "k7f2.CP001", StationId: "CP001",
				FromOrgId: "0193b2c4-0000-7000-8000-0000000000a1", ToOrgId: "0193b2c4-0000-7000-8000-0000000000b2",
				ClearedTariffRefs: []string{}, Actor: "svc:platform"},
			want: `{"station_key":"k7f2.CP001","station_id":"CP001","from_org_id":"0193b2c4-0000-7000-8000-0000000000a1",` +
				`"to_org_id":"0193b2c4-0000-7000-8000-0000000000b2","location_id":null,"location_moved":false,"evses_moved":0,` +
				`"connectors_moved":0,"cleared_tariff_refs":[],"actor":"svc:platform","reason":null}`,
		},
		"location.updated of a location": {
			payload: events.LocationUpdatedData{LocationId: "0193b2c4-0000-7000-8000-000000000001", OcpiId: "LOC1",
				PartyRef: "cpo-1", Change: events.LocationChangeUpdated, PublishChanged: true},
			want: `{"location_id":"0193b2c4-0000-7000-8000-000000000001","ocpi_id":"LOC1","party_ref":"cpo-1",` +
				`"change":"updated","publish":false,"publish_changed":true}`,
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
	assert.Len(t, events.AllTypes, 16)
	assert.Equal(t, []string{
		"station.registered", "station.connected", "station.disconnected", "connector.status_changed",
		"session.started", "session.updated", "session.completed", "command.completed", "command.progress",
		"location.updated", "station.decommissioned", "station.pending", "station.approved",
		"station.approval_failed", "station.rejected", "station.transferred",
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
