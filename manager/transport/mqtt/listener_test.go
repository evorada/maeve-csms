package mqtt_test

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	server "github.com/mochi-co/mqtt/v2"
	"github.com/mochi-co/mqtt/v2/packets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"github.com/thoughtworks/maeve-csms/manager/transport/mqtt"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

func TestListenerProcessesMessagesReceivedFromTheBroker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// start the broker
	broker, clientUrl := mqtt.NewBroker(t)
	defer func() {
		err := broker.Close()
		assert.NoError(t, err)
	}()
	err := broker.Serve()
	require.NoError(t, err)

	// setup the handler
	receivedMsgCh := make(chan struct{})
	handler := func(ctx context.Context, chargeStationId string, msg *transport.Message) {
		assert.Equal(t, "cs001", chargeStationId)
		assert.Equal(t, transport.MessageTypeCall, msg.MessageType)
		assert.Equal(t, "Test", msg.Action)
		assert.Equal(t, "my-message-id", msg.MessageId)
		assert.Equal(t, json.RawMessage(`{"someKey":"someValue"}`), msg.RequestPayload)
		receivedMsgCh <- struct{}{}
	}

	// connect the listener to the broker
	listener := mqtt.NewListener(mqtt.WithMqttBrokerUrl[mqtt.Listener](clientUrl))
	conn, err := listener.Connect(ctx, transport.OcppVersion201, nil, transport.MessageHandlerFunc(handler))
	require.NoError(t, err)
	defer func() {
		if conn != nil {
			err := conn.Disconnect(ctx)
			require.NoError(t, err)
		}
	}()

	// publish message
	publishMessage(t, ctx, broker, transport.Message{
		MessageType:    transport.MessageTypeCall,
		Action:         "Test",
		MessageId:      "my-message-id",
		RequestPayload: json.RawMessage(`{"someKey":"someValue"}`),
	})

	// wait for message to be received / timeout
	select {
	case <-ctx.Done():
		assert.Fail(t, "timeout waiting for test to complete")
	case <-receivedMsgCh:
		// do nothing
	}
}

func TestListenerExtractsTraceIdFromPayload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tracer, exporter := testutil.GetTracer()

	// start the broker
	broker, clientUrl := mqtt.NewBroker(t)
	defer func() {
		err := broker.Close()
		assert.NoError(t, err)
	}()
	err := broker.Serve()
	require.NoError(t, err)

	// setup the handler
	receivedMsgCh := make(chan struct{})
	handler := func(ctx context.Context, chargeStationId string, msg *transport.Message) {
		receivedMsgCh <- struct{}{}
	}

	// connect the listener to the broker
	listener := mqtt.NewListener(
		mqtt.WithMqttBrokerUrl[mqtt.Listener](clientUrl),
		mqtt.WithOtelTracer[mqtt.Listener](tracer))
	conn, err := listener.Connect(ctx, transport.OcppVersion201, nil, transport.MessageHandlerFunc(handler))
	require.NoError(t, err)
	defer func() {
		if conn != nil {
			err := conn.Disconnect(ctx)
			require.NoError(t, err)
		}
	}()

	// publish message
	newCtx, span := tracer.Start(ctx, "test span")
	defer span.End()
	publishMessage(t, newCtx, broker, transport.Message{
		MessageType:    transport.MessageTypeCall,
		Action:         "Test",
		MessageId:      "my-message-id",
		RequestPayload: json.RawMessage(`{"someKey":"someValue"}`),
	})

	// wait for message to be received / timeout
	select {
	case <-ctx.Done():
		assert.Fail(t, "timeout waiting for test to complete")
	case <-receivedMsgCh:
		require.Greater(t, len(exporter.GetSpans()), 0)
		assert.True(t, exporter.GetSpans()[0].Parent.HasTraceID())
	}
}

func TestListenerAddsTraceInformation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tracer, exporter := testutil.GetTracer()

	// start the broker
	broker, clientUrl := mqtt.NewBroker(t)
	defer func() {
		err := broker.Close()
		assert.NoError(t, err)
	}()
	err := broker.Serve()
	require.NoError(t, err)

	// setup the handler
	receivedMsgCh := make(chan struct{})
	handler := func(ctx context.Context, chargeStationId string, msg *transport.Message) {
		receivedMsgCh <- struct{}{}
	}

	// connect the listener to the broker
	listener := mqtt.NewListener(
		mqtt.WithMqttBrokerUrl[mqtt.Listener](clientUrl),
		mqtt.WithOtelTracer[mqtt.Listener](tracer))
	conn, err := listener.Connect(ctx, transport.OcppVersion201, nil, transport.MessageHandlerFunc(handler))
	require.NoError(t, err)
	defer func() {
		if conn != nil {
			err := conn.Disconnect(ctx)
			require.NoError(t, err)
		}
	}()

	// publish message
	newCtx, span := tracer.Start(ctx, "test span")
	defer span.End()
	publishMessage(t, newCtx, broker, transport.Message{
		MessageType:    transport.MessageTypeCall,
		Action:         "Test",
		MessageId:      "my-message-id",
		RequestPayload: json.RawMessage(`{"someKey":"someValue"}`),
	})

	// wait for message to be received / timeout
	select {
	case <-ctx.Done():
		assert.Fail(t, "timeout waiting for test to complete")
	case <-receivedMsgCh:
		require.Greater(t, len(exporter.GetSpans()), 0)
		testutil.AssertSpan(t, &exporter.GetSpans()[0], "cs/in/ocpp2.0.1/# receive", map[string]any{
			"messaging.system":                     "mqtt",
			"messaging.operation":                  "receive",
			"messaging.message.payload_size_bytes": 81,
			"ocpp.version":                         "2.0.1",
			"csId":                                 "cs001",
			"call.action":                          "Test",
			"messaging.consumer.id": func(val attribute.Value) bool {
				return strings.HasPrefix(val.AsString(), "manager-")
			},
			"messaging.message.conversation_id": func(val attribute.Value) bool {
				return val.AsString() != ""
			},
		})
	}
}

func publishMessage(t *testing.T, ctx context.Context, broker *server.Server, msg transport.Message) {
	msgBytes, err := json.Marshal(msg)
	require.NoError(t, err)

	correlationMap := make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(correlationMap))
	correlationData, err := json.Marshal(correlationMap)
	require.NoError(t, err)

	cl := broker.NewClient(nil, "local", "inline", true)
	err = broker.InjectPacket(cl, packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type:   packets.Publish,
			Qos:    0,
			Retain: false,
		},
		TopicName: "cs/in/ocpp2.0.1/cs001",
		Payload:   msgBytes,
		PacketID:  uint16(0),
		Properties: packets.Properties{
			CorrelationData: correlationData,
		},
	})
	require.NoError(t, err)
}

// startConnEventBroker starts a test broker that is closed at the end of the test,
// once its clients have gone: the test broker can deadlock when it closes while a
// client disconnects.
func startConnEventBroker(t *testing.T) (*server.Server, *url.URL) {
	t.Helper()
	broker, clientUrl := mqtt.NewBroker(t)
	require.NoError(t, broker.Serve())
	t.Cleanup(func() {
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
	return broker, clientUrl
}

// publishConnectionEvent injects a connection event as the gateway publishes it.
func publishConnectionEvent(t *testing.T, broker *server.Server, topic string, payload []byte) {
	t.Helper()
	cl := broker.NewClient(nil, "local", "gateway", true)
	require.NoError(t, broker.InjectPacket(cl, packets.Packet{
		FixedHeader: packets.FixedHeader{Type: packets.Publish},
		TopicName:   topic,
		Payload:     payload,
	}))
}

func TestListenConnectionEventsReceivesTheEventsOfTheGateway(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	broker, clientUrl := startConnEventBroker(t)

	handled := make(chan *transport.ConnectionEvent, 16)
	listener := mqtt.NewListener(mqtt.WithMqttBrokerUrl[mqtt.Listener](clientUrl))
	conn, err := listener.ListenConnectionEvents(ctx, transport.ConnectionEventHandlerFunc(func(_ context.Context, ev *transport.ConnectionEvent) {
		handled <- ev
	}))
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, conn.Disconnect(ctx))
	}()

	next := func() *transport.ConnectionEvent {
		t.Helper()
		select {
		case ev := <-handled:
			return ev
		case <-ctx.Done():
			t.Fatal("timeout waiting for a connection event")
			return nil
		}
	}

	publishConnectionEvent(t, broker, "cs/conn/k7f2.CP.001", []byte(`{"type":"disconnected","station_key":"k7f2.CP.001",
		"ocpp_version":"ocpp1.6","connection_id":"0123456789abcdef0123456789abcdef","gateway_id":"gw-1",
		"at":"2026-11-27T12:00:00.123456789Z","remote_addr":"192.0.2.1:4711","close_code":1000,"reason":"closed"}`))
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
	}, next())

	// an event that names another station than its topic, or that cannot be decoded, is dropped
	publishConnectionEvent(t, broker, "cs/conn/k7f2.CP001", []byte(`{"type":"connected","station_key":"k7f2.CP002"}`))
	publishConnectionEvent(t, broker, "cs/conn/k7f2.CP001", []byte(`not json`))
	// an OCPP message is not a connection event
	publishConnectionEvent(t, broker, "cs/in/ocpp1.6/k7f2.CP001", []byte(`{"type":"connected","station_key":"k7f2.CP001"}`))
	publishConnectionEvent(t, broker, "cs/conn/k7f2.CP001", []byte(`{"type":"connected","station_key":"k7f2.CP001","connection_id":"good"}`))
	assert.Equal(t, "good", next().ConnectionId)
	select {
	case ev := <-handled:
		t.Errorf("unexpected event %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestListenConnectionEventsFailsWithoutABroker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	brokerUrl, err := url.Parse("mqtt://" + l.Addr().String())
	require.NoError(t, err)
	require.NoError(t, l.Close())

	listener := mqtt.NewListener(
		mqtt.WithMqttBrokerUrl[mqtt.Listener](brokerUrl),
		mqtt.WithMqttConnectSettings[mqtt.Listener](300*time.Millisecond, 50*time.Millisecond, time.Second))
	start := time.Now()
	conn, err := listener.ListenConnectionEvents(ctx, transport.ConnectionEventHandlerFunc(func(context.Context, *transport.ConnectionEvent) {
		t.Error("unexpected connection event")
	}))
	assert.Error(t, err)
	assert.Nil(t, conn)
	assert.Less(t, time.Since(start), 5*time.Second)
}
