// SPDX-License-Identifier: Apache-2.0

package connectivity_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	"github.com/thoughtworks/maeve-csms/manager/transport"
)

func TestAppliedConnectEmitsStationConnected(t *testing.T) {
	f := newFixture()

	at := time.Date(2026, 11, 27, 11, 0, 0, 0, time.FixedZone("CET", 3600))
	f.event(transport.ConnectionEventConnected, "conn-a", at)

	evs := f.events.Take(t)
	require.Len(t, evs, 1)
	ev := evs[0]
	assert.Equal(t, "station.connected", ev.Type)
	assert.Equal(t, orgId, ev.OrgId)
	assert.Equal(t, "urn:cpms:"+orgId+":station/"+stationKey, ev.Subject)
	assert.Equal(t, "station:"+stationKey, ev.Actor)
	assert.Equal(t, at.UTC(), ev.Time, "the time of the gateway")
	assert.Equal(t, events.StationConnectedData{
		StationKey:   stationKey,
		StationId:    "CP001",
		OcppVersion:  "ocpp1.6",
		ConnectionId: "conn-a",
		ConnectedAt:  at.UTC(),
	}, eventstest.Data[events.StationConnectedData](t, ev))
	assert.Contains(t, string(ev.Data), `"connected_at":"2026-11-27T10:00:00Z"`)
}

func TestConnectEventsThatDoNotApplyEmitNothing(t *testing.T) {
	f := newFixture()
	f.event(transport.ConnectionEventConnected, "conn-b", t0)
	require.Len(t, f.events.Take(t), 1)

	// an older socket's late connected event and the alive events of the current one
	f.event(transport.ConnectionEventConnected, "conn-a", t0.Add(-time.Minute))
	f.event(transport.ConnectionEventAlive, "conn-b", t0.Add(time.Minute))
	assert.Empty(t, f.events.Take(t))
	assert.Equal(t, "conn-b", *f.status(t).ConnectionId)
}

func TestAppliedDisconnectEmitsStationDisconnected(t *testing.T) {
	f := newFixture()
	f.event(transport.ConnectionEventConnected, "conn-a", t0)
	f.events.Take(t)

	f.disconnected("conn-a", t0.Add(time.Minute))

	evs := f.events.Take(t)
	require.Len(t, evs, 1)
	assert.Equal(t, "station.disconnected", evs[0].Type)
	assert.Equal(t, "station:"+stationKey, evs[0].Actor)
	assert.Equal(t, t0.Add(time.Minute), evs[0].Time)
	assert.Equal(t, events.StationDisconnectedData{
		StationKey:     stationKey,
		StationId:      "CP001",
		ConnectionId:   "conn-a",
		DisconnectedAt: t0.Add(time.Minute),
		Reason:         "closed",
		CloseCode:      1000,
	}, eventstest.Data[events.StationDisconnectedData](t, evs[0]))
}

func TestDisconnectReasonOfTheGatewayAndItsDefault(t *testing.T) {
	for _, tc := range []struct {
		reason, want string
		closeCode    int
	}{
		{reason: transport.DisconnectReasonPingTimeout, want: "ping_timeout", closeCode: 1008},
		{reason: transport.DisconnectReasonGatewayShutdown, want: "gateway_shutdown", closeCode: 1001},
		{reason: "", want: "closed", closeCode: -1},
	} {
		f := newFixture()
		f.event(transport.ConnectionEventConnected, "conn-a", t0)
		f.events.Take(t)

		f.tracker.HandleConnectionEvent(context.Background(), &transport.ConnectionEvent{
			Type: transport.ConnectionEventDisconnected, StationKey: stationKey, ConnectionId: "conn-a",
			At: t0.Add(time.Second), CloseCode: tc.closeCode, Reason: tc.reason,
		})

		evs := f.events.Take(t)
		require.Len(t, evs, 1, tc.reason)
		data := eventstest.Data[events.StationDisconnectedData](t, evs[0])
		assert.Equal(t, tc.want, data.Reason)
		assert.Equal(t, tc.closeCode, data.CloseCode)
	}
}

func TestSupersededDisconnectEmitsNothing(t *testing.T) {
	f := newFixture()
	f.event(transport.ConnectionEventConnected, "conn-a", t0)
	f.event(transport.ConnectionEventConnected, "conn-b", t0.Add(time.Second))
	f.events.Take(t)

	// the old socket closes after the station reconnected
	f.disconnected("conn-a", t0.Add(2*time.Second))

	assert.Empty(t, f.events.Take(t))
	assert.True(t, f.status(t).Connected)
}

func TestSweepEmitsStationDisconnectedPerStaleStation(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	for _, key := range []string{"k7f2.CP002", "k7f2.CP001", "k7f2.CP003"} {
		_, err := f.engine.RecordStationConnected(ctx, key, "conn-"+key, t0)
		require.NoError(t, err)
	}
	// CP003 is still alive
	require.NoError(t, f.engine.TouchStationLastSeen(ctx, "k7f2.CP003", t0.Add(4*time.Minute)))

	sweptAt := t0.Add(5*time.Minute + time.Second)
	f.clock.SetTime(sweptAt)
	require.NoError(t, f.tracker.SweepStale(ctx))

	evs := f.events.Take(t)
	require.Len(t, evs, 2)
	for i, key := range []string{"k7f2.CP001", "k7f2.CP002"} {
		assert.Equal(t, "station.disconnected", evs[i].Type)
		assert.Equal(t, "urn:cpms:"+orgId+":station/"+key, evs[i].Subject)
		assert.Equal(t, "svc:maeve", evs[i].Actor, "the manager marked the station")
		assert.Equal(t, sweptAt, evs[i].Time)
		assert.Equal(t, events.StationDisconnectedData{
			StationKey:     key,
			StationId:      strings.TrimPrefix(key, "k7f2."),
			DisconnectedAt: sweptAt,
			Reason:         "stale",
		}, eventstest.Data[events.StationDisconnectedData](t, evs[i]))
	}
	assert.NotContains(t, string(evs[0].Data), "connection_id")

	// a second sweep finds nothing
	require.NoError(t, f.tracker.SweepStale(ctx))
	assert.Empty(t, f.events.Take(t))
}

func TestFailingEventsAreLoggedAndNeverReachTheCaller(t *testing.T) {
	logs := captureLogs(t)
	f := newFixture()
	f.events.Events.Err = errors.New("outbox unavailable")

	f.event(transport.ConnectionEventConnected, "conn-a", t0)
	f.disconnected("conn-a", t0.Add(time.Second))
	f.clock.SetTime(t0.Add(time.Hour))
	_, err := f.engine.RecordStationConnected(context.Background(), "k7f2.CP009", "conn-x", t0)
	require.NoError(t, err)
	err = f.tracker.SweepStale(context.Background())

	require.Error(t, err, "the job runner logs the error of the sweep")
	assert.ErrorContains(t, err, "outbox unavailable")
	assert.Empty(t, f.events.Take(t))
	assert.Equal(t, 2, strings.Count(logs.String(), "level=ERROR"), logs.String())
	assert.Contains(t, logs.String(), "outbox unavailable")
}

func TestEventsOfAFailedTransactionAreDropped(t *testing.T) {
	f := newFixture()
	f.events.Tx.CommitErr = errors.New("commit failed")

	f.event(transport.ConnectionEventConnected, "conn-a", t0)

	assert.Empty(t, f.events.Take(t))
}
