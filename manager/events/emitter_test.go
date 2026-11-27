// SPDX-License-Identifier: Apache-2.0

package events_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"golang.org/x/exp/slog"
	clocktest "k8s.io/utils/clock/testing"
)

const testOrgId = "0193b2c4-0000-7000-8000-0000000000a1"

var testNow = time.Date(2026, 11, 27, 10, 30, 0, 0, time.UTC)

func newEmitter(s store.OutboxStore) *events.OutboxEmitter {
	return &events.OutboxEmitter{
		Store:   s,
		Orgs:    events.StaticOrgResolver{OrgId: testOrgId},
		Clock:   clocktest.NewFakePassiveClock(testNow),
		Product: events.Product,
	}
}

// sessionEvent is a valid event of a charge station.
func sessionEvent() events.Event {
	return events.Event{
		Type:         "session.completed",
		StationKey:   "chrg01.cs001",
		ResourceType: "session",
		ResourceId:   "tx-1",
		Data:         map[string]any{"energy_wh": 1200},
	}
}

// outbox returns every event in the outbox of s, in Seq order.
func outbox(t *testing.T, s store.OutboxStore) []*store.OutboxEvent {
	t.Helper()
	var stored []*store.OutboxEvent
	_, err := s.RelayOutboxEvents(context.Background(), store.MaxOutboxEventListLimit,
		func(_ context.Context, events []*store.OutboxEvent) (int, error) {
			stored = append(stored, events...)
			return len(events), nil
		})
	require.NoError(t, err)
	return stored
}

func TestEmitFillsTheEnvelope(t *testing.T) {
	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	emitter := newEmitter(s)

	err := s.RunInTx(context.Background(), func(ctx context.Context) error {
		return emitter.Emit(ctx, sessionEvent())
	})
	require.NoError(t, err)

	stored := outbox(t, s)
	require.Len(t, stored, 1)
	ev := stored[0]

	id, err := uuid.Parse(ev.Id)
	require.NoError(t, err)
	assert.Equal(t, uuid.Version(7), id.Version())
	assert.Equal(t, ev.Id, id.String(), "the id should be in its canonical form")

	assert.Equal(t, testOrgId, ev.OrgId)
	assert.Equal(t, "session.completed", ev.Type)
	assert.Equal(t, "urn:cpms:"+testOrgId+":session/tx-1", ev.Subject)
	assert.Equal(t, "station:chrg01.cs001", ev.Actor)
	assert.Equal(t, "", ev.TraceParent, "the ctx has no span")
	assert.Equal(t, testNow, ev.Time)
	assert.JSONEq(t, `{"energy_wh":1200}`, string(ev.Data))
	assert.Equal(t, int64(1), ev.Seq)
	assert.Equal(t, testNow, ev.CreatedAt)
}

func TestEmitKeepsTheGivenOrgAndTime(t *testing.T) {
	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	emitter := newEmitter(s)
	// the resolver would fail: it must not be asked when the event has an org
	emitter.Orgs = events.StaticOrgResolver{}

	const orgId = "0193b2c4-0000-7000-8000-0000000000b2"
	happened := time.Date(2026, 11, 27, 11, 30, 0, 0, time.FixedZone("CET", 3600))
	ev := sessionEvent()
	ev.OrgId = orgId
	ev.Time = happened
	ev.Data = struct{}{}

	require.NoError(t, emitter.Emit(context.Background(), ev))

	stored := outbox(t, s)
	require.Len(t, stored, 1)
	assert.Equal(t, orgId, stored[0].OrgId)
	assert.Equal(t, "urn:cpms:"+orgId+":session/tx-1", stored[0].Subject)
	assert.Equal(t, happened.UTC(), stored[0].Time)
	assert.Equal(t, time.UTC, stored[0].Time.Location())
	assert.JSONEq(t, `{}`, string(stored[0].Data))
}

func TestEmitActorPrecedence(t *testing.T) {
	tests := map[string]struct {
		eventActor string
		ctxActor   string
		stationKey string
		want       string
	}{
		"the actor of the event comes first": {
			eventActor: "user:alice", ctxActor: "svc:billing", stationKey: "chrg01.cs001", want: "user:alice",
		},
		"then the actor of the ctx": {
			ctxActor: "svc:billing", stationKey: "chrg01.cs001", want: "svc:billing",
		},
		"then the charge station": {
			stationKey: "chrg01.cs001", want: "station:chrg01.cs001",
		},
		"then the manager": {
			want: events.DefaultActor,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
			ctx := context.Background()
			if tc.ctxActor != "" {
				ctx = events.WithActor(ctx, tc.ctxActor)
			}
			ev := sessionEvent()
			ev.Actor = tc.eventActor
			ev.StationKey = tc.stationKey
			ev.OrgId = testOrgId

			require.NoError(t, newEmitter(s).Emit(ctx, ev))

			stored := outbox(t, s)
			require.Len(t, stored, 1)
			assert.Equal(t, tc.want, stored[0].Actor)
		})
	}

	assert.Equal(t, "svc:maeve", events.DefaultActor)
}

func TestEmitCapturesTheTraceParent(t *testing.T) {
	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	provider := sdktrace.NewTracerProvider()
	defer func() { _ = provider.Shutdown(context.Background()) }()

	ctx, span := provider.Tracer("test").Start(context.Background(), "emit")
	err := newEmitter(s).Emit(ctx, sessionEvent())
	span.End()
	require.NoError(t, err)

	stored := outbox(t, s)
	require.Len(t, stored, 1)
	sc := span.SpanContext()
	assert.Equal(t, fmt.Sprintf("00-%s-%s-01", sc.TraceID(), sc.SpanID()), stored[0].TraceParent)
}

func TestEmitStoresWithTheTransaction(t *testing.T) {
	t.Run("a transaction that fails stores no event", func(t *testing.T) {
		s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
		errStop := errors.New("the change failed")

		err := s.RunInTx(context.Background(), func(ctx context.Context) error {
			require.NoError(t, newEmitter(s).Emit(ctx, sessionEvent()))
			return errStop
		})
		require.ErrorIs(t, err, errStop)

		assert.Empty(t, outbox(t, s))
	})

	t.Run("a transaction that succeeds stores the events in order", func(t *testing.T) {
		s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
		first, second := sessionEvent(), sessionEvent()
		second.Type = "session.updated"

		err := s.RunInTx(context.Background(), func(ctx context.Context) error {
			return newEmitter(s).Emit(ctx, first, second)
		})
		require.NoError(t, err)

		stored := outbox(t, s)
		require.Len(t, stored, 2)
		assert.Equal(t, "session.completed", stored[0].Type)
		assert.Equal(t, "session.updated", stored[1].Type)
		assert.NotEqual(t, stored[0].Id, stored[1].Id)
	})
}

func TestEmitRejectsInvalidEvents(t *testing.T) {
	tests := map[string]struct {
		change     func(ev *events.Event)
		orgs       events.OrgResolver
		noResolver bool
		wantErr    error
	}{
		"type without a dot":       {change: func(ev *events.Event) { ev.Type = "session" }},
		"type with a capital":      {change: func(ev *events.Event) { ev.Type = "Session.completed" }},
		"type with a hyphen":       {change: func(ev *events.Event) { ev.Type = "session.half-done" }},
		"type with an empty word":  {change: func(ev *events.Event) { ev.Type = "session..completed" }},
		"type with a wildcard":     {change: func(ev *events.Event) { ev.Type = "session.*" }},
		"empty type":               {change: func(ev *events.Event) { ev.Type = "" }},
		"no resource type":         {change: func(ev *events.Event) { ev.ResourceType = "" }},
		"no resource id":           {change: func(ev *events.Event) { ev.ResourceId = "" }},
		"no data":                  {change: func(ev *events.Event) { ev.Data = nil }},
		"data that is null":        {change: func(ev *events.Event) { ev.Data = (*struct{})(nil) }},
		"data that is not JSON":    {change: func(ev *events.Event) { ev.Data = make(chan int) }},
		"org id with a dot":        {change: func(ev *events.Event) { ev.OrgId = "org.a" }},
		"org id with a wildcard":   {change: func(ev *events.Event) { ev.OrgId = "org*" }},
		"org id that is too long":  {change: func(ev *events.Event) { ev.OrgId = strings.Repeat("a", 65) }},
		"org id from the resolver": {orgs: events.StaticOrgResolver{OrgId: "org a"}},
		"org that cannot be found": {orgs: events.StaticOrgResolver{}, wantErr: events.ErrUnknownOrg},
		"neither org nor station":  {change: func(ev *events.Event) { ev.StationKey = "" }, wantErr: events.ErrUnknownOrg},
		"resolver that fails":      {orgs: failingResolver{}, wantErr: errResolver},
		"emitter without resolver": {noResolver: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
			emitter := newEmitter(s)
			if tc.orgs != nil {
				emitter.Orgs = tc.orgs
			}
			if tc.noResolver {
				emitter.Orgs = nil
			}
			// the invalid event comes after a valid one: neither may be stored
			valid, invalid := sessionEvent(), sessionEvent()
			if tc.change != nil {
				tc.change(&invalid)
			}

			err := emitter.Emit(context.Background(), valid, invalid)
			require.Error(t, err)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			}

			assert.Empty(t, outbox(t, s), "no event of the call should be stored")
		})
	}
}

func TestEmitWithoutProductFails(t *testing.T) {
	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	emitter := newEmitter(s)
	emitter.Product = ""

	require.Error(t, emitter.Emit(context.Background(), sessionEvent()))
	assert.Empty(t, outbox(t, s))
}

func TestEmitNothing(t *testing.T) {
	s := &recordingStore{}
	require.NoError(t, newEmitter(s).Emit(context.Background()))
	assert.Equal(t, 0, s.calls, "no event, no store call")
}

func TestEmitStoresTheEventsOfOneCallTogether(t *testing.T) {
	s := &recordingStore{}
	ids := []string{"id-1", "id-2"}
	emitter := newEmitter(s)
	emitter.NewId = func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}

	require.NoError(t, emitter.Emit(context.Background(), sessionEvent(), sessionEvent()))

	assert.Equal(t, 1, s.calls)
	require.Len(t, s.appended, 2)
	assert.Equal(t, "id-1", s.appended[0].Id)
	assert.Equal(t, "id-2", s.appended[1].Id)
}

func TestEmitReturnsErrorsOfTheStore(t *testing.T) {
	errStore := errors.New("database is down")
	s := &recordingStore{err: errStore}

	err := newEmitter(s).Emit(context.Background(), sessionEvent())
	assert.ErrorIs(t, err, errStore)
}

func TestEmitWithoutOutboxDropsEventsAndWarnsOnce(t *testing.T) {
	var logs bytes.Buffer
	previous, previousOutput, previousFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer func() {
		// SetDefault also sends the output of the log package to the handler
		slog.SetDefault(previous)
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
	}()

	s := &recordingStore{err: fmt.Errorf("AppendOutboxEvents: %w", store.ErrNotImplemented)}
	emitter := newEmitter(s)

	require.NoError(t, emitter.Emit(context.Background(), sessionEvent()))
	require.NoError(t, emitter.Emit(context.Background(), sessionEvent()))

	assert.Equal(t, 2, s.calls)
	assert.Equal(t, 1, strings.Count(logs.String(), "level=WARN"), logs.String())
	assert.Contains(t, logs.String(), "domain events are dropped")
}

func TestNopEmitter(t *testing.T) {
	var emitter events.Emitter = events.NopEmitter{}
	assert.NoError(t, emitter.Emit(context.Background(), events.Event{}))
}

func TestURN(t *testing.T) {
	assert.Equal(t, "urn:cpms:"+testOrgId+":connector/chrg01.cs001#1",
		events.URN("cpms", testOrgId, "connector", "chrg01.cs001#1"))
}

func TestActorFrom(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, "", events.ActorFrom(ctx))
	assert.Equal(t, "user:alice", events.ActorFrom(events.WithActor(ctx, "user:alice")))
}

var errResolver = errors.New("resolver failed")

type failingResolver struct{}

func (failingResolver) OrgIdForStation(context.Context, string) (string, error) {
	return "", errResolver
}

// recordingStore is an outbox that records what is appended to it, or fails with err.
type recordingStore struct {
	store.OutboxStore
	err      error
	calls    int
	appended []*store.OutboxEvent
}

func (s *recordingStore) AppendOutboxEvents(_ context.Context, events ...*store.OutboxEvent) error {
	s.calls++
	if s.err != nil {
		return s.err
	}
	s.appended = append(s.appended, events...)
	return nil
}
