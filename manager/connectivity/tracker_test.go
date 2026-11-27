// SPDX-License-Identifier: Apache-2.0

package connectivity_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/connectivity"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"golang.org/x/exp/slog"
	clockTest "k8s.io/utils/clock/testing"
)

const stationKey = "k7f2.CP001"

const orgId = "0193b2c4-0000-7000-8000-0000000000a1"

var t0 = time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

// countingStore counts the calls to a ConnectivityStore and can make them fail.
type countingStore struct {
	store.ConnectivityStore
	err error

	mu      sync.Mutex
	touches []time.Time
	calls   int
	// beforeCall runs at the start of every call, before the store is used
	beforeCall func()
}

func (s *countingStore) call() error {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.beforeCall != nil {
		s.beforeCall()
	}
	return s.err
}

func (s *countingStore) RecordStationConnected(ctx context.Context, chargeStationId, connectionId string, at time.Time) (bool, error) {
	if err := s.call(); err != nil {
		return false, err
	}
	return s.ConnectivityStore.RecordStationConnected(ctx, chargeStationId, connectionId, at)
}

func (s *countingStore) RecordStationDisconnected(ctx context.Context, chargeStationId, connectionId string, at time.Time) (bool, error) {
	if err := s.call(); err != nil {
		return false, err
	}
	return s.ConnectivityStore.RecordStationDisconnected(ctx, chargeStationId, connectionId, at)
}

func (s *countingStore) TouchStationLastSeen(ctx context.Context, chargeStationId string, at time.Time) error {
	if err := s.call(); err != nil {
		return err
	}
	s.mu.Lock()
	s.touches = append(s.touches, at)
	s.mu.Unlock()
	return s.ConnectivityStore.TouchStationLastSeen(ctx, chargeStationId, at)
}

func (s *countingStore) DisconnectStaleStations(ctx context.Context, seenBefore, now time.Time) ([]string, error) {
	if err := s.call(); err != nil {
		return nil, err
	}
	return s.ConnectivityStore.DisconnectStaleStations(ctx, seenBefore, now)
}

func (s *countingStore) touchCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.touches)
}

type fixture struct {
	clock   *clockTest.FakePassiveClock
	engine  *inmemory.Store
	store   *countingStore
	events  *eventstest.Fixture
	tracker *connectivity.Tracker
}

func newFixture() *fixture {
	clk := clockTest.NewFakePassiveClock(t0)
	engine := inmemory.NewStore(clk)
	counting := &countingStore{ConnectivityStore: engine}
	domainEvents := eventstest.New(engine, orgId, clk)
	return &fixture{
		clock:  clk,
		engine: engine,
		store:  counting,
		events: domainEvents,
		tracker: &connectivity.Tracker{
			Store:          counting,
			Tx:             domainEvents.Tx,
			Events:         domainEvents.Events,
			Clock:          clk,
			SeenWriteEvery: 30 * time.Second,
			StaleAfter:     5 * time.Minute,
		},
	}
}

func (f *fixture) event(eventType, connectionId string, at time.Time) {
	f.tracker.HandleConnectionEvent(context.Background(), &transport.ConnectionEvent{
		Type:         eventType,
		StationKey:   stationKey,
		OcppVersion:  "ocpp1.6",
		ConnectionId: connectionId,
		GatewayId:    "gateway-0",
		At:           at,
		RemoteAddr:   "192.0.2.10:52314",
	})
}

func (f *fixture) disconnected(connectionId string, at time.Time) {
	f.tracker.HandleConnectionEvent(context.Background(), &transport.ConnectionEvent{
		Type:         transport.ConnectionEventDisconnected,
		StationKey:   stationKey,
		OcppVersion:  "ocpp1.6",
		ConnectionId: connectionId,
		GatewayId:    "gateway-0",
		At:           at,
		CloseCode:    1000,
		Reason:       transport.DisconnectReasonClosed,
	})
}

func (f *fixture) status(t *testing.T) *store.ChargeStationStatus {
	t.Helper()
	status, err := f.engine.GetChargeStationStatus(context.Background(), stationKey)
	require.NoError(t, err)
	return status
}

// captureLogs sends the default logger to a buffer until the test ends.
func captureLogs(t *testing.T) *bytes.Buffer {
	var logs bytes.Buffer
	previous, previousOutput, previousFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		// SetDefault also sends the output of the log package to the handler
		slog.SetDefault(previous)
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
	})
	return &logs
}

func TestConnectionEventsAreRecorded(t *testing.T) {
	f := newFixture()

	f.event(transport.ConnectionEventConnected, "conn-a", t0)
	status := f.status(t)
	assert.True(t, status.Connected)
	assert.Equal(t, t0, *status.LastConnectedAt)
	assert.Equal(t, t0, *status.LastSeenAt)
	assert.Equal(t, "conn-a", *status.ConnectionId)

	f.event(transport.ConnectionEventAlive, "conn-a", t0.Add(2*time.Minute))
	status = f.status(t)
	assert.True(t, status.Connected)
	assert.Equal(t, t0.Add(2*time.Minute), *status.LastSeenAt)

	f.disconnected("conn-a", t0.Add(3*time.Minute))
	status = f.status(t)
	assert.False(t, status.Connected)
	assert.Equal(t, t0.Add(3*time.Minute), *status.LastDisconnectedAt)
}

func TestEventsOfASupersededSocketDoNotFlipTheState(t *testing.T) {
	f := newFixture()
	logs := captureLogs(t)

	f.event(transport.ConnectionEventConnected, "conn-a", t0)
	f.event(transport.ConnectionEventConnected, "conn-b", t0.Add(time.Minute))
	// the old socket is reported closed after the station reconnected
	f.disconnected("conn-a", t0.Add(30*time.Second))
	// and its connected event is delivered again, late
	f.event(transport.ConnectionEventConnected, "conn-a", t0)

	status := f.status(t)
	assert.True(t, status.Connected)
	assert.Equal(t, "conn-b", *status.ConnectionId)
	assert.Equal(t, t0.Add(time.Minute), *status.LastConnectedAt)
	assert.Nil(t, status.LastDisconnectedAt)
	assert.Contains(t, logs.String(), "ignored disconnected event")
	assert.Contains(t, logs.String(), "ignored connected event")
}

func TestUnknownConnectionEventTypeIsIgnored(t *testing.T) {
	f := newFixture()
	logs := captureLogs(t)
	f.event(transport.ConnectionEventConnected, "conn-a", t0)
	calls := f.store.calls

	f.event("reconnected", "conn-a", t0.Add(time.Minute))

	assert.Equal(t, calls, f.store.calls, "the store is not called")
	assert.True(t, f.status(t).Connected)
	assert.Contains(t, logs.String(), "level=WARN")
	assert.Contains(t, logs.String(), "type=reconnected")
}

func TestWrapHandlerCallsNextAndThrottlesTheWrites(t *testing.T) {
	f := newFixture()
	_, err := f.engine.RecordStationConnected(context.Background(), stationKey, "conn-a", t0.Add(-time.Hour))
	require.NoError(t, err)

	var handled []string
	next := transport.MessageHandlerFunc(func(_ context.Context, chargeStationId string, message *transport.Message) {
		handled = append(handled, chargeStationId+"/"+message.MessageId)
	})
	handler := f.tracker.WrapHandler(next)
	handle := func(chargeStationId, messageId string) {
		handler.Handle(context.Background(), chargeStationId, &transport.Message{
			MessageType: transport.MessageTypeCall,
			Action:      "Heartbeat",
			MessageId:   messageId,
		})
	}

	handle(stationKey, "1")
	assert.Equal(t, 1, f.store.touchCount())
	assert.Equal(t, t0, *f.status(t).LastSeenAt)

	f.clock.SetTime(t0.Add(29 * time.Second))
	handle(stationKey, "2")
	assert.Equal(t, 1, f.store.touchCount(), "two messages within 30s: one write")

	// another station has its own interval
	handle("k7f2.CP002", "3")
	assert.Equal(t, 2, f.store.touchCount())

	f.clock.SetTime(t0.Add(30 * time.Second))
	handle(stationKey, "4")
	assert.Equal(t, 3, f.store.touchCount())
	assert.Equal(t, t0.Add(30*time.Second), *f.status(t).LastSeenAt)

	assert.Equal(t, []string{stationKey + "/1", stationKey + "/2", "k7f2.CP002/3", stationKey + "/4"}, handled)
}

func TestWrapHandlerCallsNextBeforeTheStoreAndEvenWhenItFails(t *testing.T) {
	f := newFixture()
	logs := captureLogs(t)
	f.store.err = errors.New("database is down")

	var handled int
	f.store.beforeCall = func() {
		assert.Equal(t, 1, handled, "next has handled the message before the store is called")
	}
	handler := f.tracker.WrapHandler(transport.MessageHandlerFunc(func(context.Context, string, *transport.Message) {
		handled++
	}))

	handler.Handle(context.Background(), stationKey, &transport.Message{MessageType: transport.MessageTypeCall, Action: "Heartbeat", MessageId: "1"})

	assert.Equal(t, 1, handled)
	assert.Equal(t, 1, f.store.calls)
	assert.Contains(t, logs.String(), "level=ERROR")
	assert.Contains(t, logs.String(), "database is down")
}

func TestAliveCountsAsAWrite(t *testing.T) {
	f := newFixture()
	f.event(transport.ConnectionEventConnected, "conn-a", t0.Add(-time.Hour))
	f.clock.SetTime(t0.Add(time.Hour))
	f.event(transport.ConnectionEventAlive, "conn-a", t0.Add(time.Hour))
	touches := f.store.touchCount()

	f.tracker.WrapHandler(transport.MessageHandlerFunc(func(context.Context, string, *transport.Message) {})).
		Handle(context.Background(), stationKey, &transport.Message{MessageType: transport.MessageTypeCall, Action: "Heartbeat", MessageId: "1"})

	assert.Equal(t, touches, f.store.touchCount(), "no write right after an alive event")
}

func TestDisconnectedForgetsTheThrottle(t *testing.T) {
	f := newFixture()
	handler := f.tracker.WrapHandler(transport.MessageHandlerFunc(func(context.Context, string, *transport.Message) {}))
	message := &transport.Message{MessageType: transport.MessageTypeCall, Action: "Heartbeat", MessageId: "1"}

	f.event(transport.ConnectionEventConnected, "conn-a", t0)
	f.disconnected("conn-a", t0)
	handler.Handle(context.Background(), stationKey, message)
	assert.Equal(t, 1, f.store.touchCount(), "the first message after a disconnection is written")
}

func TestSweepMarksAStationWithoutAliveDisconnected(t *testing.T) {
	f := newFixture()
	// two stations connect; only one keeps sending alive events
	f.event(transport.ConnectionEventConnected, "conn-a", t0)
	f.tracker.HandleConnectionEvent(context.Background(), &transport.ConnectionEvent{
		Type: transport.ConnectionEventConnected, StationKey: "k7f2.CP002", ConnectionId: "conn-b", At: t0,
	})
	alive := func(at time.Time) {
		f.tracker.HandleConnectionEvent(context.Background(), &transport.ConnectionEvent{
			Type: transport.ConnectionEventAlive, StationKey: "k7f2.CP002", ConnectionId: "conn-b", At: at,
		})
	}
	alive(t0.Add(2 * time.Minute))
	alive(t0.Add(4 * time.Minute))

	f.clock.SetTime(t0.Add(5 * time.Minute))
	require.NoError(t, f.tracker.SweepStale(context.Background()))
	assert.True(t, f.status(t).Connected, "exactly stale_after without a sign of life")

	logs := captureLogs(t)
	now := t0.Add(5*time.Minute + time.Second)
	f.clock.SetTime(now)
	require.NoError(t, f.tracker.SweepStale(context.Background()))

	status := f.status(t)
	assert.False(t, status.Connected)
	assert.Equal(t, now, *status.LastDisconnectedAt)
	other, err := f.engine.GetChargeStationStatus(context.Background(), "k7f2.CP002")
	require.NoError(t, err)
	assert.True(t, other.Connected)
	assert.Contains(t, logs.String(), stationKey)
	assert.NotContains(t, logs.String(), "k7f2.CP002")
}

func TestSweepReturnsStoreErrors(t *testing.T) {
	f := newFixture()
	f.store.err = errors.New("database is down")
	assert.ErrorContains(t, f.tracker.SweepStale(context.Background()), "database is down")
}

func TestSweepJob(t *testing.T) {
	f := newFixture()
	f.event(transport.ConnectionEventConnected, "conn-a", t0)
	job := f.tracker.SweepJob(45 * time.Second)

	assert.Equal(t, "connectivity-sweep", job.Name)
	assert.Equal(t, 45*time.Second, job.Interval)
	require.NoError(t, jobs.Validate([]jobs.Job{job}))

	f.clock.SetTime(t0.Add(time.Hour))
	require.NoError(t, job.Run(context.Background()))
	assert.False(t, f.status(t).Connected)
}

func TestTrackerDefaults(t *testing.T) {
	clk := clockTest.NewFakePassiveClock(t0)
	engine := inmemory.NewStore(clk)
	counting := &countingStore{ConnectivityStore: engine}
	tracker := &connectivity.Tracker{Store: counting, Tx: engine, Events: events.NopEmitter{}, Clock: clk}
	ctx := context.Background()
	_, err := engine.RecordStationConnected(ctx, stationKey, "conn-a", t0)
	require.NoError(t, err)

	handler := tracker.WrapHandler(transport.MessageHandlerFunc(func(context.Context, string, *transport.Message) {}))
	message := &transport.Message{MessageType: transport.MessageTypeCall, Action: "Heartbeat", MessageId: "1"}
	handler.Handle(ctx, stationKey, message)
	clk.SetTime(t0.Add(connectivity.DefaultSeenWriteEvery - time.Second))
	handler.Handle(ctx, stationKey, message)
	assert.Equal(t, 1, counting.touchCount(), "SeenWriteEvery 0 is the default")

	clk.SetTime(t0.Add(connectivity.DefaultStaleAfter))
	require.NoError(t, tracker.SweepStale(ctx))
	status, err := engine.GetChargeStationStatus(ctx, stationKey)
	require.NoError(t, err)
	assert.True(t, status.Connected, "StaleAfter 0 is the default")
}

// notImplementedStore is the connectivity store of a backend that does not track
// connectivity.
type notImplementedStore struct {
	calls int
}

func (s *notImplementedStore) RecordStationConnected(context.Context, string, string, time.Time) (bool, error) {
	s.calls++
	return false, fmt.Errorf("RecordStationConnected: %w", store.ErrNotImplemented)
}

func (s *notImplementedStore) RecordStationDisconnected(context.Context, string, string, time.Time) (bool, error) {
	s.calls++
	return false, fmt.Errorf("RecordStationDisconnected: %w", store.ErrNotImplemented)
}

func (s *notImplementedStore) TouchStationLastSeen(context.Context, string, time.Time) error {
	s.calls++
	return fmt.Errorf("TouchStationLastSeen: %w", store.ErrNotImplemented)
}

func (s *notImplementedStore) DisconnectStaleStations(context.Context, time.Time, time.Time) ([]string, error) {
	s.calls++
	return nil, fmt.Errorf("DisconnectStaleStations: %w", store.ErrNotImplemented)
}

func TestStoreWithoutConnectivityWarnsOnce(t *testing.T) {
	logs := captureLogs(t)
	s := &notImplementedStore{}
	tracker := &connectivity.Tracker{Store: s, Tx: inmemory.NewStore(clockTest.NewFakePassiveClock(t0)), Events: events.NopEmitter{},
		Clock: clockTest.NewFakePassiveClock(t0)}
	ctx := context.Background()

	for _, eventType := range []string{transport.ConnectionEventConnected, transport.ConnectionEventAlive, transport.ConnectionEventDisconnected} {
		tracker.HandleConnectionEvent(ctx, &transport.ConnectionEvent{Type: eventType, StationKey: stationKey, ConnectionId: "conn-a", At: t0})
	}
	handled := false
	tracker.WrapHandler(transport.MessageHandlerFunc(func(context.Context, string, *transport.Message) { handled = true })).
		Handle(ctx, stationKey, &transport.Message{MessageType: transport.MessageTypeCall, Action: "Heartbeat", MessageId: "1"})
	assert.NoError(t, tracker.SweepStale(ctx))

	assert.True(t, handled)
	assert.Equal(t, 5, s.calls)
	assert.Equal(t, 1, strings.Count(logs.String(), "level=WARN"), logs.String())
	assert.NotContains(t, logs.String(), "level=ERROR")
	assert.Contains(t, logs.String(), "not tracked")
}
