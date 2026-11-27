// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/connectivity"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/transport"
)

// getStatus returns the decoded body of GET /cs/{csId}/status.
func getStatus(t *testing.T, r *chi.Mux, csId string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/cs/"+csId+"/status", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Result().StatusCode, rr.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	return body
}

func TestGetChargeStationStatusReturnsTheConnectivity(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()
	ctx := context.Background()
	registerStation(t, engine, "cs001")

	heartbeat := time.Date(2026, 11, 27, 9, 59, 0, 0, time.UTC)
	model := "Model X"
	connectedAt := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	require.NoError(t, engine.SetChargeStationStatus(ctx, "cs001", &store.ChargeStationStatus{
		ChargeStationId: "cs001",
		Connected:       true,
		LastHeartbeat:   &heartbeat,
		Model:           &model,
	}))
	_, err := engine.RecordStationConnected(ctx, "cs001", "conn-a", connectedAt)
	require.NoError(t, err)
	require.NoError(t, engine.TouchStationLastSeen(ctx, "cs001", connectedAt.Add(time.Minute)))

	assert.Equal(t, map[string]any{
		"id":              "cs001",
		"connected":       true,
		"lastHeartbeat":   "2026-11-27T09:59:00Z",
		"lastConnectedAt": "2026-11-27T10:00:00Z",
		"lastSeenAt":      "2026-11-27T10:01:00Z",
		"model":           "Model X",
	}, getStatus(t, r, "cs001"))

	_, err = engine.RecordStationDisconnected(ctx, "cs001", "conn-a", connectedAt.Add(2*time.Minute))
	require.NoError(t, err)

	body := getStatus(t, r, "cs001")
	assert.Equal(t, false, body["connected"])
	assert.Equal(t, "2026-11-27T10:00:00Z", body["lastConnectedAt"])
	assert.Equal(t, "2026-11-27T10:02:00Z", body["lastDisconnectedAt"])
	assert.Equal(t, "2026-11-27T10:01:00Z", body["lastSeenAt"])
}

// A station that closes its socket is shown as disconnected: the gateway's disconnected
// event reaches the tracker, which records it in the store the API reads.
func TestStationThatClosesItsSocketShowsDisconnected(t *testing.T) {
	server, r, engine, clk := setupServer(t)
	defer server.Close()
	ctx := context.Background()
	registerStation(t, engine, "cs001")
	tracker := &connectivity.Tracker{Store: engine, Tx: engine, Events: events.NopEmitter{}, Clock: clk}
	connectedAt := clk.Now().UTC()

	tracker.HandleConnectionEvent(ctx, &transport.ConnectionEvent{
		Type:         transport.ConnectionEventConnected,
		StationKey:   "cs001",
		OcppVersion:  "ocpp1.6",
		ConnectionId: "conn-a",
		GatewayId:    "gateway-0",
		At:           connectedAt,
	})
	// the station talks OCPP: its heartbeats set connected too
	handler := tracker.WrapHandler(transport.MessageHandlerFunc(func(ctx context.Context, chargeStationId string, _ *transport.Message) {
		_, err := ocpp16.HeartbeatHandler{Clock: clk, StatusStore: engine}.HandleCall(ctx, chargeStationId, &types.HeartbeatJson{})
		require.NoError(t, err)
	}))
	handler.Handle(ctx, "cs001", &transport.Message{MessageType: transport.MessageTypeCall, Action: "Heartbeat", MessageId: "1"})
	assert.Equal(t, true, getStatus(t, r, "cs001")["connected"])

	tracker.HandleConnectionEvent(ctx, &transport.ConnectionEvent{
		Type:         transport.ConnectionEventDisconnected,
		StationKey:   "cs001",
		OcppVersion:  "ocpp1.6",
		ConnectionId: "conn-a",
		GatewayId:    "gateway-0",
		At:           connectedAt.Add(time.Second),
		CloseCode:    1000,
		Reason:       transport.DisconnectReasonClosed,
	})

	body := getStatus(t, r, "cs001")
	assert.Equal(t, false, body["connected"])
	assert.Equal(t, connectedAt.Add(time.Second).Format(time.RFC3339Nano), body["lastDisconnectedAt"])
}
