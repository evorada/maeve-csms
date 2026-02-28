// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Store) SetChargeStationStatus(ctx context.Context, chargeStationId string, status *store.ChargeStationStatus) error {
	var lastHeartbeat pgtype.Timestamptz
	if status.LastHeartbeat != nil {
		lastHeartbeat = pgtype.Timestamptz{Time: *status.LastHeartbeat, Valid: true}
	}

	var firmwareVersion pgtype.Text
	if status.FirmwareVersion != nil {
		firmwareVersion = pgtype.Text{String: *status.FirmwareVersion, Valid: true}
	}

	var model pgtype.Text
	if status.Model != nil {
		model = pgtype.Text{String: *status.Model, Valid: true}
	}

	var vendor pgtype.Text
	if status.Vendor != nil {
		vendor = pgtype.Text{String: *status.Vendor, Valid: true}
	}

	var serialNumber pgtype.Text
	if status.SerialNumber != nil {
		serialNumber = pgtype.Text{String: *status.SerialNumber, Valid: true}
	}

	err := s.writeQueries().UpsertChargeStationStatus(ctx, UpsertChargeStationStatusParams{
		ChargeStationID: chargeStationId,
		Connected:       status.Connected,
		LastHeartbeat:   lastHeartbeat,
		FirmwareVersion: firmwareVersion,
		Model:           model,
		Vendor:          vendor,
		SerialNumber:    serialNumber,
		UpdatedAt:       pgtype.Timestamptz{Time: status.UpdatedAt, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to set charge station status: %w", err)
	}
	return nil
}

func (s *Store) GetChargeStationStatus(ctx context.Context, chargeStationId string) (*store.ChargeStationStatus, error) {
	row, err := s.readQueries().GetChargeStationStatus(ctx, chargeStationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("charge station %s not found", chargeStationId)
		}
		return nil, fmt.Errorf("failed to get charge station status: %w", err)
	}

	status := &store.ChargeStationStatus{
		ChargeStationId: row.ChargeStationID,
		Connected:       row.Connected,
		UpdatedAt:       row.UpdatedAt.Time.UTC(),
	}

	if row.LastHeartbeat.Valid {
		t := row.LastHeartbeat.Time.UTC()
		status.LastHeartbeat = &t
	}

	if row.FirmwareVersion.Valid {
		status.FirmwareVersion = &row.FirmwareVersion.String
	}

	if row.Model.Valid {
		status.Model = &row.Model.String
	}

	if row.Vendor.Valid {
		status.Vendor = &row.Vendor.String
	}

	if row.SerialNumber.Valid {
		status.SerialNumber = &row.SerialNumber.String
	}

	return status, nil
}

func (s *Store) UpdateHeartbeat(ctx context.Context, chargeStationId string, timestamp time.Time) error {
	err := s.writeQueries().UpdateHeartbeat(ctx, UpdateHeartbeatParams{
		ChargeStationID: chargeStationId,
		LastHeartbeat:   pgtype.Timestamptz{Time: timestamp, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}
	return nil
}

func (s *Store) SetConnectorStatus(ctx context.Context, chargeStationId string, connectorId int, status *store.ConnectorStatus) error {
	var info pgtype.Text
	if status.Info != nil {
		info = pgtype.Text{String: *status.Info, Valid: true}
	}

	var timestamp pgtype.Timestamptz
	if status.Timestamp != nil {
		timestamp = pgtype.Timestamptz{Time: *status.Timestamp, Valid: true}
	}

	var vendorErrorCode pgtype.Text
	if status.VendorErrorCode != nil {
		vendorErrorCode = pgtype.Text{String: *status.VendorErrorCode, Valid: true}
	}

	var vendorId pgtype.Text
	if status.VendorId != nil {
		vendorId = pgtype.Text{String: *status.VendorId, Valid: true}
	}

	var currentTransactionId pgtype.Text
	if status.CurrentTransactionId != nil {
		currentTransactionId = pgtype.Text{String: *status.CurrentTransactionId, Valid: true}
	}

	err := s.writeQueries().UpsertConnectorStatus(ctx, UpsertConnectorStatusParams{
		ChargeStationID:      chargeStationId,
		ConnectorID:          int32(connectorId),
		Status:               string(status.Status),
		ErrorCode:            string(status.ErrorCode),
		Info:                 info,
		Timestamp:            timestamp,
		VendorErrorCode:      vendorErrorCode,
		VendorID:             vendorId,
		CurrentTransactionID: currentTransactionId,
		UpdatedAt:            pgtype.Timestamptz{Time: status.UpdatedAt, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to set connector status: %w", err)
	}
	return nil
}

func (s *Store) GetConnectorStatus(ctx context.Context, chargeStationId string, connectorId int) (*store.ConnectorStatus, error) {
	row, err := s.readQueries().GetConnectorStatus(ctx, GetConnectorStatusParams{
		ChargeStationID: chargeStationId,
		ConnectorID:     int32(connectorId),
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("connector %d on charge station %s not found", connectorId, chargeStationId)
		}
		return nil, fmt.Errorf("failed to get connector status: %w", err)
	}

	status := &store.ConnectorStatus{
		ChargeStationId: row.ChargeStationID,
		ConnectorId:     int(row.ConnectorID),
		Status:          store.ConnectorStatusType(row.Status),
		ErrorCode:       store.ConnectorErrorCode(row.ErrorCode),
		UpdatedAt:       row.UpdatedAt.Time.UTC(),
	}

	if row.Info.Valid {
		status.Info = &row.Info.String
	}

	if row.Timestamp.Valid {
		status.Timestamp = &row.Timestamp.Time
	}

	if row.VendorErrorCode.Valid {
		status.VendorErrorCode = &row.VendorErrorCode.String
	}

	if row.VendorID.Valid {
		status.VendorId = &row.VendorID.String
	}

	if row.CurrentTransactionID.Valid {
		status.CurrentTransactionId = &row.CurrentTransactionID.String
	}

	return status, nil
}

func (s *Store) ListConnectorStatuses(ctx context.Context, chargeStationId string) ([]*store.ConnectorStatus, error) {
	rows, err := s.readQueries().ListConnectorStatuses(ctx, chargeStationId)
	if err != nil {
		return nil, fmt.Errorf("failed to list connector statuses: %w", err)
	}

	statuses := make([]*store.ConnectorStatus, 0, len(rows))
	for _, row := range rows {
		status := &store.ConnectorStatus{
			ChargeStationId: row.ChargeStationID,
			ConnectorId:     int(row.ConnectorID),
			Status:          store.ConnectorStatusType(row.Status),
			ErrorCode:       store.ConnectorErrorCode(row.ErrorCode),
			UpdatedAt:       row.UpdatedAt.Time.UTC(),
		}

		if row.Info.Valid {
			status.Info = &row.Info.String
		}

		if row.Timestamp.Valid {
			status.Timestamp = &row.Timestamp.Time
		}

		if row.VendorErrorCode.Valid {
			status.VendorErrorCode = &row.VendorErrorCode.String
		}

		if row.VendorID.Valid {
			status.VendorId = &row.VendorID.String
		}

		if row.CurrentTransactionID.Valid {
			status.CurrentTransactionId = &row.CurrentTransactionID.String
		}

		statuses = append(statuses, status)
	}

	return statuses, nil
}
