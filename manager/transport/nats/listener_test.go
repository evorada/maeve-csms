// SPDX-License-Identifier: Apache-2.0

package nats_test

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	natstransport "github.com/thoughtworks/maeve-csms/manager/transport/nats"
	"github.com/thoughtworks/maeve-csms/manager/transport/nats/natstest"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// listen connects a listener for all charge stations and disconnects it at the end of
// the test.
func listen(t *testing.T, conn *nats.Conn, version transport.OcppVersion, chargeStationId *string, handler transport.MessageHandler, opts ...natstransport.Opt[natstransport.Listener]) transport.Connection {
	t.Helper()
	c, err := natstransport.NewListener(conn, opts...).Connect(context.Background(), version, chargeStationId, handler)
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, c.Disconnect(context.Background()))
	})
	return c
}

func TestListenerReceivesTheMessagesOfAllStations(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)
	rec := newRecorder()
	listen(t, conn, transport.OcppVersion16, nil, rec.handler())

	// a station id with a dot, as the gateway encodes it
	publish(t, conn, "ocpp.in.ocpp16.k7f2.CP%2E001", call("dot"))
	assert.Equal(t, received{stationKey: "k7f2.CP.001", messageId: "dot"}, rec.next(t))

	// the key that the handler gets is the station key that the subject was made from,
	// in both tenancy modes and with every character that is special in a subject
	want := map[string]bool{}
	for _, v := range stationKeyVectors {
		subject, err := natstransport.InSubject("ocpp", transport.OcppVersion16, v.key)
		require.NoError(t, err)
		publish(t, conn, subject, call(v.key))
		want[v.key] = true
	}
	got := map[string]bool{}
	for range stationKeyVectors {
		rcv := rec.next(t)
		assert.Equal(t, rcv.messageId, rcv.stationKey)
		got[rcv.stationKey] = true
	}
	assert.Equal(t, want, got)

	// another OCPP version is not received
	publish(t, conn, "ocpp.in.ocpp201.k7f2.CP001", call("201"))
	publish(t, conn, "ocpp.in.ocpp16.k7f2.CP001", call("16"))
	assert.Equal(t, received{stationKey: "k7f2.CP001", messageId: "16"}, rec.next(t))
}

func TestListenerUsesTheSubjectPrefix(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)
	rec := newRecorder()
	listen(t, conn, transport.OcppVersion201, nil, rec.handler(),
		natstransport.WithSubjectPrefix[natstransport.Listener]("csms"))

	publish(t, conn, "ocpp.in.ocpp201.k7f2.CP001", call("other prefix"))
	publish(t, conn, "csms.in.ocpp201.k7f2.CP001", call("prefix"))
	assert.Equal(t, received{stationKey: "k7f2.CP001", messageId: "prefix"}, rec.next(t))
}

func TestListenersInTheSameQueueGroupHandleEachMessageOnce(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	publisher := connect(t, url)

	recorders := []*recorder{newRecorder(), newRecorder()}
	for _, rec := range recorders {
		listen(t, connect(t, url), transport.OcppVersion16, nil, rec.handler())
	}

	const messages = 100
	for i := 0; i < messages; i++ {
		publish(t, publisher, fmt.Sprintf("ocpp.in.ocpp16.k7f2.CP%03d", i%10), call(strconv.Itoa(i)))
	}
	require.NoError(t, publisher.Flush())

	require.Eventually(t, func() bool {
		return recorders[0].count()+recorders[1].count() >= messages
	}, waitTimeout, 10*time.Millisecond)
	// nothing arrives twice, a little later either
	time.Sleep(100 * time.Millisecond)

	seen := map[string]int{}
	for _, rec := range recorders {
		rec.mu.Lock()
		for _, rcv := range rec.got {
			seen[rcv.messageId]++
		}
		rec.mu.Unlock()
		assert.Positive(t, rec.count(), "both members of the group get messages")
	}
	assert.Len(t, seen, messages)
	for id, n := range seen {
		assert.Equal(t, 1, n, "message %s handled %d times", id, n)
	}
}

func TestListenerKeepsTheOrderOfEachStation(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)

	var mu sync.Mutex
	order := map[string][]int{}
	done := make(chan struct{})
	const messages = 200
	handled := 0
	handler := transport.MessageHandlerFunc(func(_ context.Context, stationKey string, msg *transport.Message) {
		seq, err := strconv.Atoi(msg.MessageId)
		assert.NoError(t, err)
		// uneven handling times: workers that ran in parallel for one station would
		// finish out of order
		time.Sleep(time.Duration(seq%7) * 100 * time.Microsecond)
		mu.Lock()
		defer mu.Unlock()
		order[stationKey] = append(order[stationKey], seq)
		handled++
		if handled == messages {
			close(done)
		}
	})
	listen(t, conn, transport.OcppVersion16, nil, handler, natstransport.WithWorkers(4))

	stations := []string{"k7f2.CP001", "k7f2.CP002", "a1b2.CP001"}
	for i := 0; i < messages; i++ {
		subject, err := natstransport.InSubject("ocpp", transport.OcppVersion16, stations[i%len(stations)])
		require.NoError(t, err)
		publish(t, conn, subject, call(strconv.Itoa(i)))
	}

	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("timeout waiting for the messages to be handled")
	}
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, order, len(stations))
	for station, seqs := range order {
		assert.IsIncreasing(t, seqs, "station %s", station)
	}
}

func TestListenerHandlesStationsInParallel(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)

	const workers = 4
	slow := "k7f2.CP001"
	var fast string
	for i := 2; fast == ""; i++ {
		key := fmt.Sprintf("k7f2.CP%03d", i)
		if natstransport.WorkerIndex(key, workers) != natstransport.WorkerIndex(slow, workers) {
			fast = key
		}
	}

	fastHandled := make(chan struct{})
	slowDone := make(chan bool, 1)
	handler := transport.MessageHandlerFunc(func(_ context.Context, stationKey string, _ *transport.Message) {
		switch stationKey {
		case slow:
			// waits for the other station: handling one station at a time never gets past this
			select {
			case <-fastHandled:
				slowDone <- true
			case <-time.After(waitTimeout):
				slowDone <- false
			}
		case fast:
			close(fastHandled)
		}
	})
	listen(t, conn, transport.OcppVersion16, nil, handler, natstransport.WithWorkers(workers))

	for _, key := range []string{slow, fast} {
		subject, err := natstransport.InSubject("ocpp", transport.OcppVersion16, key)
		require.NoError(t, err)
		publish(t, conn, subject, call(key))
	}
	select {
	case ok := <-slowDone:
		assert.True(t, ok, "the second station was not handled while the first one was busy")
	case <-time.After(2 * waitTimeout):
		t.Fatal("timeout waiting for the first station to be handled")
	}
}

func TestListenerForOneStation(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)
	rec := newRecorder()
	stationKey := "k7f2.CP.001"
	listen(t, conn, transport.OcppVersion16, &stationKey, rec.handler())

	publish(t, conn, "ocpp.in.ocpp16.k7f2.CP002", call("other station"))
	publish(t, conn, "ocpp.in.ocpp16.a1b2.CP%2E001", call("other org"))
	publish(t, conn, "ocpp.in.ocpp201.k7f2.CP%2E001", call("other version"))
	publish(t, conn, "ocpp.in.ocpp16.k7f2.CP%2E001", call("mine"))
	assert.Equal(t, received{stationKey: stationKey, messageId: "mine"}, rec.next(t))

	require.NoError(t, conn.Flush())
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, rec.count())
}

func TestListenerForOneStationInSingleTenancyMode(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)
	rec := newRecorder()
	stationKey := "CP001"
	listen(t, conn, transport.OcppVersion201, &stationKey, rec.handler())

	publish(t, conn, "ocpp.in.ocpp201._.CP001", call("mine"))
	assert.Equal(t, received{stationKey: "CP001", messageId: "mine"}, rec.next(t))
}

func TestListenerDropsMalformedMessages(t *testing.T) {
	logs := captureLogs(t)
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)
	rec := newRecorder()
	listen(t, conn, transport.OcppVersion16, nil, rec.handler())

	require.NoError(t, conn.Publish("ocpp.in.ocpp16.k7f2.CP001", []byte("not json")))
	// tokens that the gateway would not produce
	publish(t, conn, "ocpp.in.ocpp16.k7f2.CP%2", call("bad escape"))
	publish(t, conn, "ocpp.in.ocpp16._.k7f2%2ECP001", call("not canonical"))
	publish(t, conn, "ocpp.in.ocpp16.k7f2.CP001", call("good"))

	assert.Equal(t, received{stationKey: "k7f2.CP001", messageId: "good"}, rec.next(t))
	require.NoError(t, conn.Flush())
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, rec.count())
	assert.Contains(t, logs.String(), "unable to unmarshal message")
	assert.Contains(t, logs.String(), "unexpected subject")
}

func TestListenerContinuesTheTraceOfThePublisher(t *testing.T) {
	tracer, exporter := testutil.GetTracer()
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)

	handlerSpans := make(chan trace.SpanContext, 2)
	handler := transport.MessageHandlerFunc(func(ctx context.Context, _ string, _ *transport.Message) {
		handlerSpans <- trace.SpanContextFromContext(ctx)
	})
	listen(t, conn, transport.OcppVersion16, nil, handler, natstransport.WithOtelTracer[natstransport.Listener](tracer))

	for name, headerKey := range map[string]string{
		"W3C header name":     "traceparent",
		"HTTP canonical name": "Traceparent",
	} {
		t.Run(name, func(t *testing.T) {
			exporter.Reset()
			ctx, publisher := tracer.Start(context.Background(), "gateway publish")
			carrier := propagation.MapCarrier{}
			otel.GetTextMapPropagator().Inject(ctx, carrier)
			publisher.End()

			msg := nats.NewMsg("ocpp.in.ocpp16.k7f2.CP001")
			msg.Header.Set(headerKey, carrier.Get("traceparent"))
			msg.Data = []byte(`{"type":2,"action":"Heartbeat","id":"traced","request":{}}`)
			require.NoError(t, conn.PublishMsg(msg))

			var handlerSpan trace.SpanContext
			select {
			case handlerSpan = <-handlerSpans:
			case <-time.After(waitTimeout):
				t.Fatal("timeout waiting for the message to be handled")
			}
			assert.Equal(t, publisher.SpanContext().TraceID(), handlerSpan.TraceID())

			// the receive span ends after the handler returns
			var receive *tracetest.SpanStub
			require.Eventually(t, func() bool {
				for _, span := range exporter.GetSpans() {
					if span.Name == "ocpp.in.ocpp16 receive" {
						receive = &span
						return true
					}
				}
				return false
			}, waitTimeout, 10*time.Millisecond)
			assert.Equal(t, trace.SpanKindConsumer, receive.SpanKind)
			assert.Equal(t, publisher.SpanContext().SpanID(), receive.Parent.SpanID(), "the publisher's span is the parent")
			assert.Equal(t, receive.SpanContext.SpanID(), handlerSpan.SpanID(), "the handler runs in the receive span")
			testutil.AssertAttributes(t, receive.Attributes, map[string]any{
				"messaging.system":                     "nats",
				"messaging.operation":                  "receive",
				"messaging.message.payload_size_bytes": len(msg.Data),
				"messaging.consumer.id":                "maeve-manager",
				"messaging.message.conversation_id":    "traced",
				"ocpp.version":                         "1.6",
				"csId":                                 "k7f2.CP001",
				"call.action":                          "Heartbeat",
			})
		})
	}
}

func TestListenerStopsDeliveringAfterDisconnect(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)
	rec := newRecorder()
	c, err := natstransport.NewListener(conn).Connect(context.Background(), transport.OcppVersion16, nil, rec.handler())
	require.NoError(t, err)

	publish(t, conn, "ocpp.in.ocpp16.k7f2.CP001", call("before"))
	assert.Equal(t, "before", rec.next(t).messageId)

	require.NoError(t, c.Disconnect(context.Background()))
	publish(t, conn, "ocpp.in.ocpp16.k7f2.CP001", call("after"))
	require.NoError(t, conn.Flush())
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, rec.count())

	// a second Disconnect does nothing
	assert.NoError(t, c.Disconnect(context.Background()))
}

func TestDisconnectHandlesTheQueuedMessagesAndIsBoundedByTheContext(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)

	started := make(chan struct{}, 10)
	release := make(chan struct{})
	var mu sync.Mutex
	var handled []string
	handler := transport.MessageHandlerFunc(func(_ context.Context, _ string, msg *transport.Message) {
		started <- struct{}{}
		<-release
		mu.Lock()
		handled = append(handled, msg.MessageId)
		mu.Unlock()
	})
	c, err := natstransport.NewListener(conn, natstransport.WithWorkers(1)).
		Connect(context.Background(), transport.OcppVersion16, nil, handler)
	require.NoError(t, err)

	publish(t, conn, "ocpp.in.ocpp16.k7f2.CP001", call("1"))
	publish(t, conn, "ocpp.in.ocpp16.k7f2.CP001", call("2"))
	select {
	case <-started:
	case <-time.After(waitTimeout):
		t.Fatal("timeout waiting for the handler to start")
	}
	// give the subscription time to put the second message on the queue of the worker
	require.NoError(t, conn.Flush())
	time.Sleep(100 * time.Millisecond)

	// the handler is busy: Disconnect gives up when its context is done
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = c.Disconnect(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// the message that was queued before Disconnect is still handled
	close(release)
	require.NoError(t, c.Disconnect(context.Background()))
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"1", "2"}, handled)
}

func TestListenerConnectFailsOnAClosedConnection(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)
	conn.Close()

	_, err := natstransport.NewListener(conn).Connect(context.Background(), transport.OcppVersion16, nil, newRecorder().handler())
	assert.ErrorIs(t, err, nats.ErrConnectionClosed)
}
