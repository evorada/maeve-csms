// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/gateway/ocpp"
	"github.com/thoughtworks/maeve-csms/gateway/pipe"
	"github.com/thoughtworks/maeve-csms/gateway/registry"
	"github.com/thoughtworks/maeve-csms/gateway/server"
	"github.com/thoughtworks/maeve-csms/gateway/transport"
	"github.com/thoughtworks/maeve-csms/gateway/transport/mqtt"
	natstransport "github.com/thoughtworks/maeve-csms/gateway/transport/nats"
	"github.com/thoughtworks/maeve-csms/gateway/transport/nats/natstest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"nhooyr.io/websocket"
)

func TestTransportFlagDefaults(t *testing.T) {
	for flag, want := range map[string]string{
		"transport":           "nats",
		"nats-url":            "[nats://127.0.0.1:4222]",
		"nats-creds":          "",
		"nats-tls-ca":         "",
		"nats-name":           "maeve-gateway",
		"nats-subject-prefix": "ocpp",
		"mqtt-addr":           "mqtt://127.0.0.1:1883",
		"mqtt-prefix":         "cs",
	} {
		f := serveCmd.Flags().Lookup(flag)
		if assert.NotNil(t, f, "flag --%s", flag) {
			assert.Equal(t, want, f.DefValue, "default of --%s", flag)
		}
	}
}

func TestSocketFlagDefaults(t *testing.T) {
	for flag, want := range map[string]string{
		"gateway-id":          server.DefaultGatewayId(),
		"ws-ping-interval":    "30s",
		"ws-pong-timeout":     "10s",
		"conn-alive-interval": "2m0s",
		"shutdown-timeout":    "10s",
	} {
		f := serveCmd.Flags().Lookup(flag)
		if assert.NotNil(t, f, "flag --%s", flag) {
			assert.Equal(t, want, f.DefValue, "default of --%s", flag)
		}
	}
	assert.NotEmpty(t, server.DefaultGatewayId())
}

func TestSocketSettingsValidation(t *testing.T) {
	valid := socketSettings{pingInterval: 30 * time.Second, pongTimeout: 10 * time.Second, aliveInterval: 2 * time.Minute, shutdownTimeout: 10 * time.Second}
	assert.NoError(t, valid.validate())
	assert.NoError(t, socketSettings{}.validate(), "no pings, no alive events, no wait")

	for name, change := range map[string]func(*socketSettings){
		"negative ping interval":     func(s *socketSettings) { s.pingInterval = -time.Second },
		"negative pong timeout":      func(s *socketSettings) { s.pongTimeout = -time.Second },
		"no pong timeout with pings": func(s *socketSettings) { s.pongTimeout = 0 },
		"negative alive interval":    func(s *socketSettings) { s.aliveInterval = -time.Second },
		"negative shutdown timeout":  func(s *socketSettings) { s.shutdownTimeout = -time.Second },
	} {
		t.Run(name, func(t *testing.T) {
			settings := valid
			change(&settings)
			assert.Error(t, settings.validate())
		})
	}
}

// On a signal the gateway stops accepting connections, closes the station sockets with
// 1001 and publishes their disconnected events; then it returns, and the caller closes
// the transport.
func TestServeUntilDoneShutsDownTheSockets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, url := natstest.RunServer(t)
	tr, err := newTransport(transportSettings{
		kind:              "nats",
		natsUrls:          []string{url},
		natsName:          "maeve-gateway",
		natsSubjectPrefix: "ocpp",
	}, noopTracer())
	require.NoError(t, err)

	csms, err := nats.Connect(url)
	require.NoError(t, err)
	defer csms.Close()
	events := make(chan *nats.Msg, 16)
	_, err = csms.ChanSubscribe("ocpp.conn.>", events)
	require.NoError(t, err)
	require.NoError(t, csms.Flush())

	reg := registry.NewMockRegistry()
	reg.Add("127.0.0.1", &registry.ChargeStation{
		ClientId:             "CP001",
		StationKey:           "k7f2.CP001",
		OrgKey:               "k7f2",
		SecurityProfile:      registry.UnsecuredTransportWithBasicAuth,
		Base64SHA256Password: "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg=", // "password"
	})
	settings := socketSettings{gatewayId: "gw-1", pingInterval: 30 * time.Second, pongTimeout: 10 * time.Second}
	handler, shutdownSockets := server.NewWebsocketHandlerWithShutdown(append(settings.options(),
		server.WithTransport(tr),
		server.WithDeviceRegistry(reg))...)
	wsServer := server.New("ws", "127.0.0.1:0", nil, handler)
	errCh := make(chan error, 1)
	wsServer.Start(errCh)

	signalled, signal := context.WithCancel(ctx)
	defer signal()
	done := make(chan error, 1)
	go func() {
		done <- serveUntilDone(signalled, errCh, shutdownSockets, 5*time.Second, wsServer)
	}()

	dial := func() (*websocket.Conn, *http.Response, error) {
		header := http.Header{}
		header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("CP001:password")))
		return websocket.Dial(ctx, "ws://"+wsServer.Addr()+"/ws/CP001", &websocket.DialOptions{
			Subprotocols: []string{"ocpp1.6"},
			HTTPHeader:   header,
		})
	}
	conn, _, err := dial()
	require.NoError(t, err)
	closed := make(chan error, 1)
	go func() {
		_, _, err := conn.Read(ctx)
		closed <- err
	}()
	assert.Equal(t, transport.ConnectionEventConnected, awaitConnectionEvent(ctx, t, events).Type)

	signal()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("timeout waiting for the shutdown")
	}
	select {
	case err := <-closed:
		assert.Equal(t, websocket.StatusGoingAway, websocket.CloseStatus(err), "read: %v", err)
	case <-ctx.Done():
		t.Fatal("timeout waiting for the websocket to close")
	}

	// the disconnected event is published before the transport closes
	require.NoError(t, tr.Close())
	ev := awaitConnectionEvent(ctx, t, events)
	assert.Equal(t, transport.ConnectionEventDisconnected, ev.Type)
	assert.Equal(t, transport.DisconnectReasonGatewayShutdown, ev.Reason)
	assert.Equal(t, "gw-1", ev.GatewayId)

	// the server no longer accepts connections
	_, _, err = dial()
	assert.Error(t, err)
}

func TestServeUntilDoneReturnsTheErrorOfAServer(t *testing.T) {
	errCh := make(chan error, 1)
	errCh <- errors.New("listen failed")
	err := serveUntilDone(context.Background(), errCh, func(context.Context) error {
		t.Error("the sockets are shut down after a server error")
		return nil
	}, time.Second)
	assert.EqualError(t, err, "listen failed")
}

func awaitConnectionEvent(ctx context.Context, t *testing.T, events chan *nats.Msg) transport.ConnectionEvent {
	t.Helper()
	select {
	case m := <-events:
		var ev transport.ConnectionEvent
		require.NoError(t, json.Unmarshal(m.Data, &ev))
		assert.Equal(t, "ocpp.conn.k7f2.CP001", m.Subject)
		return ev
	case <-ctx.Done():
		t.Fatal("timeout waiting for a connection event")
		return transport.ConnectionEvent{}
	}
}

func TestNatsTransportUsesTheSubjectPrefix(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, url := natstest.RunServer(t)
	tr, err := newTransport(transportSettings{
		kind:              "nats",
		natsUrls:          []string{url},
		natsName:          "maeve-gateway",
		natsSubjectPrefix: "csms",
	}, noopTracer())
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, tr.Close())
	}()
	require.IsType(t, &natstransport.Transport{}, tr)

	csms, err := nats.Connect(url)
	require.NoError(t, err)
	defer csms.Close()

	delivered := make(chan *pipe.GatewayMessage, 1)
	session := openSession(ctx, t, tr, delivered)
	defer func() {
		assert.NoError(t, session.Close(ctx))
	}()

	require.NoError(t, csms.Publish("csms.out.ocpp16.k7f2.CP001", callResult(t)))
	assertDelivered(ctx, t, delivered)
}

func TestMqttTransportUsesTheTopicPrefix(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	broker, addr := server.NewBroker(t)
	require.NoError(t, broker.Serve())
	t.Cleanup(func() {
		// the test broker can deadlock when it closes while a client disconnects
		assert.Eventually(t, func() bool { return atomic.LoadInt64(&broker.Info.ClientsConnected) == 0 },
			10*time.Second, time.Millisecond, "clients are still connected to the test broker")
		closed := make(chan error, 1)
		go func() { closed <- broker.Close() }()
		select {
		case <-closed:
		case <-time.After(10 * time.Second):
			t.Error("the test broker did not close")
		}
	})

	tr, err := newTransport(transportSettings{
		kind:       "mqtt",
		mqttAddr:   addr.String(),
		mqttPrefix: "csx",
	}, noopTracer())
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, tr.Close())
	}()
	require.IsType(t, &mqtt.Transport{}, tr)

	delivered := make(chan *pipe.GatewayMessage, 1)
	session := openSession(ctx, t, tr, delivered)

	// the subscription follows the connection: publish until it is there
	payload := callResult(t)
	for got := false; !got; {
		require.NoError(t, broker.Publish("csx/out/ocpp1.6/k7f2.CP001", payload, false, 0))
		select {
		case msg := <-delivered:
			assert.Equal(t, "1", msg.MessageId)
			got = true
		case <-time.After(20 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("timeout waiting for the message of the CSMS")
		}
	}

	require.NoError(t, session.Close(ctx))
}

func TestNewTransportRejectsBadSettings(t *testing.T) {
	for name, settings := range map[string]transportSettings{
		"unknown transport":   {kind: "amqp"},
		"no transport":        {},
		"nats without url":    {kind: "nats", natsSubjectPrefix: "ocpp"},
		"dotted nats prefix":  {kind: "nats", natsUrls: []string{natstransport.DefaultURL}, natsSubjectPrefix: "ocpp.cs"},
		"empty nats prefix":   {kind: "nats", natsUrls: []string{natstransport.DefaultURL}},
		"missing creds file":  {kind: "nats", natsUrls: []string{natstransport.DefaultURL}, natsSubjectPrefix: "ocpp", natsCreds: "testdata/missing.creds"},
		"invalid mqtt broker": {kind: "mqtt", mqttAddr: "mqtt://[::1", mqttPrefix: "cs"},
	} {
		t.Run(name, func(t *testing.T) {
			tr, err := newTransport(settings, noopTracer())
			assert.Error(t, err)
			assert.Nil(t, tr)
		})
	}
}

func noopTracer() trace.Tracer {
	return noop.NewTracerProvider().Tracer("")
}

func openSession(ctx context.Context, t *testing.T, tr transport.Transport, delivered chan *pipe.GatewayMessage) transport.Session {
	t.Helper()
	session, err := tr.Open(ctx, "k7f2.CP001", "ocpp1.6", func(msg *pipe.GatewayMessage) {
		select {
		case delivered <- msg:
		default:
		}
	})
	require.NoError(t, err)
	return session
}

func callResult(t *testing.T) []byte {
	t.Helper()
	data, err := json.Marshal(pipe.GatewayMessage{
		MessageType:     ocpp.MessageTypeCallResult,
		MessageId:       "1",
		ResponsePayload: json.RawMessage(`{}`),
	})
	require.NoError(t, err)
	return data
}

func assertDelivered(ctx context.Context, t *testing.T, delivered chan *pipe.GatewayMessage) {
	t.Helper()
	select {
	case msg := <-delivered:
		assert.Equal(t, ocpp.MessageTypeCallResult, msg.MessageType)
		assert.Equal(t, "1", msg.MessageId)
	case <-ctx.Done():
		t.Fatal("timeout waiting for the message of the CSMS")
	}
}
