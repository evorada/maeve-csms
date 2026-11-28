// SPDX-License-Identifier: Apache-2.0

package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mochi "github.com/mochi-co/mqtt/v2"
	"github.com/mochi-co/mqtt/v2/packets"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/gateway/ocpp"
	"github.com/thoughtworks/maeve-csms/gateway/pipe"
	"github.com/thoughtworks/maeve-csms/gateway/registry"
	"github.com/thoughtworks/maeve-csms/gateway/server"
	"github.com/thoughtworks/maeve-csms/gateway/transport"
	natstransport "github.com/thoughtworks/maeve-csms/gateway/transport/nats"
	"github.com/thoughtworks/maeve-csms/gateway/transport/nats/natstest"
	"nhooyr.io/websocket"
)

// publishedEvent is a connection event that the gateway published.
type publishedEvent struct {
	subject string
	raw     []byte
	ev      transport.ConnectionEvent
}

// natsEventGateway serves a gateway with the NATS transport on an embedded server and
// returns it with its shutdown function and the connection events it publishes.
func natsEventGateway(t *testing.T, reg registry.DeviceRegistry, opts ...server.WebsocketOpt) (*httptest.Server, func(context.Context) error, chan publishedEvent) {
	t.Helper()

	_, natsURL := natstest.RunServer(t)
	gatewayConn, err := nats.Connect(natsURL)
	require.NoError(t, err)
	tr := natstransport.New(gatewayConn)
	t.Cleanup(func() { assert.NoError(t, tr.Close()) })

	csms, err := nats.Connect(natsURL)
	require.NoError(t, err)
	t.Cleanup(csms.Close)
	events := make(chan publishedEvent, 64)
	_, err = csms.Subscribe("ocpp.conn.>", func(m *nats.Msg) {
		e := publishedEvent{subject: m.Subject, raw: m.Data}
		if err := json.Unmarshal(m.Data, &e.ev); err != nil {
			t.Errorf("unmarshaling connection event: %v", err)
			return
		}
		select {
		case events <- e:
		default:
		}
	})
	require.NoError(t, err)
	require.NoError(t, csms.Flush())

	srv, shutdown := serveGatewayWithShutdown(t, nil, append([]server.WebsocketOpt{
		server.WithTransport(tr),
		server.WithDeviceRegistry(reg),
		server.WithGatewayId("gw-1"),
	}, opts...)...)
	return srv, shutdown, events
}

func nextEvent(ctx context.Context, t *testing.T, events chan publishedEvent) publishedEvent {
	t.Helper()
	select {
	case e := <-events:
		return e
	case <-ctx.Done():
		t.Fatal("timeout waiting for a connection event")
		return publishedEvent{}
	}
}

// nextEventOfType skips the events of other types.
func nextEventOfType(ctx context.Context, t *testing.T, events chan publishedEvent, typ string) publishedEvent {
	t.Helper()
	for {
		if e := nextEvent(ctx, t, events); e.ev.Type == typ {
			return e
		}
	}
}

func assertNoEvent(t *testing.T, events chan publishedEvent, wait time.Duration) {
	t.Helper()
	select {
	case e := <-events:
		t.Errorf("unexpected connection event %s", e.raw)
	case <-time.After(wait):
	}
}

// readUntilClosed reads from the websocket of a station, which answers pings and close
// frames while it reads, and returns the error that ended the reads.
func readUntilClosed(ctx context.Context, conn *websocket.Conn) chan error {
	closed := make(chan error, 1)
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				closed <- err
				return
			}
		}
	}()
	return closed
}

func awaitClosed(ctx context.Context, t *testing.T, closed chan error) error {
	t.Helper()
	select {
	case err := <-closed:
		return err
	case <-ctx.Done():
		t.Fatal("timeout waiting for the websocket to close")
		return nil
	}
}

func TestConnectedAndDisconnectedEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	reg := registry.NewMockRegistry()
	reg.Add("a.example", tenantStation("k7f2", "CP.001"))
	srv, _, events := natsEventGateway(t, reg)

	before := time.Now()
	conn, _, err := dialStationWithProtocol(ctx, t, srv, "a.example", "CP.001", "CP.001", "password", nil, "ocpp1.6")
	require.NoError(t, err)

	connected := nextEvent(ctx, t, events)
	assert.Equal(t, "ocpp.conn.k7f2.CP%2E001", connected.subject)
	assert.Equal(t, transport.ConnectionEventConnected, connected.ev.Type)
	assert.Equal(t, "k7f2.CP.001", connected.ev.StationKey)
	assert.Equal(t, "ocpp1.6", connected.ev.OcppVersion)
	assert.Regexp(t, "^[0-9a-f]{32}$", connected.ev.ConnectionId)
	assert.Equal(t, "gw-1", connected.ev.GatewayId)
	assert.WithinRange(t, connected.ev.At, before, time.Now())
	host, _, err := net.SplitHostPort(connected.ev.RemoteAddr)
	assert.NoError(t, err)
	assert.Equal(t, "127.0.0.1", host)
	assert.Zero(t, connected.ev.CloseCode)
	assert.Empty(t, connected.ev.Reason)
	// the JSON that the manager reads
	var fields map[string]any
	require.NoError(t, json.Unmarshal(connected.raw, &fields))
	assert.ElementsMatch(t, []string{"type", "station_key", "ocpp_version", "connection_id", "gateway_id", "at", "remote_addr"},
		keys(fields))
	assert.True(t, strings.HasSuffix(fields["at"].(string), "Z"), "at %v is not in UTC", fields["at"])

	require.NoError(t, conn.Close(websocket.StatusNormalClosure, "bye"))

	disconnected := nextEvent(ctx, t, events)
	assert.Equal(t, "ocpp.conn.k7f2.CP%2E001", disconnected.subject)
	assert.Equal(t, transport.ConnectionEventDisconnected, disconnected.ev.Type)
	assert.Equal(t, connected.ev.ConnectionId, disconnected.ev.ConnectionId)
	assert.Equal(t, "k7f2.CP.001", disconnected.ev.StationKey)
	assert.Equal(t, int(websocket.StatusNormalClosure), disconnected.ev.CloseCode)
	assert.Equal(t, transport.DisconnectReasonClosed, disconnected.ev.Reason)
	assert.False(t, disconnected.ev.At.Before(connected.ev.At))
	assertNoEvent(t, events, 200*time.Millisecond)

	// a new socket of the same station has a connection id of its own
	conn, _, err = dialStationWithProtocol(ctx, t, srv, "a.example", "CP.001", "CP.001", "password", nil, "ocpp1.6")
	require.NoError(t, err)
	again := nextEvent(ctx, t, events)
	assert.Equal(t, transport.ConnectionEventConnected, again.ev.Type)
	assert.NotEqual(t, connected.ev.ConnectionId, again.ev.ConnectionId)
	require.NoError(t, conn.Close(websocket.StatusNormalClosure, "bye"))
	assert.Equal(t, again.ev.ConnectionId, nextEventOfType(ctx, t, events, transport.ConnectionEventDisconnected).ev.ConnectionId)
}

func keys(m map[string]any) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// Behind a proxy that the gateway trusts, the address of the station is the first
// entry of X-Forwarded-For.
func TestConnectedEventTakesTheAddressFromTheProxy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	reg := registry.NewMockRegistry()
	reg.Add("a.example", tenantStation("aaaa", "CP001"))
	tr := &fakeTransport{opened: make(chan *fakeSession, 1)}
	srv := startGateway(t,
		server.WithTransport(tr),
		server.WithDeviceRegistry(reg),
		server.WithTrustProxyHeaders(true))

	header := http.Header{}
	header.Set("X-Forwarded-For", " 203.0.113.7 , 10.0.0.1")
	conn, _, err := dialStation(ctx, t, srv, "a.example", "CP001", "CP001", "password", header)
	require.NoError(t, err)
	defer func() {
		_ = conn.Close(websocket.StatusNormalClosure, "OK")
	}()

	session := awaitSession(ctx, t, tr)
	ev := awaitFakeEvent(ctx, t, session)
	assert.Equal(t, transport.ConnectionEventConnected, ev.Type)
	assert.Equal(t, "203.0.113.7", ev.RemoteAddr)
}

func awaitSession(ctx context.Context, t *testing.T, tr *fakeTransport) *fakeSession {
	t.Helper()
	select {
	case session := <-tr.opened:
		return session
	case <-ctx.Done():
		t.Fatal("timeout waiting for the session")
		return nil
	}
}

func awaitFakeEvent(ctx context.Context, t *testing.T, session *fakeSession) *transport.ConnectionEvent {
	t.Helper()
	select {
	case ev := <-session.events:
		return ev
	case <-ctx.Done():
		t.Fatal("timeout waiting for a connection event")
		return nil
	}
}

// While the station answers the pings, the gateway publishes alive events for its
// socket and keeps it open.
func TestAliveEventsWhileTheStationAnswersPings(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	reg := registry.NewMockRegistry()
	reg.Add("a.example", tenantStation("k7f2", "CP001"))
	srv, _, events := natsEventGateway(t, reg,
		server.WithPing(50*time.Millisecond, 2*time.Second),
		server.WithAliveInterval(100*time.Millisecond))

	conn, _, err := dialStation(ctx, t, srv, "a.example", "CP001", "CP001", "password", nil)
	require.NoError(t, err)
	closed := readUntilClosed(ctx, conn)

	connected := nextEvent(ctx, t, events)
	require.Equal(t, transport.ConnectionEventConnected, connected.ev.Type)
	last := connected.ev.At
	for i := 0; i < 4; i++ {
		alive := nextEvent(ctx, t, events)
		assert.Equal(t, "ocpp.conn.k7f2.CP001", alive.subject)
		require.Equal(t, transport.ConnectionEventAlive, alive.ev.Type, "event %s", alive.raw)
		assert.Equal(t, connected.ev.ConnectionId, alive.ev.ConnectionId)
		assert.Equal(t, "k7f2.CP001", alive.ev.StationKey)
		assert.Empty(t, alive.ev.Reason)
		assert.True(t, alive.ev.At.After(last), "alive events are in order")
		last = alive.ev.At
	}

	require.NoError(t, conn.Close(websocket.StatusNormalClosure, "bye"))
	awaitClosed(ctx, t, closed)
	disconnected := nextEventOfType(ctx, t, events, transport.ConnectionEventDisconnected)
	assert.Equal(t, transport.DisconnectReasonClosed, disconnected.ev.Reason)
	assertNoEvent(t, events, 300*time.Millisecond)
}

// A station that does not answer a ping in time has its socket closed with 1008, and
// the gateway publishes one disconnected event with the reason ping_timeout. While
// the ping goes unanswered no alive event is published. (The close handshake with a
// station that does not read takes the 5 seconds that the websocket library waits.)
func TestUnansweredPingClosesTheSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	const (
		pingInterval  = 275 * time.Millisecond
		pongTimeout   = 300 * time.Millisecond
		aliveInterval = 50 * time.Millisecond
	)
	reg := registry.NewMockRegistry()
	reg.Add("a.example", tenantStation("k7f2", "CP001"))
	srv, _, events := natsEventGateway(t, reg,
		server.WithPing(pingInterval, pongTimeout),
		server.WithAliveInterval(aliveInterval))

	// the station does not read, so it does not answer pings
	conn, _, err := dialStation(ctx, t, srv, "a.example", "CP001", "CP001", "password", nil)
	require.NoError(t, err)
	defer func() {
		_ = conn.Close(websocket.StatusNormalClosure, "OK")
	}()

	connected := nextEvent(ctx, t, events)
	require.Equal(t, transport.ConnectionEventConnected, connected.ev.Type)

	var alives int
	var disconnected publishedEvent
	for disconnected.subject == "" {
		e := nextEvent(ctx, t, events)
		assert.Equal(t, connected.ev.ConnectionId, e.ev.ConnectionId)
		switch e.ev.Type {
		case transport.ConnectionEventAlive:
			alives++
			// the first ping starts at pingInterval and fails at pingInterval+pongTimeout
			assert.True(t, e.ev.At.Before(connected.ev.At.Add(pingInterval+pongTimeout/2)),
				"alive event %s after the connected event, while the ping goes unanswered", e.ev.At.Sub(connected.ev.At))
		case transport.ConnectionEventDisconnected:
			disconnected = e
		default:
			t.Errorf("unexpected event %s", e.raw)
		}
	}
	assert.Positive(t, alives, "alive events before the ping")
	assert.Equal(t, transport.DisconnectReasonPingTimeout, disconnected.ev.Reason)
	assert.Equal(t, int(websocket.StatusPolicyViolation), disconnected.ev.CloseCode)
	assert.False(t, disconnected.ev.At.Before(connected.ev.At.Add(pingInterval+pongTimeout)))
	assertNoEvent(t, events, 500*time.Millisecond)
}

// The read loop also reads the pongs. While it waits for the pipe to take a message of
// the station, because the CSMS does not answer, a pong waits unread: that must not
// close the socket.
func TestSlowCSMSDoesNotFailThePings(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	reg := registry.NewMockRegistry()
	reg.Add("a.example", tenantStation("aaaa", "CP001"))
	tr := &fakeTransport{opened: make(chan *fakeSession, 1)}
	const responseTimeout = 500 * time.Millisecond
	srv := startGateway(t,
		server.WithTransport(tr),
		server.WithDeviceRegistry(reg),
		server.WithPing(50*time.Millisecond, 100*time.Millisecond),
		server.WithAliveInterval(0),
		server.WithPipeOption(pipe.WithResponseTimeout(responseTimeout)))

	conn, _, err := dialStation(ctx, t, srv, "a.example", "CP001", "CP001", "password", nil)
	require.NoError(t, err)
	received := make(chan []byte, 16)
	closed := make(chan error, 1)
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				closed <- err
				return
			}
			received <- data
		}
	}()
	session := awaitSession(ctx, t, tr)
	assert.Equal(t, transport.ConnectionEventConnected, awaitFakeEvent(ctx, t, session).Type)

	// The CSMS answers none of the calls. The pipe takes the next call of the station
	// only when the previous one has timed out, so the read loop waits for the pipe.
	written := time.Now()
	for _, id := range []string{"1", "2", "3", "4"} {
		require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte(`[2,"`+id+`","Heartbeat",{}]`)))
	}
	for _, id := range []string{"1", "2", "3", "4"} {
		select {
		case msg := <-session.sent:
			assert.Equal(t, id, msg.MessageId)
		case <-ctx.Done():
			t.Fatalf("timeout waiting for call %s", id)
		}
	}
	// the read loop has waited for the pipe for much longer than the pong timeout
	assert.Greater(t, time.Since(written), 2*responseTimeout)

	// the socket is still open and works
	session.deliver(&pipe.GatewayMessage{
		Context:         context.Background(),
		MessageType:     ocpp.MessageTypeCallResult,
		MessageId:       "4",
		ResponsePayload: json.RawMessage(`{"currentTime":"2026-11-27T12:00:00Z"}`),
	})
	select {
	case data := <-received:
		assert.JSONEq(t, `[3,"4",{"currentTime":"2026-11-27T12:00:00Z"}]`, string(data))
	case err := <-closed:
		t.Fatalf("the websocket closed: %v", err)
	case <-ctx.Done():
		t.Fatal("timeout waiting for the answer of the CSMS")
	}

	require.NoError(t, conn.Close(websocket.StatusNormalClosure, "bye"))
	ev := awaitFakeEvent(ctx, t, session)
	assert.Equal(t, transport.ConnectionEventDisconnected, ev.Type)
	assert.Equal(t, transport.DisconnectReasonClosed, ev.Reason)
}

// A connection event that cannot be published leaves the socket and the OCPP messages
// alone, whether the transport fails at once or hangs.
func TestFailingConnectionEventsKeepTheSocketOpen(t *testing.T) {
	for name, hang := range map[string]bool{"failing": false, "hanging": true} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			attempted := make(chan *transport.ConnectionEvent, 16)
			release := make(chan struct{})
			tr := &fakeTransport{
				opened: make(chan *fakeSession, 1),
				sendEvent: func(ctx context.Context, ev *transport.ConnectionEvent) error {
					attempted <- ev
					if hang {
						select {
						case <-release:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
					return errors.New("the broker refuses the event")
				},
			}
			reg := registry.NewMockRegistry()
			reg.Add("a.example", tenantStation("aaaa", "CP001"))
			srv := startGateway(t,
				server.WithTransport(tr),
				server.WithDeviceRegistry(reg))

			conn, _, err := dialStation(ctx, t, srv, "a.example", "CP001", "CP001", "password", nil)
			require.NoError(t, err)
			session := awaitSession(ctx, t, tr)
			select {
			case ev := <-attempted:
				assert.Equal(t, transport.ConnectionEventConnected, ev.Type)
			case <-ctx.Done():
				t.Fatal("timeout waiting for the connected event")
			}

			// the OCPP messages go through while the events fail or hang
			for _, id := range []string{"1", "2"} {
				require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte(`[2,"`+id+`","Heartbeat",{}]`)))
				select {
				case msg := <-session.sent:
					assert.Equal(t, id, msg.MessageId)
				case <-ctx.Done():
					t.Fatal("timeout waiting for the call of the charge station")
				}
				session.deliver(&pipe.GatewayMessage{
					Context:         context.Background(),
					MessageType:     ocpp.MessageTypeCallResult,
					MessageId:       id,
					ResponsePayload: json.RawMessage(`{"currentTime":"2026-11-27T12:00:00Z"}`),
				})
				_, data, err := conn.Read(ctx)
				require.NoError(t, err)
				assert.JSONEq(t, `[3,"`+id+`",{"currentTime":"2026-11-27T12:00:00Z"}]`, string(data))
			}

			close(release)
			require.NoError(t, conn.Close(websocket.StatusNormalClosure, "bye"))
			select {
			case <-session.closed:
			case <-ctx.Done():
				t.Fatal("timeout waiting for the gateway to close the session")
			}
		})
	}
}

// The shutdown function closes every socket with 1001, publishes one disconnected
// event with the reason gateway_shutdown for each, and refuses new connections.
func TestShutdownClosesEverySocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	reg := registry.NewMockRegistry()
	reg.Add("a.example", tenantStation("aaaa", "CP001"))
	reg.Add("b.example", tenantStation("bbbb", "CP001"))
	srv, shutdown, events := natsEventGateway(t, reg)

	connectionIds := map[string]string{}
	var closed []chan error
	for host, key := range map[string]string{"a.example": "aaaa.CP001", "b.example": "bbbb.CP001"} {
		conn, _, err := dialStation(ctx, t, srv, host, "CP001", "CP001", "password", nil)
		require.NoError(t, err)
		closed = append(closed, readUntilClosed(ctx, conn))
		connected := nextEvent(ctx, t, events)
		require.Equal(t, transport.ConnectionEventConnected, connected.ev.Type)
		require.Equal(t, key, connected.ev.StationKey)
		connectionIds[connected.ev.ConnectionId] = key
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(ctx, 10*time.Second)
	defer cancelShutdown()
	start := time.Now()
	require.NoError(t, shutdown(shutdownCtx))
	assert.Less(t, time.Since(start), 5*time.Second)

	for _, c := range closed {
		err := awaitClosed(ctx, t, c)
		assert.Equal(t, websocket.StatusGoingAway, websocket.CloseStatus(err), "read: %v", err)
	}

	// the handlers have published the events before the shutdown returned
	got := map[string]string{}
	for range connectionIds {
		select {
		case e := <-events:
			assert.Equal(t, transport.ConnectionEventDisconnected, e.ev.Type)
			assert.Equal(t, transport.DisconnectReasonGatewayShutdown, e.ev.Reason)
			assert.Equal(t, int(websocket.StatusGoingAway), e.ev.CloseCode)
			got[e.ev.ConnectionId] = e.ev.StationKey
		case <-time.After(time.Second):
			t.Fatal("no disconnected event after the shutdown")
		}
	}
	assert.Equal(t, connectionIds, got)
	assertNoEvent(t, events, 300*time.Millisecond)

	// a station that connects now is turned away
	_, resp, err := dialStation(ctx, t, srv, "a.example", "CP001", "CP001", "password", nil)
	assert.Error(t, err)
	if assert.NotNil(t, resp) {
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	}
}

// The shutdown function returns at the deadline of its context when a station does not
// complete the close handshake; the disconnected event follows.
func TestShutdownReturnsAtItsDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	reg := registry.NewMockRegistry()
	reg.Add("a.example", tenantStation("aaaa", "CP001"))
	srv, shutdown, events := natsEventGateway(t, reg)

	// the station does not read, so it does not answer the close frame
	conn, _, err := dialStation(ctx, t, srv, "a.example", "CP001", "CP001", "password", nil)
	require.NoError(t, err)
	defer func() {
		_ = conn.Close(websocket.StatusNormalClosure, "OK")
	}()
	connected := nextEvent(ctx, t, events)

	shutdownCtx, cancelShutdown := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancelShutdown()
	start := time.Now()
	err = shutdown(shutdownCtx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 2*time.Second)

	disconnected := nextEvent(ctx, t, events)
	assert.Equal(t, transport.ConnectionEventDisconnected, disconnected.ev.Type)
	assert.Equal(t, connected.ev.ConnectionId, disconnected.ev.ConnectionId)
	assert.Equal(t, transport.DisconnectReasonGatewayShutdown, disconnected.ev.Reason)
}

// connEventHook reports the connection events that clients publish on the test broker.
type connEventHook struct {
	mochi.HookBase
	published chan publishedEvent
}

func (h *connEventHook) ID() string {
	return "connection-events"
}

func (h *connEventHook) Provides(b byte) bool {
	return b == mochi.OnPublished
}

func (h *connEventHook) OnPublished(cl *mochi.Client, pk packets.Packet) {
	if !strings.HasPrefix(pk.TopicName, "cs/conn/") {
		return
	}
	e := publishedEvent{subject: pk.TopicName, raw: pk.Payload}
	if err := json.Unmarshal(pk.Payload, &e.ev); err != nil {
		e.ev.Type = "invalid: " + err.Error()
	}
	select {
	case h.published <- e:
	default:
	}
}

func TestConnectionEventsOverMqtt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	hook := &connEventHook{published: make(chan publishedEvent, 16)}
	_, addr := startBroker(t, hook)

	reg := registry.NewMockRegistry()
	reg.Add("a.example", tenantStation("aaaa", "CP001"))
	srv := startGateway(t,
		server.WithMqttBrokerUrl(addr),
		server.WithMqttTopicPrefix("cs"),
		server.WithDeviceRegistry(reg),
		server.WithGatewayId("gw-mqtt"))

	conn, _, err := dialStation(ctx, t, srv, "a.example", "CP001", "CP001", "password", nil)
	require.NoError(t, err)

	connected := nextEvent(ctx, t, hook.published)
	assert.Equal(t, "cs/conn/aaaa.CP001", connected.subject)
	assert.Equal(t, transport.ConnectionEventConnected, connected.ev.Type)
	assert.Equal(t, "aaaa.CP001", connected.ev.StationKey)
	assert.Equal(t, "ocpp2.0.1", connected.ev.OcppVersion)
	assert.Equal(t, "gw-mqtt", connected.ev.GatewayId)
	assert.Regexp(t, "^[0-9a-f]{32}$", connected.ev.ConnectionId)

	require.NoError(t, conn.Close(websocket.StatusNormalClosure, "bye"))

	disconnected := nextEvent(ctx, t, hook.published)
	assert.Equal(t, "cs/conn/aaaa.CP001", disconnected.subject)
	assert.Equal(t, transport.ConnectionEventDisconnected, disconnected.ev.Type)
	assert.Equal(t, connected.ev.ConnectionId, disconnected.ev.ConnectionId)
	assert.Equal(t, int(websocket.StatusNormalClosure), disconnected.ev.CloseCode)
	assert.Equal(t, transport.DisconnectReasonClosed, disconnected.ev.Reason)
}
