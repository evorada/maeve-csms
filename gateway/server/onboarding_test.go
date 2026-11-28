// SPDX-License-Identifier: Apache-2.0

package server_test

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/gateway/registry"
	"github.com/thoughtworks/maeve-csms/gateway/server"
	"nhooyr.io/websocket"
)

// onboardingPasswordHash is the hash of the onboarding password "onboarding".
var onboardingPasswordHash = hashOf("onboarding")

// stationPasswordHash is the hash of the new password "new-password" of a station.
var stationPasswordHash = hashOf("new-password")

// unregisteredStation is a charge station that the tenant with org key aaaa does not
// know, answered as a manager answers it: with the onboarding rules and no security
// profile.
func unregisteredStation(clientId string, openSP1 bool, onboardingPassword string) *registry.ChargeStation {
	return &registry.ChargeStation{
		ClientId:   clientId,
		StationKey: "aaaa." + clientId,
		OrgKey:     "aaaa",
		Onboarding: &registry.Onboarding{OpenSP1: openSP1, Base64SHA256Password: onboardingPassword},
	}
}

// onboardingGateway serves the gateway with a transport that hands the sessions to the
// test, plain (ws) and with TLS (wss), for the registry.
type onboardingGateway struct {
	reg       *registry.MockRegistry
	transport *fakeTransport
	plain     *httptest.Server
	secure    *httptest.Server
	clientCA  *x509.Certificate
	client    tls.Certificate
}

func newOnboardingGateway(t *testing.T) *onboardingGateway {
	t.Helper()
	caCert, _, clientCert, clientKey := createTestKeyPairsForOrg(t, "CP001", "Charge Co")
	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(caCert)

	g := &onboardingGateway{
		reg:       registry.NewMockRegistry(),
		transport: &fakeTransport{opened: make(chan *fakeSession, 16)},
		clientCA:  caCert,
		client:    tls.Certificate{Certificate: [][]byte{clientCert.Raw, caCert.Raw}, PrivateKey: clientKey},
	}
	opts := []server.WebsocketOpt{server.WithTransport(g.transport), server.WithDeviceRegistry(g.reg)}
	g.plain = startGateway(t, opts...)
	g.secure = startGatewayTLS(t, clientCAs, opts...)
	return g
}

// connection is how a charge station connects in a case.
type connection struct {
	tls         bool
	basicAuth   bool
	username    string
	password    string
	certificate bool
}

// dial connects a charge station to host with the id clientId and returns the status
// of the answer of the gateway. A websocket that opens is closed again.
func (g *onboardingGateway) dial(t *testing.T, host, clientId string, c connection) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	srv, scheme := g.plain, "ws"
	transport := &http.Transport{}
	if c.tls {
		srv, scheme = g.secure, "wss"
		rootCAs := x509.NewCertPool()
		rootCAs.AddCert(srv.Certificate())
		transport.TLSClientConfig = &tls.Config{RootCAs: rootCAs, ServerName: "example.com"}
		if c.certificate {
			transport.TLSClientConfig.Certificates = []tls.Certificate{g.client}
		}
	}
	addr := srv.Listener.Addr().String()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, addr)
	}
	t.Cleanup(transport.CloseIdleConnections)

	header := http.Header{}
	if c.basicAuth {
		header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.username+":"+c.password)))
	}
	conn, resp, err := websocket.Dial(ctx, fmt.Sprintf("%s://%s/ws/%s", scheme, host, clientId), &websocket.DialOptions{
		HTTPClient:   &http.Client{Transport: transport},
		Subprotocols: []string{"ocpp1.6"},
		HTTPHeader:   header,
	})
	if err == nil {
		_ = conn.Close(websocket.StatusNormalClosure, "OK")
	}
	require.NotNil(t, resp, "no answer from the gateway: %v", err)
	return resp.StatusCode
}

// session returns the station key of the session that the gateway opened for an admitted
// station.
func (g *onboardingGateway) session(t *testing.T) string {
	t.Helper()
	select {
	case s := <-g.transport.opened:
		return s.stationKey
	case <-time.After(10 * time.Second):
		t.Fatal("no session was opened")
		return ""
	}
}

func (g *onboardingGateway) noSession(t *testing.T) {
	t.Helper()
	select {
	case s := <-g.transport.opened:
		t.Fatalf("a session was opened for %s", s.stationKey)
	default:
	}
}

func TestUnregisteredStationIsAdmittedForItsOnboarding(t *testing.T) {
	cases := []struct {
		name       string
		station    *registry.ChargeStation
		connection connection
		want       int
		// the credential that the connection is recorded with; "" when it is not recorded
		credential string
	}{
		{
			name:       "open SP1: without TLS and without credentials",
			station:    unregisteredStation("CP100", true, ""),
			connection: connection{},
			want:       http.StatusSwitchingProtocols, credential: registry.OnboardingCredentialOpen,
		},
		{
			name:       "open SP1: a password that is not known does not matter",
			station:    unregisteredStation("CP100", true, onboardingPasswordHash),
			connection: connection{basicAuth: true, username: "anyone", password: "wrong"},
			want:       http.StatusSwitchingProtocols, credential: registry.OnboardingCredentialOpen,
		},
		{
			name:       "open SP1: the onboarding password is what the station gave",
			station:    unregisteredStation("CP100", true, onboardingPasswordHash),
			connection: connection{basicAuth: true, username: "anyone", password: "onboarding"},
			want:       http.StatusSwitchingProtocols, credential: registry.OnboardingCredentialOnboardingPassword,
		},
		{
			name:       "SP2 with the onboarding password",
			station:    unregisteredStation("CP100", true, onboardingPasswordHash),
			connection: connection{tls: true, basicAuth: true, username: "anyone", password: "onboarding"},
			want:       http.StatusSwitchingProtocols, credential: registry.OnboardingCredentialOnboardingPassword,
		},
		{
			name:       "SP2 needs a password also when SP1 is open",
			station:    unregisteredStation("CP100", true, onboardingPasswordHash),
			connection: connection{tls: true},
			want:       http.StatusUnauthorized,
		},
		{
			name:       "SP2 with a wrong password",
			station:    unregisteredStation("CP100", true, onboardingPasswordHash),
			connection: connection{tls: true, basicAuth: true, username: "CP100", password: "wrong"},
			want:       http.StatusUnauthorized,
		},
		{
			name:       "a client certificate alone is not onboarded",
			station:    unregisteredStation("CP100", true, onboardingPasswordHash),
			connection: connection{tls: true, certificate: true},
			want:       http.StatusUnauthorized,
		},
		{
			name:       "SP1 closed: without credentials",
			station:    unregisteredStation("CP100", false, onboardingPasswordHash),
			connection: connection{},
			want:       http.StatusUnauthorized,
		},
		{
			name:       "SP1 closed: a wrong password",
			station:    unregisteredStation("CP100", false, onboardingPasswordHash),
			connection: connection{basicAuth: true, username: "CP100", password: "wrong"},
			want:       http.StatusUnauthorized,
		},
		{
			name:       "SP1 closed: the onboarding password without TLS",
			station:    unregisteredStation("CP100", false, onboardingPasswordHash),
			connection: connection{basicAuth: true, username: "CP100", password: "onboarding"},
			want:       http.StatusSwitchingProtocols, credential: registry.OnboardingCredentialOnboardingPassword,
		},
		{
			name:       "a tenant without an onboarding password and SP1 closed admits nothing",
			station:    unregisteredStation("CP100", false, ""),
			connection: connection{basicAuth: true, username: "CP100", password: ""},
			want:       http.StatusUnauthorized,
		},
		{
			name: "a pending station with its new password",
			station: func() *registry.ChargeStation {
				cs := unregisteredStation("CP100", false, onboardingPasswordHash)
				cs.PendingBase64SHA256Password = stationPasswordHash
				return cs
			}(),
			connection: connection{tls: true, basicAuth: true, username: "CP100", password: "new-password"},
			want:       http.StatusSwitchingProtocols, credential: registry.OnboardingCredentialPendingPassword,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newOnboardingGateway(t)
			g.reg.Add(testHost, tc.station)

			got := g.dial(t, testHost, "CP100", tc.connection)
			assert.Equal(t, tc.want, got)

			if tc.credential == "" {
				assert.Empty(t, g.reg.OnboardingConnections(), "a station that is not admitted is not recorded")
				g.noSession(t)
				return
			}
			transport := registry.OnboardingTransportWs
			if tc.connection.tls {
				transport = registry.OnboardingTransportWss
			}
			assert.Equal(t, []registry.OnboardingConnection{
				{Host: testHost, ClientId: "CP100", Transport: transport, Credential: tc.credential},
			}, g.reg.OnboardingConnections())
			assert.Equal(t, "aaaa.CP100", g.session(t))
		})
	}
}

func TestUnregisteredStationIsRefusedWhenItsConnectionCannotBeRecorded(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"the tenant has as many pending stations as it allows": {registry.ErrPendingLimitReached, http.StatusTooManyRequests},
		"the manager cannot record it":                         {errors.New("http status 500"), http.StatusServiceUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			g := newOnboardingGateway(t)
			g.reg.Add(testHost, unregisteredStation("CP100", true, ""))
			g.reg.SetOnboardingError(tc.err)

			assert.Equal(t, tc.want, g.dial(t, testHost, "CP100", connection{}))
			assert.Len(t, g.reg.OnboardingConnections(), 1)
			g.noSession(t)
		})
	}
}

func TestRegisteredStationsAreAdmittedAsBefore(t *testing.T) {
	g := newOnboardingGateway(t)
	g.reg.Add(testHost, tenantStation("aaaa", "CP001"))

	// a registered station is never admitted with the onboarding rules
	assert.Equal(t, http.StatusUnauthorized, g.dial(t, testHost, "CP001", connection{}))
	assert.Equal(t, http.StatusUnauthorized, g.dial(t, testHost, "CP001", connection{basicAuth: true, username: "CP001", password: "onboarding"}))
	assert.Equal(t, http.StatusSwitchingProtocols, g.dial(t, testHost, "CP001", connection{basicAuth: true, username: "CP001", password: "password"}))
	assert.Equal(t, "aaaa.CP001", g.session(t))
	assert.Empty(t, g.reg.OnboardingConnections())
}

func TestPendingPasswordIsAcceptedForARegisteredStation(t *testing.T) {
	g := newOnboardingGateway(t)
	cs := tenantStation("aaaa", "CP001")
	cs.PendingBase64SHA256Password = stationPasswordHash
	g.reg.Add(testHost, cs)

	for password, want := range map[string]int{
		"password":     http.StatusSwitchingProtocols, // the password it has
		"new-password": http.StatusSwitchingProtocols, // the one it is being told to use
		"wrong":        http.StatusUnauthorized,
	} {
		assert.Equal(t, want, g.dial(t, testHost, "CP001", connection{basicAuth: true, username: "CP001", password: password}), password)
		if want == http.StatusSwitchingProtocols {
			assert.Equal(t, "aaaa.CP001", g.session(t))
		}
	}
	// the username is still checked
	assert.Equal(t, http.StatusUnauthorized, g.dial(t, testHost, "CP001", connection{basicAuth: true, username: "other", password: "new-password"}))
	assert.Empty(t, g.reg.OnboardingConnections())
}

// A station of another tenant is looked up for the tenant of the host: the manager
// answers it as a station that this tenant does not know, so the credentials of the
// other tenant do not apply and its station key is never used.
func TestStationOfAnotherTenantIsOnboardedAsAStationOfTheHost(t *testing.T) {
	g := newOnboardingGateway(t)
	stationB := tenantStation("bbbb", "CP001")
	g.reg.Add("b.example", stationB)
	g.reg.Add(testHost, unregisteredStation("CP001", false, onboardingPasswordHash))

	// the password of the station of tenant B is not accepted on the host of A
	assert.Equal(t, http.StatusUnauthorized, g.dial(t, testHost, "CP001", connection{basicAuth: true, username: "CP001", password: "password"}))
	assert.Empty(t, g.reg.OnboardingConnections())

	assert.Equal(t, http.StatusSwitchingProtocols, g.dial(t, testHost, "CP001", connection{basicAuth: true, username: "CP001", password: "onboarding"}))
	assert.Equal(t, "aaaa.CP001", g.session(t))
}

// hashOf returns the hash of a password as the manager keeps it.
func hashOf(password string) string {
	sum := sha256.Sum256([]byte(password))
	return base64.StdEncoding.EncodeToString(sum[:])
}
