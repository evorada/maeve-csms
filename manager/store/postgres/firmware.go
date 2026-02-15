// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Store) SetFirmwareUpdateStatus(ctx context.Context, chargeStationId string, status *store.FirmwareUpdateStatus) error {
	err := s.q.UpsertFirmwareUpdateStatus(ctx, UpsertFirmwareUpdateStatusParams{
		ChargeStationID: chargeStationId,
		Status:          string(status.Status),
		Location:        status.Location,
		RetrieveDate:    pgtype.Timestamptz{Time: status.RetrieveDate, Valid: true},
		RetryCount:      int32(status.RetryCount),
		UpdatedAt:       pgtype.Timestamptz{Time: status.UpdatedAt, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to set firmware update status: %w", err)
	}
	return nil
}

func (s *Store) GetFirmwareUpdateStatus(ctx context.Context, chargeStationId string) (*store.FirmwareUpdateStatus, error) {
	row, err := s.q.GetFirmwareUpdateStatus(ctx, chargeStationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get firmware update status: %w", err)
	}

	return &store.FirmwareUpdateStatus{
		ChargeStationId: row.ChargeStationID,
		Status:          store.FirmwareUpdateStatusType(row.Status),
		Location:        row.Location,
		RetrieveDate:    row.RetrieveDate.Time,
		RetryCount:      int(row.RetryCount),
		UpdatedAt:       row.UpdatedAt.Time,
	}, nil
}

func (s *Store) SetDiagnosticsStatus(ctx context.Context, chargeStationId string, status *store.DiagnosticsStatus) error {
	err := s.q.UpsertDiagnosticsStatus(ctx, UpsertDiagnosticsStatusParams{
		ChargeStationID: chargeStationId,
		Status:          string(status.Status),
		Location:        status.Location,
		UpdatedAt:       pgtype.Timestamptz{Time: status.UpdatedAt, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to set diagnostics status: %w", err)
	}
	return nil
}

func (s *Store) GetDiagnosticsStatus(ctx context.Context, chargeStationId string) (*store.DiagnosticsStatus, error) {
	row, err := s.q.GetDiagnosticsStatus(ctx, chargeStationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get diagnostics status: %w", err)
	}

	return &store.DiagnosticsStatus{
		ChargeStationId: row.ChargeStationID,
		Status:          store.DiagnosticsStatusType(row.Status),
		Location:        row.Location,
		UpdatedAt:       row.UpdatedAt.Time,
	}, nil
}

func (s *Store) SetPublishFirmwareStatus(ctx context.Context, chargeStationId string, status *store.PublishFirmwareStatus) error {
	err := s.q.UpsertPublishFirmwareStatus(ctx, UpsertPublishFirmwareStatusParams{
		ChargeStationID: chargeStationId,
		Status:          string(status.Status),
		Location:        status.Location,
		Checksum:        status.Checksum,
		RequestID:       int32(status.RequestId),
		UpdatedAt:       pgtype.Timestamptz{Time: status.UpdatedAt, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to set publish firmware status: %w", err)
	}
	return nil
}

func (s *Store) GetPublishFirmwareStatus(ctx context.Context, chargeStationId string) (*store.PublishFirmwareStatus, error) {
	row, err := s.q.GetPublishFirmwareStatus(ctx, chargeStationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get publish firmware status: %w", err)
	}

	return &store.PublishFirmwareStatus{
		ChargeStationId: row.ChargeStationID,
		Status:          store.PublishFirmwareStatusType(row.Status),
		Location:        row.Location,
		Checksum:        row.Checksum,
		RequestId:       int(row.RequestID),
		UpdatedAt:       row.UpdatedAt.Time,
	}, nil
}
