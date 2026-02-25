// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// DiagnosticsRequest store methods
func (s *Store) SetDiagnosticsRequest(ctx context.Context, chargeStationId string, request *store.DiagnosticsRequest) error {
	var startTime pgtype.Timestamptz
	if request.StartTime != nil {
		startTime = pgtype.Timestamptz{Time: *request.StartTime, Valid: true}
	}

	var stopTime pgtype.Timestamptz
	if request.StopTime != nil {
		stopTime = pgtype.Timestamptz{Time: *request.StopTime, Valid: true}
	}

	var retries pgtype.Int4
	if request.Retries != nil {
		retries = pgtype.Int4{Int32: int32(*request.Retries), Valid: true}
	}

	var retryInterval pgtype.Int4
	if request.RetryInterval != nil {
		retryInterval = pgtype.Int4{Int32: int32(*request.RetryInterval), Valid: true}
	}

	err := s.q.UpsertDiagnosticsRequest(ctx, UpsertDiagnosticsRequestParams{
		ChargeStationID: chargeStationId,
		Location:        request.Location,
		StartTime:       startTime,
		StopTime:        stopTime,
		Retries:         retries,
		RetryInterval:   retryInterval,
		Status:          string(request.Status),
		SendAfter:       pgtype.Timestamptz{Time: request.SendAfter, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to set diagnostics request: %w", err)
	}
	return nil
}

func (s *Store) GetDiagnosticsRequest(ctx context.Context, chargeStationId string) (*store.DiagnosticsRequest, error) {
	row, err := s.q.GetDiagnosticsRequest(ctx, chargeStationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get diagnostics request: %w", err)
	}

	result := &store.DiagnosticsRequest{
		ChargeStationId: row.ChargeStationID,
		Location:        row.Location,
		Status:          store.DiagnosticsRequestStatus(row.Status),
		SendAfter:       row.SendAfter.Time,
	}

	if row.StartTime.Valid {
		result.StartTime = &row.StartTime.Time
	}

	if row.StopTime.Valid {
		result.StopTime = &row.StopTime.Time
	}

	if row.Retries.Valid {
		retries := int(row.Retries.Int32)
		result.Retries = &retries
	}

	if row.RetryInterval.Valid {
		retryInterval := int(row.RetryInterval.Int32)
		result.RetryInterval = &retryInterval
	}

	return result, nil
}

func (s *Store) DeleteDiagnosticsRequest(ctx context.Context, chargeStationId string) error {
	err := s.q.DeleteDiagnosticsRequest(ctx, chargeStationId)
	if err != nil {
		return fmt.Errorf("failed to delete diagnostics request: %w", err)
	}
	return nil
}

func (s *Store) ListDiagnosticsRequests(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.DiagnosticsRequest, error) {
	rows, err := s.q.ListDiagnosticsRequests(ctx, ListDiagnosticsRequestsParams{
		ChargeStationID: previousChargeStationId,
		Limit:           int32(pageSize),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list diagnostics requests: %w", err)
	}

	results := make([]*store.DiagnosticsRequest, 0, len(rows))
	for _, row := range rows {
		result := &store.DiagnosticsRequest{
			ChargeStationId: row.ChargeStationID,
			Location:        row.Location,
			Status:          store.DiagnosticsRequestStatus(row.Status),
			SendAfter:       row.SendAfter.Time,
		}

		if row.StartTime.Valid {
			result.StartTime = &row.StartTime.Time
		}

		if row.StopTime.Valid {
			result.StopTime = &row.StopTime.Time
		}

		if row.Retries.Valid {
			retries := int(row.Retries.Int32)
			result.Retries = &retries
		}

		if row.RetryInterval.Valid {
			retryInterval := int(row.RetryInterval.Int32)
			result.RetryInterval = &retryInterval
		}

		results = append(results, result)
	}

	return results, nil
}

// LogRequest store methods
func (s *Store) SetLogRequest(ctx context.Context, chargeStationId string, request *store.LogRequest) error {
	var oldestTimestamp pgtype.Timestamptz
	if request.OldestTimestamp != nil {
		oldestTimestamp = pgtype.Timestamptz{Time: *request.OldestTimestamp, Valid: true}
	}

	var latestTimestamp pgtype.Timestamptz
	if request.LatestTimestamp != nil {
		latestTimestamp = pgtype.Timestamptz{Time: *request.LatestTimestamp, Valid: true}
	}

	var retries pgtype.Int4
	if request.Retries != nil {
		retries = pgtype.Int4{Int32: int32(*request.Retries), Valid: true}
	}

	var retryInterval pgtype.Int4
	if request.RetryInterval != nil {
		retryInterval = pgtype.Int4{Int32: int32(*request.RetryInterval), Valid: true}
	}

	err := s.q.UpsertLogRequest(ctx, UpsertLogRequestParams{
		ChargeStationID: chargeStationId,
		LogType:         request.LogType,
		RequestID:       int32(request.RequestId),
		RemoteLocation:  request.RemoteLocation,
		OldestTimestamp: oldestTimestamp,
		LatestTimestamp: latestTimestamp,
		Retries:         retries,
		RetryInterval:   retryInterval,
		Status:          string(request.Status),
		SendAfter:       pgtype.Timestamptz{Time: request.SendAfter, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to set log request: %w", err)
	}
	return nil
}

func (s *Store) GetLogRequest(ctx context.Context, chargeStationId string) (*store.LogRequest, error) {
	row, err := s.q.GetLogRequest(ctx, chargeStationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get log request: %w", err)
	}

	result := &store.LogRequest{
		ChargeStationId: row.ChargeStationID,
		LogType:         row.LogType,
		RequestId:       int(row.RequestID),
		RemoteLocation:  row.RemoteLocation,
		Status:          store.LogRequestStatus(row.Status),
		SendAfter:       row.SendAfter.Time,
	}

	if row.OldestTimestamp.Valid {
		result.OldestTimestamp = &row.OldestTimestamp.Time
	}

	if row.LatestTimestamp.Valid {
		result.LatestTimestamp = &row.LatestTimestamp.Time
	}

	if row.Retries.Valid {
		retries := int(row.Retries.Int32)
		result.Retries = &retries
	}

	if row.RetryInterval.Valid {
		retryInterval := int(row.RetryInterval.Int32)
		result.RetryInterval = &retryInterval
	}

	return result, nil
}

func (s *Store) DeleteLogRequest(ctx context.Context, chargeStationId string) error {
	err := s.q.DeleteLogRequest(ctx, chargeStationId)
	if err != nil {
		return fmt.Errorf("failed to delete log request: %w", err)
	}
	return nil
}

func (s *Store) ListLogRequests(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.LogRequest, error) {
	rows, err := s.q.ListLogRequests(ctx, ListLogRequestsParams{
		ChargeStationID: previousChargeStationId,
		Limit:           int32(pageSize),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list log requests: %w", err)
	}

	results := make([]*store.LogRequest, 0, len(rows))
	for _, row := range rows {
		result := &store.LogRequest{
			ChargeStationId: row.ChargeStationID,
			LogType:         row.LogType,
			RequestId:       int(row.RequestID),
			RemoteLocation:  row.RemoteLocation,
			Status:          store.LogRequestStatus(row.Status),
			SendAfter:       row.SendAfter.Time,
		}

		if row.OldestTimestamp.Valid {
			result.OldestTimestamp = &row.OldestTimestamp.Time
		}

		if row.LatestTimestamp.Valid {
			result.LatestTimestamp = &row.LatestTimestamp.Time
		}

		if row.Retries.Valid {
			retries := int(row.Retries.Int32)
			result.Retries = &retries
		}

		if row.RetryInterval.Valid {
			retryInterval := int(row.RetryInterval.Int32)
			result.RetryInterval = &retryInterval
		}

		results = append(results, result)
	}

	return results, nil
}
