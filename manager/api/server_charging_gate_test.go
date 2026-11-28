// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The charge stations of A that may not charge: an OCPP 1.6 station that is pending
// (connected, so its commands go through the queue), a pending OCPP 2.0.1 station (its
// remote start and reservation are written to the store) and a decommissioned OCPP 1.6
// station.
const (
	pendingStationOfA      = "aaaa.NEW16"
	pending201StationOfA   = "aaaa.NEW201"
	retiredStationOfA      = "aaaa.OLD16"
	stationNotActiveAnswer = "station_not_active"
)

func newChargingGateFixture(t *testing.T) *commandsFixture {
	t.Helper()
	ctx := context.Background()
	f := newWrappersFixture(t)
	for station, version := range map[string]string{pendingStationOfA: "1.6", pending201StationOfA: "2.0.1"} {
		_, err := f.engine.RecordOnboardingConnection(ctx, station, orgA, store.OnboardingTransportWs,
			store.OnboardingCredentialOpen, 50, time.Now())
		require.NoError(t, err)
		require.NoError(t, f.engine.SetChargeStationRuntimeDetails(ctx, station, &store.ChargeStationRuntimeDetails{OcppVersion: version}))
		f.connected.set(station, true)
	}
	require.NoError(t, f.engine.SetChargeStationAuth(ctx, retiredStationOfA, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgA}))
	require.NoError(t, f.engine.SetChargeStationRuntimeDetails(ctx, retiredStationOfA, &store.ChargeStationRuntimeDetails{OcppVersion: "1.6"}))
	require.NoError(t, f.engine.DecommissionChargeStation(ctx, retiredStationOfA, nil))
	f.connected.set(retiredStationOfA, true)
	return f
}

func TestChargingCommandsAreRefusedForAStationThatMayNotCharge(t *testing.T) {
	requests := map[string]struct{ method, path, body string }{
		"remote start":                {http.MethodPost, "/cs/%s/start-transaction", `{"idTag":"DEADBEEF","connectorId":1}`},
		"reservation":                 {http.MethodPost, "/cs/%s/reservation", `{"reservationId":7,"connectorId":1,"idTag":"DEADBEEF","expiryDate":"2026-01-02T05:00:00Z"}`},
		"remote start as a command":   {http.MethodPost, "/cs/%s/commands", `{"action":"RemoteStartTransaction","payload":{"idTag":"DEADBEEF","connectorId":1}}`},
		"reservation as a command":    {http.MethodPost, "/cs/%s/commands", `{"action":"ReserveNow","payload":{"reservationId":7,"connectorId":1,"idTag":"DEADBEEF","expiryDate":"2026-01-02T05:00:00Z"}}`},
		"remote start as a 2.0.1 one": {http.MethodPost, "/cs/%s/commands", `{"action":"RequestStartTransaction","payload":{"idToken":{"idToken":"DEADBEEF","type":"ISO14443"},"remoteStartId":1}}`},
	}
	for _, station := range []string{pendingStationOfA, pending201StationOfA, retiredStationOfA} {
		for name, req := range requests {
			if station == pending201StationOfA && req.path == "/cs/%s/commands" && name != "remote start as a 2.0.1 one" {
				continue // not actions of OCPP 2.0.1
			}
			if station != pending201StationOfA && name == "remote start as a 2.0.1 one" {
				continue
			}
			t.Run(name+"/"+station, func(t *testing.T) {
				f := newChargingGateFixture(t)
				rr := f.call(commandCaller(), req.method, fmt.Sprintf(req.path, station), req.body)
				if name == "remote start as a 2.0.1 one" {
					// OCPP 2.0.1 has no command catalogue yet: refused whatever the state
					require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
					return
				}
				require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
				var answer api.ErrResponse
				require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &answer))
				assert.Contains(t, answer.ErrorText, stationNotActiveAnswer)

				assert.Zero(t, f.storedCommands(t), "nothing is queued")
				assert.Empty(t, f.sentCalls(), "nothing reaches the station")
				remoteStart, err := f.engine.GetRemoteStartTransactionRequest(context.Background(), station)
				require.NoError(t, err)
				assert.Nil(t, remoteStart, "nothing is written for the station")
				reservation, err := f.engine.GetReservation(context.Background(), 7)
				require.NoError(t, err)
				assert.Nil(t, reservation, "nothing is written for the station")
			})
		}
	}
}

func TestOtherCommandsAreSentToAPendingStation(t *testing.T) {
	f := newChargingGateFixture(t)
	rr := f.call(commandCaller(), http.MethodPost, "/cs/"+pendingStationOfA+"/commands", `{"action":"ChangeConfiguration","payload":{"key":"HeartbeatInterval","value":"60"}}`)
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	rr = f.call(commandCaller(), http.MethodPost, "/cs/"+pendingStationOfA+"/reset", `{"type":"Soft"}`)
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	assert.Equal(t, 2, f.storedCommands(t))
}

func TestChargingCommandsOfAnActiveStationAreSentAsBefore(t *testing.T) {
	for name, req := range map[string]struct{ path, body string }{
		"remote start":              {"/cs/%s/start-transaction", `{"idTag":"DEADBEEF","connectorId":1}`},
		"reservation":               {"/cs/%s/reservation", `{"reservationId":7,"connectorId":1,"idTag":"DEADBEEF","expiryDate":"2026-01-02T05:00:00Z"}`},
		"remote start as a command": {"/cs/%s/commands", `{"action":"RemoteStartTransaction","payload":{"idTag":"DEADBEEF","connectorId":1}}`},
	} {
		t.Run(name, func(t *testing.T) {
			f := newChargingGateFixture(t)
			rr := f.call(commandCaller(), http.MethodPost, fmt.Sprintf(req.path, stationOfA), req.body)
			require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
			assert.Equal(t, 1, f.storedCommands(t))
		})
	}

	t.Run("an OCPP 2.0.1 station that is active", func(t *testing.T) {
		f := newChargingGateFixture(t)
		rr := f.call(commandCaller(), http.MethodPost, "/cs/"+stationOfA201+"/start-transaction", `{"idTag":"DEADBEEF","connectorId":1}`)
		require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
		remoteStart, err := f.engine.GetRemoteStartTransactionRequest(context.Background(), stationOfA201)
		require.NoError(t, err)
		assert.NotNil(t, remoteStart)
	})
}
