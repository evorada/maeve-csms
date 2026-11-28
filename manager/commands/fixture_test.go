// SPDX-License-Identifier: Apache-2.0

package commands_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	testingclock "k8s.io/utils/clock/testing"
)

const (
	testOrgId   = "0193b2c4-0000-7000-8000-0000000000a1"
	testStation = "k7f2.CP001"
	// waitFor bounds every wait for something that happens in the background.
	waitFor = 5 * time.Second
	tick    = 5 * time.Millisecond
)

var testStart = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

// sentCall is one call that the dispatcher sent to a station.
type sentCall struct {
	version    transport.OcppVersion
	stationKey string
	msg        *transport.Message
}

// recordingEmitter records the messages sent to the stations; it fails with err when
// that is set.
type recordingEmitter struct {
	mu    sync.Mutex
	calls []sentCall
	err   error
}

func (e *recordingEmitter) Emit(_ context.Context, version transport.OcppVersion, stationKey string, msg *transport.Message) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return e.err
	}
	e.calls = append(e.calls, sentCall{version: version, stationKey: stationKey, msg: msg})
	return nil
}

func (e *recordingEmitter) setErr(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.err = err
}

func (e *recordingEmitter) sent() []sentCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]sentCall(nil), e.calls...)
}

// fakeConnectivity says which stations are connected.
type fakeConnectivity struct {
	mu        sync.Mutex
	connected map[string]bool
}

func (c *fakeConnectivity) IsConnected(_ context.Context, stationKey string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected[stationKey], nil
}

func (c *fakeConnectivity) set(stationKey string, connected bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected[stationKey] = connected
}

// fixture is a dispatcher, a service and a tracker on an in-memory store with a fake
// clock. The tracker hears about the answers that the dispatcher records.
type fixture struct {
	t          *testing.T
	clock      *testingclock.FakeClock
	store      *inmemory.Store
	events     *eventstest.Fixture
	emitter    *recordingEmitter
	conn       *fakeConnectivity
	dispatcher *commands.Dispatcher
	service    *commands.Service
	tracker    *commands.Tracker
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	clk := testingclock.NewFakeClock(testStart)
	engine := inmemory.NewStore(clk)
	evs := eventstest.New(engine, testOrgId, clk)
	emitter := &recordingEmitter{}
	conn := &fakeConnectivity{connected: map[string]bool{testStation: true}}
	config := commands.Config{
		ResponseTimeout:      30 * time.Second,
		ConnectDispatchDelay: 5 * time.Second,
		SweepInterval:        5 * time.Second,
		DefaultTTL:           time.Hour,
		MaxTTL:               168 * time.Hour,
		Retention:            90 * 24 * time.Hour,
		RemoteStartWindow:    5 * time.Minute,
		RemoteStopWindow:     2 * time.Minute,
		AvailabilityWindow:   2 * time.Minute,
	}
	tracker := &commands.Tracker{
		Store:  engine,
		Tx:     evs.Tx,
		Events: evs.Events,
		Clock:  clk,
		Config: config,
	}
	dispatcher := &commands.Dispatcher{
		Store:  engine,
		Tx:     evs.Tx,
		Events: evs.Events,
		Senders: map[transport.OcppVersion]handlers.RawCallMaker{
			transport.OcppVersion16: ocpp16.NewCallMaker(emitter),
		},
		Connectivity: conn,
		SchemaFS:     schemas.OcppSchemas,
		Clock:        clk,
		Config:       config,
		AfterResult:  []commands.ResultHook{tracker},
		AfterSweep:   []commands.Sweeper{tracker},
	}
	service := &commands.Service{
		Store:          engine,
		Tx:             evs.Tx,
		Events:         evs.Events,
		RuntimeDetails: engine,
		Dispatcher:     dispatcher,
		SchemaFS:       schemas.OcppSchemas,
		Clock:          clk,
		Config:         config,
	}
	require.NoError(t, engine.SetChargeStationRuntimeDetails(context.Background(), testStation,
		&store.ChargeStationRuntimeDetails{OcppVersion: "1.6"}))
	return &fixture{t: t, clock: clk, store: engine, events: evs, emitter: emitter, conn: conn,
		dispatcher: dispatcher, service: service, tracker: tracker}
}

// enqueue enqueues a command for the test station.
func (f *fixture) enqueue(action, payload string) *store.Command {
	f.t.Helper()
	cmd, err := f.service.Enqueue(context.Background(), &store.Command{
		OrgId:      testOrgId,
		StationKey: testStation,
		Action:     action,
		Payload:    json.RawMessage(payload),
		Actor:      ptr("user:alice"),
		Jti:        ptr("jti-1"),
	})
	require.NoError(f.t, err)
	return cmd
}

// get returns the stored command.
func (f *fixture) get(id string) *store.Command {
	f.t.Helper()
	cmd, err := f.store.GetCommand(context.Background(), id)
	require.NoError(f.t, err)
	require.NotNil(f.t, cmd)
	return cmd
}

// waitSent waits until n calls were sent and returns them.
func (f *fixture) waitSent(n int) []sentCall {
	f.t.Helper()
	require.Eventually(f.t, func() bool { return len(f.emitter.sent()) >= n }, waitFor, tick,
		"waiting for %d calls to be sent", n)
	sent := f.emitter.sent()
	require.Len(f.t, sent, n)
	return sent
}

// waitStatus waits until the command has the status.
func (f *fixture) waitStatus(id string, status store.CommandStatus) *store.Command {
	f.t.Helper()
	var cmd *store.Command
	require.Eventually(f.t, func() bool {
		var err error
		cmd, err = f.store.GetCommand(context.Background(), id)
		return err == nil && cmd != nil && cmd.Status == status
	}, waitFor, tick, "waiting for command %s to be %s", id, status)
	return cmd
}

// result is the CallResult of the station for cmd.
func result(cmd *store.Command, response string) *transport.Message {
	return &transport.Message{
		MessageType:     transport.MessageTypeCallResult,
		Action:          cmd.Action,
		MessageId:       cmd.Id,
		RequestPayload:  cmd.Payload,
		ResponsePayload: json.RawMessage(response),
	}
}

// callError is the CallError of the station for cmd.
func callError(cmd *store.Command, code transport.ErrorCode, description string) *transport.Message {
	return &transport.Message{
		MessageType:      transport.MessageTypeCallError,
		Action:           cmd.Action,
		MessageId:        cmd.Id,
		RequestPayload:   cmd.Payload,
		ErrorCode:        code,
		ErrorDescription: description,
	}
}

// completedEvents returns the command.completed events stored since the last call.
func (f *fixture) completedEvents() []*store.OutboxEvent {
	f.t.Helper()
	var completed []*store.OutboxEvent
	for _, ev := range f.events.Take(f.t) {
		if ev.Type == events.TypeCommandCompleted {
			completed = append(completed, ev)
		}
	}
	return completed
}

// assertCompletedEvent checks the one command.completed event of cmd.
func assertCompletedEvent(t *testing.T, ev *store.OutboxEvent, cmd *store.Command, late bool) {
	t.Helper()
	assert.Equal(t, events.TypeCommandCompleted, ev.Type)
	assert.Equal(t, cmd.OrgId, ev.OrgId)
	assert.Equal(t, "urn:cpms:"+cmd.OrgId+":command/"+cmd.Id, ev.Subject)
	assert.Equal(t, "user:alice", ev.Actor)
	require.NotNil(t, cmd.CompletedAt)
	assert.Equal(t, cmd.CompletedAt.UTC(), ev.Time)

	data := eventstest.Data[events.CommandCompletedData](t, ev)
	assert.Equal(t, cmd.Id, data.CommandId)
	assert.Equal(t, cmd.OrgId, data.OrgId)
	assert.Equal(t, cmd.StationKey, data.StationKey)
	assert.Equal(t, events.StationId(cmd.StationKey), data.StationId)
	assert.Equal(t, cmd.OcppVersion, data.OcppVersion)
	assert.Equal(t, cmd.Action, data.Action)
	assert.Equal(t, string(cmd.Status), data.Status)
	if cmd.Result == nil {
		assert.Nil(t, data.Result)
	} else {
		assert.JSONEq(t, string(cmd.Result), string(data.Result))
	}
	assert.Equal(t, deref(cmd.ErrorCode), data.ErrorCode)
	assert.Equal(t, deref(cmd.ErrorDescription), data.ErrorDescription)
	assert.Equal(t, cmd.Actor, data.Actor)
	assert.Equal(t, cmd.Jti, data.Jti)
	assert.Equal(t, cmd.CreatedAt.UTC(), data.CreatedAt)
	if cmd.SentAt == nil {
		assert.Nil(t, data.SentAt)
	} else {
		require.NotNil(t, data.SentAt)
		assert.Equal(t, cmd.SentAt.UTC(), *data.SentAt)
	}
	assert.Equal(t, cmd.CompletedAt.UTC(), data.CompletedAt)
	assert.Equal(t, late, data.Late)
}

func ptr[T any](v T) *T {
	return &v
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
