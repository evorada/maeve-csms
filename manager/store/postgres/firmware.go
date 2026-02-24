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

// FirmwareUpdateRequest store methods
func (s *Store) SetFirmwareUpdateRequest(ctx context.Context, chargeStationId string, request *store.FirmwareUpdateRequest) error {
	var retrieveDate pgtype.Timestamptz
	if request.RetrieveDate != nil {
		retrieveDate = pgtype.Timestamptz{Time: *request.RetrieveDate, Valid: true}
	}

	var retries pgtype.Int4
	if request.Retries != nil {
		retries = pgtype.Int4{Int32: int32(*request.Retries), Valid: true}
	}

	var retryInterval pgtype.Int4
	if request.RetryInterval != nil {
		retryInterval = pgtype.Int4{Int32: int32(*request.RetryInterval), Valid: true}
	}

	var signature pgtype.Text
	if request.Signature != nil {
		signature = pgtype.Text{String: *request.Signature, Valid: true}
	}

	var signingCertificate pgtype.Text
	if request.SigningCertificate != nil {
		signingCertificate = pgtype.Text{String: *request.SigningCertificate, Valid: true}
	}

	err := s.q.UpsertFirmwareUpdateRequest(ctx, UpsertFirmwareUpdateRequestParams{
		ChargeStationID:    chargeStationId,
		Location:           request.Location,
		RetrieveDate:       retrieveDate,
		Retries:            retries,
		RetryInterval:      retryInterval,
		Signature:          signature,
		SigningCertificate: signingCertificate,
		Status:             string(request.Status),
		SendAfter:          pgtype.Timestamptz{Time: request.SendAfter, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to set firmware update request: %w", err)
	}
	return nil
}

func (s *Store) GetFirmwareUpdateRequest(ctx context.Context, chargeStationId string) (*store.FirmwareUpdateRequest, error) {
	row, err := s.q.GetFirmwareUpdateRequest(ctx, chargeStationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get firmware update request: %w", err)
	}

	result := &store.FirmwareUpdateRequest{
		ChargeStationId: row.ChargeStationID,
		Location:        row.Location,
		Status:          store.FirmwareUpdateRequestStatus(row.Status),
		SendAfter:       row.SendAfter.Time,
	}

	if row.RetrieveDate.Valid {
		result.RetrieveDate = &row.RetrieveDate.Time
	}

	if row.Retries.Valid {
		retries := int(row.Retries.Int32)
		result.Retries = &retries
	}

	if row.RetryInterval.Valid {
		retryInterval := int(row.RetryInterval.Int32)
		result.RetryInterval = &retryInterval
	}

	if row.Signature.Valid {
		result.Signature = &row.Signature.String
	}

	if row.SigningCertificate.Valid {
		result.SigningCertificate = &row.SigningCertificate.String
	}

	return result, nil
}

func (s *Store) DeleteFirmwareUpdateRequest(ctx context.Context, chargeStationId string) error {
	err := s.q.DeleteFirmwareUpdateRequest(ctx, chargeStationId)
	if err != nil {
		return fmt.Errorf("failed to delete firmware update request: %w", err)
	}
	return nil
}

func (s *Store) ListFirmwareUpdateRequests(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.FirmwareUpdateRequest, error) {
	rows, err := s.q.ListFirmwareUpdateRequests(ctx, ListFirmwareUpdateRequestsParams{
		ChargeStationID: previousChargeStationId,
		Limit:           int32(pageSize),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list firmware update requests: %w", err)
	}

	results := make([]*store.FirmwareUpdateRequest, 0, len(rows))
	for _, row := range rows {
		result := &store.FirmwareUpdateRequest{
			ChargeStationId: row.ChargeStationID,
			Location:        row.Location,
			Status:          store.FirmwareUpdateRequestStatus(row.Status),
			SendAfter:       row.SendAfter.Time,
		}

		if row.RetrieveDate.Valid {
			result.RetrieveDate = &row.RetrieveDate.Time
		}

		if row.Retries.Valid {
			retries := int(row.Retries.Int32)
			result.Retries = &retries
		}

		if row.RetryInterval.Valid {
			retryInterval := int(row.RetryInterval.Int32)
			result.RetryInterval = &retryInterval
		}

		if row.Signature.Valid {
			result.Signature = &row.Signature.String
		}

		if row.SigningCertificate.Valid {
			result.SigningCertificate = &row.SigningCertificate.String
		}

		results = append(results, result)
	}

	return results, nil
}
