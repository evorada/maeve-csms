// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Store) CreateReservation(ctx context.Context, reservation *store.Reservation) error {
	params := CreateReservationParams{
		ReservationID:   int32(reservation.ReservationId),
		ChargeStationID: reservation.ChargeStationId,
		ConnectorID:     int32(reservation.ConnectorId),
		IDTag:           reservation.IdTag,
		ExpiryDate:      pgtype.Timestamptz{Time: reservation.ExpiryDate, Valid: true},
		Status:          string(reservation.Status),
		CreatedAt:       pgtype.Timestamptz{Time: reservation.CreatedAt, Valid: true},
	}
	if reservation.ParentIdTag != nil {
		params.ParentIDTag = pgtype.Text{String: *reservation.ParentIdTag, Valid: true}
	}

	err := s.writeQueries().CreateReservation(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to create reservation: %w", err)
	}
	return nil
}

func (s *Store) GetReservation(ctx context.Context, reservationId int) (*store.Reservation, error) {
	r, err := s.readQueries().GetReservation(ctx, int32(reservationId))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get reservation: %w", err)
	}
	return toStoreReservation(&r), nil
}

func (s *Store) CancelReservation(ctx context.Context, reservationId int) error {
	err := s.writeQueries().CancelReservation(ctx, int32(reservationId))
	if err != nil {
		return fmt.Errorf("failed to cancel reservation: %w", err)
	}
	return nil
}

func (s *Store) UpdateReservationStatus(ctx context.Context, reservationId int, status store.ReservationStatus) error {
	err := s.writeQueries().UpdateReservationStatus(ctx, UpdateReservationStatusParams{
		ReservationID: int32(reservationId),
		Status:        string(status),
	})
	if err != nil {
		return fmt.Errorf("failed to update reservation status: %w", err)
	}
	return nil
}

func (s *Store) GetActiveReservations(ctx context.Context, chargeStationId string) ([]*store.Reservation, error) {
	rows, err := s.readQueries().GetActiveReservations(ctx, chargeStationId)
	if err != nil {
		return nil, fmt.Errorf("failed to get active reservations: %w", err)
	}
	result := make([]*store.Reservation, len(rows))
	for i := range rows {
		result[i] = toStoreReservation(&rows[i])
	}
	return result, nil
}

func (s *Store) GetReservationByConnector(ctx context.Context, chargeStationId string, connectorId int) (*store.Reservation, error) {
	r, err := s.readQueries().GetReservationByConnector(ctx, GetReservationByConnectorParams{
		ChargeStationID: chargeStationId,
		ConnectorID:     int32(connectorId),
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get reservation by connector: %w", err)
	}
	return toStoreReservation(&r), nil
}

func (s *Store) ExpireReservations(ctx context.Context) (int, error) {
	count, err := s.writeQueries().ExpireReservations(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to expire reservations: %w", err)
	}
	return int(count), nil
}

func toStoreReservation(r *Reservation) *store.Reservation {
	res := &store.Reservation{
		ReservationId:   int(r.ReservationID),
		ChargeStationId: r.ChargeStationID,
		ConnectorId:     int(r.ConnectorID),
		IdTag:           r.IDTag,
		ExpiryDate:      r.ExpiryDate.Time,
		Status:          store.ReservationStatus(r.Status),
		CreatedAt:       r.CreatedAt.Time,
	}
	if r.ParentIDTag.Valid {
		res.ParentIdTag = &r.ParentIDTag.String
	}
	return res
}
