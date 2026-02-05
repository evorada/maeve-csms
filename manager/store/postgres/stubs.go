// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// ChargeStation store interfaces implemented in charge_stations.go
// TransactionStore interface implemented in transactions.go

// TODO: Implement CertificateStore interface
func (s *Store) SetCertificate(ctx context.Context, pemCertificate string) error {
	return fmt.Errorf("not implemented")
}

func (s *Store) LookupCertificate(ctx context.Context, certificateHash string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (s *Store) DeleteCertificate(ctx context.Context, certificateHash string) error {
	return fmt.Errorf("not implemented")
}

// TODO: Implement OcpiStore interface
func (s *Store) SetRegistrationDetails(ctx context.Context, token string, registration *store.OcpiRegistration) error {
	return fmt.Errorf("not implemented")
}

func (s *Store) GetRegistrationDetails(ctx context.Context, token string) (*store.OcpiRegistration, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *Store) DeleteRegistrationDetails(ctx context.Context, token string) error {
	return fmt.Errorf("not implemented")
}

func (s *Store) SetPartyDetails(ctx context.Context, partyDetails *store.OcpiParty) error {
	return fmt.Errorf("not implemented")
}

func (s *Store) GetPartyDetails(ctx context.Context, role, countryCode, partyId string) (*store.OcpiParty, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *Store) ListPartyDetailsForRole(ctx context.Context, role string) ([]*store.OcpiParty, error) {
	return nil, fmt.Errorf("not implemented")
}

// TODO: Implement LocationStore interface
func (s *Store) SetLocation(ctx context.Context, location *store.Location) error {
	return fmt.Errorf("not implemented")
}

func (s *Store) LookupLocation(ctx context.Context, locationId string) (*store.Location, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *Store) ListLocations(ctx context.Context, offset int, limit int) ([]*store.Location, error) {
	return nil, fmt.Errorf("not implemented")
}
