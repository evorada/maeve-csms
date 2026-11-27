// SPDX-License-Identifier: Apache-2.0

package server_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"github.com/mochi-co/mqtt/v2"
	"github.com/mochi-co/mqtt/v2/packets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/gateway/ocpp"
	"github.com/thoughtworks/maeve-csms/gateway/pipe"
	"github.com/thoughtworks/maeve-csms/gateway/registry"
	"github.com/thoughtworks/maeve-csms/gateway/server"
	"nhooyr.io/websocket"
)

// testHost is the host that a client of a httptest server connects to.
const testHost = "127.0.0.1"

// passwordHash is the base64 encoded SHA-256 hash of "password".
const passwordHash = "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg="

func TestWebSocketHandler(t *testing.T) {
	//defer goleak.VerifyNone(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	broker, addr := startBroker(t)

	// simulate manager connection
	client, err := autopaho.NewConnection(ctx, autopaho.ClientConfig{
		BrokerUrls:        []*url.URL{addr},
		KeepAlive:         10,
		ConnectRetryDelay: 2 * time.Second,
		OnConnectionUp: func(manager *autopaho.ConnectionManager, connack *paho.Connack) {
			// a variable of its own: this runs in a goroutine of the connection manager
			_, err := manager.Subscribe(ctx, &paho.Subscribe{
				Subscriptions: map[string]paho.SubscribeOptions{
					"cs/in/ocpp2.0.1/+": {},
				},
			})
			require.NoError(t, err)
		},
		ClientConfig: paho.ClientConfig{
			ClientID: "test",
			Router: paho.NewSingleHandlerRouter(func(publish *paho.Publish) {
				var reqMsg pipe.GatewayMessage
				err := json.Unmarshal(publish.Payload, &reqMsg)
				require.NoError(t, err)

				respMsg := pipe.GatewayMessage{
					MessageType:     ocpp.MessageTypeCallResult,
					MessageId:       reqMsg.MessageId,
					ResponsePayload: reqMsg.RequestPayload,
				}

				b, err := json.Marshal(respMsg)
				require.NoError(t, err)
				err = broker.Publish(publish.Properties.ResponseTopic, b, false, 0)
				require.NoError(t, err)
			}),
		},
	})
	defer func() {
		err := client.Disconnect(ctx)
		if err != nil {
			t.Logf("WARN: mqtt client disconnect: %v", err)
		}
	}()

	err = client.AwaitConnection(ctx)
	require.NoError(t, err)

	cs := &registry.ChargeStation{
		ClientId:             "cs1",
		SecurityProfile:      registry.UnsecuredTransportWithBasicAuth,
		Base64SHA256Password: "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg=", // password
	}

	mockRegistry := registry.NewMockRegistry()
	mockRegistry.Add(testHost, cs)

	srv := startGateway(t,
		server.WithMqttBrokerUrl(addr),
		server.WithMqttTopicPrefix("cs"),
		server.WithDeviceRegistry(mockRegistry),
		server.WithMqttConnectSettings(15*time.Second, 15*time.Second, 5*time.Second))

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", cs.ClientId, "password")))
	dialOptions := &websocket.DialOptions{
		Subprotocols: []string{"ocpp1.6", "ocpp2.0.1"},
		HTTPHeader: http.Header{
			"authorization": []string{authHeader},
		},
	}

	conn, _, err := websocket.Dial(ctx, fmt.Sprintf("%s/ws/cs1", srv.URL), dialOptions)
	if err != nil {
		t.Fatalf("dialing server: %v", err)
	}
	defer func() {
		err := conn.Close(websocket.StatusNormalClosure, "OK")
		if err != nil {
			t.Logf("WARN: closing websocket connection: %v", err)
		}
	}()

	if conn.Subprotocol() != "ocpp2.0.1" {
		t.Errorf("subprotocol: want %s, got %s", "ocpp2.0.1", conn.Subprotocol())
	}

	call := ocpp.Message{
		MessageTypeId: ocpp.MessageTypeCall,
		MessageId:     "1",
		Data: []json.RawMessage{
			json.RawMessage(`"EchoRequest"`),
			json.RawMessage(`"Payload"`),
		},
	}
	data, err := json.Marshal(call)
	if err != nil {
		t.Fatalf("encoding call: %v", err)
	}

	err = conn.Write(ctx, websocket.MessageText, data)
	if err != nil {
		t.Fatalf("sending message: %v", err)
	}

	typ, b, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("receiving message: %v", err)
	}
	if typ != websocket.MessageText {
		t.Errorf("message type: want %d, got %d", websocket.MessageText, typ)
	}
	var msg ocpp.Message
	err = json.Unmarshal(b, &msg)
	if err != nil {
		t.Errorf("unmarshaling message: %v", err)
	}

	doneCh := make(chan struct{}, 1)
	if msg.MessageTypeId == ocpp.MessageTypeCallResult {
		if string(msg.Data[0]) != `"Payload"` {
			t.Fatalf("call result: want %s, got %s", "Payload", string(msg.Data[0]))
		}
		doneCh <- struct{}{}
	} else if msg.MessageTypeId == ocpp.MessageTypeCallError {
		t.Fatalf("received error: %s", msg.Data[1])
	}

	select {
	case <-doneCh:
		// do nothing
	case <-ctx.Done():
		t.Fatal("timeout waiting for test to complete")
	}
}

func TestConnectionFromUnknownChargeStation(t *testing.T) {
	//defer goleak.VerifyNone(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockRegistry := registry.NewMockRegistry()

	srv := httptest.NewServer(server.NewWebsocketHandler(server.WithDeviceRegistry(mockRegistry)))
	defer srv.Close()

	dialOptions := &websocket.DialOptions{
		Subprotocols: []string{"ocpp1.6", "ocpp2.0.1"},
	}

	conn, resp, err := websocket.Dial(ctx, fmt.Sprintf("%s/ws/unknownCS1", srv.URL), dialOptions)
	if err == nil {
		_ = conn.Close(websocket.StatusGoingAway, "Shutdown")
		t.Fatalf("expected error dialing CSMS")
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status code: want %d, got %d", http.StatusNotFound, resp.StatusCode)
	}
}

func TestHttpConnectionWithBasicAuth(t *testing.T) {
	//defer goleak.VerifyNone(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs := &registry.ChargeStation{
		ClientId:             "basicAuthCS1",
		SecurityProfile:      registry.UnsecuredTransportWithBasicAuth,
		Base64SHA256Password: "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg=", // password,
	}

	mockRegistry := registry.NewMockRegistry()
	mockRegistry.Add(testHost, cs)

	srv := httptest.NewServer(server.NewWebsocketHandler(server.WithDeviceRegistry(mockRegistry)))
	defer srv.Close()

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", cs.ClientId, "password")))
	dialOptions := &websocket.DialOptions{
		Subprotocols: []string{"ocpp1.6", "ocpp2.0.1"},
		HTTPHeader: http.Header{
			"authorization": []string{authHeader},
		},
	}

	conn, resp, err := websocket.Dial(ctx, fmt.Sprintf("%s/ws/%s", srv.URL, cs.ClientId), dialOptions)
	if err != nil {
		t.Fatalf("dialing CSMS: %v", err)
	}
	defer func() {
		err := conn.Close(websocket.StatusGoingAway, "Shutdown")
		if err != nil {
			t.Logf("WARN: websocket close: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status code: want %d, got %d", http.StatusSwitchingProtocols, resp.StatusCode)
	}
}

func TestHttpConnectionWithBasicAuthWrongPassword(t *testing.T) {
	//defer goleak.VerifyNone(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs := &registry.ChargeStation{
		ClientId:             "basicAuthCS1WrongPassword",
		SecurityProfile:      registry.UnsecuredTransportWithBasicAuth,
		Base64SHA256Password: "bPYV1byqx3g1Ko8fM2DSPwLzTsGC4lmJf9bOSF14cNQ=", // password2,
	}

	mockRegistry := registry.NewMockRegistry()
	mockRegistry.Add(testHost, cs)

	srv := httptest.NewServer(server.NewWebsocketHandler(server.WithDeviceRegistry(mockRegistry)))
	defer srv.Close()

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", cs.ClientId, "password")))
	dialOptions := &websocket.DialOptions{
		Subprotocols: []string{"ocpp1.6", "ocpp2.0.1"},
		HTTPHeader: http.Header{
			"authorization": []string{authHeader},
		},
	}

	conn, resp, err := websocket.Dial(ctx, fmt.Sprintf("%s/ws/%s", srv.URL, cs.ClientId), dialOptions)
	if err == nil {
		_ = conn.Close(websocket.StatusGoingAway, "Shutdown")
		t.Fatalf("expected error dialing CSMS")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status code: want %d, got %d", http.StatusUnauthorized, resp.StatusCode)
	}
}

func TestHttpConnectionWithBasicAuthWrongUsername(t *testing.T) {
	//defer goleak.VerifyNone(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs := &registry.ChargeStation{
		ClientId:             "basicAuthCS1WrongUsername",
		SecurityProfile:      registry.UnsecuredTransportWithBasicAuth,
		Base64SHA256Password: "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg=", // password,
	}

	mockRegistry := registry.NewMockRegistry()
	mockRegistry.Add(testHost, cs)

	srv := httptest.NewServer(server.NewWebsocketHandler(server.WithDeviceRegistry(mockRegistry)))
	defer srv.Close()

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", "wrong", "password")))
	dialOptions := &websocket.DialOptions{
		Subprotocols: []string{"ocpp1.6", "ocpp2.0.1"},
		HTTPHeader: http.Header{
			"authorization": []string{authHeader},
		},
	}

	conn, resp, err := websocket.Dial(ctx, fmt.Sprintf("%s/ws/%s", srv.URL, cs.ClientId), dialOptions)
	if err == nil {
		_ = conn.Close(websocket.StatusGoingAway, "Shutdown")
		t.Fatalf("expected error dialing CSMS")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status code: want %d, got %d", http.StatusUnauthorized, resp.StatusCode)
	}
}

func TestHttpConnectionWithBasicAuthInvalidUsernameAllowed(t *testing.T) {
	//defer goleak.VerifyNone(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs := &registry.ChargeStation{
		ClientId:               "basicAuthCS1WrongUsername",
		SecurityProfile:        registry.UnsecuredTransportWithBasicAuth,
		Base64SHA256Password:   "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg=", // password,
		InvalidUsernameAllowed: true,
	}

	mockRegistry := registry.NewMockRegistry()
	mockRegistry.Add(testHost, cs)

	srv := httptest.NewServer(server.NewWebsocketHandler(server.WithDeviceRegistry(mockRegistry)))
	defer srv.Close()

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", "wrong", "password")))
	dialOptions := &websocket.DialOptions{
		Subprotocols: []string{"ocpp1.6", "ocpp2.0.1"},
		HTTPHeader: http.Header{
			"authorization": []string{authHeader},
		},
	}

	conn, resp, err := websocket.Dial(ctx, fmt.Sprintf("%s/ws/%s", srv.URL, cs.ClientId), dialOptions)
	if err != nil {
		t.Fatalf("dialing CSMS: %v", err)
	}
	defer func() {
		err := conn.Close(websocket.StatusGoingAway, "Shutdown")
		if err != nil {
			t.Logf("WARN: websocket close: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status code: want %d, got %d", http.StatusSwitchingProtocols, resp.StatusCode)
	}
}

func TestTlsConnectionWithBasicAuth(t *testing.T) {
	//defer goleak.VerifyNone(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs := &registry.ChargeStation{
		ClientId:             "tlsWithBasicAuthCS1",
		SecurityProfile:      registry.TLSWithBasicAuth,
		Base64SHA256Password: "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg=", // password,
	}

	mockRegistry := registry.NewMockRegistry()
	mockRegistry.Add(testHost, cs)

	srv := httptest.NewTLSServer(server.NewWebsocketHandler(server.WithDeviceRegistry(mockRegistry)))
	defer srv.Close()

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", cs.ClientId, "password")))
	dialOptions := &websocket.DialOptions{
		HTTPClient:   srv.Client(),
		Subprotocols: []string{"ocpp1.6", "ocpp2.0.1"},
		HTTPHeader: http.Header{
			"authorization": []string{authHeader},
		},
	}

	conn, resp, err := websocket.Dial(ctx, fmt.Sprintf("%s/ws/%s", srv.URL, cs.ClientId), dialOptions)
	if err != nil {
		t.Fatalf("dialing CSMS: %v", err)
	}
	defer func() {
		err := conn.Close(websocket.StatusGoingAway, "Shutdown")
		if err != nil {
			t.Logf("WARN: websocket close: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status code: want %d, got %d", http.StatusSwitchingProtocols, resp.StatusCode)
	}
}

func TestTlsConnectionWithBasicAuthWrongPassword(t *testing.T) {
	//defer goleak.VerifyNone(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs := &registry.ChargeStation{
		ClientId:             "tlsWithBasicAuthCS1WrongPassword",
		SecurityProfile:      registry.TLSWithBasicAuth,
		Base64SHA256Password: "bPYV1byqx3g1Ko8fM2DSPwLzTsGC4lmJf9bOSF14cNQ=", // password2,
	}

	mockRegistry := registry.NewMockRegistry()
	mockRegistry.Add(testHost, cs)

	srv := httptest.NewServer(server.NewWebsocketHandler(server.WithDeviceRegistry(mockRegistry)))
	defer srv.Close()

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", cs.ClientId, "password")))
	dialOptions := &websocket.DialOptions{
		HTTPClient:   srv.Client(),
		Subprotocols: []string{"ocpp1.6", "ocpp2.0.1"},
		HTTPHeader: http.Header{
			"authorization": []string{authHeader},
		},
	}

	conn, resp, err := websocket.Dial(ctx, fmt.Sprintf("%s/ws/%s", srv.URL, cs.ClientId), dialOptions)
	if err == nil {
		_ = conn.Close(websocket.StatusGoingAway, "Shutdown")
		t.Fatalf("expected error dialing CSMS")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status code: want %d, got %d", http.StatusUnauthorized, resp.StatusCode)
	}
}

func TestTlsConnectionWithCertificateAuth(t *testing.T) {
	//defer goleak.VerifyNone(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs := &registry.ChargeStation{
		ClientId:        "tlsWithCertificateAuth",
		SecurityProfile: registry.TLSWithClientSideCertificates,
	}

	caCert, _, clientCert, clientKeyPair := createTestKeyPairs(t, cs.ClientId)

	mockRegistry := registry.NewMockRegistry()
	mockRegistry.Add(testHost, cs)

	srv := httptest.NewUnstartedServer(server.NewWebsocketHandler(
		server.WithDeviceRegistry(mockRegistry),
		server.WithOrgName("Thoughtworks")))
	clientCAPool := x509.NewCertPool()
	clientCAPool.AddCert(caCert)
	srv.TLS = &tls.Config{
		ClientCAs:  clientCAPool,
		ClientAuth: tls.VerifyClientCertIfGiven,
	}
	srv.StartTLS()
	defer srv.Close()

	rootCAPool := x509.NewCertPool()
	rootCAPool.AddCert(srv.Certificate())

	clientTlsCert := tls.Certificate{
		Certificate: [][]byte{clientCert.Raw, caCert.Raw},
		PrivateKey:  clientKeyPair,
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:      rootCAPool,
				Certificates: []tls.Certificate{clientTlsCert},
			},
		},
	}
	dialOptions := &websocket.DialOptions{
		HTTPClient:   httpClient,
		Subprotocols: []string{"ocpp1.6", "ocpp2.0.1"},
	}

	conn, resp, err := websocket.Dial(ctx, fmt.Sprintf("%s/ws/%s", srv.URL, cs.ClientId), dialOptions)
	if err != nil {
		t.Fatalf("dialing CSMS: %v", err)
	}
	defer func() {
		err := conn.Close(websocket.StatusGoingAway, "Shutdown")
		if err != nil {
			t.Logf("WARN: websocket close: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status code: want %d, got %d", http.StatusSwitchingProtocols, resp.StatusCode)
	}
}

func TestTlsConnectionWithCertificateAuthUntrustedRoot(t *testing.T) {
	//defer goleak.VerifyNone(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs := &registry.ChargeStation{
		ClientId:        "tlsWithCertificateAuthUntrustedRoot",
		SecurityProfile: registry.TLSWithClientSideCertificates,
	}

	caCert, _, clientCert, clientKeyPair := createTestKeyPairs(t, cs.ClientId)

	mockRegistry := registry.NewMockRegistry()
	mockRegistry.Add(testHost, cs)

	srv := httptest.NewUnstartedServer(server.NewWebsocketHandler(
		server.WithDeviceRegistry(mockRegistry),
		server.WithOrgName("Thoughtworks")))
	srv.TLS = &tls.Config{
		ClientAuth: tls.VerifyClientCertIfGiven,
	}
	srv.StartTLS()
	defer srv.Close()

	rootCAPool := x509.NewCertPool()
	rootCAPool.AddCert(srv.Certificate())

	clientTlsCert := tls.Certificate{
		Certificate: [][]byte{clientCert.Raw, caCert.Raw},
		PrivateKey:  clientKeyPair,
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:      rootCAPool,
				Certificates: []tls.Certificate{clientTlsCert},
			},
		},
	}
	dialOptions := &websocket.DialOptions{
		HTTPClient:   httpClient,
		Subprotocols: []string{"ocpp1.6", "ocpp2.0.1"},
	}

	conn, _, err := websocket.Dial(ctx, fmt.Sprintf("%s/ws/%s", srv.URL, cs.ClientId), dialOptions)
	if err == nil {
		_ = conn.Close(websocket.StatusGoingAway, "Shutdown")
		t.Fatalf("expected error dialing CSMS")
	}
}

func TestTlsConnectionWithCertificateAuthExpired(t *testing.T) {
	//defer goleak.VerifyNone(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs := &registry.ChargeStation{
		ClientId:        "tlsWithCertificateAuthExpired",
		SecurityProfile: registry.TLSWithClientSideCertificates,
	}

	caCert, _, clientCert, clientKeyPair := createTestKeyPairs(t, cs.ClientId)

	mockRegistry := registry.NewMockRegistry()
	mockRegistry.Add(testHost, cs)

	srv := httptest.NewUnstartedServer(server.NewWebsocketHandler(
		server.WithDeviceRegistry(mockRegistry),
		server.WithOrgName("Thoughtworks")))
	clientCAPool := x509.NewCertPool()
	clientCAPool.AddCert(caCert)
	srv.TLS = &tls.Config{
		Time:       func() time.Time { return time.Now().Add(10 * time.Minute) },
		ClientCAs:  clientCAPool,
		ClientAuth: tls.VerifyClientCertIfGiven,
	}
	srv.StartTLS()
	defer srv.Close()

	rootCAPool := x509.NewCertPool()
	rootCAPool.AddCert(srv.Certificate())

	clientTlsCert := tls.Certificate{
		Certificate: [][]byte{clientCert.Raw, caCert.Raw},
		PrivateKey:  clientKeyPair,
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:      rootCAPool,
				Certificates: []tls.Certificate{clientTlsCert},
			},
		},
	}
	dialOptions := &websocket.DialOptions{
		HTTPClient:   httpClient,
		Subprotocols: []string{"ocpp1.6", "ocpp2.0.1"},
	}

	_, _, err := websocket.Dial(ctx, fmt.Sprintf("%s/ws/%s", srv.URL, cs.ClientId), dialOptions)
	if err == nil {
		t.Fatalf("expected error dialing CSMS")
	}
}

// recordingRegistry is a mock registry that records the charge station lookups.
type recordingRegistry struct {
	*registry.MockRegistry
	mu      sync.Mutex
	lookups []string
}

func newRecordingRegistry() *recordingRegistry {
	return &recordingRegistry{MockRegistry: registry.NewMockRegistry()}
}

func (r *recordingRegistry) LookupChargeStation(ctx context.Context, host, clientId string) (*registry.ChargeStation, error) {
	r.mu.Lock()
	r.lookups = append(r.lookups, host+"/"+clientId)
	r.mu.Unlock()
	return r.MockRegistry.LookupChargeStation(ctx, host, clientId)
}

func (r *recordingRegistry) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lookups...)
}

// teardownTimeout is how long the end of a test waits for the gateway and for the test
// broker before it fails the test.
const teardownTimeout = 10 * time.Second

// startBroker starts a test broker with the hooks; it is closed when the test ends.
//
// The test broker must not be closed while one of its clients connects or disconnects.
// Its Close reads the clients under a read lock that it takes twice
// (Clients.GetByListener calls Clients.Len); a client that is added or removed in
// between asks for the write lock, and then neither of them gets any further. So the
// end of the test first waits until every client has gone, which needs the gateway to
// be served with startGateway, and nothing here waits without a deadline.
func startBroker(t *testing.T, hooks ...mqtt.Hook) (*mqtt.Server, *url.URL) {
	t.Helper()

	broker, addr := server.NewBroker(t)
	for _, hook := range hooks {
		require.NoError(t, broker.AddHook(hook, nil))
	}
	require.NoError(t, broker.Serve())
	t.Cleanup(func() {
		// counted from before a client is added until after it has been removed
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
	return broker, addr
}

// startGateway serves the websocket handler with the options on a test server. The
// server is closed when the test ends, and the end of the test then waits until the
// handler has returned for every connection: a websocket outlives the close of the
// server, and only when its handler has returned has the gateway left the broker.
func startGateway(t *testing.T, opts ...server.WebsocketOpt) *httptest.Server {
	t.Helper()
	return serveGateway(t, nil, opts...)
}

// startGatewayTLS is startGateway with TLS. A charge station may connect without a
// client certificate; one that it sends must be signed by one of clientCAs.
func startGatewayTLS(t *testing.T, clientCAs *x509.CertPool, opts ...server.WebsocketOpt) *httptest.Server {
	t.Helper()
	return serveGateway(t, &tls.Config{ClientCAs: clientCAs, ClientAuth: tls.VerifyClientCertIfGiven}, opts...)
}

func serveGateway(t *testing.T, tlsConfig *tls.Config, opts ...server.WebsocketOpt) *httptest.Server {
	t.Helper()

	handler := server.NewWebsocketHandler(opts...)
	var handling atomic.Int64
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handling.Add(1)
		defer handling.Add(-1)
		handler.ServeHTTP(w, r)
	}))
	if tlsConfig != nil {
		srv.TLS = tlsConfig
		srv.StartTLS()
	} else {
		srv.Start()
	}
	t.Cleanup(func() {
		srv.Close()
		assert.Eventually(t, func() bool { return handling.Load() == 0 }, teardownTimeout, time.Millisecond,
			"the gateway is still handling a connection at the end of the test: is a websocket left open?")
	})
	return srv
}

// tenantStation returns a charge station of the tenant with the org key that
// authenticates with basic auth and the password "password".
func tenantStation(orgKey, clientId string) *registry.ChargeStation {
	return &registry.ChargeStation{
		ClientId:             clientId,
		StationKey:           orgKey + "." + clientId,
		OrgKey:               orgKey,
		SecurityProfile:      registry.UnsecuredTransportWithBasicAuth,
		Base64SHA256Password: passwordHash,
	}
}

// dialStation connects a charge station to the server, with host as the Host header of
// its request (as if the charge station had resolved host to the address of the server).
func dialStation(ctx context.Context, t *testing.T, srv *httptest.Server, host, clientId, username, password string, header http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, srv.Listener.Addr().String())
		},
	}
	t.Cleanup(transport.CloseIdleConnections)

	if header == nil {
		header = http.Header{}
	}
	header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))

	return websocket.Dial(ctx, fmt.Sprintf("ws://%s/ws/%s", host, clientId), &websocket.DialOptions{
		HTTPClient:   &http.Client{Transport: transport},
		Subprotocols: []string{"ocpp2.0.1"},
		HTTPHeader:   header,
	})
}

// stationRequest returns the request of a charge station that asks for a websocket,
// with the credentials of tenantStation.
func stationRequest(target string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Sec-WebSocket-Protocol", "ocpp2.0.1")
	req.SetBasicAuth("CP001", "password")
	return req
}

func TestUnknownHostIsRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	reg := newRecordingRegistry()
	reg.Add("a.example", tenantStation("aaaa", "CP001"))

	_, addr := startBroker(t)
	srv := startGateway(t,
		server.WithMqttBrokerUrl(addr),
		server.WithDeviceRegistry(reg))

	// no tenant has the host, and a forwarding header does not change which host it is
	forwarded := http.Header{
		"X-Forwarded-Host": []string{"a.example"},
		"Forwarded":        []string{"host=a.example"},
	}
	conn, resp, err := dialStation(ctx, t, srv, "c.example", "CP001", "CP001", "password", forwarded)
	if err == nil {
		_ = conn.Close(websocket.StatusGoingAway, "Shutdown")
		t.Fatalf("expected error dialing CSMS")
	}
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("Upgrade"))

	// the same charge station is accepted on the host of its tenant, however that is spelled
	conn, resp, err = dialStation(ctx, t, srv, "A.Example:8080", "CP001", "CP001", "password", nil)
	require.NoError(t, err)
	defer func() {
		_ = conn.Close(websocket.StatusGoingAway, "Shutdown")
	}()
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	assert.Equal(t, []string{"c.example/CP001", "a.example/CP001"}, reg.recorded())
}

func TestRequestWithoutHostIsRejected(t *testing.T) {
	reg := newRecordingRegistry()
	reg.Add("a.example", tenantStation("aaaa", "CP001"))
	reg.Add("", tenantStation("aaaa", "CP001"))

	handler := server.NewWebsocketHandler(server.WithDeviceRegistry(reg))

	for name, host := range map[string]string{"no host": "", "only a port": ":8080", "only a dot": "."} {
		t.Run(name, func(t *testing.T) {
			req := stationRequest("http://a.example/ws/CP001")
			req.Host = host
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusBadRequest, rr.Code)
		})
	}
	assert.Empty(t, reg.recorded())
}

func TestInvalidStationIdIsRejected(t *testing.T) {
	reg := newRecordingRegistry()

	handler := server.NewWebsocketHandler(server.WithDeviceRegistry(reg))

	paths := map[string]string{
		"space":            "/ws/CP%20001",
		"plus":             "/ws/CP+001",
		"hash":             "/ws/CP%23001",
		"asterisk":         "/ws/CP*001",
		"greater than":     "/ws/CP%3E001",
		"leading dot":      "/ws/.CP001",
		"trailing dot":     "/ws/CP001.",
		"control char":     "/ws/CP%07001",
		"zero width space": "/ws/CP%E2%80%8B001",
		"invalid UTF-8":    "/ws/CP%FF001",
		"49 characters":    "/ws/" + strings.Repeat("a", 49),
	}
	for name, path := range paths {
		t.Run(name, func(t *testing.T) {
			// the charge station is registered with exactly the id of the request
			req := stationRequest("http://a.example" + path)
			id := strings.TrimPrefix(req.URL.Path, "/ws/")
			reg.Add("a.example", &registry.ChargeStation{
				ClientId:             id,
				SecurityProfile:      registry.UnsecuredTransportWithBasicAuth,
				Base64SHA256Password: passwordHash,
			})
			req.SetBasicAuth(id, "password")

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusBadRequest, rr.Code)
		})
	}
	assert.Empty(t, reg.recorded())
}

func TestFailedLookupIsAnInternalServerError(t *testing.T) {
	handler := server.NewWebsocketHandler(server.WithDeviceRegistry(errorRegistry{}))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, stationRequest("http://a.example/ws/CP001"))

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestChargeStationWithoutStationKeyIsRejected(t *testing.T) {
	// a registry that does not say which key the charge station has: the plain station
	// id must not be used in its place
	reg := registry.NewMockRegistry()
	cs := tenantStation("aaaa", "CP001")
	cs.StationKey = ""
	reg.ChargeStations["a.example/CP001"] = cs

	handler := server.NewWebsocketHandler(server.WithDeviceRegistry(reg))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, stationRequest("http://a.example/ws/CP001"))

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestBasicAuthUsernameIsThePlainStationId(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	reg := registry.NewMockRegistry()
	reg.Add("a.example", tenantStation("aaaa", "CP001"))

	_, addr := startBroker(t)
	srv := startGateway(t,
		server.WithMqttBrokerUrl(addr),
		server.WithDeviceRegistry(reg))

	// the station key is not a username
	conn, resp, err := dialStation(ctx, t, srv, "a.example", "CP001", "aaaa.CP001", "password", nil)
	if err == nil {
		_ = conn.Close(websocket.StatusGoingAway, "Shutdown")
		t.Fatalf("expected error dialing CSMS")
	}
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	conn, resp, err = dialStation(ctx, t, srv, "a.example", "CP001", "CP001", "password", nil)
	require.NoError(t, err)
	defer func() {
		_ = conn.Close(websocket.StatusGoingAway, "Shutdown")
	}()
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
}

func TestCredentialsOfAnotherTenantAreNotAccepted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	stationA := tenantStation("aaaa", "CP001")
	stationB := tenantStation("bbbb", "CP001")
	stationB.Base64SHA256Password = "bPYV1byqx3g1Ko8fM2DSPwLzTsGC4lmJf9bOSF14cNQ=" // password2

	reg := registry.NewMockRegistry()
	reg.Add("a.example", stationA)
	reg.Add("b.example", stationB)

	_, addr := startBroker(t)
	srv := startGateway(t,
		server.WithMqttBrokerUrl(addr),
		server.WithDeviceRegistry(reg))

	accepted := map[string]string{"a.example": "password", "b.example": "password2"}
	for host, ownPassword := range accepted {
		for _, password := range accepted {
			conn, resp, err := dialStation(ctx, t, srv, host, "CP001", "CP001", password, nil)
			if conn != nil {
				_ = conn.Close(websocket.StatusGoingAway, "Shutdown")
			}
			require.NotNil(t, resp)
			if password == ownPassword {
				require.NoError(t, err)
				assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode, "host %s", host)
			} else {
				require.Error(t, err)
				assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "host %s", host)
			}
		}
	}
}

// brokerEvent is a subscription or a publication of a client of the test broker.
type brokerEvent struct {
	clientId string
	topic    string
}

// brokerHook reports what the clients of the test broker subscribe to and, when it has
// a channel for that, what they publish.
type brokerHook struct {
	mqtt.HookBase
	subscribed chan brokerEvent
	published  chan brokerEvent
}

func (h *brokerHook) ID() string {
	return "broker-events"
}

func (h *brokerHook) Provides(b byte) bool {
	return b == mqtt.OnSubscribed || b == mqtt.OnPublished
}

func (h *brokerHook) OnSubscribed(cl *mqtt.Client, pk packets.Packet, _ []byte) {
	for _, filter := range pk.Filters {
		select {
		case h.subscribed <- brokerEvent{clientId: cl.ID, topic: filter.Filter}:
		default:
		}
	}
}

func (h *brokerHook) OnPublished(cl *mqtt.Client, pk packets.Packet) {
	select {
	case h.published <- brokerEvent{clientId: cl.ID, topic: pk.TopicName}:
	default:
	}
}

// publishedMessage is a message that the gateway published for the manager.
type publishedMessage struct {
	topic         string
	responseTopic string
	message       pipe.GatewayMessage
}

func TestTwoTenantsSameStationIdUseSeparateTopics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	subscribed := make(chan brokerEvent, 16)
	broker, addr := startBroker(t, &brokerHook{subscribed: subscribed})

	awaitSubscription := func() brokerEvent {
		t.Helper()
		select {
		case subscription := <-subscribed:
			return subscription
		case <-ctx.Done():
			t.Fatal("timeout waiting for a subscription")
			return brokerEvent{}
		}
	}

	// simulate manager connection: collects what the gateway publishes
	published := make(chan publishedMessage, 16)
	manager, err := autopaho.NewConnection(ctx, autopaho.ClientConfig{
		BrokerUrls:        []*url.URL{addr},
		KeepAlive:         10,
		ConnectRetryDelay: 2 * time.Second,
		OnConnectionUp: func(cm *autopaho.ConnectionManager, connack *paho.Connack) {
			_, err := cm.Subscribe(ctx, &paho.Subscribe{
				Subscriptions: map[string]paho.SubscribeOptions{
					"cs/in/ocpp2.0.1/+": {},
				},
			})
			if err != nil {
				t.Errorf("subscribing as manager: %v", err)
			}
		},
		ClientConfig: paho.ClientConfig{
			ClientID: "manager",
			Router: paho.NewSingleHandlerRouter(func(publish *paho.Publish) {
				msg := publishedMessage{topic: publish.Topic}
				if publish.Properties != nil {
					msg.responseTopic = publish.Properties.ResponseTopic
				}
				if err := json.Unmarshal(publish.Payload, &msg.message); err != nil {
					t.Errorf("unmarshaling published message: %v", err)
				}
				published <- msg
			}),
		},
	})
	require.NoError(t, err)
	defer func() {
		err := manager.Disconnect(ctx)
		if err != nil {
			t.Logf("WARN: mqtt client disconnect: %v", err)
		}
	}()
	require.Equal(t, brokerEvent{clientId: "manager", topic: "cs/in/ocpp2.0.1/+"}, awaitSubscription())

	awaitPublished := func() publishedMessage {
		t.Helper()
		select {
		case msg := <-published:
			return msg
		case <-ctx.Done():
			t.Fatal("timeout waiting for a message of the gateway")
			return publishedMessage{}
		}
	}

	// the same station id on the hosts of two tenants
	mockRegistry := registry.NewMockRegistry()
	mockRegistry.Add("a.example", tenantStation("aaaa", "CP001"))
	mockRegistry.Add("b.example", tenantStation("bbbb", "CP001"))

	srv := startGateway(t,
		server.WithMqttBrokerUrl(addr),
		server.WithMqttTopicPrefix("cs"),
		server.WithDeviceRegistry(mockRegistry))

	connect := func(host string) *websocket.Conn {
		t.Helper()
		conn, _, err := dialStation(ctx, t, srv, host, "CP001", "CP001", "password", nil)
		require.NoError(t, err)
		return conn
	}
	connA := connect("a.example")
	defer func() {
		_ = connA.Close(websocket.StatusNormalClosure, "OK")
	}()
	connB := connect("b.example")
	defer func() {
		_ = connB.Close(websocket.StatusNormalClosure, "OK")
	}()

	// each connection has its own client id on the broker and its own topic
	require.ElementsMatch(t,
		[]brokerEvent{
			{clientId: "aaaa.CP001", topic: "cs/out/ocpp2.0.1/aaaa.CP001"},
			{clientId: "bbbb.CP001", topic: "cs/out/ocpp2.0.1/bbbb.CP001"},
		},
		[]brokerEvent{awaitSubscription(), awaitSubscription()})

	// both charge stations send a call with the same message id
	call := func(conn *websocket.Conn, messageId string) {
		t.Helper()
		data := fmt.Sprintf(`[2,%q,"Heartbeat",{}]`, messageId)
		require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte(data)))
	}

	call(connA, "1")
	fromA := awaitPublished()
	assert.Equal(t, "cs/in/ocpp2.0.1/aaaa.CP001", fromA.topic)
	assert.Equal(t, "cs/out/ocpp2.0.1/aaaa.CP001", fromA.responseTopic)
	assert.Equal(t, "1", fromA.message.MessageId)

	call(connB, "1")
	fromB := awaitPublished()
	assert.Equal(t, "cs/in/ocpp2.0.1/bbbb.CP001", fromB.topic)
	assert.Equal(t, "cs/out/ocpp2.0.1/bbbb.CP001", fromB.responseTopic)
	assert.Equal(t, "1", fromB.message.MessageId)

	// a reply on the topic of a charge station reaches only that charge station: the
	// first thing that the other one reads is its own reply
	reply := func(topic, payload string) {
		t.Helper()
		data, err := json.Marshal(pipe.GatewayMessage{
			MessageType:     ocpp.MessageTypeCallResult,
			MessageId:       "1",
			ResponsePayload: json.RawMessage(payload),
		})
		require.NoError(t, err)
		require.NoError(t, broker.Publish(topic, data, false, 0))
	}
	read := func(conn *websocket.Conn) string {
		t.Helper()
		_, data, err := conn.Read(ctx)
		require.NoError(t, err)
		return string(data)
	}

	reply("cs/out/ocpp2.0.1/aaaa.CP001", `{"for":"a"}`)
	assert.JSONEq(t, `[3,"1",{"for":"a"}]`, read(connA))

	reply("cs/out/ocpp2.0.1/bbbb.CP001", `{"for":"b"}`)
	assert.JSONEq(t, `[3,"1",{"for":"b"}]`, read(connB))

	// both connections are still up: neither took over the client id of the other
	for _, clientId := range []string{"aaaa.CP001", "bbbb.CP001"} {
		client, ok := broker.Clients.Get(clientId)
		require.True(t, ok, "no client %s on the broker", clientId)
		assert.False(t, client.Closed(), "client %s is closed", clientId)
	}
	_, ok := broker.Clients.Get("CP001")
	assert.False(t, ok, "a client with the plain station id is on the broker")

	call(connA, "2")
	assert.Equal(t, "cs/in/ocpp2.0.1/aaaa.CP001", awaitPublished().topic)
	call(connB, "2")
	assert.Equal(t, "cs/in/ocpp2.0.1/bbbb.CP001", awaitPublished().topic)
}

func TestSingleTenantStationKeepsItsTopics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	hook := &brokerHook{subscribed: make(chan brokerEvent, 16), published: make(chan brokerEvent, 16)}
	_, addr := startBroker(t, hook)

	await := func(events chan brokerEvent) brokerEvent {
		t.Helper()
		select {
		case event := <-events:
			return event
		case <-ctx.Done():
			t.Fatal("timeout waiting for the gateway")
			return brokerEvent{}
		}
	}

	// the answer of a manager in single-tenant mode: the station key is the station id
	mockRegistry := registry.NewMockRegistry()
	mockRegistry.Add(testHost, &registry.ChargeStation{
		ClientId:             "cs.1",
		StationKey:           "cs.1",
		SecurityProfile:      registry.UnsecuredTransportWithBasicAuth,
		Base64SHA256Password: passwordHash,
	})

	srv := startGateway(t,
		server.WithMqttBrokerUrl(addr),
		server.WithMqttTopicPrefix("cs"),
		server.WithDeviceRegistry(mockRegistry))

	conn, _, err := dialStation(ctx, t, srv, srv.Listener.Addr().String(), "cs.1", "cs.1", "password", nil)
	require.NoError(t, err)
	defer func() {
		_ = conn.Close(websocket.StatusNormalClosure, "OK")
	}()

	// the client id and the topics are the ones of a gateway without tenants
	assert.Equal(t, brokerEvent{clientId: "cs.1", topic: "cs/out/ocpp2.0.1/cs.1"}, await(hook.subscribed))

	require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte(`[2,"1","Heartbeat",{}]`)))
	assert.Equal(t, brokerEvent{clientId: "cs.1", topic: "cs/in/ocpp2.0.1/cs.1"}, await(hook.published))
}

// The organisation names that the tenant of a charge station accepts in a client
// certificate take the place of the list of the gateway (--org-name).
func TestCertificateOrganizationOfTheTenant(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const clientId = "CP001"
	caCert, _, clientCert, clientKey := createTestKeyPairsForOrg(t, clientId, "Charge Co")
	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(caCert)
	certificate := &tls.Certificate{Certificate: [][]byte{clientCert.Raw, caCert.Raw}, PrivateKey: clientKey}

	// the charge station as a manager in multi-tenant mode answers it: with the list of
	// its tenant, which may be empty
	ofTenant := func(names ...string) *registry.ChargeStation {
		return &registry.ChargeStation{
			ClientId:             clientId,
			StationKey:           "aaaa." + clientId,
			OrgKey:               "aaaa",
			SecurityProfile:      registry.TLSWithClientSideCertificates,
			HasTenantOrgNames:    true,
			AcceptedCertOrgNames: names,
		}
	}
	// the charge station as a manager in single-tenant mode answers it: without a list
	withoutTenant := &registry.ChargeStation{
		ClientId:        clientId,
		StationKey:      clientId,
		SecurityProfile: registry.TLSWithClientSideCertificates,
	}

	cases := []struct {
		name            string
		station         *registry.ChargeStation
		gatewayOrgNames []string
		certificate     *tls.Certificate
		want            int
	}{
		{
			name:            "the tenant accepts the organisation",
			station:         ofTenant("Charge Co"),
			gatewayOrgNames: []string{"Thoughtworks"},
			certificate:     certificate,
			want:            http.StatusSwitchingProtocols,
		},
		{
			name:            "the tenant accepts it among others",
			station:         ofTenant("Other", "Charge Co"),
			gatewayOrgNames: []string{"Thoughtworks"},
			certificate:     certificate,
			want:            http.StatusSwitchingProtocols,
		},
		{
			name:            "the tenant accepts another organisation",
			station:         ofTenant("Other"),
			gatewayOrgNames: []string{"Thoughtworks"},
			certificate:     certificate,
			want:            http.StatusUnauthorized,
		},
		{
			name:            "the list of the gateway adds nothing to the list of the tenant",
			station:         ofTenant("Other"),
			gatewayOrgNames: []string{"Charge Co"},
			certificate:     certificate,
			want:            http.StatusUnauthorized,
		},
		{
			name:            "a tenant without names has no check of the organisation",
			station:         ofTenant(),
			gatewayOrgNames: []string{"Thoughtworks"},
			certificate:     certificate,
			want:            http.StatusSwitchingProtocols,
		},
		{
			name:            "a tenant without names still needs a client certificate",
			station:         ofTenant(),
			gatewayOrgNames: []string{"Thoughtworks"},
			certificate:     nil,
			want:            http.StatusUnauthorized,
		},
		{
			name:            "without a list of a tenant the list of the gateway accepts the organisation",
			station:         withoutTenant,
			gatewayOrgNames: []string{"Thoughtworks", "Charge Co"},
			certificate:     certificate,
			want:            http.StatusSwitchingProtocols,
		},
		{
			name:            "without a list of a tenant the list of the gateway refuses the organisation",
			station:         withoutTenant,
			gatewayOrgNames: []string{"Thoughtworks"},
			certificate:     certificate,
			want:            http.StatusUnauthorized,
		},
		{
			name:            "without a list of a tenant an empty list of the gateway accepts nothing",
			station:         withoutTenant,
			gatewayOrgNames: nil,
			certificate:     certificate,
			want:            http.StatusUnauthorized,
		},
	}

	_, addr := startBroker(t)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := registry.NewMockRegistry()
			reg.Add(testHost, tc.station)
			srv := startGatewayTLS(t, clientCAs,
				server.WithMqttBrokerUrl(addr),
				server.WithMqttTopicPrefix("cs"),
				server.WithDeviceRegistry(reg),
				server.WithOrgNames(tc.gatewayOrgNames))

			rootCAs := x509.NewCertPool()
			rootCAs.AddCert(srv.Certificate())
			tlsConfig := &tls.Config{RootCAs: rootCAs}
			if tc.certificate != nil {
				tlsConfig.Certificates = []tls.Certificate{*tc.certificate}
			}
			transport := &http.Transport{TLSClientConfig: tlsConfig}
			t.Cleanup(transport.CloseIdleConnections)

			conn, resp, err := websocket.Dial(ctx, fmt.Sprintf("%s/ws/%s", srv.URL, clientId), &websocket.DialOptions{
				HTTPClient:   &http.Client{Transport: transport},
				Subprotocols: []string{"ocpp2.0.1"},
			})
			if err == nil {
				defer func() {
					_ = conn.Close(websocket.StatusNormalClosure, "OK")
				}()
			}
			require.NotNil(t, resp, "no answer from the gateway: %v", err)
			assert.Equal(t, tc.want, resp.StatusCode)
		})
	}
}

//func TestTlsConnectionWithCertificateAuthMismatchedName(t *testing.T) {
//	//defer goleak.VerifyNone(t)
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	cs := &registry.ChargeStation{
//		ClientId:        "tlsWithCertificateAuthMismatchedName",
//		SecurityProfile: registry.TLSWithClientSideCertificates,
//	}
//
//	caCert, _, clientCert, clientKeyPair := createTestKeyPairs(t, cs.ClientId+"Wrong")
//
//	mockRegistry := registry.NewMockRegistry()
//	mockRegistry.Add(testHost, cs)
//
//	srv := httptest.NewUnstartedServer(server.NewWebsocketHandler(
//		server.WithDeviceRegistry(mockRegistry),
//		server.WithOrgName("Thoughtworks")))
//	clientCAPool := x509.NewCertPool()
//	clientCAPool.AddCert(caCert)
//	srv.TLS = &tls.Config{
//		ClientCAs:  clientCAPool,
//		ClientAuth: tls.VerifyClientCertIfGiven,
//	}
//	srv.StartTLS()
//	defer srv.Close()
//
//	rootCAPool := x509.NewCertPool()
//	rootCAPool.AddCert(srv.Certificate())
//
//	clientTlsCert := tls.Certificate{
//		Certificate: [][]byte{clientCert.Raw, caCert.Raw},
//		PrivateKey:  clientKeyPair,
//	}
//
//	httpClient := &http.Client{
//		Transport: &http.Transport{
//			TLSClientConfig: &tls.Config{
//				RootCAs:      rootCAPool,
//				Certificates: []tls.Certificate{clientTlsCert},
//			},
//		},
//	}
//	dialOptions := &websocket.DialOptions{
//		HTTPClient:   httpClient,
//		Subprotocols: []string{"ocpp1.6", "ocpp2.0.1"},
//	}
//
//	_, resp, err := websocket.Dial(ctx, fmt.Sprintf("%s/ws/%s", srv.URL, cs.ClientId), dialOptions)
//	if err == nil {
//		t.Fatalf("expected error dialing CSMS")
//	}
//	if resp.StatusCode != http.StatusUnauthorized {
//		t.Fatalf("status code: want %d, got %d", http.StatusUnauthorized, resp.StatusCode)
//	}
//}

//func TestConnectionWhenUnableToConnectToMqtt(t *testing.T) {
//	//defer goleak.VerifyNone(t)
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	cs := &registry.ChargeStation{
//		ClientId:             "basicAuthCS1",
//		SecurityProfile:      registry.UnsecuredTransportWithBasicAuth,
//		Base64SHA256Password: "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg=", // password,
//	}
//
//	mockRegistry := registry.NewMockRegistry()
//	mockRegistry.Add(testHost, cs)
//
//	srv := httptest.NewServer(server.NewWebsocketHandler(
//		server.WithDeviceRegistry(mockRegistry),
//		server.WithMqttBrokerUrlString("mqtt://fizzbuz.example.com:1833/"),
//		server.WithMqttConnectSettings(200*time.Millisecond, 100*time.Millisecond, 0)))
//	defer srv.Close()
//
//	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", cs.ClientId, "password")))
//	dialOptions := &websocket.DialOptions{
//		Subprotocols: []string{"ocpp1.6", "ocpp2.0.1"},
//		HTTPHeader: http.Header{
//			"authorization": []string{authHeader},
//		},
//	}
//
//	conn, resp, err := websocket.Dial(ctx, fmt.Sprintf("%s/ws/%s", srv.URL, cs.ClientId), dialOptions)
//	if err != nil {
//		t.Fatalf("dialing CSMS: %v", err)
//	}
//	if resp.StatusCode != http.StatusSwitchingProtocols {
//		t.Fatalf("status code: want %d, got %d", http.StatusSwitchingProtocols, resp.StatusCode)
//	}
//
//	call := ocpp.Call{
//		MessageId: "1",
//		Action:    "EchoRequest",
//		Payload:   json.RawMessage("\"Payload\""),
//	}
//	data, err := json.Marshal(call)
//	if err != nil {
//		t.Fatalf("encoding call: %v", err)
//	}
//
//	err = conn.Write(ctx, websocket.MessageText, data)
//	if err != nil {
//		t.Fatalf("writing to websocket: %v", err)
//	}
//
//	_, _, err = conn.Read(ctx)
//	if status := websocket.CloseStatus(err); status != websocket.StatusProtocolError {
//		if status == -1 {
//			_ = conn.Close(websocket.StatusGoingAway, "Shutdown")
//		}
//		t.Errorf("reading from websocket: want status %d got status %d", websocket.StatusProtocolError, status)
//	}
//}

//	func TestConnectionWhenChargeStationCallReceivedAndNoCSMSResponse(t *testing.T) {
//		defer goleak.VerifyNone(t)
//
//		ctx, cancel := context.WithCancel(context.Background())
//		defer cancel()
//
//		broker, addr := server.NewBroker(t)
//		err := broker.Serve()
//		if err != nil {
//			t.Fatalf("starting broker: %v", err)
//		}
//		defer func() {
//			err := broker.Close()
//			if err != nil {
//				t.Logf("WARN: broker close: %v", err)
//			}
//		}()
//
//		cs := &registry.ChargeStation{
//			ClientId:             "basicAuthCS1",
//			SecurityProfile:      registry.UnsecuredTransportWithBasicAuth,
//			Base64SHA256Password: "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg=", // password,
//		}
//
//		mockRegistry := registry.NewMockRegistry()
//		mockRegistry.Add(testHost, cs)
//
//		srv := httptest.NewServer(server.NewWebsocketHandler(
//			server.WithDeviceRegistry(mockRegistry),
//			server.WithMqttBrokerUrl(addr),
//			server.WithPipeOption(pipe.WithResponseTimeout(200*time.Millisecond))))
//		defer srv.Close()
//
//		authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", cs.ClientId, "password")))
//		dialOptions := &websocket.DialOptions{
//			Subprotocols: []string{"ocpp1.6", "ocpp2.0.1"},
//			HTTPHeader: http.Header{
//				"authorization": []string{authHeader},
//			},
//		}
//
//		conn, resp, err := websocket.Dial(ctx, fmt.Sprintf("%s/ws/%s", srv.URL, cs.ClientId), dialOptions)
//		if err != nil {
//			t.Fatalf("dialing CSMS: %v", err)
//		}
//		defer func() {
//			err := conn.Close(websocket.StatusGoingAway, "Shutdown")
//			if err != nil {
//				t.Logf("websocket close: %v", err)
//			}
//		}()
//		if resp.StatusCode != http.StatusSwitchingProtocols {
//			t.Fatalf("status code: want %d, got %d", http.StatusSwitchingProtocols, resp.StatusCode)
//		}
//
//		call := ocpp.Call{
//			MessageId: "1",
//			Action:    "EchoRequest",
//			Payload:   json.RawMessage("\"Payload\""),
//		}
//		data, err := json.Marshal(call)
//		if err != nil {
//			t.Fatalf("encoding call: %v", err)
//		}
//
//		err = conn.Write(ctx, websocket.MessageText, data)
//		if err != nil {
//			t.Fatalf("writing to websocket: %v", err)
//		}
//
//		typ, b, err := conn.Read(ctx)
//		if status := websocket.CloseStatus(err); status != -1 {
//			t.Errorf("reading from websocket: want status %d got status %d", -1, status)
//		}
//
//		if typ != websocket.MessageText {
//			t.Errorf("message type: want %d got %d", websocket.MessageText, typ)
//		}
//
//		var msg ocpp.Message
//		err = json.Unmarshal(b, &msg)
//		if err != nil {
//			t.Fatalf("unmarshalling response: %v", err)
//		}
//
//		if msg.MessageTypeId != ocpp.MessageTypeCallError {
//			t.Errorf("message type id: want %d got %d", ocpp.MessageTypeCallError, msg.MessageTypeId)
//		}
//	}
//
//	func TestConnectionShutsDownWhenChargeStationDisconnectsMidRequest(t *testing.T) {
//		defer goleak.VerifyNone(t)
//
//		ctx, cancel := context.WithCancel(context.Background())
//		defer cancel()
//
//		broker, addr := server.NewBroker(t)
//		err := broker.Serve()
//		if err != nil {
//			t.Fatalf("starting broker: %v", err)
//		}
//		defer func() {
//			err := broker.Close()
//			if err != nil {
//				t.Logf("WARN: broker close: %v", err)
//			}
//		}()
//
//		client := mqtttest.NewClient(t, ctx, addr, "test", func(mqttMsg *paho.Publish) {
//			var msg ocpp.Message
//			err := json.Unmarshal(mqttMsg.Payload, &msg)
//			if err != nil {
//				t.Logf("ERROR: %v", err)
//			}
//
//			time.Sleep(200 * time.Millisecond)
//
//			res := ocpp.CallResult{
//				MessageId: msg.MessageId,
//				Payload:   msg.Data[1],
//			}
//
//			data, err := json.Marshal(res)
//			if err != nil {
//				t.Logf("marshaling error: %v", err)
//			}
//
//			err = broker.Publish(mqttMsg.Properties.ResponseTopic, data, false, 0)
//			if err != nil {
//				t.Logf("sending response: %v", err)
//			}
//		})
//		defer func() {
//			err := client.Disconnect(ctx)
//			if err != nil {
//				t.Logf("WARN: mqtt client disconnect: %v", err)
//			}
//		}()
//
//		_, err = client.Subscribe(ctx, &paho.Subscribe{
//			Subscriptions: map[string]paho.SubscribeOptions{
//				"/in/ocpp2.0.1/cs1": {},
//			},
//		})
//		if err != nil {
//			t.Fatalf("subscribing to topic: %v", err)
//		}
//
//		cs := &registry.ChargeStation{
//			ClientId:             "cs1",
//			SecurityProfile:      registry.UnsecuredTransportWithBasicAuth,
//			Base64SHA256Password: "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg=", // password
//		}
//
//		mockRegistry := registry.NewMockRegistry()
//		mockRegistry.Add(testHost, cs)
//
//		srv := httptest.NewServer(server.NewWebsocketHandler(
//			server.WithMqttBrokerUrl(addr),
//			server.WithDeviceRegistry(mockRegistry)))
//		defer srv.Close()
//
//		authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", cs.ClientId, "password")))
//		dialOptions := &websocket.DialOptions{
//			Subprotocols: []string{"ocpp1.6", "ocpp2.0.1"},
//			HTTPHeader: http.Header{
//				"authorization": []string{authHeader},
//			},
//		}
//
//		conn, _, err := websocket.Dial(ctx, fmt.Sprintf("%s/ws/cs1", srv.URL), dialOptions)
//		if err != nil {
//			t.Fatalf("dialing server: %v", err)
//		}
//		defer func() {
//			err := conn.Close(websocket.StatusNormalClosure, "OK")
//			if err != nil {
//				t.Logf("WARN: websocket close: %v", err)
//			}
//		}()
//
//		if conn.Subprotocol() != "ocpp2.0.1" {
//			t.Errorf("subprotocol: want %s, got %s", "ocpp2.0.1", conn.Subprotocol())
//		}
//
//		call := ocpp.Call{
//			MessageId: "1",
//			Action:    "EchoRequest",
//			Payload:   json.RawMessage("\"Payload\""),
//		}
//		data, err := json.Marshal(call)
//		if err != nil {
//			t.Fatalf("encoding call: %v", err)
//		}
//
//		err = conn.Write(ctx, websocket.MessageText, data)
//		if err != nil {
//			t.Fatalf("sending message: %v", err)
//		}
//
//		_ = conn.Close(websocket.StatusNormalClosure, "Something went wrong")
//	}
func createTestKeyPairs(t *testing.T, clientId string) (*x509.Certificate, *ecdsa.PrivateKey, *x509.Certificate, *ecdsa.PrivateKey) {
	return createTestKeyPairsForOrg(t, clientId, "Thoughtworks")
}

// createTestKeyPairsForOrg returns a CA and a client certificate of the organisation
// for the charge station, with their keys.
func createTestKeyPairsForOrg(t *testing.T, clientId, organization string) (*x509.Certificate, *ecdsa.PrivateKey, *x509.Certificate, *ecdsa.PrivateKey) {
	caKeyPair, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating CA key pair: %v", err)
	}

	caSerialNumber, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		t.Fatalf("generating CA serial number: %v", err)
	}

	caCertTemplate := x509.Certificate{
		SerialNumber: caSerialNumber,
		Subject: pkix.Name{
			CommonName:   "Test CA",
			Organization: []string{"Thoughtworks"},
		},
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Minute),
	}

	caCertBytes, err :=
		x509.CreateCertificate(rand.Reader, &caCertTemplate, &caCertTemplate, &caKeyPair.PublicKey, caKeyPair)
	if err != nil {
		t.Fatalf("creating CA certificate: %v", err)
	}
	caCert, err := x509.ParseCertificate(caCertBytes)
	if err != nil {
		t.Fatalf("parsing CA certificate: %v", err)
	}

	clientKeyPair, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating client key pair: %v", err)
	}

	clientSerialNumber, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		t.Fatalf("generating client serial number: %v", err)
	}

	clientCertTemplate := x509.Certificate{
		SerialNumber: clientSerialNumber,
		Subject: pkix.Name{
			CommonName:   clientId,
			Organization: []string{organization},
		},
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Minute),
	}

	clientCertBytes, err := x509.CreateCertificate(rand.Reader, &clientCertTemplate, caCert, &clientKeyPair.PublicKey, caKeyPair)
	if err != nil {
		t.Fatalf("creating client certificate: %v", err)
	}
	clientCert, err := x509.ParseCertificate(clientCertBytes)
	if err != nil {
		t.Fatalf("parsing client certificate: %v", err)
	}

	return caCert, caKeyPair, clientCert, clientKeyPair
}
