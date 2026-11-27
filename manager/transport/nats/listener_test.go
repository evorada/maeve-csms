// SPDX-License-Identifier: Apache-2.0

package nats_test

import (
	"context"
	"encoding/json"
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

// eventRecorder is a transport.ConnectionEventHandler that records the events it
// handles.
type eventRecorder struct {
	mu  sync.Mutex
	got []*transport.ConnectionEvent
	ch  chan *transport.ConnectionEvent
}

func newEventRecorder() *eventRecorder {
	return &eventRecorder{ch: make(chan *transport.ConnectionEvent, 1000)}
}

func (r *eventRecorder) HandleConnectionEvent(_ context.Context, ev *transport.ConnectionEvent) {
	r.mu.Lock()
	r.got = append(r.got, ev)
	r.mu.Unlock()
	r.ch <- ev
}

func (r *eventRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

func (r *eventRecorder) next(t *testing.T) *transport.ConnectionEvent {
	t.Helper()
	select {
	case ev := <-r.ch:
		return ev
	case <-time.After(waitTimeout):
		t.Fatal("timeout waiting for a connection event to be handled")
		return nil
	}
}

// listenEvents listens for connection events and disconnects at the end of the test.
func listenEvents(t *testing.T, conn *nats.Conn, handler transport.ConnectionEventHandler, opts ...natstransport.Opt[natstransport.Listener]) transport.Connection {
	t.Helper()
	c, err := natstransport.NewListener(conn, opts...).ListenConnectionEvents(context.Background(), handler)
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, c.Disconnect(context.Background()))
	})
	return c
}

// publishEvent sends a connection event as the gateway does.
func publishEvent(t *testing.T, conn *nats.Conn, subject string, ev transport.ConnectionEvent) {
	t.Helper()
	data, err := json.Marshal(ev)
	require.NoError(t, err)
	require.NoError(t, conn.Publish(subject, data))
}

// The JSON of the gateway, as gateway/transport/nats publishes it.
const gatewayConnectionEvent = `{"type":"disconnected","station_key":"k7f2.CP.001","ocpp_version":"ocpp1.6",
	"connection_id":"0123456789abcdef0123456789abcdef","gateway_id":"gw-1","at":"2026-11-27T12:00:00.123456789Z",
	"remote_addr":"192.0.2.1:4711","close_code":1000,"reason":"closed"}`

func TestListenConnectionEventsDecodesTheEventsOfTheGateway(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)
	rec := newEventRecorder()
	listenEvents(t, conn, rec)

	require.NoError(t, conn.Publish("ocpp.conn.k7f2.CP%2E001", []byte(gatewayConnectionEvent)))
	assert.Equal(t, &transport.ConnectionEvent{
		Type:         transport.ConnectionEventDisconnected,
		StationKey:   "k7f2.CP.001",
		OcppVersion:  "ocpp1.6",
		ConnectionId: "0123456789abcdef0123456789abcdef",
		GatewayId:    "gw-1",
		At:           time.Date(2026, 11, 27, 12, 0, 0, 123456789, time.UTC),
		RemoteAddr:   "192.0.2.1:4711",
		CloseCode:    1000,
		Reason:       transport.DisconnectReasonClosed,
	}, rec.next(t))

	// the handler gets the event of every station key, in both tenancy modes
	want := map[string]bool{}
	for _, v := range stationKeyVectors {
		subject, err := natstransport.ConnSubject("ocpp", v.key)
		require.NoError(t, err)
		assert.Equal(t, "ocpp.conn."+v.orgToken+"."+v.stationToken, subject)
		publishEvent(t, conn, subject, transport.ConnectionEvent{Type: transport.ConnectionEventConnected, StationKey: v.key})
		want[v.key] = true
	}
	got := map[string]bool{}
	for range stationKeyVectors {
		got[rec.next(t).StationKey] = true
	}
	assert.Equal(t, want, got)
}

func TestListenConnectionEventsUsesTheSubjectPrefix(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)
	rec := newEventRecorder()
	listenEvents(t, conn, rec, natstransport.WithSubjectPrefix[natstransport.Listener]("csms"))

	publishEvent(t, conn, "ocpp.conn.k7f2.CP001", transport.ConnectionEvent{Type: "connected", StationKey: "k7f2.CP001", ConnectionId: "other prefix"})
	publishEvent(t, conn, "csms.conn.k7f2.CP001", transport.ConnectionEvent{Type: "connected", StationKey: "k7f2.CP001", ConnectionId: "prefix"})
	assert.Equal(t, "prefix", rec.next(t).ConnectionId)
	require.NoError(t, conn.Flush())
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, rec.count())
}

// An event whose station key is not the one of its subject, which a gateway that may
// only publish for its own stations could otherwise use to report on other stations,
// is dropped, as are events that cannot be decoded.
func TestListenConnectionEventsDropsMismatchesAndMalformedEvents(t *testing.T) {
	logs := captureLogs(t)
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)
	rec := newEventRecorder()
	listenEvents(t, conn, rec)

	publishEvent(t, conn, "ocpp.conn.k7f2.CP001", transport.ConnectionEvent{Type: "connected", StationKey: "k7f2.CP002"})
	publishEvent(t, conn, "ocpp.conn.k7f2.CP001", transport.ConnectionEvent{Type: "connected", StationKey: "CP001"})
	require.NoError(t, conn.Publish("ocpp.conn.k7f2.CP001", []byte("not json")))
	// tokens that the gateway would not produce
	publishEvent(t, conn, "ocpp.conn.k7f2.CP%2e001", transport.ConnectionEvent{Type: "connected", StationKey: "k7f2.CP.001"})
	publishEvent(t, conn, "ocpp.conn._.k7f2%2ECP001", transport.ConnectionEvent{Type: "connected", StationKey: "k7f2.CP001"})
	publishEvent(t, conn, "ocpp.conn.k7f2.CP001", transport.ConnectionEvent{Type: "connected", StationKey: "k7f2.CP001", ConnectionId: "good"})

	assert.Equal(t, "good", rec.next(t).ConnectionId)
	require.NoError(t, conn.Flush())
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, rec.count())
	assert.Contains(t, logs.String(), "station key is not the one of its subject")
	assert.Contains(t, logs.String(), "cannot be unmarshaled")
	assert.Contains(t, logs.String(), "unexpected subject")
}

// The manager replicas share the events: each is handled once, and the events of one
// station keep their order on the replica that handles them.
func TestListenConnectionEventsInTheQueueGroup(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	publisher := connect(t, url)

	recorders := []*eventRecorder{newEventRecorder(), newEventRecorder()}
	for _, rec := range recorders {
		listenEvents(t, connect(t, url), rec, natstransport.WithWorkers(4))
	}
	// an OCPP listener in the same queue group does not take the events
	ocpp := newRecorder()
	listen(t, connect(t, url), transport.OcppVersion16, nil, ocpp.handler())

	const events = 100
	for i := 0; i < events; i++ {
		key := fmt.Sprintf("k7f2.CP%03d", i%10)
		subject, err := natstransport.ConnSubject("ocpp", key)
		require.NoError(t, err)
		publishEvent(t, publisher, subject, transport.ConnectionEvent{Type: "alive", StationKey: key, ConnectionId: strconv.Itoa(i)})
	}
	require.NoError(t, publisher.Flush())

	require.Eventually(t, func() bool {
		return recorders[0].count()+recorders[1].count() >= events
	}, waitTimeout, 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond)

	seen := map[string]int{}
	for _, rec := range recorders {
		rec.mu.Lock()
		last := map[string]int{}
		for _, ev := range rec.got {
			seen[ev.ConnectionId]++
			n, err := strconv.Atoi(ev.ConnectionId)
			require.NoError(t, err)
			if previous, ok := last[ev.StationKey]; ok {
				assert.Greater(t, n, previous, "events of %s out of order", ev.StationKey)
			}
			last[ev.StationKey] = n
		}
		rec.mu.Unlock()
	}
	assert.Len(t, seen, events)
	for id, n := range seen {
		assert.Equal(t, 1, n, "event %s handled %d times", id, n)
	}
	assert.Zero(t, ocpp.count())
}

func TestListenConnectionEventsStopsAfterDisconnect(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)
	rec := newEventRecorder()
	c, err := natstransport.NewListener(conn).ListenConnectionEvents(context.Background(), rec)
	require.NoError(t, err)

	publishEvent(t, conn, "ocpp.conn.k7f2.CP001", transport.ConnectionEvent{Type: "connected", StationKey: "k7f2.CP001", ConnectionId: "before"})
	assert.Equal(t, "before", rec.next(t).ConnectionId)

	require.NoError(t, c.Disconnect(context.Background()))
	publishEvent(t, conn, "ocpp.conn.k7f2.CP001", transport.ConnectionEvent{Type: "alive", StationKey: "k7f2.CP001", ConnectionId: "after"})
	require.NoError(t, conn.Flush())
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, rec.count())
	assert.NoError(t, c.Disconnect(context.Background()))
}

func TestListenConnectionEventsFailsOnAClosedConnection(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)
	conn.Close()
	_, err := natstransport.NewListener(conn).ListenConnectionEvents(context.Background(), newEventRecorder())
	assert.Error(t, err)
}
