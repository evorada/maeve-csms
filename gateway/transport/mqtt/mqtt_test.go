// SPDX-License-Identifier: Apache-2.0

package mqtt_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	mochi "github.com/mochi-co/mqtt/v2"
	"github.com/mochi-co/mqtt/v2/hooks/auth"
	"github.com/mochi-co/mqtt/v2/listeners"
	"github.com/mochi-co/mqtt/v2/packets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/gateway/ocpp"
	"github.com/thoughtworks/maeve-csms/gateway/pipe"
	"github.com/thoughtworks/maeve-csms/gateway/transport"
	"github.com/thoughtworks/maeve-csms/gateway/transport/mqtt"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/goleak"
)

// teardownTimeout is how long the end of a test waits for the test broker.
const teardownTimeout = 10 * time.Second

func TestSessionReceivesOnTheOutTopicOfTheStation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	useTraceContext(t)
	tracer, exporter := newTracer()

	hook := newRecorder()
	broker, addr := startBroker(t, new(auth.AllowHook), hook)

	tr := mqtt.New(mqtt.WithBrokerURLs(addr), mqtt.WithTopicPrefix("cs"), mqtt.WithTracer(tracer))
	delivered := make(chan *pipe.GatewayMessage, 1)
	session, err := tr.Open(ctx, "aaaa.CP001", "ocpp1.6", func(msg *pipe.GatewayMessage) {
		delivered <- msg
	})
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, session.Close(ctx))
	}()

	// the station key is the client id and names the topic
	assert.Equal(t, brokerEvent{clientId: "aaaa.CP001", topic: "cs/out/ocpp1.6/aaaa.CP001"}, await(ctx, t, hook.subscribed))

	// the CSMS replies in a trace of its own, which it passes as correlation data
	csmsCtx, csmsSpan := tracer.Start(context.Background(), "csms")
	csmsSpan.End()
	correlation := make(map[string]string)
	otel.GetTextMapPropagator().Inject(csmsCtx, propagation.MapCarrier(correlation))
	correlationData, err := json.Marshal(correlation)
	require.NoError(t, err)
	payload, err := json.Marshal(pipe.GatewayMessage{
		MessageType:     ocpp.MessageTypeCallResult,
		MessageId:       "1",
		ResponsePayload: json.RawMessage(`{"status":"Accepted"}`),
	})
	require.NoError(t, err)
	require.NoError(t, broker.InjectPacket(broker.NewClient(nil, "local", "csms", true), packets.Packet{
		FixedHeader: packets.FixedHeader{Type: packets.Publish},
		TopicName:   "cs/out/ocpp1.6/aaaa.CP001",
		Payload:     payload,
		Properties:  packets.Properties{CorrelationData: correlationData},
	}))

	msg := await(ctx, t, delivered)
	assert.Equal(t, ocpp.MessageTypeCallResult, msg.MessageType)
	assert.Equal(t, "1", msg.MessageId)
	assert.JSONEq(t, `{"status":"Accepted"}`, string(msg.ResponsePayload))

	// the message carries a consumer span in the trace of the CSMS
	receive := trace.SpanFromContext(msg.Context).SpanContext()
	assert.Equal(t, csmsSpan.SpanContext().TraceID(), receive.TraceID())
	span := awaitSpan(t, exporter, receive.SpanID())
	assert.Equal(t, "cs/out/ocpp1.6/# receive", span.Name)
	assert.Equal(t, trace.SpanKindConsumer, span.SpanKind)
	assert.Equal(t, csmsSpan.SpanContext().SpanID(), span.Parent.SpanID())

	select {
	case <-session.Done():
		t.Error("the session is done")
	default:
	}
}

func TestSessionSendsOnTheInTopicOfTheStation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	useTraceContext(t)
	tracer, exporter := newTracer()

	hook := newRecorder()
	_, addr := startBroker(t, new(auth.AllowHook), hook)

	tr := mqtt.New(mqtt.WithBrokerURLs(addr), mqtt.WithTopicPrefix("cs"), mqtt.WithTracer(tracer))
	session, err := tr.Open(ctx, "aaaa.CP001", "ocpp1.6", func(msg *pipe.GatewayMessage) {
		t.Errorf("unexpected message for the station: %+v", msg)
	})
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, session.Close(ctx))
	}()
	await(ctx, t, hook.subscribed)

	stationCtx, stationSpan := tracer.Start(context.Background(), "station")
	err = session.Send(stationCtx, &pipe.GatewayMessage{
		Context:        stationCtx,
		MessageType:    ocpp.MessageTypeCall,
		Action:         "Heartbeat",
		MessageId:      "7",
		RequestPayload: json.RawMessage(`{}`),
	})
	stationSpan.End()
	require.NoError(t, err)

	published := await(ctx, t, hook.published)
	assert.Equal(t, "aaaa.CP001", published.clientId)
	assert.Equal(t, "cs/in/ocpp1.6/aaaa.CP001", published.packet.TopicName)
	assert.Equal(t, "cs/out/ocpp1.6/aaaa.CP001", published.packet.Properties.ResponseTopic)
	assert.Equal(t, "application/json", published.packet.Properties.ContentType)
	assert.JSONEq(t, `{"type":2,"action":"Heartbeat","id":"7","request":{}}`, string(published.packet.Payload))

	// the correlation data carries the producer span, a child of the span of the station
	correlation := make(map[string]string)
	require.NoError(t, json.Unmarshal(published.packet.Properties.CorrelationData, &correlation))
	publish := trace.SpanContextFromContext(otel.GetTextMapPropagator().Extract(context.Background(), propagation.MapCarrier(correlation)))
	assert.Equal(t, stationSpan.SpanContext().TraceID(), publish.TraceID())
	span := awaitSpan(t, exporter, publish.SpanID())
	assert.Equal(t, "cs/in/ocpp1.6/# publish", span.Name)
	assert.Equal(t, trace.SpanKindProducer, span.SpanKind)
	assert.Equal(t, stationSpan.SpanContext().SpanID(), span.Parent.SpanID())
}

func TestSessionSendsConnectionEventsOnTheConnTopic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	hook := newRecorder()
	_, addr := startBroker(t, new(auth.AllowHook), hook)

	tr := mqtt.New(mqtt.WithBrokerURLs(addr), mqtt.WithTopicPrefix("cs"))
	session, err := tr.Open(ctx, "aaaa.CP.001", "ocpp2.0.1", func(msg *pipe.GatewayMessage) {
		t.Errorf("unexpected message for the station: %+v", msg)
	})
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, session.Close(ctx))
	}()
	await(ctx, t, hook.subscribed)

	require.NoError(t, session.SendConnectionEvent(ctx, &transport.ConnectionEvent{
		Type:         transport.ConnectionEventConnected,
		StationKey:   "aaaa.CP.001",
		OcppVersion:  "ocpp2.0.1",
		ConnectionId: "0123456789abcdef0123456789abcdef",
		GatewayId:    "gw-1",
		At:           time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC),
		RemoteAddr:   "192.0.2.1:4711",
	}))

	published := await(ctx, t, hook.published)
	assert.Equal(t, "aaaa.CP.001", published.clientId)
	assert.Equal(t, "cs/conn/aaaa.CP.001", published.packet.TopicName)
	assert.Equal(t, "application/json", published.packet.Properties.ContentType)
	assert.JSONEq(t, `{"type":"connected","station_key":"aaaa.CP.001","ocpp_version":"ocpp2.0.1",
		"connection_id":"0123456789abcdef0123456789abcdef","gateway_id":"gw-1","at":"2026-11-27T12:00:00Z",
		"remote_addr":"192.0.2.1:4711"}`, string(published.packet.Payload))
}

func TestOpenFailsWhenTheBrokerIsNotReachedWithinTheConnectTimeout(t *testing.T) {
	// the connection manager has stopped retrying when Open returns
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	// longer than the connect timeout: Open must not wait for it
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const connectTimeout = 300 * time.Millisecond
	tr := mqtt.New(
		mqtt.WithBrokerURLs(unreachableBroker(t)),
		mqtt.WithConnectSettings(connectTimeout, 50*time.Millisecond, 0))

	start := time.Now()
	session, err := tr.Open(ctx, "aaaa.CP001", "ocpp1.6", func(msg *pipe.GatewayMessage) {
		t.Errorf("unexpected message for the station: %+v", msg)
	})
	elapsed := time.Since(start)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, session)
	assert.GreaterOrEqual(t, elapsed, connectTimeout)
	assert.Less(t, elapsed, connectTimeout+2*time.Second)
}

func TestSessionIsDoneWhenTheSubscriptionIsRefused(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, addr := startBroker(t, new(refuseSubscriptions))

	tr := mqtt.New(mqtt.WithBrokerURLs(addr), mqtt.WithTopicPrefix("cs"))
	session, err := tr.Open(ctx, "aaaa.CP001", "ocpp1.6", func(msg *pipe.GatewayMessage) {
		t.Errorf("unexpected message for the station: %+v", msg)
	})
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, session.Close(ctx))
	}()

	select {
	case <-session.Done():
	case <-ctx.Done():
		t.Fatal("the session is not done although it cannot receive")
	}
}

// useTraceContext propagates trace context as W3C trace context headers until the
// test ends, as the gateway does.
func useTraceContext(t *testing.T) {
	previous := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTextMapPropagator(previous)
	})
}

func newTracer() (trace.Tracer, *tracetest.InMemoryExporter) {
	exporter := tracetest.NewInMemoryExporter()
	provider := tracesdk.NewTracerProvider(
		tracesdk.WithSampler(tracesdk.AlwaysSample()),
		tracesdk.WithSyncer(exporter))
	return provider.Tracer("test"), exporter
}

// awaitSpan returns the span with the id once it has ended.
func awaitSpan(t *testing.T, exporter *tracetest.InMemoryExporter, id trace.SpanID) tracetest.SpanStub {
	t.Helper()
	var found tracetest.SpanStub
	require.Eventually(t, func() bool {
		for _, span := range exporter.GetSpans() {
			if span.SpanContext.SpanID() == id {
				found = span
				return true
			}
		}
		return false
	}, 5*time.Second, time.Millisecond, "no span %s", id)
	return found
}

func await[T any](ctx context.Context, t *testing.T, ch chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-ctx.Done():
		t.Fatal("timeout waiting for the broker")
		var zero T
		return zero
	}
}

// brokerEvent is a subscription of a client of the test broker.
type brokerEvent struct {
	clientId string
	topic    string
}

// publication is a message that a client published on the test broker.
type publication struct {
	clientId string
	packet   packets.Packet
}

// recorder reports what the clients of the test broker subscribe to and publish.
type recorder struct {
	mochi.HookBase
	subscribed chan brokerEvent
	published  chan publication
}

func newRecorder() *recorder {
	return &recorder{subscribed: make(chan brokerEvent, 16), published: make(chan publication, 16)}
}

func (h *recorder) ID() string {
	return "recorder"
}

func (h *recorder) Provides(b byte) bool {
	return b == mochi.OnSubscribed || b == mochi.OnPublished
}

func (h *recorder) OnSubscribed(cl *mochi.Client, pk packets.Packet, _ []byte) {
	for _, filter := range pk.Filters {
		select {
		case h.subscribed <- brokerEvent{clientId: cl.ID, topic: filter.Filter}:
		default:
		}
	}
}

func (h *recorder) OnPublished(cl *mochi.Client, pk packets.Packet) {
	if cl.Net.Inline {
		return
	}
	select {
	case h.published <- publication{clientId: cl.ID, packet: pk}:
	default:
	}
}

// refuseSubscriptions lets every client connect and publish but not subscribe.
type refuseSubscriptions struct {
	mochi.HookBase
}

func (h *refuseSubscriptions) ID() string {
	return "refuse-subscriptions"
}

func (h *refuseSubscriptions) Provides(b byte) bool {
	return b == mochi.OnConnectAuthenticate || b == mochi.OnACLCheck
}

func (h *refuseSubscriptions) OnConnectAuthenticate(*mochi.Client, packets.Packet) bool {
	return true
}

func (h *refuseSubscriptions) OnACLCheck(_ *mochi.Client, _ string, write bool) bool {
	return write
}

// startBroker starts a test broker with the hooks; it is closed when the test ends.
//
// The test broker must not be closed while one of its clients connects or disconnects
// (mochi-co/mqtt v2.2.11 can deadlock then), so the end of the test first waits until
// every client has gone, and nothing here waits without a deadline.
func startBroker(t *testing.T, hooks ...mochi.Hook) (*mochi.Server, *url.URL) {
	t.Helper()

	broker := mochi.New(nil)
	for _, hook := range hooks {
		require.NoError(t, broker.AddHook(hook, nil))
	}

	address := freeAddress(t)
	require.NoError(t, broker.AddListener(listeners.NewTCP("broker", address, nil)))
	require.NoError(t, broker.Serve())

	t.Cleanup(func() {
		connected := func() int64 {
			return atomic.LoadInt64(&broker.Info.ClientsConnected)
		}
		assert.Eventually(t, func() bool { return connected() == 0 }, teardownTimeout, time.Millisecond,
			"clients are still connected to the test broker at the end of the test")

		closed := make(chan error, 1)
		go func() {
			closed <- broker.Close()
		}()
		select {
		case err := <-closed:
			if err != nil {
				t.Logf("WARN: broker close: %v", err)
			}
		case <-time.After(teardownTimeout):
			t.Errorf("the test broker did not close within %s", teardownTimeout)
		}
	})

	addr, err := url.Parse(fmt.Sprintf("mqtt://%s", address))
	require.NoError(t, err)
	return broker, addr
}

// unreachableBroker returns the URL of a broker that refuses every connection.
func unreachableBroker(t *testing.T) *url.URL {
	t.Helper()
	addr, err := url.Parse(fmt.Sprintf("mqtt://%s", freeAddress(t)))
	require.NoError(t, err)
	return addr
}

// freeAddress returns a local address that nothing listens on.
func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := l.Addr().String()
	require.NoError(t, l.Close())
	return address
}
