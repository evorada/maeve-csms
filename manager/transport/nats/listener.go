// SPDX-License-Identifier: Apache-2.0

package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"golang.org/x/exp/slog"
)

const (
	// DefaultQueueGroup is the queue group of the manager replicas: each message from a
	// charge station is handled by one of them.
	DefaultQueueGroup = "maeve-manager"
	// DefaultWorkers is how many messages a listener connection handles at the same time.
	DefaultWorkers = 16

	// workerQueueSize is the number of messages that wait for each worker. When it is
	// full the subscription waits too, and NATS reports a slow consumer if that lasts.
	workerQueueSize = 256
	// subscribeTimeout bounds the wait for the server to register a subscription.
	subscribeTimeout = 5 * time.Second
)

// Listener is an implementation of transport.Listener that receives the messages of
// charge stations from <prefix>.in.<ocpp16|ocpp201>.<org>.<station> with core NATS.
//
// The messages of one charge station are handled in the order they arrive, one at a
// time; messages of different charge stations are handled in parallel by a fixed
// number of workers.
type Listener struct {
	conn       *nats.Conn
	prefix     string
	queueGroup string
	workers    int
	tracer     trace.Tracer
}

// NewListener creates a Listener that subscribes on the shared connection.
func NewListener(conn *nats.Conn, opts ...Opt[Listener]) *Listener {
	l := &Listener{conn: conn}
	for _, opt := range opts {
		opt(l)
	}
	if l.prefix == "" {
		l.prefix = DefaultSubjectPrefix
	}
	if l.queueGroup == "" {
		l.queueGroup = DefaultQueueGroup
	}
	if l.workers <= 0 {
		l.workers = DefaultWorkers
	}
	if l.tracer == nil {
		l.tracer = noop.NewTracerProvider().Tracer("")
	}
	return l
}

// Connect subscribes to the messages of all charge stations (chargeStationId nil, in
// the queue group of the listener) or of one charge station (no queue group) for one
// OCPP version. It returns once the server has registered the subscription.
func (l *Listener) Connect(ctx context.Context, ocppVersion transport.OcppVersion, chargeStationId *string, handler transport.MessageHandler) (transport.Connection, error) {
	ver, err := VersionToken(ocppVersion)
	if err != nil {
		return nil, err
	}

	c := &connection{
		listener:    l,
		ocppVersion: ocppVersion,
		spanName:    fmt.Sprintf("%s.%s.%s receive", l.prefix, DirectionIn, ver),
		handler:     handler,
		queues:      make([]chan delivery, l.workers),
		abort:       make(chan struct{}),
	}
	for i := range c.queues {
		c.queues[i] = make(chan delivery, workerQueueSize)
		c.workers.Add(1)
		go c.work(c.queues[i])
	}

	if chargeStationId == nil {
		subject, err := InWildcard(l.prefix, ocppVersion)
		if err != nil {
			c.stop()
			return nil, err
		}
		c.subject, c.consumerId = subject, l.queueGroup
		c.sub, err = l.conn.QueueSubscribe(subject, l.queueGroup, c.receive)
		if err != nil {
			c.stop()
			return nil, fmt.Errorf("subscribing to %s in queue group %s: %w", subject, l.queueGroup, err)
		}
	} else {
		subject, err := InSubject(l.prefix, ocppVersion, *chargeStationId)
		if err != nil {
			c.stop()
			return nil, err
		}
		c.subject = subject
		c.sub, err = l.conn.Subscribe(subject, c.receive)
		if err != nil {
			c.stop()
			return nil, fmt.Errorf("subscribing to %s: %w", subject, err)
		}
	}

	flushCtx, cancel := context.WithTimeout(ctx, subscribeTimeout)
	defer cancel()
	err = l.conn.FlushWithContext(flushCtx)
	if err != nil {
		_ = c.Disconnect(context.Background())
		return nil, fmt.Errorf("registering the subscription to %s: %w", c.subject, err)
	}
	return c, nil
}

// delivery is a received message on its way to a worker.
type delivery struct {
	ctx        context.Context // carries the receive span
	span       trace.Span
	stationKey string
	msg        *transport.Message
}

// connection is one subscription and its workers.
type connection struct {
	listener    *Listener
	ocppVersion transport.OcppVersion
	spanName    string
	consumerId  string
	handler     transport.MessageHandler
	subject     string
	sub         *nats.Subscription

	// mu is held for reading while a message is put on a queue and for writing while
	// the queues are closed, so that nothing is sent on a closed queue.
	mu     sync.RWMutex
	closed bool
	queues []chan delivery
	// abort is closed when the connection stops: a message that waits for room in a
	// queue is then dropped instead of holding up the stop.
	abort    chan struct{}
	stopOnce sync.Once
	workers  sync.WaitGroup
}

// receive is the callback of the subscription. NATS calls it for one message at a
// time, in the order the messages arrive.
func (c *connection) receive(m *nats.Msg) {
	direction, version, stationKey, err := ParseStationSubject(m.Subject)
	if err == nil && (direction != DirectionIn || version != c.ocppVersion) {
		err = fmt.Errorf("subject %q is not an %s subject of %s", m.Subject, DirectionIn, c.ocppVersion)
	}
	if err != nil {
		slog.Warn("dropping message with an unexpected subject", "subject", m.Subject, "err", err)
		return
	}

	ctx := otel.GetTextMapPropagator().Extract(context.Background(), headerCarrier(m.Header))
	ctx, span := c.listener.tracer.Start(ctx, c.spanName,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			semconv.MessagingSystem("nats"),
			semconv.MessagingMessagePayloadSizeBytes(len(m.Data)),
			semconv.MessagingOperationKey.String("receive"),
			attribute.String("csId", stationKey),
		))
	if c.consumerId != "" {
		span.SetAttributes(semconv.MessagingConsumerID(c.consumerId))
	}

	var msg transport.Message
	err = json.Unmarshal(m.Data, &msg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "unable to unmarshal message")
		span.End()
		slog.Warn("unable to unmarshal message", "subject", m.Subject, "err", err)
		return
	}

	versionNumber, _ := strings.CutPrefix(string(c.ocppVersion), "ocpp")
	span.SetAttributes(
		attribute.String("ocpp.version", versionNumber),
		attribute.String(fmt.Sprintf("%s.action", msg.MessageType), msg.Action),
		semconv.MessagingMessageConversationID(msg.MessageId),
	)
	if msg.MessageType == transport.MessageTypeCallError {
		span.SetAttributes(
			attribute.String(fmt.Sprintf("%s.code", msg.MessageType), string(msg.ErrorCode)),
			attribute.String(fmt.Sprintf("%s.description", msg.MessageType), msg.ErrorDescription))
	}

	if !c.enqueue(delivery{ctx: ctx, span: span, stationKey: stationKey, msg: &msg}) {
		span.SetStatus(codes.Error, "listener disconnected")
		span.End()
	}
}

// enqueue puts a message on the queue of the worker of its charge station. It returns
// false when the connection has stopped and the message was dropped.
func (c *connection) enqueue(d delivery) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return false
	}
	select {
	case c.queues[workerIndex(d.stationKey, len(c.queues))] <- d:
		return true
	case <-c.abort:
		return false
	}
}

// workerIndex picks the worker of a charge station, so that its messages stay in order.
func workerIndex(stationKey string, workers int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(stationKey))
	return int(h.Sum32() % uint32(workers))
}

func (c *connection) work(queue <-chan delivery) {
	defer c.workers.Done()
	for d := range queue {
		c.handler.Handle(d.ctx, d.stationKey, d.msg)
		d.span.End()
	}
}

// stop closes the queues once: the workers handle what is queued and exit.
func (c *connection) stop() {
	c.stopOnce.Do(func() {
		close(c.abort)
		c.mu.Lock()
		c.closed = true
		for _, q := range c.queues {
			close(q)
		}
		c.mu.Unlock()
	})
}

// Disconnect removes the subscription, lets the workers handle the messages that are
// already queued and waits for them, at most until ctx is done.
func (c *connection) Disconnect(ctx context.Context) error {
	var unsubscribeErr error
	if c.sub != nil {
		err := c.sub.Unsubscribe()
		// the subscription is gone with a closed connection or a second Disconnect
		if err != nil && !errors.Is(err, nats.ErrConnectionClosed) && !errors.Is(err, nats.ErrBadSubscription) {
			unsubscribeErr = fmt.Errorf("unsubscribing from %s: %w", c.subject, err)
		}
	}
	c.stop()

	done := make(chan struct{})
	go func() {
		c.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return unsubscribeErr
	case <-ctx.Done():
		return errors.Join(unsubscribeErr, fmt.Errorf("waiting for the handlers of %s: %w", c.subject, ctx.Err()))
	}
}
