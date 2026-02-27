// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"encoding/json"
	"errors"
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

	_, err := s.writeQueries().SetChargeStationAuth(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to set charge station auth: %w", err)
	}

	return nil
}

func (s *Store) LookupChargeStationAuth(ctx context.Context, csId string) (*store.ChargeStationAuth, error) {
	cs, err := s.readQueries().GetChargeStationAuth(ctx, csId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup charge station auth: %w", err)
	}

	securityProfile, err := securityProfileFromInt32(cs.SecurityProfile)
	if err != nil {
		return nil, fmt.Errorf("invalid security profile: %w", err)
	}

	return &store.ChargeStationAuth{
		SecurityProfile:        securityProfile,
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

	_, err = s.writeQueries().SetChargeStationSettings(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to update charge station settings: %w", err)
	}

	return nil
}

func (s *Store) LookupChargeStationSettings(ctx context.Context, chargeStationId string) (*store.ChargeStationSettings, error) {
	dbSettings, err := s.readQueries().GetChargeStationSettings(ctx, chargeStationId)
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

	pageSizeInt32, err := safeIntToInt32(pageSize)
	if err != nil {
		return nil, fmt.Errorf("invalid page size value: %w", err)
	}

	dbSettingsList, err := s.readQueries().ListChargeStationSettings(ctx, ListChargeStationSettingsParams{
		ChargeStationID: previousChargeStationId,
		Limit:           pageSizeInt32,
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
	err := s.writeQueries().DeleteChargeStationSettings(ctx, chargeStationId)
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

	_, err := s.writeQueries().SetChargeStationRuntime(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to set charge station runtime details: %w", err)
	}

	return nil
}

func (s *Store) LookupChargeStationRuntimeDetails(ctx context.Context, chargeStationId string) (*store.ChargeStationRuntimeDetails, error) {
	runtime, err := s.readQueries().GetChargeStationRuntime(ctx, chargeStationId)
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
	if err := s.writeQueries().DeleteChargeStationCertificates(ctx, chargeStationId); err != nil {
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

		_, err := s.writeQueries().AddChargeStationCertificate(ctx, params)
		if err != nil {
			return fmt.Errorf("failed to add certificate: %w", err)
		}
	}

	return nil
}

func (s *Store) LookupChargeStationInstallCertificates(ctx context.Context, chargeStationId string) (*store.ChargeStationInstallCertificates, error) {
	dbCerts, err := s.readQueries().GetChargeStationCertificates(ctx, chargeStationId)
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

	pageSizeInt32, err := safeIntToInt32(pageSize)
	if err != nil {
		return nil, fmt.Errorf("invalid page size value: %w", err)
	}

	dbCertsList, err := s.readQueries().ListChargeStationCertificates(ctx, ListChargeStationCertificatesParams{
		ChargeStationID: previousChargeStationId,
		Limit:           pageSizeInt32,
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
		ConnectorID:     toNullInt32(triggerMessage.ConnectorId),
		TriggerStatus:   string(triggerMessage.TriggerStatus),
		SendAfter:       toPgTimestamp(triggerMessage.SendAfter),
	}

	_, err := s.writeQueries().SetChargeStationTrigger(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to set charge station trigger message: %w", err)
	}

	return nil
}

func (s *Store) DeleteChargeStationTriggerMessage(ctx context.Context, chargeStationId string) error {
	err := s.writeQueries().DeleteChargeStationTrigger(ctx, chargeStationId)
	if err != nil {
		return fmt.Errorf("failed to delete charge station trigger message: %w", err)
	}
	return nil
}

func (s *Store) LookupChargeStationTriggerMessage(ctx context.Context, chargeStationId string) (*store.ChargeStationTriggerMessage, error) {
	trigger, err := s.readQueries().GetChargeStationTrigger(ctx, chargeStationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup charge station trigger message: %w", err)
	}

	return &store.ChargeStationTriggerMessage{
		ChargeStationId: chargeStationId,
		TriggerMessage:  store.TriggerMessage(trigger.MessageType),
		ConnectorId:     fromNullInt32(trigger.ConnectorID),
		TriggerStatus:   store.TriggerStatus(trigger.TriggerStatus),
		SendAfter:       fromPgTimestamp(trigger.SendAfter),
	}, nil
}

func (s *Store) ListChargeStationTriggerMessages(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationTriggerMessage, error) {
	if previousChargeStationId == "" {
		previousChargeStationId = ""
	}

	pageSizeInt32, err := safeIntToInt32(pageSize)
	if err != nil {
		return nil, fmt.Errorf("invalid page size value: %w", err)
	}

	triggers, err := s.readQueries().ListChargeStationTriggers(ctx, ListChargeStationTriggersParams{
		ChargeStationID: previousChargeStationId,
		Limit:           pageSizeInt32,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list charge station trigger messages: %w", err)
	}

	result := make([]*store.ChargeStationTriggerMessage, 0, len(triggers))
	for _, trigger := range triggers {
		result = append(result, &store.ChargeStationTriggerMessage{
			ChargeStationId: trigger.ChargeStationID,
			TriggerMessage:  store.TriggerMessage(trigger.MessageType),
			ConnectorId:     fromNullInt32(trigger.ConnectorID),
			TriggerStatus:   store.TriggerStatus(trigger.TriggerStatus),
			SendAfter:       fromPgTimestamp(trigger.SendAfter),
		})
	}

	return result, nil
}

// ChargeStationDataTransferStore implementation

func (s *Store) SetChargeStationDataTransfer(ctx context.Context, chargeStationId string, dataTransfer *store.ChargeStationDataTransfer) error {
	params := SetChargeStationDataTransferParams{
		ChargeStationID: chargeStationId,
		VendorID:        dataTransfer.VendorId,
		MessageID:       toNullableTextPtr(dataTransfer.MessageId),
		Data:            toNullableTextPtr(dataTransfer.Data),
		Status:          string(dataTransfer.Status),
		SendAfter:       toPgTimestamptz(dataTransfer.SendAfter),
	}

	_, err := s.writeQueries().SetChargeStationDataTransfer(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to set charge station data transfer: %w", err)
	}

	return nil
}

func (s *Store) LookupChargeStationDataTransfer(ctx context.Context, chargeStationId string) (*store.ChargeStationDataTransfer, error) {
	dataTransfer, err := s.readQueries().GetChargeStationDataTransfer(ctx, chargeStationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup charge station data transfer: %w", err)
	}

	return &store.ChargeStationDataTransfer{
		ChargeStationId: dataTransfer.ChargeStationID,
		VendorId:        dataTransfer.VendorID,
		MessageId:       fromNullableTextPtr(dataTransfer.MessageID),
		Data:            fromNullableTextPtr(dataTransfer.Data),
		Status:          store.DataTransferStatus(dataTransfer.Status),
		ResponseData:    fromNullableTextPtr(dataTransfer.ResponseData),
		SendAfter:       fromPgTimestamptz(dataTransfer.SendAfter),
	}, nil
}

func (s *Store) ListChargeStationDataTransfers(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationDataTransfer, error) {
	if previousChargeStationId == "" {
		previousChargeStationId = ""
	}

	pageSizeInt32, err := safeIntToInt32(pageSize)
	if err != nil {
		return nil, fmt.Errorf("invalid page size value: %w", err)
	}

	dataTransfers, err := s.readQueries().ListChargeStationDataTransfers(ctx, ListChargeStationDataTransfersParams{
		ChargeStationID: previousChargeStationId,
		Limit:           pageSizeInt32,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list charge station data transfers: %w", err)
	}

	result := make([]*store.ChargeStationDataTransfer, 0, len(dataTransfers))
	for _, dt := range dataTransfers {
		result = append(result, &store.ChargeStationDataTransfer{
			ChargeStationId: dt.ChargeStationID,
			VendorId:        dt.VendorID,
			MessageId:       fromNullableTextPtr(dt.MessageID),
			Data:            fromNullableTextPtr(dt.Data),
			Status:          store.DataTransferStatus(dt.Status),
			ResponseData:    fromNullableTextPtr(dt.ResponseData),
			SendAfter:       fromPgTimestamptz(dt.SendAfter),
		})
	}

	return result, nil
}

func (s *Store) DeleteChargeStationDataTransfer(ctx context.Context, chargeStationId string) error {
	err := s.writeQueries().DeleteChargeStationDataTransfer(ctx, chargeStationId)
	if err != nil {
		return fmt.Errorf("failed to delete charge station data transfer: %w", err)
	}
	return nil
}

// ChargeStationClearCacheStore implementation

func (s *Store) SetChargeStationClearCache(ctx context.Context, chargeStationId string, clearCache *store.ChargeStationClearCache) error {
	params := SetChargeStationClearCacheParams{
		ChargeStationID: chargeStationId,
		Status:          string(clearCache.Status),
		SendAfter:       toPgTimestamptz(clearCache.SendAfter),
	}

	_, err := s.writeQueries().SetChargeStationClearCache(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to set charge station clear cache: %w", err)
	}

	return nil
}

func (s *Store) LookupChargeStationClearCache(ctx context.Context, chargeStationId string) (*store.ChargeStationClearCache, error) {
	clearCache, err := s.readQueries().GetChargeStationClearCache(ctx, chargeStationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup charge station clear cache: %w", err)
	}

	return &store.ChargeStationClearCache{
		ChargeStationId: clearCache.ChargeStationID,
		Status:          store.ClearCacheStatus(clearCache.Status),
		SendAfter:       fromPgTimestamptz(clearCache.SendAfter),
	}, nil
}

func (s *Store) ListChargeStationClearCaches(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationClearCache, error) {
	if previousChargeStationId == "" {
		previousChargeStationId = ""
	}

	pageSizeInt32, err := safeIntToInt32(pageSize)
	if err != nil {
		return nil, fmt.Errorf("invalid page size value: %w", err)
	}

	clearCaches, err := s.readQueries().ListChargeStationClearCaches(ctx, ListChargeStationClearCachesParams{
		ChargeStationID: previousChargeStationId,
		Limit:           pageSizeInt32,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list charge station clear caches: %w", err)
	}

	result := make([]*store.ChargeStationClearCache, 0, len(clearCaches))
	for _, cc := range clearCaches {
		result = append(result, &store.ChargeStationClearCache{
			ChargeStationId: cc.ChargeStationID,
			Status:          store.ClearCacheStatus(cc.Status),
			SendAfter:       fromPgTimestamptz(cc.SendAfter),
		})
	}

	return result, nil
}

func (s *Store) DeleteChargeStationClearCache(ctx context.Context, chargeStationId string) error {
	err := s.writeQueries().DeleteChargeStationClearCache(ctx, chargeStationId)
	if err != nil {
		return fmt.Errorf("failed to delete charge station clear cache: %w", err)
	}
	return nil
}

// ChargeStationChangeAvailabilityStore implementation

func (s *Store) SetChargeStationChangeAvailability(ctx context.Context, chargeStationId string, changeAvailability *store.ChargeStationChangeAvailability) error {
	params := SetChargeStationChangeAvailabilityParams{
		ChargeStationID:  chargeStationId,
		ConnectorID:      toNullableInt32(changeAvailability.ConnectorId),
		EvseID:           toNullableInt32(changeAvailability.EvseId),
		AvailabilityType: string(changeAvailability.Type),
		Status:           string(changeAvailability.Status),
		SendAfter:        toPgTimestamptz(changeAvailability.SendAfter),
	}

	_, err := s.writeQueries().SetChargeStationChangeAvailability(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to set charge station change availability: %w", err)
	}

	return nil
}

func (s *Store) LookupChargeStationChangeAvailability(ctx context.Context, chargeStationId string) (*store.ChargeStationChangeAvailability, error) {
	changeAvailability, err := s.readQueries().GetChargeStationChangeAvailability(ctx, chargeStationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup charge station change availability: %w", err)
	}

	return &store.ChargeStationChangeAvailability{
		ChargeStationId: changeAvailability.ChargeStationID,
		ConnectorId:     fromNullableInt32(changeAvailability.ConnectorID),
		EvseId:          fromNullableInt32(changeAvailability.EvseID),
		Type:            store.AvailabilityType(changeAvailability.AvailabilityType),
		Status:          store.AvailabilityStatus(changeAvailability.Status),
		SendAfter:       fromPgTimestamptz(changeAvailability.SendAfter),
	}, nil
}

func (s *Store) ListChargeStationChangeAvailabilities(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationChangeAvailability, error) {
	if previousChargeStationId == "" {
		previousChargeStationId = ""
	}

	pageSizeInt32, err := safeIntToInt32(pageSize)
	if err != nil {
		return nil, fmt.Errorf("invalid page size value: %w", err)
	}

	changeAvailabilities, err := s.readQueries().ListChargeStationChangeAvailabilities(ctx, ListChargeStationChangeAvailabilitiesParams{
		ChargeStationID: previousChargeStationId,
		Limit:           pageSizeInt32,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list charge station change availabilities: %w", err)
	}

	result := make([]*store.ChargeStationChangeAvailability, 0, len(changeAvailabilities))
	for _, ca := range changeAvailabilities {
		result = append(result, &store.ChargeStationChangeAvailability{
			ChargeStationId: ca.ChargeStationID,
			ConnectorId:     fromNullableInt32(ca.ConnectorID),
			EvseId:          fromNullableInt32(ca.EvseID),
			Type:            store.AvailabilityType(ca.AvailabilityType),
			Status:          store.AvailabilityStatus(ca.Status),
			SendAfter:       fromPgTimestamptz(ca.SendAfter),
		})
	}

	return result, nil
}

func (s *Store) DeleteChargeStationChangeAvailability(ctx context.Context, chargeStationId string) error {
	err := s.writeQueries().DeleteChargeStationChangeAvailability(ctx, chargeStationId)
	if err != nil {
		return fmt.Errorf("failed to delete charge station change availability: %w", err)
	}
	return nil
}

func (s *Store) SetChargeStationCertificateQuery(ctx context.Context, chargeStationId string, query *store.ChargeStationCertificateQuery) error {
	_, err := s.writeQueries().SetChargeStationCertificateQuery(ctx, SetChargeStationCertificateQueryParams{
		ChargeStationID: chargeStationId,
		CertificateType: textFromString(query.CertificateType),
		QueryStatus:     string(query.QueryStatus),
		SendAfter:       toPgTimestamp(query.SendAfter),
	})
	if err != nil {
		return fmt.Errorf("failed to set certificate query: %w", err)
	}
	return nil
}

func (s *Store) DeleteChargeStationCertificateQuery(ctx context.Context, chargeStationId string) error {
	err := s.writeQueries().DeleteChargeStationCertificateQuery(ctx, chargeStationId)
	if err != nil {
		return fmt.Errorf("failed to delete certificate query: %w", err)
	}
	return nil
}

func (s *Store) LookupChargeStationCertificateQuery(ctx context.Context, chargeStationId string) (*store.ChargeStationCertificateQuery, error) {
	row, err := s.readQueries().LookupChargeStationCertificateQuery(ctx, chargeStationId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup certificate query: %w", err)
	}
	return &store.ChargeStationCertificateQuery{
		ChargeStationId: row.ChargeStationID,
		CertificateType: stringFromText(row.CertificateType),
		QueryStatus:     store.CertificateQueryStatus(row.QueryStatus),
		SendAfter:       fromPgTimestamp(row.SendAfter),
	}, nil
}

func (s *Store) ListChargeStationCertificateQueries(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationCertificateQuery, error) {
	rows, err := s.readQueries().ListChargeStationCertificateQueries(ctx, ListChargeStationCertificateQueriesParams{
		ChargeStationID: previousChargeStationId,
		Limit:           int32(pageSize),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list certificate queries: %w", err)
	}

	var result []*store.ChargeStationCertificateQuery
	for _, row := range rows {
		result = append(result, &store.ChargeStationCertificateQuery{
			ChargeStationId: row.ChargeStationID,
			CertificateType: stringFromText(row.CertificateType),
			QueryStatus:     store.CertificateQueryStatus(row.QueryStatus),
			SendAfter:       fromPgTimestamp(row.SendAfter),
		})
	}
	return result, nil
}

func (s *Store) SetChargeStationCertificateDeletion(ctx context.Context, chargeStationId string, deletion *store.ChargeStationCertificateDeletion) error {
	_, err := s.writeQueries().SetChargeStationCertificateDeletion(ctx, SetChargeStationCertificateDeletionParams{
		ChargeStationID: chargeStationId,
		HashAlgorithm:   deletion.HashAlgorithm,
		IssuerNameHash:  deletion.IssuerNameHash,
		IssuerKeyHash:   deletion.IssuerKeyHash,
		SerialNumber:    deletion.SerialNumber,
		DeletionStatus:  string(deletion.DeletionStatus),
		SendAfter:       toPgTimestamp(deletion.SendAfter),
	})
	if err != nil {
		return fmt.Errorf("failed to set certificate deletion: %w", err)
	}
	return nil
}

func (s *Store) DeleteChargeStationCertificateDeletion(ctx context.Context, chargeStationId string) error {
	err := s.writeQueries().DeleteChargeStationCertificateDeletion(ctx, chargeStationId)
	if err != nil {
		return fmt.Errorf("failed to delete certificate deletion: %w", err)
	}
	return nil
}

func (s *Store) LookupChargeStationCertificateDeletion(ctx context.Context, chargeStationId string) (*store.ChargeStationCertificateDeletion, error) {
	row, err := s.readQueries().LookupChargeStationCertificateDeletion(ctx, chargeStationId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup certificate deletion: %w", err)
	}
	return &store.ChargeStationCertificateDeletion{
		ChargeStationId: row.ChargeStationID,
		HashAlgorithm:   row.HashAlgorithm,
		IssuerNameHash:  row.IssuerNameHash,
		IssuerKeyHash:   row.IssuerKeyHash,
		SerialNumber:    row.SerialNumber,
		DeletionStatus:  store.CertificateDeletionStatus(row.DeletionStatus),
		SendAfter:       fromPgTimestamp(row.SendAfter),
	}, nil
}

func (s *Store) ListChargeStationCertificateDeletions(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationCertificateDeletion, error) {
	rows, err := s.readQueries().ListChargeStationCertificateDeletions(ctx, ListChargeStationCertificateDeletionsParams{
		ChargeStationID: previousChargeStationId,
		Limit:           int32(pageSize),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list certificate deletions: %w", err)
	}

	var result []*store.ChargeStationCertificateDeletion
	for _, row := range rows {
		result = append(result, &store.ChargeStationCertificateDeletion{
			ChargeStationId: row.ChargeStationID,
			HashAlgorithm:   row.HashAlgorithm,
			IssuerNameHash:  row.IssuerNameHash,
			IssuerKeyHash:   row.IssuerKeyHash,
			SerialNumber:    row.SerialNumber,
			DeletionStatus:  store.CertificateDeletionStatus(row.DeletionStatus),
			SendAfter:       fromPgTimestamp(row.SendAfter),
		})
	}
	return result, nil
}
