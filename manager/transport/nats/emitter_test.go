// SPDX-License-Identifier: Apache-2.0

package nats_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	natstransport "github.com/thoughtworks/maeve-csms/manager/transport/nats"
	"github.com/thoughtworks/maeve-csms/manager/transport/nats/natstest"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestEmitterPublishesTheMessageWithTraceContext(t *testing.T) {
	tracer, exporter := testutil.GetTracer()
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)

	sub, err := conn.SubscribeSync("ocpp.out.ocpp16.k7f2.CP001")
	require.NoError(t, err)
	require.NoError(t, conn.Flush())

	emitter := natstransport.NewEmitter(conn, natstransport.WithOtelTracer[natstransport.Emitter](tracer))
	ctx, parent := tracer.Start(context.Background(), "test span")
	msg := &transport.Message{
		MessageType:     transport.MessageTypeCallResult,
		Action:          "Heartbeat",
		MessageId:       "my-message-id",
		ResponsePayload: json.RawMessage(`{"currentTime":"2026-11-27T10:00:00Z"}`),
	}
	require.NoError(t, emitter.Emit(ctx, transport.OcppVersion16, "k7f2.CP001", msg))
	parent.End()

	got, err := sub.NextMsg(waitTimeout)
	require.NoError(t, err)
	want, err := json.Marshal(msg)
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(got.Data))

	// the W3C header name, as other NATS clients read it
	traceparent := got.Header.Get("traceparent")
	require.NotEmpty(t, traceparent)
	assert.Empty(t, got.Header.Get("Traceparent"))
	remote := trace.SpanContextFromContext(propagation.TraceContext{}.Extract(context.Background(),
		propagation.MapCarrier{"traceparent": traceparent}))
	assert.Equal(t, parent.SpanContext().TraceID(), remote.TraceID())

	found := false
	for _, span := range exporter.GetSpans() {
		if span.Name == "ocpp.out.ocpp16 publish" {
			found = true
			assert.Equal(t, trace.SpanKindProducer, span.SpanKind)
			assert.Equal(t, parent.SpanContext().SpanID(), span.Parent.SpanID())
			assert.Equal(t, remote.SpanID(), span.SpanContext.SpanID(), "the header carries the publish span")
			testutil.AssertAttributes(t, span.Attributes, map[string]any{
				"messaging.system":                     "nats",
				"messaging.operation":                  "publish",
				"messaging.message.payload_size_bytes": len(want),
				"messaging.message.conversation_id":    "my-message-id",
				"csId":                                 "k7f2.CP001",
				"call_result.action":                   "Heartbeat",
			})
		}
	}
	assert.True(t, found, "no publish span")
}

func TestEmitterEncodesTheStationKey(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)

	sub, err := conn.SubscribeSync("ocpp.out.ocpp201.*.*")
	require.NoError(t, err)
	require.NoError(t, conn.Flush())

	emitter := natstransport.NewEmitter(conn)
	for _, v := range stationKeyVectors {
		require.NoError(t, emitter.Emit(context.Background(), transport.OcppVersion201, v.key, call(v.key)))

		got, err := sub.NextMsg(waitTimeout)
		require.NoError(t, err)
		assert.Equal(t, "ocpp.out.ocpp201."+v.orgToken+"."+v.stationToken, got.Subject)
		direction, version, key, err := natstransport.ParseStationSubject(got.Subject)
		require.NoError(t, err)
		assert.Equal(t, natstransport.DirectionOut, direction)
		assert.Equal(t, transport.OcppVersion201, version)
		assert.Equal(t, v.key, key)
	}
}

func TestEmitterUsesTheSubjectPrefix(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)

	sub, err := conn.SubscribeSync("csms.out.ocpp16._.CP001")
	require.NoError(t, err)
	require.NoError(t, conn.Flush())

	emitter := natstransport.NewEmitter(conn, natstransport.WithSubjectPrefix[natstransport.Emitter]("csms"))
	require.NoError(t, emitter.Emit(context.Background(), transport.OcppVersion16, "CP001", call("1")))
	_, err = sub.NextMsg(waitTimeout)
	require.NoError(t, err)
}

func TestEmitterFailsOnAClosedConnection(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)
	conn.Close()

	emitter := natstransport.NewEmitter(conn)
	err := emitter.Emit(context.Background(), transport.OcppVersion16, "k7f2.CP001", call("1"))
	require.Error(t, err)
	assert.ErrorIs(t, err, nats.ErrConnectionClosed)
}

func TestEmitterRefusesBadInput(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)

	emitter := natstransport.NewEmitter(conn)
	assert.Error(t, emitter.Emit(context.Background(), "ocpp2.1", "k7f2.CP001", call("1")))
	assert.Error(t, emitter.Emit(context.Background(), transport.OcppVersion16, "", call("1")))
}

func TestEmitterLogsAndReturnsAnErrorForAMessageAboveTheMaxPayload(t *testing.T) {
	logs := captureLogs(t)
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)
	require.Eventually(t, conn.IsConnected, waitTimeout, 10*time.Millisecond)

	sub, err := conn.SubscribeSync("ocpp.out.ocpp16.k7f2.CP001")
	require.NoError(t, err)
	require.NoError(t, conn.Flush())

	emitter := natstransport.NewEmitter(conn)
	big := &transport.Message{
		MessageType:     transport.MessageTypeCallResult,
		Action:          "GetConfiguration",
		MessageId:       "big",
		ResponsePayload: json.RawMessage(`"` + strings.Repeat("x", int(conn.MaxPayload())) + `"`),
	}
	err = emitter.Emit(context.Background(), transport.OcppVersion16, "k7f2.CP001", big)
	require.Error(t, err)
	assert.ErrorIs(t, err, nats.ErrMaxPayload)
	assert.Contains(t, logs.String(), "max payload")
	assert.Contains(t, logs.String(), "subject=ocpp.out.ocpp16.k7f2.CP001")

	// the connection is still usable
	require.True(t, conn.IsConnected())
	require.NoError(t, emitter.Emit(context.Background(), transport.OcppVersion16, "k7f2.CP001", call("small")))
	got, err := sub.NextMsg(waitTimeout)
	require.NoError(t, err)
	assert.Contains(t, string(got.Data), `"small"`)
}
