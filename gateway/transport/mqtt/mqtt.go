// SPDX-License-Identifier: Apache-2.0

// Package mqtt is the transport of the gateway over an MQTT 5 broker. Every station
// socket has a connection of its own to the broker, with the station key as client id:
// the station's messages are published to <prefix>/in/<protocol>/<station key>, the
// messages of the CSMS arrive on <prefix>/out/<protocol>/<station key> and the
// connection events of the socket are published to <prefix>/conn/<station key>.
package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"github.com/thoughtworks/maeve-csms/gateway/pipe"
	"github.com/thoughtworks/maeve-csms/gateway/transport"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"golang.org/x/exp/slog"
)

// Transport opens one connection to the broker per station socket.
type Transport struct {
	brokerURLs     []*url.URL
	prefix         string
	connectTimeout time.Duration
	retryDelay     time.Duration
	keepAlive      uint16
	tracer         trace.Tracer
}

type Opt func(*Transport)

// WithBrokerURLs adds broker URLs; without one the transport uses
// mqtt://127.0.0.1:1883/.
func WithBrokerURLs(brokerURLs ...*url.URL) Opt {
	return func(t *Transport) {
		t.brokerURLs = append(t.brokerURLs, brokerURLs...)
	}
}

// WithTopicPrefix sets the first level of the topics (empty by default).
func WithTopicPrefix(prefix string) Opt {
	return func(t *Transport) {
		t.prefix = prefix
	}
}

// WithConnectSettings sets how long Open waits for the broker (5s by default), how
// long the connection waits between attempts (1s by default) and the keep alive
// interval, rounded to seconds (10s by default). A zero value keeps the default.
func WithConnectSettings(connectTimeout, connectRetryDelay, keepAliveInterval time.Duration) Opt {
	return func(t *Transport) {
		t.connectTimeout = connectTimeout
		t.retryDelay = connectRetryDelay
		t.keepAlive = uint16(keepAliveInterval.Round(time.Second).Seconds())
	}
}

// WithTracer sets the tracer of the publish and receive spans.
func WithTracer(tracer trace.Tracer) Opt {
	return func(t *Transport) {
		t.tracer = tracer
	}
}

func New(opts ...Opt) *Transport {
	t := new(Transport)

	for _, opt := range opts {
		opt(t)
	}

	if t.brokerURLs == nil {
		u, err := url.Parse("mqtt://127.0.0.1:1883/")
		if err != nil {
			panic(err)
		}
		t.brokerURLs = []*url.URL{u}
	}

	if t.connectTimeout == 0 {
		t.connectTimeout = 5 * time.Second
	}

	if t.retryDelay == 0 {
		t.retryDelay = 1 * time.Second
	}

	if t.keepAlive == 0 {
		t.keepAlive = 10
	}

	if t.tracer == nil {
		t.tracer = noop.NewTracerProvider().Tracer("")
	}

	return t
}

// Open connects to the broker with the station key as client id; each time the
// connection comes up it subscribes to the out topic of the station. The connection
// lives until the session is closed or ctx is done. Open fails when the broker is not
// reached within the connect timeout.
func (t *Transport) Open(ctx context.Context, stationKey, ocppProtocol string, deliver func(msg *pipe.GatewayMessage)) (transport.Session, error) {
	span := trace.SpanFromContext(ctx)

	brokerURLStrings := make([]string, len(t.brokerURLs))
	for i, u := range t.brokerURLs {
		brokerURLStrings[i] = u.String()
	}
	span.SetAttributes(attribute.StringSlice("mqtt.broker_urls", brokerURLStrings))

	ctx, cancel := context.WithCancel(ctx)
	s := &session{
		cancel:     cancel,
		tracer:     t.tracer,
		prefix:     t.prefix,
		protocol:   ocppProtocol,
		stationKey: stationKey,
		failed:     make(chan struct{}),
	}

	conn, err := autopaho.NewConnection(ctx, autopaho.ClientConfig{
		BrokerUrls:        t.brokerURLs,
		KeepAlive:         t.keepAlive,
		ConnectRetryDelay: t.retryDelay,
		OnConnectionUp: func(manager *autopaho.ConnectionManager, connack *paho.Connack) {
			topicName := fmt.Sprintf("%s/out/%s/%s", t.prefix, ocppProtocol, stationKey)
			span.SetAttributes(attribute.String("mqtt.topic", topicName))
			// a variable of its own: this runs in a goroutine of the connection manager
			_, err := manager.Subscribe(ctx, &paho.Subscribe{
				Subscriptions: map[string]paho.SubscribeOptions{
					topicName: {},
				},
			})
			if err != nil {
				span.SetStatus(codes.Error, "subscribing to mqtt topic failed")
				span.RecordError(err)
				span.SetAttributes(semconv.HTTPStatusCode(http.StatusInternalServerError))

				s.fail()
				return
			}
		},
		ClientConfig: paho.ClientConfig{
			ClientID: stationKey,
			Router: paho.NewSingleHandlerRouter(func(mqttMsg *paho.Publish) {
				// route requests from the CSMS
				var msg pipe.GatewayMessage
				err := json.Unmarshal(mqttMsg.Payload, &msg)
				if err != nil {
					slog.Error("unmarshalling CSMS message", "err", err)
					return
				}

				correlationMap := make(map[string]string)
				err = json.Unmarshal(mqttMsg.Properties.CorrelationData, &correlationMap)
				if err != nil {
					slog.Warn("unmarshalling correlation map", "err", err)
				}
				requestContext := otel.GetTextMapPropagator().Extract(context.Background(), propagation.MapCarrier(correlationMap))

				newCtx, span := t.tracer.Start(requestContext, fmt.Sprintf("%s/out/%s/# receive", t.prefix, ocppProtocol),
					trace.WithSpanKind(trace.SpanKindConsumer),
					trace.WithAttributes(
						semconv.MessagingSystem("mqtt"),
						semconv.MessagingMessagePayloadSizeBytes(len(mqttMsg.Payload)),
						semconv.MessagingMessageConversationID(msg.MessageId),
						semconv.MessagingOperationKey.String("receive"),
						attribute.String("csId", stationKey),
					))
				defer span.End()

				msg.Context = newCtx

				deliver(&msg)
			}),
			OnServerDisconnect: func(disconnect *paho.Disconnect) {
				span.SetAttributes(attribute.String("mqtt.disconnect_reason", disconnect.Properties.ReasonString))
			},
		},
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("connecting to mqtt %v: %w", brokerURLStrings, err)
	}
	s.conn = conn

	// The connection manager retries until the broker answers. A station socket does
	// not wait for longer than the connect timeout: it is closed instead, and the
	// connection manager is stopped.
	awaitCtx, cancelAwait := context.WithTimeout(ctx, t.connectTimeout)
	defer cancelAwait()
	err = conn.AwaitConnection(awaitCtx)
	if err != nil {
		cancel()
		<-conn.Done()
		return nil, fmt.Errorf("waiting for mqtt %v: %w", brokerURLStrings, err)
	}

	return s, nil
}

// Close does nothing: every session has a connection of its own.
func (t *Transport) Close() error {
	return nil
}

type session struct {
	conn       *autopaho.ConnectionManager
	cancel     context.CancelFunc
	tracer     trace.Tracer
	prefix     string
	protocol   string
	stationKey string
	failed     chan struct{}
	failOnce   sync.Once
}

func (s *session) fail() {
	s.failOnce.Do(func() {
		close(s.failed)
	})
}

// Send publishes the message on the in topic of the station, with its out topic as
// response topic and the trace context as correlation data.
func (s *session) Send(ctx context.Context, msg *pipe.GatewayMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling message for publication: %w", err)
	}
	return publish(ctx, s.tracer, s.conn, s.prefix, s.protocol, s.stationKey, msg.MessageId, data)
}

// SendConnectionEvent publishes the event on the connection event topic of the
// station, on the connection of the session.
func (s *session) SendConnectionEvent(ctx context.Context, ev *transport.ConnectionEvent) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshaling connection event: %w", err)
	}
	topic := fmt.Sprintf("%s/conn/%s", s.prefix, s.stationKey)
	_, err = s.conn.Publish(ctx, &paho.Publish{
		Topic:   topic,
		Payload: data,
		Properties: &paho.PublishProperties{
			ContentType: "application/json",
		},
	})
	if err != nil {
		return fmt.Errorf("publishing to %s: %w", topic, err)
	}
	return nil
}

// Done is closed when subscribing to the out topic fails.
func (s *session) Done() <-chan struct{} {
	return s.failed
}

// Close disconnects from the broker.
func (s *session) Close(ctx context.Context) error {
	defer s.cancel()
	return s.conn.Disconnect(ctx)
}

func publish(ctx context.Context, tracer trace.Tracer, mqttConn *autopaho.ConnectionManager, topicPrefix, protocol, stationKey, messageId string, data []byte) error {
	topic := fmt.Sprintf("%s/in/%s/%s", topicPrefix, protocol, stationKey)

	newCtx, span := tracer.Start(ctx,
		fmt.Sprintf("%s/in/%s/# publish", topicPrefix, protocol),
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			semconv.MessagingSystem("mqtt"),
			semconv.MessagingMessagePayloadSizeBytes(len(data)),
			semconv.MessagingOperationKey.String("publish"),
			semconv.MessagingMessageConversationID(messageId),
			attribute.String("csId", stationKey),
		))
	defer span.End()

	correlationMap := make(map[string]string)
	otel.GetTextMapPropagator().Inject(newCtx, propagation.MapCarrier(correlationMap))

	correlationData, err := json.Marshal(correlationMap)
	if err != nil {
		slog.Warn("marshalling correlation map", "err", err)
	}

	_, err = mqttConn.Publish(newCtx, &paho.Publish{
		Topic:   topic,
		Payload: data,
		Properties: &paho.PublishProperties{
			ContentType:     "application/json",
			ResponseTopic:   fmt.Sprintf("%s/out/%s/%s", topicPrefix, protocol, stationKey),
			CorrelationData: correlationData,
		},
	})

	return err
}
