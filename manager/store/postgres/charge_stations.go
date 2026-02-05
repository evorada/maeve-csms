// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// ChargeStationAuthStore implementation

func (s *Store) SetChargeStationAuth(ctx context.Context, csId string, csAuth *store.ChargeStationAuth) error {
	params := SetChargeStationAuthParams{
		ChargeStationID:        csId,
		SecurityProfile:        int32(csAuth.SecurityProfile),
		Base64Sha256Password:   toNullableText(csAuth.Base64SHA256Password),
		InvalidUsernameAllowed: csAuth.InvalidUsernameAllowed,
	}

	_, err := s.q.SetChargeStationAuth(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to set charge station auth: %w", err)
	}

	return nil
}

func (s *Store) LookupChargeStationAuth(ctx context.Context, csId string) (*store.ChargeStationAuth, error) {
	cs, err := s.q.GetChargeStationAuth(ctx, csId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup charge station auth: %w", err)
	}

	return &store.ChargeStationAuth{
		SecurityProfile:        store.SecurityProfile(cs.SecurityProfile),
		Base64SHA256Password:   fromNullableText(cs.Base64Sha256Password),
		InvalidUsernameAllowed: cs.InvalidUsernameAllowed,
	}, nil
}

// ChargeStationSettingsStore implementation

func (s *Store) UpdateChargeStationSettings(ctx context.Context, chargeStationId string, settings *store.ChargeStationSettings) error {
	// Marshal settings map to JSON
	settingsJSON, err := json.Marshal(settings.Settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	params := SetChargeStationSettingsParams{
		ChargeStationID: chargeStationId,
		Settings:        settingsJSON,
	}

	_, err = s.q.SetChargeStationSettings(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to update charge station settings: %w", err)
	}

	return nil
}

func (s *Store) LookupChargeStationSettings(ctx context.Context, chargeStationId string) (*store.ChargeStationSettings, error) {
	dbSettings, err := s.q.GetChargeStationSettings(ctx, chargeStationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup charge station settings: %w", err)
	}

	// Unmarshal JSON to settings map
	var settings map[string]*store.ChargeStationSetting
	if err := json.Unmarshal(dbSettings.Settings, &settings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
	}

	return &store.ChargeStationSettings{
		ChargeStationId: chargeStationId,
		Settings:        settings,
	}, nil
}

func (s *Store) ListChargeStationSettings(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationSettings, error) {
	if previousChargeStationId == "" {
		previousChargeStationId = ""
	}

	dbSettingsList, err := s.q.ListChargeStationSettings(ctx, ListChargeStationSettingsParams{
		ChargeStationID: previousChargeStationId,
		Limit:           int32(pageSize),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list charge station settings: %w", err)
	}

	result := make([]*store.ChargeStationSettings, 0, len(dbSettingsList))
	for _, dbSettings := range dbSettingsList {
		var settings map[string]*store.ChargeStationSetting
		if err := json.Unmarshal(dbSettings.Settings, &settings); err != nil {
			return nil, fmt.Errorf("failed to unmarshal settings for %s: %w", dbSettings.ChargeStationID, err)
		}

		result = append(result, &store.ChargeStationSettings{
			ChargeStationId: dbSettings.ChargeStationID,
			Settings:        settings,
		})
	}

	return result, nil
}

func (s *Store) DeleteChargeStationSettings(ctx context.Context, chargeStationId string) error {
	err := s.q.DeleteChargeStationSettings(ctx, chargeStationId)
	if err != nil {
		return fmt.Errorf("failed to delete charge station settings: %w", err)
	}
	return nil
}

// ChargeStationRuntimeDetailsStore implementation

func (s *Store) SetChargeStationRuntimeDetails(ctx context.Context, chargeStationId string, details *store.ChargeStationRuntimeDetails) error {
	params := SetChargeStationRuntimeParams{
		ChargeStationID: chargeStationId,
		OcppVersion:     details.OcppVersion,
		Vendor:          pgtype.Text{Valid: false}, // Not in store interface currently
		Model:           pgtype.Text{Valid: false},
		SerialNumber:    pgtype.Text{Valid: false},
		FirmwareVersion: pgtype.Text{Valid: false},
	}

	_, err := s.q.SetChargeStationRuntime(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to set charge station runtime details: %w", err)
	}

	return nil
}

func (s *Store) LookupChargeStationRuntimeDetails(ctx context.Context, chargeStationId string) (*store.ChargeStationRuntimeDetails, error) {
	runtime, err := s.q.GetChargeStationRuntime(ctx, chargeStationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup charge station runtime details: %w", err)
	}

	return &store.ChargeStationRuntimeDetails{
		OcppVersion: runtime.OcppVersion,
	}, nil
}

// ChargeStationInstallCertificatesStore implementation

func (s *Store) UpdateChargeStationInstallCertificates(ctx context.Context, chargeStationId string, certificates *store.ChargeStationInstallCertificates) error {
	// First, delete all existing certificates for this station
	if err := s.q.DeleteChargeStationCertificates(ctx, chargeStationId); err != nil {
		return fmt.Errorf("failed to delete existing certificates: %w", err)
	}

	// Then insert all new certificates
	for _, cert := range certificates.Certificates {
		params := AddChargeStationCertificateParams{
			ChargeStationID:               chargeStationId,
			CertificateID:                 cert.CertificateId,
			CertificateType:               string(cert.CertificateType),
			Certificate:                   cert.CertificateData,
			CertificateInstallationStatus: string(cert.CertificateInstallationStatus),
			SendAfter:                     toPgTimestamp(cert.SendAfter),
		}

		_, err := s.q.AddChargeStationCertificate(ctx, params)
		if err != nil {
			return fmt.Errorf("failed to add certificate: %w", err)
		}
	}

	return nil
}

func (s *Store) LookupChargeStationInstallCertificates(ctx context.Context, chargeStationId string) (*store.ChargeStationInstallCertificates, error) {
	dbCerts, err := s.q.GetChargeStationCertificates(ctx, chargeStationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup charge station certificates: %w", err)
	}

	if len(dbCerts) == 0 {
		return nil, nil
	}

	certs := make([]*store.ChargeStationInstallCertificate, 0, len(dbCerts))
	for _, dbCert := range dbCerts {
		certs = append(certs, &store.ChargeStationInstallCertificate{
			CertificateType:               store.CertificateType(dbCert.CertificateType),
			CertificateId:                 dbCert.CertificateID,
			CertificateData:               dbCert.Certificate,
			CertificateInstallationStatus: store.CertificateInstallationStatus(dbCert.CertificateInstallationStatus),
			SendAfter:                     fromPgTimestamp(dbCert.SendAfter),
		})
	}

	return &store.ChargeStationInstallCertificates{
		ChargeStationId: chargeStationId,
		Certificates:    certs,
	}, nil
}

func (s *Store) ListChargeStationInstallCertificates(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationInstallCertificates, error) {
	if previousChargeStationId == "" {
		previousChargeStationId = ""
	}

	dbCertsList, err := s.q.ListChargeStationCertificates(ctx, ListChargeStationCertificatesParams{
		ChargeStationID: previousChargeStationId,
		Limit:           int32(pageSize),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list charge station certificates: %w", err)
	}

	// Group certificates by charge station ID
	certsByStation := make(map[string][]*store.ChargeStationInstallCertificate)
	for _, dbCert := range dbCertsList {
		cert := &store.ChargeStationInstallCertificate{
			CertificateType:               store.CertificateType(dbCert.CertificateType),
			CertificateId:                 dbCert.CertificateID,
			CertificateData:               dbCert.Certificate,
			CertificateInstallationStatus: store.CertificateInstallationStatus(dbCert.CertificateInstallationStatus),
			SendAfter:                     fromPgTimestamp(dbCert.SendAfter),
		}
		certsByStation[dbCert.ChargeStationID] = append(certsByStation[dbCert.ChargeStationID], cert)
	}

	result := make([]*store.ChargeStationInstallCertificates, 0, len(certsByStation))
	for csId, certs := range certsByStation {
		result = append(result, &store.ChargeStationInstallCertificates{
			ChargeStationId: csId,
			Certificates:    certs,
		})
	}

	return result, nil
}

// ChargeStationTriggerMessageStore implementation

func (s *Store) SetChargeStationTriggerMessage(ctx context.Context, chargeStationId string, triggerMessage *store.ChargeStationTriggerMessage) error {
	params := SetChargeStationTriggerParams{
		ChargeStationID: chargeStationId,
		MessageType:     string(triggerMessage.TriggerMessage),
		TriggerStatus:   string(triggerMessage.TriggerStatus),
		SendAfter:       toPgTimestamp(triggerMessage.SendAfter),
	}

	_, err := s.q.SetChargeStationTrigger(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to set charge station trigger message: %w", err)
	}

	return nil
}

func (s *Store) DeleteChargeStationTriggerMessage(ctx context.Context, chargeStationId string) error {
	err := s.q.DeleteChargeStationTrigger(ctx, chargeStationId)
	if err != nil {
		return fmt.Errorf("failed to delete charge station trigger message: %w", err)
	}
	return nil
}

func (s *Store) LookupChargeStationTriggerMessage(ctx context.Context, chargeStationId string) (*store.ChargeStationTriggerMessage, error) {
	trigger, err := s.q.GetChargeStationTrigger(ctx, chargeStationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup charge station trigger message: %w", err)
	}

	return &store.ChargeStationTriggerMessage{
		ChargeStationId: chargeStationId,
		TriggerMessage:  store.TriggerMessage(trigger.MessageType),
		TriggerStatus:   store.TriggerStatus(trigger.TriggerStatus),
		SendAfter:       fromPgTimestamp(trigger.SendAfter),
	}, nil
}

func (s *Store) ListChargeStationTriggerMessages(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationTriggerMessage, error) {
	if previousChargeStationId == "" {
		previousChargeStationId = ""
	}

	triggers, err := s.q.ListChargeStationTriggers(ctx, ListChargeStationTriggersParams{
		ChargeStationID: previousChargeStationId,
		Limit:           int32(pageSize),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list charge station trigger messages: %w", err)
	}

	result := make([]*store.ChargeStationTriggerMessage, 0, len(triggers))
	for _, trigger := range triggers {
		result = append(result, &store.ChargeStationTriggerMessage{
			ChargeStationId: trigger.ChargeStationID,
			TriggerMessage:  store.TriggerMessage(trigger.MessageType),
			TriggerStatus:   store.TriggerStatus(trigger.TriggerStatus),
			SendAfter:       fromPgTimestamp(trigger.SendAfter),
		})
	}

	return result, nil
}
