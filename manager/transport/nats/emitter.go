// SPDX-License-Identifier: Apache-2.0

package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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

// Emitter is an implementation of transport.Emitter that publishes the messages for a
// charge station on <prefix>.out.<ocpp16|ocpp201>.<org>.<station> with core NATS. The
// trace context travels in the W3C headers of the message.
type Emitter struct {
	conn   *nats.Conn
	prefix string
	tracer trace.Tracer
}

// NewEmitter creates an Emitter that publishes on the shared connection.
func NewEmitter(conn *nats.Conn, opts ...Opt[Emitter]) *Emitter {
	e := &Emitter{conn: conn}
	for _, opt := range opts {
		opt(e)
	}
	if e.prefix == "" {
		e.prefix = DefaultSubjectPrefix
	}
	if e.tracer == nil {
		e.tracer = noop.NewTracerProvider().Tracer("")
	}
	return e
}

func (e *Emitter) Emit(ctx context.Context, ocppVersion transport.OcppVersion, chargeStationId string, message *transport.Message) error {
	ver, err := VersionToken(ocppVersion)
	if err != nil {
		return err
	}
	subject, err := OutSubject(e.prefix, ocppVersion, chargeStationId)
	if err != nil {
		return fmt.Errorf("subject for charge station %q: %w", chargeStationId, err)
	}

	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshalling message of type %s: %w", message.Action, err)
	}

	newCtx, span := e.tracer.Start(ctx,
		fmt.Sprintf("%s.%s.%s publish", e.prefix, DirectionOut, ver),
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			semconv.MessagingSystem("nats"),
			semconv.MessagingMessagePayloadSizeBytes(len(payload)),
			semconv.MessagingOperationKey.String("publish"),
			semconv.MessagingMessageConversationID(message.MessageId),
			attribute.String("csId", chargeStationId),
			attribute.String(actionAttribute(message), message.Action),
		))
	defer span.End()

	msg := &nats.Msg{Subject: subject, Data: payload, Header: nats.Header{}}
	otel.GetTextMapPropagator().Inject(newCtx, headerCarrier(msg.Header))

	err = e.conn.PublishMsg(msg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "publish failed")
		if errors.Is(err, nats.ErrMaxPayload) {
			slog.Warn("message is larger than the max payload of the NATS server: raise max_payload on the server",
				"subject", subject, "action", message.Action, "payload_bytes", len(payload), "max_payload_bytes", e.conn.MaxPayload())
		}
		return fmt.Errorf("publishing to %s: %w", subject, err)
	}
	return nil
}

func actionAttribute(msg *transport.Message) string {
	switch msg.MessageType {
	case transport.MessageTypeCall:
		return "call.action"
	case transport.MessageTypeCallResult:
		return "call_result.action"
	default:
		return "call_error.action"
	}
}
