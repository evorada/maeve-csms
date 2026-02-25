// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// ResetRequestStore implementation

func (s *Store) SetResetRequest(ctx context.Context, chargeStationId string, request *store.ResetRequest) error {
	params := SetResetRequestParams{
		ChargeStationID: chargeStationId,
		Type:            string(request.Type),
		Status:          string(request.Status),
		CreatedAt:       pgtype.Timestamptz{Time: request.CreatedAt, Valid: !request.CreatedAt.IsZero()},
		UpdatedAt:       pgtype.Timestamptz{Time: request.UpdatedAt, Valid: !request.UpdatedAt.IsZero()},
	}

	_, err := s.q.SetResetRequest(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to set reset request: %w", err)
	}

	return nil
}

func (s *Store) GetResetRequest(ctx context.Context, chargeStationId string) (*store.ResetRequest, error) {
	req, err := s.q.GetResetRequest(ctx, chargeStationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get reset request: %w", err)
	}

	return &store.ResetRequest{
		ChargeStationId: req.ChargeStationID,
		Type:            store.ResetType(req.Type),
		Status:          store.ResetRequestStatus(req.Status),
		CreatedAt:       req.CreatedAt.Time,
		UpdatedAt:       req.UpdatedAt.Time,
	}, nil
}

func (s *Store) DeleteResetRequest(ctx context.Context, chargeStationId string) error {
	err := s.q.DeleteResetRequest(ctx, chargeStationId)
	if err != nil {
		return fmt.Errorf("failed to delete reset request: %w", err)
	}
	return nil
}

// UnlockConnectorRequestStore implementation

func (s *Store) SetUnlockConnectorRequest(ctx context.Context, chargeStationId string, request *store.UnlockConnectorRequest) error {
	params := SetUnlockConnectorRequestParams{
		ChargeStationID: chargeStationId,
		ConnectorID:     int32(request.ConnectorId),
		Status:          string(request.Status),
		CreatedAt:       pgtype.Timestamptz{Time: request.CreatedAt, Valid: !request.CreatedAt.IsZero()},
		UpdatedAt:       pgtype.Timestamptz{Time: request.UpdatedAt, Valid: !request.UpdatedAt.IsZero()},
	}

	_, err := s.q.SetUnlockConnectorRequest(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to set unlock connector request: %w", err)
	}

	return nil
}

func (s *Store) GetUnlockConnectorRequest(ctx context.Context, chargeStationId string) (*store.UnlockConnectorRequest, error) {
	req, err := s.q.GetUnlockConnectorRequest(ctx, chargeStationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get unlock connector request: %w", err)
	}

	return &store.UnlockConnectorRequest{
		ChargeStationId: req.ChargeStationID,
		ConnectorId:     int(req.ConnectorID),
		Status:          store.UnlockConnectorRequestStatus(req.Status),
		CreatedAt:       req.CreatedAt.Time,
		UpdatedAt:       req.UpdatedAt.Time,
	}, nil
}

func (s *Store) DeleteUnlockConnectorRequest(ctx context.Context, chargeStationId string) error {
	err := s.q.DeleteUnlockConnectorRequest(ctx, chargeStationId)
	if err != nil {
		return fmt.Errorf("failed to delete unlock connector request: %w", err)
	}
	return nil
}
