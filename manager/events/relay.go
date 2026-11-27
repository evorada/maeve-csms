// SPDX-License-Identifier: Apache-2.0

package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

const (
	// RelayJobName is the name of the job that Relay.Job returns.
	RelayJobName = "outbox-relay"
	// PurgeJobName is the name of the job that PurgeJob returns.
	PurgeJobName = "outbox-purge"
	// DefaultRelayBatchSize is the number of events of one batch when Relay.BatchSize
	// is 0 or less.
	DefaultRelayBatchSize = 100
	// DefaultRelayMaxBatches is the number of batches of one run when Relay.MaxBatches
	// is 0 or less.
	DefaultRelayMaxBatches = 50
)

var (
	publishedEvents = promauto.NewCounter(prometheus.CounterOpts{
		Name: "maeve_outbox_published_total",
		Help: "Outbox events published by the relay and acknowledged by the server.",
	})
	publishErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "maeve_outbox_publish_errors_total",
		Help: "Publish errors of the outbox relay; each one stops a batch.",
	})
)

// Relay publishes the events of the outbox in order, as CloudEvents JSON on
// "<SubjectPrefix>.<type>.<org_id>" with the event id as message id.
//
// An event is marked published only after the server acknowledged it. A publish that
// fails stops the batch: the events before it are marked, it and the later ones stay
// in the outbox for the next run, so no event overtakes an earlier one.
type Relay struct {
	Store         store.OutboxStore
	Publisher     Publisher
	SubjectPrefix string // DefaultSubjectPrefix when empty
	Product       string // Product when empty
	BatchSize     int    // DefaultRelayBatchSize when 0 or less
	MaxBatches    int    // per run, DefaultRelayMaxBatches when 0 or less
	Tracer        trace.Tracer
}

// RunOnce publishes batches until a batch is not full, MaxBatches is reached or ctx
// ends. Another replica that is publishing a batch makes it return at once.
func (r *Relay) RunOnce(ctx context.Context) error {
	batchSize := r.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultRelayBatchSize
	}
	maxBatches := r.MaxBatches
	if maxBatches <= 0 {
		maxBatches = DefaultRelayMaxBatches
	}

	for range maxBatches {
		// stopping between two batches loses nothing: the next run continues
		if ctx.Err() != nil {
			return nil
		}
		n, err := r.Store.RelayOutboxEvents(ctx, batchSize, r.publishBatch)
		if err != nil {
			return fmt.Errorf("relay outbox events: %w", err)
		}
		if n < batchSize {
			return nil
		}
	}
	return nil
}

// Job returns the job that runs RunOnce every interval.
func (r *Relay) Job(interval time.Duration) jobs.Job {
	return jobs.Job{
		Name:     RelayJobName,
		Interval: interval,
		Run:      r.RunOnce,
	}
}

// publishBatch is the store.OutboxPublishFunc of the relay, in one producer span that
// links the spans that produced the events.
func (r *Relay) publishBatch(ctx context.Context, batch []*store.OutboxEvent) (int, error) {
	ctx, span := r.tracer().Start(ctx, "outbox publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithLinks(producerLinks(batch)...),
		trace.WithAttributes(attribute.Int("messaging.batch.message_count", len(batch))))
	defer span.End()

	published, err := r.publishInOrder(ctx, batch)
	span.SetAttributes(attribute.Int("outbox.published_count", published))
	publishedEvents.Add(float64(published))
	if err != nil {
		publishErrors.Inc()
		span.RecordError(err)
		span.SetStatus(codes.Error, "publish failed")
	}
	return published, err
}

// publishInOrder publishes the events one after the other and stops at the first one
// that fails. It returns how many were published.
func (r *Relay) publishInOrder(ctx context.Context, batch []*store.OutboxEvent) (int, error) {
	product := r.Product
	if product == "" {
		product = Product
	}
	prefix := r.SubjectPrefix
	if prefix == "" {
		prefix = DefaultSubjectPrefix
	}

	for i, ev := range batch {
		body, err := json.Marshal(ToCloudEvent(ev, product))
		if err != nil {
			return i, fmt.Errorf("marshal event %s: %w", ev.Id, err)
		}
		headers := map[string]string{"Content-Type": CloudEventContentType}
		if ev.TraceParent != "" {
			headers["traceparent"] = ev.TraceParent
		}
		if err := r.Publisher.Publish(ctx, Subject(prefix, ev), ev.Id, headers, body); err != nil {
			return i, err
		}
	}
	return len(batch), nil
}

func (r *Relay) tracer() trace.Tracer {
	if r.Tracer == nil {
		return noop.NewTracerProvider().Tracer("")
	}
	return r.Tracer
}

// producerLinks returns a link to the span that produced each event that has one.
func producerLinks(batch []*store.OutboxEvent) []trace.Link {
	var links []trace.Link
	for _, ev := range batch {
		if ev.TraceParent == "" {
			continue
		}
		ctx := propagation.TraceContext{}.Extract(context.Background(),
			propagation.MapCarrier{"traceparent": ev.TraceParent})
		if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
			links = append(links, trace.Link{SpanContext: sc})
		}
	}
	return links
}

// PurgeJob returns the job that deletes, every interval, the events that were published
// more than retention ago.
func PurgeJob(s store.OutboxStore, clk clock.PassiveClock, retention, interval time.Duration) jobs.Job {
	return jobs.Job{
		Name:     PurgeJobName,
		Interval: interval,
		Run: func(ctx context.Context) error {
			publishedBefore := clk.Now().Add(-retention)
			n, err := s.PurgePublishedOutboxEvents(ctx, publishedBefore)
			if n > 0 {
				slog.Info("purged published outbox events", "count", n, "published_before", publishedBefore)
			}
			if err != nil {
				return fmt.Errorf("purge published outbox events: %w", err)
			}
			return nil
		},
	}
}
