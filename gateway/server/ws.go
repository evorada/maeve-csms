// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/thoughtworks/maeve-csms/gateway/ocpp"
	"github.com/thoughtworks/maeve-csms/gateway/pipe"
	"github.com/thoughtworks/maeve-csms/gateway/registry"
	"github.com/thoughtworks/maeve-csms/gateway/transport"
	"github.com/thoughtworks/maeve-csms/gateway/transport/mqtt"
	"golang.org/x/exp/slices"
	"golang.org/x/exp/slog"
	"nhooyr.io/websocket"
)

type WebsocketHandler struct {
	transport         transport.Transport
	mqttOpts          []mqtt.Opt
	deviceRegistry    registry.DeviceRegistry
	orgNames          []string
	pipeOptions       []pipe.Opt
	trustProxyHeaders bool
	tracer            trace.Tracer
}

type WebsocketOpt func(handler *WebsocketHandler)

// WithTransport sets the transport that connects the charge stations to the CSMS.
// Without it the handler uses an MQTT transport built from the WithMqtt options, which
// are ignored when a transport is set.
func WithTransport(t transport.Transport) WebsocketOpt {
	return func(handler *WebsocketHandler) {
		handler.transport = t
	}
}

func WithMqttBrokerUrl(brokerUrl *url.URL) WebsocketOpt {
	return func(handler *WebsocketHandler) {
		handler.mqttOpts = append(handler.mqttOpts, mqtt.WithBrokerURLs(brokerUrl))
	}
}

func WithMqttBrokerUrlString(brokerUrl string) WebsocketOpt {
	u, err := url.Parse(brokerUrl)
	if err != nil {
		panic(err)
	}
	return func(handler *WebsocketHandler) {
		handler.mqttOpts = append(handler.mqttOpts, mqtt.WithBrokerURLs(u))
	}
}

func WithMqttBrokerUrls(brokerUrls []*url.URL) WebsocketOpt {
	return func(handler *WebsocketHandler) {
		handler.mqttOpts = append(handler.mqttOpts, mqtt.WithBrokerURLs(brokerUrls...))
	}
}

func WithMqttTopicPrefix(topicPrefix string) WebsocketOpt {
	return func(handler *WebsocketHandler) {
		handler.mqttOpts = append(handler.mqttOpts, mqtt.WithTopicPrefix(topicPrefix))
	}
}

func WithMqttConnectSettings(mqttConnectTimeout, mqttConnectRetryDelay, mqttKeepAliveInterval time.Duration) WebsocketOpt {
	return func(handler *WebsocketHandler) {
		handler.mqttOpts = append(handler.mqttOpts, mqtt.WithConnectSettings(mqttConnectTimeout, mqttConnectRetryDelay, mqttKeepAliveInterval))
	}
}

func WithDeviceRegistry(deviceRegistry registry.DeviceRegistry) WebsocketOpt {
	return func(handler *WebsocketHandler) {
		handler.deviceRegistry = deviceRegistry
	}
}

func WithOrgName(orgName string) WebsocketOpt {
	return func(handler *WebsocketHandler) {
		handler.orgNames = append(handler.orgNames, orgName)
	}
}

func WithOrgNames(orgNames []string) WebsocketOpt {
	return func(handler *WebsocketHandler) {
		handler.orgNames = append(handler.orgNames, orgNames...)
	}
}

func WithTrustProxyHeaders(trustProxyHeaders bool) WebsocketOpt {
	return func(handler *WebsocketHandler) {
		handler.trustProxyHeaders = trustProxyHeaders
	}
}

func WithPipeOption(pipeOption pipe.Opt) WebsocketOpt {
	return func(handler *WebsocketHandler) {
		handler.pipeOptions = append(handler.pipeOptions, pipeOption)
	}
}

func WithPipeOptions(pipeOption []pipe.Opt) WebsocketOpt {
	return func(handler *WebsocketHandler) {
		handler.pipeOptions = append(handler.pipeOptions, pipeOption...)
	}
}

func WithOtelTracer(tracer trace.Tracer) WebsocketOpt {
	return func(handler *WebsocketHandler) {
		handler.tracer = tracer
	}
}

func NewWebsocketHandler(opts ...WebsocketOpt) http.Handler {
	s := new(WebsocketHandler)

	for _, opt := range opts {
		opt(s)
	}

	ensureDefaults(s)

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(TraceRequest(s.tracer))
	if s.trustProxyHeaders {
		r.Use(TLSOffload(s.deviceRegistry))
	}
	r.Handle("/ws/{id}", s)
	return r
}

func ensureDefaults(handler *WebsocketHandler) {
	if handler.deviceRegistry == nil {
		panic("must provide device registry implementation")
	}

	if handler.tracer == nil {
		handler.tracer = trace.NewNoopTracerProvider().Tracer("")
	}

	if handler.transport == nil {
		handler.transport = mqtt.New(append([]mqtt.Opt{mqtt.WithTracer(handler.tracer)}, handler.mqttOpts...)...)
	}
}

func (s *WebsocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	slog.Info("websocket connection received", "path", r.URL.Path, "method", r.Method)
	slog.Info("processing connection", "uri", r.RequestURI)

	span := trace.SpanFromContext(r.Context())

	clientId := chi.URLParam(r, "id")
	if err := validateStationId(clientId); err != nil {
		span.SetStatus(codes.Error, "invalid station id")
		span.RecordError(err)
		span.SetAttributes(semconv.HTTPStatusCode(http.StatusBadRequest))
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	span.SetAttributes(
		attribute.String("csId", clientId),
		attribute.String("ocpp.station_id", clientId))

	// The host that the charge station connected to names its tenant. Only the Host header
	// of the request counts: a forwarding header can be set by whoever connects.
	host := registry.NormalizeHost(r.Host)
	if host == "" {
		span.SetStatus(codes.Error, "no host")
		span.SetAttributes(semconv.HTTPStatusCode(http.StatusBadRequest))
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	span.SetAttributes(semconv.NetHostName(host))

	cs, err := s.deviceRegistry.LookupChargeStation(r.Context(), host, clientId)
	if err != nil {
		slog.Error("lookup charge station", "host", host, "stationId", clientId, "err", err)
		span.SetStatus(codes.Error, "lookup charge station failed")
		span.RecordError(err)
		span.SetAttributes(semconv.HTTPStatusCode(http.StatusInternalServerError))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if cs == nil {
		span.SetStatus(codes.Error, "unknown charge station")
		span.SetAttributes(semconv.HTTPStatusCode(http.StatusNotFound))
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	// The station key names the charge station from here on: on the message broker two
	// tenants must never share a client id or a topic, so there is no fallback to the
	// plain station id.
	stationKey := cs.StationKey
	if stationKey == "" {
		slog.Error("charge station without a station key", "host", host, "stationId", clientId)
		span.SetStatus(codes.Error, "charge station without a station key")
		span.SetAttributes(semconv.HTTPStatusCode(http.StatusInternalServerError))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	span.SetAttributes(
		attribute.String("csId", stationKey),
		attribute.String("tenant.org_key", cs.OrgKey),
		attribute.Int("ocpp.security_profile", int(cs.SecurityProfile)))

	switch cs.SecurityProfile {
	case registry.UnsecuredTransportWithBasicAuth:
		if r.TLS != nil || !checkAuthorization(r.Context(), r, cs) {
			if r.TLS != nil {
				span.SetAttributes(attribute.String("auth.failure_reason", "tls for unsecured transport"))
			}
			span.SetStatus(codes.Error, "unauthorized")
			span.SetAttributes(semconv.HTTPStatusCode(http.StatusUnauthorized))
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
	case registry.TLSWithBasicAuth:
		if r.TLS == nil || !checkAuthorization(r.Context(), r, cs) {
			if r.TLS == nil {
				span.SetAttributes(attribute.String("auth.failure_reason", "no tls for secured transport"))
			}
			span.SetStatus(codes.Error, "unauthorized")
			span.SetAttributes(semconv.HTTPStatusCode(http.StatusUnauthorized))
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
	case registry.TLSWithClientSideCertificates:
		if r.TLS == nil || !checkCertificate(r.Context(), r, s.orgNames, cs) {
			if r.TLS == nil {
				span.SetAttributes(attribute.String("auth.failure_reason", "no tls for secured transport"))
			}
			span.SetStatus(codes.Error, "unauthorized")
			span.SetAttributes(semconv.HTTPStatusCode(http.StatusUnauthorized))
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
	default:
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"ocpp2.0.1", "ocpp1.6"}, InsecureSkipVerify: true})
	if err != nil {
		span.SetAttributes(attribute.String("websocket.accept_failure_reason", err.Error()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	protocol := wsConn.Subprotocol()
	if protocol == "" {
		protocol = "ocpp2.0.1"
	}

	span.SetAttributes(attribute.String("ocpp.protocol", protocol))

	p := pipe.NewPipe(s.pipeOptions...)
	p.Start()
	defer p.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	session, err := s.transport.Open(ctx, stationKey, protocol, func(msg *pipe.GatewayMessage) {
		select {
		case p.CSMSRx <- msg:
		case <-ctx.Done():
		}
	})
	if err != nil {
		span.SetStatus(codes.Error, "opening transport session")
		span.RecordError(err)
		span.SetAttributes(semconv.HTTPStatusCode(http.StatusInternalServerError))
		slog.Error("opening transport session", "stationKey", stationKey, "err", err)
		_ = wsConn.Close(websocket.StatusProtocolError, http.StatusText(http.StatusInternalServerError))
		return
	}
	defer func() {
		err := session.Close(context.Background())
		if err != nil {
			slog.Error("closing transport session", "err", err)
		}
	}()

	// a session that breaks down closes the websocket: the charge station then connects again
	go func() {
		select {
		case <-session.Done():
			_ = wsConn.Close(websocket.StatusProtocolError, http.StatusText(http.StatusInternalServerError))
		case <-ctx.Done():
		}
	}()

	// we've finished connecting... complete this span so we get to see the details in the trace
	span.End()

	// listen on the CSMS Tx channel and send those messages to the CSMS
	goPublishToCSMS(ctx, p.CSMSTx, session)

	// listen the CS Tx channel and write those messages to the websocket
	goWriteToChargeStation(ctx, s.tracer, p.ChargeStationTx, wsConn, protocol, stationKey)

	// read from the websocket and send to the CS Rx channel (CS Tx used for error)
	readFromChargeStation(ctx, s.tracer, wsConn, p.ChargeStationRx, p.ChargeStationTx, protocol, stationKey)
}

func getScheme(r *http.Request) string {
	if r.TLS != nil {
		return "wss"
	}
	return "ws"
}

func checkAuthorization(ctx context.Context, r *http.Request, cs *registry.ChargeStation) bool {
	span := trace.SpanFromContext(ctx)

	username, password, ok := r.BasicAuth()
	if !ok {
		span.SetAttributes(attribute.String("auth.failure_reason", "no basic auth"))
		return false
	}
	if username != cs.ClientId && !cs.InvalidUsernameAllowed {
		span.SetAttributes(
			attribute.String("auth.failure_reason", "invalid username"),
			attribute.String("auth.username", username))
		return false
	}
	sha256pw := sha256.Sum256([]byte(password))
	b64sha256 := base64.StdEncoding.EncodeToString(sha256pw[:])
	result := b64sha256 == cs.Base64SHA256Password

	if !result {
		span.SetAttributes(attribute.String("auth.failure_reason", "invalid password"))
	}

	return result
}

// checkCertificate reports whether the client certificate of the connection is accepted
// for the charge station. orgNames are the organisation names that the gateway accepts
// for a charge station whose tenant has no list of its own.
func checkCertificate(ctx context.Context, r *http.Request, orgNames []string, cs *registry.ChargeStation) bool {
	span := trace.SpanFromContext(ctx)

	if len(r.TLS.PeerCertificates) == 0 {
		span.SetAttributes(attribute.String("auth.failure_reason", "no client certificate"))
		return false
	}

	leafCertificate := r.TLS.PeerCertificates[0]

	span.SetAttributes(
		attribute.StringSlice("auth.organization", leafCertificate.Subject.Organization),
		attribute.String("auth.common_name", leafCertificate.Subject.CommonName))

	// The list of the tenant takes the place of the list of the gateway. Only a list of a
	// tenant can be empty on purpose: such a tenant does not want the organisation checked.
	if cs.HasTenantOrgNames {
		if len(cs.AcceptedCertOrgNames) == 0 {
			span.SetAttributes(attribute.Bool("auth.organization_checked", false))
			return true
		}
		orgNames = cs.AcceptedCertOrgNames
	}

	foundOrg := false

	for _, org := range leafCertificate.Subject.Organization {

		if slices.Contains(orgNames, org) {
			foundOrg = true
			break
		}
	}
	if !foundOrg {
		span.SetAttributes(attribute.String("auth.failure_reason", "bad organization"))
		return false
	}

	//result := cs.ClientId == leafCertificate.Subject.CommonName
	//
	//if !result {
	//	span.SetAttributes(attribute.String("auth.failure_reason", "bad common name"))
	//}

	return foundOrg
}

func goPublishToCSMS(ctx context.Context, csmsTx chan *pipe.GatewayMessage, session transport.Session) {
	go func() {
		for {
			select {
			case msg := <-csmsTx:
				err := session.Send(msg.Context, msg)
				if err != nil {
					slog.Error("publishing message", "err", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func goWriteToChargeStation(ctx context.Context, tracer trace.Tracer, chargeStationTx chan *pipe.GatewayMessage, wsConn *websocket.Conn, protocol, stationKey string) {
	go func() {
		for {
			select {
			case msg := <-chargeStationTx:
				data, err := marshalGatewayMessageAsOcpp(msg)
				if err != nil {
					slog.Error("marshaling gateway message for charge station", "err", err)
					continue
				}
				err = write(msg.Context, tracer, wsConn, protocol, stationKey, msg.MessageId, data)
				if err != nil {
					slog.Error("writing to charge station", "err", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func write(ctx context.Context, tracer trace.Tracer, wsConn *websocket.Conn, protocol, stationKey, messageId string, data []byte) error {
	newCtx, span := tracer.Start(ctx, fmt.Sprintf("%s publish", protocol), trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			semconv.MessagingSystem("websocket"),
			semconv.MessagingMessagePayloadSizeBytes(len(data)),
			semconv.MessagingOperationKey.String("publish"),
			semconv.MessagingMessageConversationID(messageId),
			attribute.String("csId", stationKey),
		))
	defer span.End()

	return wsConn.Write(newCtx, websocket.MessageText, data)
}

func readFromChargeStation(ctx context.Context, tracer trace.Tracer, wsConn *websocket.Conn, csRx, csTx chan *pipe.GatewayMessage, protocol, stationKey string) {
	for {
		msg, err := read(ctx, tracer, wsConn, protocol, stationKey)
		if err != nil {
			if msg != nil {
				slog.Warn("sending error message to client", "err", err)
				csTx <- msg
				continue
			}
			// connection closed
			break
		} else if msg != nil {
			csRx <- msg
		} else {
			// deadline exceeded
			break
		}
	}
}

var errClient = errors.New("client error")

func read(ctx context.Context, tracer trace.Tracer, wsConn *websocket.Conn, protocol, stationKey string) (*pipe.GatewayMessage, error) {
	typ, b, err := wsConn.Read(context.Background())
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, nil
	} else if status := websocket.CloseStatus(err); status != -1 {
		slog.Info("connection closed with status", "status", status)
		return nil, err
	} else if errors.Is(err, io.EOF) {
		slog.Info("connection closed")
		return nil, err
	} else if err != nil {
		return nil, err
	}

	newCtx, span := tracer.Start(context.Background(), fmt.Sprintf("%s receive", protocol), trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			semconv.MessagingSystem("websocket"),
			semconv.MessagingOperationKey.String("receive"),
			semconv.MessagingMessagePayloadSizeBytes(len(b)),
			attribute.String("csId", stationKey),
		),
		trace.WithLinks(trace.Link{
			SpanContext: trace.SpanContextFromContext(ctx),
		}))
	defer span.End()

	if typ != websocket.MessageText {
		msg := pipe.GatewayMessage{
			Context:          newCtx,
			MessageType:      ocpp.MessageTypeCallError,
			MessageId:        "-1",
			ErrorCode:        ocpp.ErrorRpcFrameworkError,
			ErrorDescription: "websocket message type is not text",
		}
		span.SetAttributes(semconv.MessagingMessageConversationID("-1"))
		span.SetStatus(codes.Error, "unmarshal ocpp message error")
		span.RecordError(err)

		return &msg, errClient
	}

	msg, err := unmarshalOcppAsGatewayMessage(b)
	if err != nil {
		msg := pipe.GatewayMessage{
			Context:          newCtx,
			MessageType:      ocpp.MessageTypeCallError,
			MessageId:        "-1",
			ErrorCode:        ocpp.ErrorRpcFrameworkError,
			ErrorDescription: err.Error(),
		}
		span.SetAttributes(semconv.MessagingMessageConversationID("-1"))
		span.SetStatus(codes.Error, "unmarshal ocpp message error")
		span.RecordError(err)
		return &msg, errClient
	}

	msg.Context = newCtx

	span.SetAttributes(semconv.MessagingMessageConversationID(msg.MessageId))

	return msg, nil
}

func marshalGatewayMessageAsOcpp(msg *pipe.GatewayMessage) ([]byte, error) {
	var err error
	ocppMsg := ocpp.Message{}
	ocppMsg.MessageTypeId = msg.MessageType
	ocppMsg.MessageId = msg.MessageId

	switch msg.MessageType {
	case ocpp.MessageTypeCall:
		ocppMsg.Data = make([]json.RawMessage, 2)
		ocppMsg.Data[0], err = json.Marshal(msg.Action)
		if err != nil {
			return nil, err
		}
		ocppMsg.Data[1] = msg.RequestPayload
	case ocpp.MessageTypeCallResult:
		ocppMsg.Data = make([]json.RawMessage, 1)
		ocppMsg.Data[0] = msg.ResponsePayload
	case ocpp.MessageTypeCallError:
		ocppMsg.Data = make([]json.RawMessage, 3)
		ocppMsg.Data[0], err = json.Marshal(msg.ErrorCode)
		if err != nil {
			return nil, err
		}
		ocppMsg.Data[1], err = json.Marshal(msg.ErrorDescription)
		if err != nil {
			return nil, err
		}
		ocppMsg.Data[2] = json.RawMessage("{}")
	}

	return json.Marshal(ocppMsg)
}

func unmarshalOcppAsGatewayMessage(b []byte) (*pipe.GatewayMessage, error) {
	ocppMsg := ocpp.Message{}
	err := json.Unmarshal(b, &ocppMsg)
	if err != nil {
		return nil, err
	}
	msg := pipe.GatewayMessage{
		MessageType: ocppMsg.MessageTypeId,
		MessageId:   ocppMsg.MessageId,
	}
	switch ocppMsg.MessageTypeId {
	case ocpp.MessageTypeCall:
		err = json.Unmarshal(ocppMsg.Data[0], &msg.Action)
		if err != nil {
			return nil, err
		}
		msg.RequestPayload = ocppMsg.Data[1]
	case ocpp.MessageTypeCallResult:
		msg.ResponsePayload = ocppMsg.Data[0]
	case ocpp.MessageTypeCallError:
		err = json.Unmarshal(ocppMsg.Data[0], &msg.ErrorCode)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(ocppMsg.Data[1], &msg.ErrorDescription)
		if err != nil {
			return nil, err
		}
	}
	return &msg, nil
}
