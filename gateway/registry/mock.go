// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"crypto/x509"
)

// MockRegistry is a registry for tests. ChargeStations is keyed by the normalized
// host + "/" + client id: use Add to fill it.
type MockRegistry struct {
	ChargeStations map[string]*ChargeStation
	Certificates   map[string]*x509.Certificate
}

func NewMockRegistry() *MockRegistry {
	return &MockRegistry{
		ChargeStations: make(map[string]*ChargeStation),
		Certificates:   make(map[string]*x509.Certificate),
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
