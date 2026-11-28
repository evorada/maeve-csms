// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"crypto/x509"
	"sync"
)

// MockRegistry is a registry for tests. ChargeStations is keyed by the normalized
// host + "/" + client id: use Add to fill it. Make it with NewMockRegistry.
type MockRegistry struct {
	ChargeStations map[string]*ChargeStation
	Certificates   map[string]*x509.Certificate

	onboarding *mockOnboarding
}

// OnboardingConnection is a call of RecordOnboardingConnection.
type OnboardingConnection struct {
	Host, ClientId, Transport, Credential string
}

type mockOnboarding struct {
	mu          sync.Mutex
	connections []OnboardingConnection
	err         error
}

func NewMockRegistry() *MockRegistry {
	return &MockRegistry{
		ChargeStations: make(map[string]*ChargeStation),
		Certificates:   make(map[string]*x509.Certificate),
		onboarding:     &mockOnboarding{},
	}
}

// Add registers the charge station for the host. A charge station without a station key
// gets "{OrgKey}.{ClientId}", or the ClientId when it has no org key.
func (m MockRegistry) Add(host string, cs *ChargeStation) {
	if cs.StationKey == "" {
		cs.StationKey = cs.ClientId
		if cs.OrgKey != "" {
			cs.StationKey = cs.OrgKey + "." + cs.ClientId
		}
	}
	m.ChargeStations[mockKey(host, cs.ClientId)] = cs
}

func (m MockRegistry) LookupChargeStation(_ context.Context, host, clientId string) (*ChargeStation, error) {
	return m.ChargeStations[mockKey(host, clientId)], nil
}

func (m MockRegistry) LookupCertificate(_ context.Context, certHash string) (*x509.Certificate, error) {
	return m.Certificates[certHash], nil
}

func mockKey(host, clientId string) string {
	return NormalizeHost(host) + "/" + clientId
}

// RecordOnboardingConnection records the call and returns the error of
// SetOnboardingError.
func (m MockRegistry) RecordOnboardingConnection(_ context.Context, host, clientId, transport, credential string) error {
	m.onboarding.mu.Lock()
	defer m.onboarding.mu.Unlock()
	m.onboarding.connections = append(m.onboarding.connections, OnboardingConnection{
		Host: NormalizeHost(host), ClientId: clientId, Transport: transport, Credential: credential,
	})
	return m.onboarding.err
}

// OnboardingConnections returns the calls of RecordOnboardingConnection, in order.
func (m MockRegistry) OnboardingConnections() []OnboardingConnection {
	m.onboarding.mu.Lock()
	defer m.onboarding.mu.Unlock()
	return append([]OnboardingConnection{}, m.onboarding.connections...)
}

// SetOnboardingError makes RecordOnboardingConnection fail with err (nil: succeed).
func (m MockRegistry) SetOnboardingError(err error) {
	m.onboarding.mu.Lock()
	defer m.onboarding.mu.Unlock()
	m.onboarding.err = err
}
