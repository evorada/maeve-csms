// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// TODO: Implement ChargeStationAuthStore interface
func (s *Store) SetChargeStationAuth(ctx context.Context, csId string, csAuth *store.ChargeStationAuth) error {
	return fmt.Errorf("not implemented")
}

func (s *Store) LookupChargeStationAuth(ctx context.Context, csId string) (*store.ChargeStationAuth, error) {
	return nil, fmt.Errorf("not implemented")
}

// TODO: Implement ChargeStationSettingsStore interface
func (s *Store) UpdateChargeStationSettings(ctx context.Context, chargeStationId string, settings *store.ChargeStationSettings) error {
	return fmt.Errorf("not implemented")
}

func (s *Store) LookupChargeStationSettings(ctx context.Context, chargeStationId string) (*store.ChargeStationSettings, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *Store) ListChargeStationSettings(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationSettings, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *Store) DeleteChargeStationSettings(ctx context.Context, chargeStationId string) error {
	return fmt.Errorf("not implemented")
}

// TODO: Implement ChargeStationRuntimeDetailsStore interface
func (s *Store) SetChargeStationRuntimeDetails(ctx context.Context, chargeStationId string, details *store.ChargeStationRuntimeDetails) error {
	return fmt.Errorf("not implemented")
}

func (s *Store) LookupChargeStationRuntimeDetails(ctx context.Context, chargeStationId string) (*store.ChargeStationRuntimeDetails, error) {
	return nil, fmt.Errorf("not implemented")
}

// TODO: Implement ChargeStationInstallCertificatesStore interface
func (s *Store) UpdateChargeStationInstallCertificates(ctx context.Context, chargeStationId string, certificates *store.ChargeStationInstallCertificates) error {
	return fmt.Errorf("not implemented")
}

func (s *Store) LookupChargeStationInstallCertificates(ctx context.Context, chargeStationId string) (*store.ChargeStationInstallCertificates, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *Store) ListChargeStationInstallCertificates(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationInstallCertificates, error) {
	return nil, fmt.Errorf("not implemented")
}

// TODO: Implement ChargeStationTriggerMessageStore interface
func (s *Store) SetChargeStationTriggerMessage(ctx context.Context, chargeStationId string, triggerMessage *store.ChargeStationTriggerMessage) error {
	return fmt.Errorf("not implemented")
}

func (s *Store) DeleteChargeStationTriggerMessage(ctx context.Context, chargeStationId string) error {
	return fmt.Errorf("not implemented")
}

func (s *Store) LookupChargeStationTriggerMessage(ctx context.Context, chargeStationId string) (*store.ChargeStationTriggerMessage, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *Store) ListChargeStationTriggerMessages(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationTriggerMessage, error) {
	return nil, fmt.Errorf("not implemented")
}

// TODO: Implement TransactionStore interface
func (s *Store) Transactions(ctx context.Context) ([]*store.Transaction, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *Store) FindTransaction(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *Store) CreateTransaction(ctx context.Context, chargeStationId, transactionId, idToken, tokenType string, meterValue []store.MeterValue, seqNo int, offline bool) error {
	return fmt.Errorf("not implemented")
}

func (s *Store) UpdateTransaction(ctx context.Context, chargeStationId, transactionId string, meterValue []store.MeterValue) error {
	return fmt.Errorf("not implemented")
}

func (s *Store) EndTransaction(ctx context.Context, chargeStationId, transactionId, idToken, tokenType string, meterValue []store.MeterValue, seqNo int) error {
	return fmt.Errorf("not implemented")
}

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
