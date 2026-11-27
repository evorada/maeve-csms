// SPDX-License-Identifier: Apache-2.0

// Package nats is the transport of the gateway over core NATS. All station sockets
// share one connection: the messages of a charge station are published on
// <prefix>.in.<ocpp16|ocpp201>.<org token>.<station token> and the messages of the
// CSMS arrive on <prefix>.out.<ocpp16|ocpp201>.<org token>.<station token>, with the
// trace context in the W3C headers. The connection events of a station socket are
// published on <prefix>.conn.<org token>.<station token>. subjects.go defines how a
// station key becomes the two last tokens, as the manager does.
package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/thoughtworks/maeve-csms/gateway/pipe"
	"github.com/thoughtworks/maeve-csms/gateway/transport"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
)

const (
	// subscribeTimeout bounds the wait for the server to register the subscription of a
	// station socket.
	subscribeTimeout = 5 * time.Second
	// drainTimeout bounds the wait for the connection to drain when the transport closes.
	drainTimeout = 5 * time.Second
)

// Transport subscribes to the out subject of each station socket on a shared
// connection.
type Transport struct {
	conn   *nats.Conn
	prefix string
	tracer trace.Tracer
}

type Opt func(*Transport)

// WithSubjectPrefix sets the first token of the subjects (DefaultSubjectPrefix when
// empty). It must be one token of [A-Za-z0-9_-]: Open fails otherwise.
func WithSubjectPrefix(prefix string) Opt {
	return func(t *Transport) {
		t.prefix = prefix
	}
}

// WithTracer sets the tracer of the publish and receive spans.
func WithTracer(tracer trace.Tracer) Opt {
	return func(t *Transport) {
		t.tracer = tracer
	}
}

// New creates a transport on the connection; Close drains it.
func New(conn *nats.Conn, opts ...Opt) *Transport {
	t := &Transport{conn: conn}
	for _, opt := range opts {
		opt(t)
	}
	if t.prefix == "" {
		t.prefix = DefaultSubjectPrefix
	}
	if t.tracer == nil {
		t.tracer = trace.NewNoopTracerProvider().Tracer("")
	}
	return t
}

// Open subscribes to the out subject of the station and returns once the server has
// registered the subscription, so that the answer to the first message of the station
// cannot be missed. It waits for that at most 5 seconds, and not after ctx is done.
func (t *Transport) Open(ctx context.Context, stationKey, ocppProtocol string, deliver func(msg *pipe.GatewayMessage)) (transport.Session, error) {
	ver, err := VersionToken(ocppProtocol)
	if err != nil {
		return nil, err
	}
	outSubject, err := OutSubject(t.prefix, ocppProtocol, stationKey)
	if err != nil {
		return nil, fmt.Errorf("subject for station %q: %w", stationKey, err)
	}
	inSubject, err := InSubject(t.prefix, ocppProtocol, stationKey)
	if err != nil {
		return nil, fmt.Errorf("subject for station %q: %w", stationKey, err)
	}
	connSubject, err := ConnSubject(t.prefix, stationKey)
	if err != nil {
		return nil, fmt.Errorf("subject for station %q: %w", stationKey, err)
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String("nats.out_subject", outSubject),
		attribute.String("nats.in_subject", inSubject))

	receiveSpanName := fmt.Sprintf("%s.%s.%s receive", t.prefix, DirectionOut, ver)
	sub, err := t.conn.Subscribe(outSubject, func(m *nats.Msg) {
		var msg pipe.GatewayMessage
		err := json.Unmarshal(m.Data, &msg)
		if err != nil {
			slog.Warn("unmarshalling CSMS message", "subject", m.Subject, "err", err)
			return
		}

		requestContext := otel.GetTextMapPropagator().Extract(context.Background(), headerCarrier(m.Header))
		newCtx, span := t.tracer.Start(requestContext, receiveSpanName,
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithAttributes(
				semconv.MessagingSystem("nats"),
				semconv.MessagingMessagePayloadSizeBytes(len(m.Data)),
				semconv.MessagingMessageConversationID(msg.MessageId),
				semconv.MessagingOperationKey.String("receive"),
				attribute.String("csId", stationKey),
			))
		defer span.End()

		msg.Context = newCtx

		deliver(&msg)
	})
	if err != nil {
		return nil, fmt.Errorf("subscribing to %s: %w", outSubject, err)
	}

	flushCtx, cancel := context.WithTimeout(ctx, subscribeTimeout)
	defer cancel()
	err = t.conn.FlushWithContext(flushCtx)
	if err != nil {
		_ = sub.Unsubscribe()
		return nil, fmt.Errorf("registering the subscription to %s: %w", outSubject, err)
	}

	return &session{
		conn:        t.conn,
		sub:         sub,
		tracer:      t.tracer,
		inSubject:   inSubject,
		connSubject: connSubject,
		publishName: fmt.Sprintf("%s.%s.%s publish", t.prefix, DirectionIn, ver),
		stationKey:  stationKey,
	}, nil
}

// Close drains the connection: the subscriptions that are left stop after their
// pending messages and what was published is flushed. It waits for that at most 5
// seconds, then closes the connection. A connection that is not up is closed at once,
// and what it had not sent yet is lost.
func (t *Transport) Close() error {
	if t.conn.IsClosed() {
		return nil
	}
	closed := t.conn.StatusChanged(nats.CLOSED)
	if err := t.conn.Drain(); err != nil {
		switch {
		case errors.Is(err, nats.ErrConnectionClosed):
			return nil
		case errors.Is(err, nats.ErrConnectionReconnecting):
			// Drain has closed the connection
			return fmt.Errorf("closing the NATS connection: not connected, nothing drained: %w", err)
		default:
			return fmt.Errorf("draining the NATS connection: %w", err)
		}
	}
	timer := time.NewTimer(drainTimeout)
	defer timer.Stop()
	select {
	case <-closed:
		return nil
	case <-timer.C:
		t.conn.Close()
		return fmt.Errorf("draining the NATS connection: not done after %s, closed it", drainTimeout)
	}
}

type session struct {
	conn        *nats.Conn
	sub         *nats.Subscription
	tracer      trace.Tracer
	inSubject   string
	connSubject string
	publishName string
	stationKey  string
}

// Send publishes the message on the in subject of the station, with the trace context
// in the headers.
func (s *session) Send(ctx context.Context, msg *pipe.GatewayMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling message for publication: %w", err)
	}

	newCtx, span := s.tracer.Start(ctx, s.publishName,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			semconv.MessagingSystem("nats"),
			semconv.MessagingMessagePayloadSizeBytes(len(data)),
			semconv.MessagingOperationKey.String("publish"),
			semconv.MessagingMessageConversationID(msg.MessageId),
			attribute.String("csId", s.stationKey),
		))
	defer span.End()

	natsMsg := &nats.Msg{Subject: s.inSubject, Data: data, Header: nats.Header{}}
	otel.GetTextMapPropagator().Inject(newCtx, headerCarrier(natsMsg.Header))

	err = s.conn.PublishMsg(natsMsg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "publish failed")
		return fmt.Errorf("publishing to %s: %w", s.inSubject, err)
	}
	return nil
}

// SendConnectionEvent publishes the event on the connection event subject of the
// station. Like every publication on the shared connection it does not wait for the
// server: while the connection is reconnecting the event waits in the buffer of the
// connection.
func (s *session) SendConnectionEvent(_ context.Context, ev *transport.ConnectionEvent) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshaling connection event: %w", err)
	}
	if err := s.conn.Publish(s.connSubject, data); err != nil {
		return fmt.Errorf("publishing to %s: %w", s.connSubject, err)
	}
	return nil
}

// Done returns nil: the connection reconnects on its own and restores the
// subscription, so a session does not break down.
func (s *session) Done() <-chan struct{} {
	return nil
}

// Close removes the subscription: no message is delivered after it returns, except
// one whose delivery had already started.
func (s *session) Close(context.Context) error {
	err := s.sub.Unsubscribe()
	// the subscription is gone with the connection, or with an earlier Close
	if err != nil && !errors.Is(err, nats.ErrConnectionClosed) && !errors.Is(err, nats.ErrConnectionDraining) && !errors.Is(err, nats.ErrBadSubscription) {
		return fmt.Errorf("unsubscribing from %s: %w", s.sub.Subject, err)
	}
	return nil
}
