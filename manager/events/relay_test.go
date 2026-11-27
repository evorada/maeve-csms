// SPDX-License-Identifier: Apache-2.0

package events_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	clocktest "k8s.io/utils/clock/testing"
)

// publishCall is one message that the fake publisher was asked to send.
type publishCall struct {
	subject string
	msgId   string
	headers map[string]string
	body    []byte
}

// fakePublisher records what it is asked to publish. fail, when set, decides for each
// call whether it fails (calls are numbered from 1).
type fakePublisher struct {
	mu    sync.Mutex
	calls []publishCall
	fail  func(call int) error
}

func (p *fakePublisher) Publish(_ context.Context, subject, msgId string, headers map[string]string, body []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, publishCall{subject: subject, msgId: msgId, headers: headers, body: body})
	if p.fail != nil {
		return p.fail(len(p.calls))
	}
	return nil
}

func (p *fakePublisher) msgIds() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := make([]string, len(p.calls))
	for i, c := range p.calls {
		ids[i] = c.msgId
	}
	return ids
}

var errPublish = errors.New("publish failed")

// appendEvents stores n events in the outbox and returns them in Seq order. Every
// other event has a traceparent.
func appendEvents(t *testing.T, s store.OutboxStore, n int) []*store.OutboxEvent {
	t.Helper()
	evs := make([]*store.OutboxEvent, n)
	for i := range evs {
		evs[i] = &store.OutboxEvent{
			Id:      fmt.Sprintf("0193b2c4-0000-7000-8000-%012d", i+1),
			OrgId:   testOrgId,
			Type:    "session.updated",
			Subject: fmt.Sprintf("urn:cpms:%s:session/tx-%d", testOrgId, i+1),
			Actor:   "station:chrg01.cs001",
			Time:    testNow.Add(time.Duration(i) * time.Second),
			Data:    json.RawMessage(fmt.Sprintf(`{"n":%d}`, i+1)),
		}
		if i%2 == 0 {
			evs[i].TraceParent = fmt.Sprintf("00-4bf92f3577b34da6a3ce929d0e0e%04x-00f067aa0ba902b7-01", i+1)
		}
	}
	require.NoError(t, s.AppendOutboxEvents(context.Background(), evs...))
	return evs
}

// publishedEvents returns the published events of the outbox in publish order.
func publishedEvents(t *testing.T, s store.OutboxStore) []*store.OutboxEvent {
	t.Helper()
	evs, err := s.ListOutboxEvents(context.Background(), store.OutboxEventFilter{Limit: store.MaxOutboxEventListLimit})
	require.NoError(t, err)
	return evs
}

func ids(evs []*store.OutboxEvent) []string {
	ids := make([]string, len(evs))
	for i, ev := range evs {
		ids[i] = ev.Id
	}
	return ids
}

// counterValue reads a counter of the default registry, which /metrics serves.
func counterValue(t *testing.T, name string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() == name {
			require.Len(t, family.GetMetric(), 1)
			return family.GetMetric()[0].GetCounter().GetValue()
		}
	}
	t.Fatalf("metric %s is not registered", name)
	return 0
}

func newRelay(s store.OutboxStore, p events.Publisher) *events.Relay {
	return &events.Relay{
		Store:         s,
		Publisher:     p,
		SubjectPrefix: "cpms",
		Product:       events.Product,
	}
}

func TestRelayPublishesInSeqOrder(t *testing.T) {
	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	evs := appendEvents(t, s, 3)
	publisher := &fakePublisher{}

	require.NoError(t, newRelay(s, publisher).RunOnce(context.Background()))

	require.Len(t, publisher.calls, 3)
	for i, call := range publisher.calls {
		ev := evs[i]
		assert.Equal(t, ev.Id, call.msgId, "the message id is the event id")
		assert.Equal(t, "cpms.session.updated."+testOrgId, call.subject)
		wantBody, err := json.Marshal(events.ToCloudEvent(ev, events.Product))
		require.NoError(t, err)
		assert.Equal(t, string(wantBody), string(call.body))

		wantHeaders := map[string]string{"Content-Type": "application/cloudevents+json"}
		if ev.TraceParent != "" {
			wantHeaders["traceparent"] = ev.TraceParent
		}
		assert.Equal(t, wantHeaders, call.headers)
	}

	published := publishedEvents(t, s)
	assert.Equal(t, ids(evs), ids(published), "every event is marked published, in Seq order")
}

func TestRelayStopsAtTheFirstFailedPublish(t *testing.T) {
	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	evs := appendEvents(t, s, 3)
	publishedBefore := counterValue(t, "maeve_outbox_published_total")
	errorsBefore := counterValue(t, "maeve_outbox_publish_errors_total")

	publisher := &fakePublisher{fail: func(call int) error {
		if call == 2 {
			return errPublish
		}
		return nil
	}}
	relay := newRelay(s, publisher)

	err := relay.RunOnce(context.Background())
	require.ErrorIs(t, err, errPublish)
	assert.Equal(t, []string{evs[0].Id, evs[1].Id}, publisher.msgIds(), "nothing is published after the failure")
	assert.Equal(t, []string{evs[0].Id}, ids(publishedEvents(t, s)), "only the acknowledged prefix is marked")

	// the next run continues at the event that failed
	publisher.fail = nil
	require.NoError(t, relay.RunOnce(context.Background()))
	assert.Equal(t, []string{evs[0].Id, evs[1].Id, evs[1].Id, evs[2].Id}, publisher.msgIds())
	assert.Equal(t, ids(evs), ids(publishedEvents(t, s)))

	assert.Equal(t, 3.0, counterValue(t, "maeve_outbox_published_total")-publishedBefore)
	assert.Equal(t, 1.0, counterValue(t, "maeve_outbox_publish_errors_total")-errorsBefore)
}

func TestRelayFailureOfTheFirstEventMarksNothing(t *testing.T) {
	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	appendEvents(t, s, 2)
	publisher := &fakePublisher{fail: func(int) error { return errPublish }}

	err := newRelay(s, publisher).RunOnce(context.Background())
	require.ErrorIs(t, err, errPublish)
	assert.Len(t, publisher.calls, 1)
	assert.Empty(t, publishedEvents(t, s))
}

func TestRelayPublishesBatchesUntilOneIsNotFull(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")

	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	evs := appendEvents(t, s, 250)
	publisher := &fakePublisher{}
	relay := newRelay(s, publisher)
	relay.BatchSize = 100
	relay.Tracer = tracer

	require.NoError(t, relay.RunOnce(context.Background()))
	assert.Equal(t, ids(evs), publisher.msgIds(), "one run publishes every event, in order")
	assert.Len(t, publishedEvents(t, s), 250)

	spans := recorder.Ended()
	require.Len(t, spans, 3, "one span per batch")
	for i, want := range []int{100, 100, 50} {
		attrs := attribute.NewSet(spans[i].Attributes()...)
		count, _ := attrs.Value("messaging.batch.message_count")
		assert.Equal(t, int64(want), count.AsInt64())
		published, _ := attrs.Value("outbox.published_count")
		assert.Equal(t, int64(want), published.AsInt64())
	}
}

func TestRelayStopsAfterMaxBatches(t *testing.T) {
	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	evs := appendEvents(t, s, 250)
	publisher := &fakePublisher{}
	relay := newRelay(s, publisher)
	relay.BatchSize = 100
	relay.MaxBatches = 1

	require.NoError(t, relay.RunOnce(context.Background()))
	assert.Equal(t, ids(evs[:100]), publisher.msgIds())
	assert.Equal(t, ids(evs[:100]), ids(publishedEvents(t, s)))
}

func TestRelayDefaults(t *testing.T) {
	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	appendEvents(t, s, events.DefaultRelayBatchSize*events.DefaultRelayMaxBatches+1)
	publisher := &fakePublisher{}

	relay := &events.Relay{Store: s, Publisher: publisher}
	require.NoError(t, relay.RunOnce(context.Background()))

	require.Len(t, publisher.calls, events.DefaultRelayBatchSize*events.DefaultRelayMaxBatches)
	assert.Equal(t, "cpms.session.updated."+testOrgId, publisher.calls[0].subject)
	var ce events.CloudEvent
	require.NoError(t, json.Unmarshal(publisher.calls[0].body, &ce))
	assert.Equal(t, "cpms.session.updated", ce.Type)
	assert.Equal(t, "urn:cpms", ce.Source)
}

func TestRelayWithAnEmptyOutbox(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	publisher := &fakePublisher{}
	relay := newRelay(s, publisher)
	relay.Tracer = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")

	require.NoError(t, relay.RunOnce(context.Background()))
	assert.Empty(t, publisher.calls)
	assert.Empty(t, recorder.Ended())
}

func TestRelayReturnsWhileAnotherRelayPublishes(t *testing.T) {
	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	appendEvents(t, s, 2)

	inside, release := make(chan struct{}), make(chan struct{})
	done := make(chan error)
	go func() {
		_, err := s.RelayOutboxEvents(context.Background(), 1, func(context.Context, []*store.OutboxEvent) (int, error) {
			close(inside)
			<-release
			return 1, nil
		})
		done <- err
	}()
	<-inside

	publisher := &fakePublisher{}
	require.NoError(t, newRelay(s, publisher).RunOnce(context.Background()))
	assert.Empty(t, publisher.calls, "the batch of the other relay holds the lock")

	close(release)
	require.NoError(t, <-done)
}

func TestRelayStopsWhenTheContextEnds(t *testing.T) {
	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	appendEvents(t, s, 5)
	publisher := &fakePublisher{}
	relay := newRelay(s, publisher)
	relay.BatchSize = 1

	ctx, cancel := context.WithCancel(context.Background())
	publisher.fail = func(call int) error {
		if call == 2 {
			cancel()
		}
		return nil
	}
	require.NoError(t, relay.RunOnce(ctx))
	assert.Len(t, publisher.calls, 2, "no batch starts after the context ended")
	assert.Len(t, publishedEvents(t, s), 2)
}

func TestRelaySpanLinksTheProducers(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	evs := appendEvents(t, s, 3)
	publisher := &fakePublisher{fail: func(call int) error {
		if call == 3 {
			return errPublish
		}
		return nil
	}}
	relay := newRelay(s, publisher)
	relay.Tracer = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")

	require.ErrorIs(t, relay.RunOnce(context.Background()), errPublish)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	span := spans[0]
	assert.Equal(t, "outbox publish", span.Name())
	assert.Equal(t, trace.SpanKindProducer, span.SpanKind())
	assert.Equal(t, "Error", span.Status().Code.String())

	attrs := attribute.NewSet(span.Attributes()...)
	count, _ := attrs.Value("messaging.batch.message_count")
	assert.Equal(t, int64(3), count.AsInt64())
	published, _ := attrs.Value("outbox.published_count")
	assert.Equal(t, int64(2), published.AsInt64())

	// the 1st and 3rd events have a traceparent
	var linked []string
	for _, link := range span.Links() {
		linked = append(linked, link.SpanContext.TraceID().String())
	}
	assert.Equal(t, []string{evs[0].TraceParent[3:35], evs[2].TraceParent[3:35]}, linked)
}

func TestRelayJob(t *testing.T) {
	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	evs := appendEvents(t, s, 2)
	publisher := &fakePublisher{}

	job := newRelay(s, publisher).Job(time.Second)
	assert.Equal(t, "outbox-relay", job.Name)
	assert.Equal(t, events.RelayJobName, job.Name)
	assert.Equal(t, time.Second, job.Interval)
	require.NoError(t, jobs.Validate([]jobs.Job{job}))

	require.NoError(t, job.Run(context.Background()))
	assert.Equal(t, ids(evs), publisher.msgIds())
}

func TestPurgeJob(t *testing.T) {
	t0 := testNow
	storeClock := clocktest.NewFakePassiveClock(t0)
	s := inmemory.NewStore(storeClock)
	relay := newRelay(s, &fakePublisher{})

	early := appendEvents(t, s, 1)[0]
	require.NoError(t, relay.RunOnce(context.Background())) // published at t0
	storeClock.SetTime(t0.Add(time.Hour))
	late := &store.OutboxEvent{
		Id: "0193b2c4-0000-7000-8000-0000000000f1", OrgId: testOrgId, Type: "session.updated",
		Subject: "urn:cpms:" + testOrgId + ":session/tx-f1", Actor: "svc:test", Time: t0, Data: json.RawMessage(`{}`),
	}
	require.NoError(t, s.AppendOutboxEvents(context.Background(), late))
	require.NoError(t, relay.RunOnce(context.Background())) // published at t0+1h
	unpublished := &store.OutboxEvent{
		Id: "0193b2c4-0000-7000-8000-0000000000f2", OrgId: testOrgId, Type: "session.updated",
		Subject: "urn:cpms:" + testOrgId + ":session/tx-f2", Actor: "svc:test", Time: t0, Data: json.RawMessage(`{}`),
	}
	require.NoError(t, s.AppendOutboxEvents(context.Background(), unpublished))
	require.Equal(t, []string{early.Id, late.Id}, ids(publishedEvents(t, s)))

	// now - retention = t0+1h: the event published at t0 goes, the one published at
	// exactly t0+1h stays
	job := events.PurgeJob(s, clocktest.NewFakePassiveClock(t0.Add(2*time.Hour)), time.Hour, 10*time.Minute)
	assert.Equal(t, "outbox-purge", job.Name)
	assert.Equal(t, events.PurgeJobName, job.Name)
	assert.Equal(t, 10*time.Minute, job.Interval)
	require.NoError(t, jobs.Validate([]jobs.Job{job}))

	require.NoError(t, job.Run(context.Background()))
	assert.Equal(t, []string{late.Id}, ids(publishedEvents(t, s)))

	// the unpublished event is still waiting for the relay
	publisher := &fakePublisher{}
	require.NoError(t, newRelay(s, publisher).RunOnce(context.Background()))
	assert.Equal(t, []string{unpublished.Id}, publisher.msgIds())
}

// failingPurgeStore is an outbox whose purge fails.
type failingPurgeStore struct {
	store.OutboxStore
}

func (failingPurgeStore) PurgePublishedOutboxEvents(context.Context, time.Time) (int64, error) {
	return 0, store.ErrNotImplemented
}

func TestPurgeJobReturnsTheStoreError(t *testing.T) {
	job := events.PurgeJob(failingPurgeStore{}, clocktest.NewFakePassiveClock(testNow), time.Hour, time.Hour)
	assert.ErrorIs(t, job.Run(context.Background()), store.ErrNotImplemented)
}
