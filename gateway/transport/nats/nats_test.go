// SPDX-License-Identifier: Apache-2.0

package nats_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/gateway/ocpp"
	"github.com/thoughtworks/maeve-csms/gateway/pipe"
	natstransport "github.com/thoughtworks/maeve-csms/gateway/transport/nats"
	"github.com/thoughtworks/maeve-csms/gateway/transport/nats/natstest"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestOpenDeliversTheMessagesOfTheOutSubject(t *testing.T) {
	for name, header := range map[string]string{
		"w3c header":            "traceparent",
		"canonical HTTP header": "Traceparent",
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			useTraceContext(t)
			tracer, exporter := newTracer()
			conn, csms := connectPair(t)

			tr := natstransport.New(conn, natstransport.WithTracer(tracer))
			delivered := make(chan *pipe.GatewayMessage, 1)
			session, err := tr.Open(ctx, "k7f2.CP001", "ocpp1.6", func(msg *pipe.GatewayMessage) {
				delivered <- msg
			})
			require.NoError(t, err)
			defer func() {
				assert.NoError(t, session.Close(ctx))
			}()
			assert.Nil(t, session.Done())

			// the CSMS answers in a trace of its own, which it passes in the headers
			csmsCtx, csmsSpan := tracer.Start(context.Background(), "csms")
			csmsSpan.End()
			carrier := propagation.MapCarrier{}
			otel.GetTextMapPropagator().Inject(csmsCtx, carrier)
			msg := callResult(t, "ocpp.out.ocpp16.k7f2.CP001", "1")
			msg.Header.Set(header, carrier.Get("traceparent"))
			require.NoError(t, csms.PublishMsg(msg))

			got := await(ctx, t, delivered)
			assert.Equal(t, ocpp.MessageTypeCallResult, got.MessageType)
			assert.Equal(t, "1", got.MessageId)
			assert.JSONEq(t, `{"status":"Accepted"}`, string(got.ResponsePayload))

			// the message carries a consumer span in the trace of the CSMS
			receive := trace.SpanFromContext(got.Context).SpanContext()
			assert.Equal(t, csmsSpan.SpanContext().TraceID(), receive.TraceID())
			span := awaitSpan(t, exporter, receive.SpanID())
			assert.Equal(t, "ocpp.out.ocpp16 receive", span.Name)
			assert.Equal(t, trace.SpanKindConsumer, span.SpanKind)
			assert.Equal(t, csmsSpan.SpanContext().SpanID(), span.Parent.SpanID())
			assert.Contains(t, span.Attributes, attribute.String("messaging.system", "nats"))
			assert.Contains(t, span.Attributes, attribute.String("csId", "k7f2.CP001"))
		})
	}
}

func TestSendPublishesOnTheInSubject(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	useTraceContext(t)
	tracer, exporter := newTracer()
	conn, csms := connectPair(t)
	received := subscribe(t, csms, "ocpp.in.ocpp16.k7f2.CP001")

	tr := natstransport.New(conn, natstransport.WithTracer(tracer))
	session, err := tr.Open(ctx, "k7f2.CP001", "ocpp1.6", func(msg *pipe.GatewayMessage) {
		t.Errorf("unexpected message for the station: %+v", msg)
	})
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, session.Close(ctx))
	}()

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

	msg := await(ctx, t, received)
	assert.JSONEq(t, `{"type":2,"action":"Heartbeat","id":"7","request":{}}`, string(msg.Data))

	// the W3C header carries the producer span, a child of the span of the station
	traceparent := msg.Header.Get("traceparent")
	require.NotEmpty(t, traceparent, "headers: %v", msg.Header)
	publish := trace.SpanContextFromContext(otel.GetTextMapPropagator().Extract(context.Background(),
		propagation.MapCarrier{"traceparent": traceparent}))
	assert.Equal(t, stationSpan.SpanContext().TraceID(), publish.TraceID())
	span := awaitSpan(t, exporter, publish.SpanID())
	assert.Equal(t, "ocpp.in.ocpp16 publish", span.Name)
	assert.Equal(t, trace.SpanKindProducer, span.SpanKind)
	assert.Equal(t, stationSpan.SpanContext().SpanID(), span.Parent.SpanID())
	assert.Contains(t, span.Attributes, attribute.String("messaging.system", "nats"))
	assert.Contains(t, span.Attributes, attribute.String("csId", "k7f2.CP001"))
}

func TestNoDeliveryAfterClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, csms := connectPair(t)

	delivered := make(chan *pipe.GatewayMessage, 2)
	session, err := natstransport.New(conn).Open(ctx, "k7f2.CP001", "ocpp2.0.1", func(msg *pipe.GatewayMessage) {
		delivered <- msg
	})
	require.NoError(t, err)

	require.NoError(t, csms.PublishMsg(callResult(t, "ocpp.out.ocpp201.k7f2.CP001", "1")))
	assert.Equal(t, "1", await(ctx, t, delivered).MessageId)

	require.NoError(t, session.Close(ctx))
	assert.Equal(t, 0, conn.NumSubscriptions())
	require.NoError(t, csms.PublishMsg(callResult(t, "ocpp.out.ocpp201.k7f2.CP001", "2")))
	require.NoError(t, csms.Flush())
	require.NoError(t, conn.Flush())

	select {
	case msg := <-delivered:
		t.Errorf("message delivered after close: %+v", msg)
	case <-time.After(200 * time.Millisecond):
	}

	// a second close is not an error
	assert.NoError(t, session.Close(ctx))
}

// Every station key is encoded the same way in both directions, the dot of
// "k7f2.CP.001" included, with the subject prefix of the transport.
func TestStationKeysAreEncodedInBothDirections(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, csms := connectPair(t)
	tr := natstransport.New(conn, natstransport.WithSubjectPrefix("csms"))

	for _, v := range stationKeyVectors {
		for protocol, ver := range map[string]string{"ocpp1.6": "ocpp16", "ocpp2.0.1": "ocpp201"} {
			t.Run(v.key+"/"+protocol, func(t *testing.T) {
				subject := func(direction string) string {
					return "csms." + direction + "." + ver + "." + v.orgToken + "." + v.stationToken
				}
				received := subscribe(t, csms, subject("in"))

				delivered := make(chan *pipe.GatewayMessage, 1)
				session, err := tr.Open(ctx, v.key, protocol, func(msg *pipe.GatewayMessage) {
					delivered <- msg
				})
				require.NoError(t, err)
				defer func() {
					assert.NoError(t, session.Close(ctx))
				}()

				require.NoError(t, csms.PublishMsg(callResult(t, subject("out"), "1")))
				assert.Equal(t, "1", await(ctx, t, delivered).MessageId)

				require.NoError(t, session.Send(ctx, &pipe.GatewayMessage{
					MessageType: ocpp.MessageTypeCall, Action: "Heartbeat", MessageId: "2", RequestPayload: json.RawMessage(`{}`),
				}))
				assert.Equal(t, subject("in"), await(ctx, t, received).Subject)
			})
		}
	}
}

func TestOpenFailsForAnUnknownProtocolOrAnEmptyKey(t *testing.T) {
	conn, _ := connectPair(t)
	tr := natstransport.New(conn)
	deliver := func(msg *pipe.GatewayMessage) {
		t.Errorf("unexpected message for the station: %+v", msg)
	}

	_, err := tr.Open(context.Background(), "k7f2.CP001", "ocpp2.1", deliver)
	assert.Error(t, err)
	_, err = tr.Open(context.Background(), "", "ocpp1.6", deliver)
	assert.Error(t, err)
	_, err = natstransport.New(conn, natstransport.WithSubjectPrefix("a.b")).Open(context.Background(), "k7f2.CP001", "ocpp1.6", deliver)
	assert.Error(t, err)
	assert.Equal(t, 0, conn.NumSubscriptions())
}

// Open waits until the server has registered the subscription. While the server is
// away it gives up at the deadline of ctx and leaves no subscription behind.
func TestOpenFailsWhenTheSubscriptionIsNotRegisteredInTime(t *testing.T) {
	srv, url := natstest.RunServer(t)
	conn, err := nats.Connect(url, nats.MaxReconnects(-1), nats.ReconnectWait(20*time.Millisecond))
	require.NoError(t, err)
	t.Cleanup(conn.Close)

	srv.Shutdown()
	srv.WaitForShutdown()
	require.Eventually(t, func() bool { return !conn.IsConnected() }, 5*time.Second, time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	session, err := natstransport.New(conn).Open(ctx, "k7f2.CP001", "ocpp1.6", func(msg *pipe.GatewayMessage) {
		t.Errorf("unexpected message for the station: %+v", msg)
	})
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Nil(t, session)
	assert.Less(t, elapsed, 3*time.Second)
	assert.Equal(t, 0, conn.NumSubscriptions())
}

func TestOpenFailsOnAClosedConnection(t *testing.T) {
	conn, _ := connectPair(t)
	conn.Close()

	_, err := natstransport.New(conn).Open(context.Background(), "k7f2.CP001", "ocpp1.6", func(msg *pipe.GatewayMessage) {
		t.Errorf("unexpected message for the station: %+v", msg)
	})
	assert.ErrorIs(t, err, nats.ErrConnectionClosed)
}

func TestCloseDrainsTheConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, csms := connectPair(t)
	received := subscribe(t, csms, "ocpp.in.ocpp201._.CP001")

	tr := natstransport.New(conn)
	session, err := tr.Open(ctx, "CP001", "ocpp2.0.1", func(*pipe.GatewayMessage) {})
	require.NoError(t, err)
	require.NoError(t, session.Send(ctx, &pipe.GatewayMessage{
		MessageType: ocpp.MessageTypeCall, Action: "Heartbeat", MessageId: "1", RequestPayload: json.RawMessage(`{}`),
	}))

	require.NoError(t, tr.Close())
	assert.True(t, conn.IsClosed())
	// what was published before the close has left
	assert.Equal(t, "ocpp.in.ocpp201._.CP001", await(ctx, t, received).Subject)

	// the session of a closed transport closes without an error, and so does the transport
	assert.NoError(t, session.Close(ctx))
	assert.NoError(t, tr.Close())
}

func TestCloseWithoutAServerClosesTheConnection(t *testing.T) {
	conn, err := natstransport.Connect([]string{"nats://" + freeAddress(t)}, "", "", "")
	require.NoError(t, err)
	t.Cleanup(conn.Close)

	assert.ErrorIs(t, natstransport.New(conn).Close(), nats.ErrConnectionReconnecting)
	assert.True(t, conn.IsClosed())
}

func TestConnect(t *testing.T) {
	_, url := natstest.RunServer(t)

	t.Run("names the connection", func(t *testing.T) {
		conn, err := natstransport.Connect([]string{url}, "", "", "")
		require.NoError(t, err)
		defer conn.Close()
		assert.True(t, conn.IsConnected())
		assert.Equal(t, "maeve-gateway", conn.Opts.Name)
		assert.Equal(t, -1, conn.Opts.MaxReconnect)
		assert.True(t, conn.Opts.RetryOnFailedConnect)

		named, err := natstransport.Connect([]string{url}, "gateway-1", "", "")
		require.NoError(t, err)
		defer named.Close()
		assert.Equal(t, "gateway-1", named.Opts.Name)
	})

	t.Run("tries the next url", func(t *testing.T) {
		conn, err := natstransport.Connect([]string{"nats://" + freeAddress(t), url}, "", "", "")
		require.NoError(t, err)
		defer conn.Close()
		require.Eventually(t, conn.IsConnected, 5*time.Second, 10*time.Millisecond)
	})

	t.Run("keeps trying when no server is reachable", func(t *testing.T) {
		conn, err := natstransport.Connect([]string{"nats://" + freeAddress(t)}, "", "", "")
		require.NoError(t, err)
		defer conn.Close()
		assert.False(t, conn.IsConnected())
		assert.False(t, conn.IsClosed())
	})

	t.Run("bad settings", func(t *testing.T) {
		_, err := natstransport.Connect(nil, "", "", "")
		assert.Error(t, err)
		_, err = natstransport.Connect([]string{url}, "", "testdata/missing.creds", "")
		assert.ErrorContains(t, err, "creds file")
		_, err = natstransport.Connect([]string{url}, "", "", "testdata/missing-ca.pem")
		assert.Error(t, err)
	})
}

// connectPair connects the transport and the test, which plays the CSMS, to a new
// embedded server.
func connectPair(t *testing.T) (*nats.Conn, *nats.Conn) {
	t.Helper()
	_, url := natstest.RunServer(t)
	conn, err := nats.Connect(url)
	require.NoError(t, err)
	t.Cleanup(conn.Close)
	csms, err := nats.Connect(url)
	require.NoError(t, err)
	t.Cleanup(csms.Close)
	return conn, csms
}

// subscribe returns the messages that conn receives on the subject.
func subscribe(t *testing.T, conn *nats.Conn, subject string) chan *nats.Msg {
	t.Helper()
	received := make(chan *nats.Msg, 16)
	sub, err := conn.ChanSubscribe(subject, received)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	require.NoError(t, conn.Flush())
	return received
}

// callResult is a CallResult of the CSMS for a station.
func callResult(t *testing.T, subject, messageId string) *nats.Msg {
	t.Helper()
	data, err := json.Marshal(pipe.GatewayMessage{
		MessageType:     ocpp.MessageTypeCallResult,
		MessageId:       messageId,
		ResponsePayload: json.RawMessage(`{"status":"Accepted"}`),
	})
	require.NoError(t, err)
	return &nats.Msg{Subject: subject, Data: data, Header: nats.Header{}}
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
		t.Fatal("timeout waiting for a message")
		var zero T
		return zero
	}
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
