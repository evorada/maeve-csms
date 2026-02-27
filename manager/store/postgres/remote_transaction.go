// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Store) SetRemoteStartTransactionRequest(ctx context.Context, chargeStationId string, request *store.RemoteStartTransactionRequest) error {
	_, err := s.writeQueries().SetRemoteStartTransactionRequest(ctx, SetRemoteStartTransactionRequestParams{
		ChargeStationID: chargeStationId,
		IDTag:           request.IdTag,
		ConnectorID:     toNullInt32(request.ConnectorId),
		ChargingProfile: textFromString(request.ChargingProfile),
		Status:          string(request.Status),
		SendAfter:       toPgTimestamp(request.SendAfter),
		RequestType:     string(request.RequestType),
	})
	if err != nil {
		return fmt.Errorf("failed to set remote start request: %w", err)
	}
	return nil
}

func (s *Store) GetRemoteStartTransactionRequest(ctx context.Context, chargeStationId string) (*store.RemoteStartTransactionRequest, error) {
	row, err := s.readQueries().GetRemoteStartTransactionRequest(ctx, chargeStationId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get remote start request: %w", err)
	}
	return &store.RemoteStartTransactionRequest{
		ChargeStationId: row.ChargeStationID,
		IdTag:           row.IDTag,
		ConnectorId:     fromNullableInt32(row.ConnectorID),
		ChargingProfile: stringFromText(row.ChargingProfile),
		Status:          store.RemoteTransactionRequestStatus(row.Status),
		SendAfter:       fromPgTimestamp(row.SendAfter),
		RequestType:     store.RemoteTransactionRequestType(row.RequestType),
	}, nil
}

func (s *Store) DeleteRemoteStartTransactionRequest(ctx context.Context, chargeStationId string) error {
	return s.writeQueries().DeleteRemoteStartTransactionRequest(ctx, chargeStationId)
}

func (s *Store) ListRemoteStartTransactionRequests(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.RemoteStartTransactionRequest, error) {
	rows, err := s.readQueries().ListRemoteStartTransactionRequests(ctx, ListRemoteStartTransactionRequestsParams{
		ChargeStationID: previousChargeStationId,
		Limit:           int32(pageSize),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list remote start requests: %w", err)
	}
	var result []*store.RemoteStartTransactionRequest
	for _, row := range rows {
		result = append(result, &store.RemoteStartTransactionRequest{
			ChargeStationId: row.ChargeStationID,
			IdTag:           row.IDTag,
			ConnectorId:     fromNullableInt32(row.ConnectorID),
			ChargingProfile: stringFromText(row.ChargingProfile),
			Status:          store.RemoteTransactionRequestStatus(row.Status),
			SendAfter:       fromPgTimestamp(row.SendAfter),
			RequestType:     store.RemoteTransactionRequestType(row.RequestType),
		})
	}
	return result, nil
}

func (s *Store) SetRemoteStopTransactionRequest(ctx context.Context, chargeStationId string, request *store.RemoteStopTransactionRequest) error {
	_, err := s.writeQueries().SetRemoteStopTransactionRequest(ctx, SetRemoteStopTransactionRequestParams{
		ChargeStationID: chargeStationId,
		TransactionID:   request.TransactionId,
		Status:          string(request.Status),
		SendAfter:       toPgTimestamp(request.SendAfter),
		RequestType:     string(request.RequestType),
	})
	if err != nil {
		return fmt.Errorf("failed to set remote stop request: %w", err)
	}
	return nil
}

func (s *Store) GetRemoteStopTransactionRequest(ctx context.Context, chargeStationId string) (*store.RemoteStopTransactionRequest, error) {
	row, err := s.readQueries().GetRemoteStopTransactionRequest(ctx, chargeStationId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get remote stop request: %w", err)
	}
	return &store.RemoteStopTransactionRequest{
		ChargeStationId: row.ChargeStationID,
		TransactionId:   row.TransactionID,
		Status:          store.RemoteTransactionRequestStatus(row.Status),
		SendAfter:       fromPgTimestamp(row.SendAfter),
		RequestType:     store.RemoteTransactionRequestType(row.RequestType),
	}, nil
}

func (s *Store) DeleteRemoteStopTransactionRequest(ctx context.Context, chargeStationId string) error {
	return s.writeQueries().DeleteRemoteStopTransactionRequest(ctx, chargeStationId)
}

func (s *Store) ListRemoteStopTransactionRequests(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.RemoteStopTransactionRequest, error) {
	rows, err := s.readQueries().ListRemoteStopTransactionRequests(ctx, ListRemoteStopTransactionRequestsParams{
		ChargeStationID: previousChargeStationId,
		Limit:           int32(pageSize),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list remote stop requests: %w", err)
	}
	var result []*store.RemoteStopTransactionRequest
	for _, row := range rows {
		result = append(result, &store.RemoteStopTransactionRequest{
			ChargeStationId: row.ChargeStationID,
			TransactionId:   row.TransactionID,
			Status:          store.RemoteTransactionRequestStatus(row.Status),
			SendAfter:       fromPgTimestamp(row.SendAfter),
			RequestType:     store.RemoteTransactionRequestType(row.RequestType),
		})
	}
	return result, nil
}
