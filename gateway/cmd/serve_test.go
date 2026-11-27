// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/gateway/ocpp"
	"github.com/thoughtworks/maeve-csms/gateway/pipe"
	"github.com/thoughtworks/maeve-csms/gateway/server"
	"github.com/thoughtworks/maeve-csms/gateway/transport"
	"github.com/thoughtworks/maeve-csms/gateway/transport/mqtt"
	natstransport "github.com/thoughtworks/maeve-csms/gateway/transport/nats"
	"github.com/thoughtworks/maeve-csms/gateway/transport/nats/natstest"
	"go.opentelemetry.io/otel/trace"
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
	return trace.NewNoopTracerProvider().Tracer("")
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
