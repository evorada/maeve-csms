// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func reservationKey(reservationId int) string {
	return fmt.Sprintf("Reservation/%d", reservationId)
}

func (s *Store) CreateReservation(ctx context.Context, reservation *store.Reservation) error {
	ref := s.client.Doc(reservationKey(reservation.ReservationId))
	_, err := ref.Set(ctx, reservation)
	if err != nil {
		return fmt.Errorf("create reservation %d: %w", reservation.ReservationId, err)
	}
	return nil
}

func (s *Store) GetReservation(ctx context.Context, reservationId int) (*store.Reservation, error) {
	ref := s.client.Doc(reservationKey(reservationId))
	snap, err := ref.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get reservation %d: %w", reservationId, err)
	}
	var r store.Reservation
	if err := snap.DataTo(&r); err != nil {
		return nil, fmt.Errorf("map reservation %d: %w", reservationId, err)
	}
	return &r, nil
}

func (s *Store) CancelReservation(ctx context.Context, reservationId int) error {
	ref := s.client.Doc(reservationKey(reservationId))
	_, err := ref.Update(ctx, []firestore.Update{
		{Path: "status", Value: string(store.ReservationStatusCancelled)},
	})
	if err != nil {
		return fmt.Errorf("cancel reservation %d: %w", reservationId, err)
	}
	return nil
}

func (s *Store) GetActiveReservations(ctx context.Context, chargeStationId string) ([]*store.Reservation, error) {
	iter := s.client.Collection("Reservation").
		Where("chargeStationId", "==", chargeStationId).
		Where("status", "==", string(store.ReservationStatusAccepted)).
		Documents(ctx)
	defer iter.Stop()

	var result []*store.Reservation
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("iterate active reservations: %w", err)
		}
		var r store.Reservation
		if err := snap.DataTo(&r); err != nil {
			return nil, fmt.Errorf("map reservation: %w", err)
		}
		result = append(result, &r)
	}
	if result == nil {
		result = make([]*store.Reservation, 0)
	}
	return result, nil
}

func (s *Store) GetReservationByConnector(ctx context.Context, chargeStationId string, connectorId int) (*store.Reservation, error) {
	iter := s.client.Collection("Reservation").
		Where("chargeStationId", "==", chargeStationId).
		Where("connectorId", "==", connectorId).
		Where("status", "==", string(store.ReservationStatusAccepted)).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	snap, err := iter.Next()
	if err == iterator.Done {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get reservation by connector: %w", err)
	}
	var r store.Reservation
	if err := snap.DataTo(&r); err != nil {
		return nil, fmt.Errorf("map reservation: %w", err)
	}
	return &r, nil
}

func (s *Store) ExpireReservations(ctx context.Context) (int, error) {
	now := s.clock.Now()
	iter := s.client.Collection("Reservation").
		Where("status", "==", string(store.ReservationStatusAccepted)).
		Where("expiryDate", "<", now).
		Documents(ctx)
	defer iter.Stop()

	count := 0
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return count, fmt.Errorf("iterate expired reservations: %w", err)
		}
		_, err = snap.Ref.Update(ctx, []firestore.Update{
			{Path: "status", Value: string(store.ReservationStatusExpired)},
		})
		if err != nil {
			return count, fmt.Errorf("expire reservation: %w", err)
		}
		count++
	}
	return count, nil
}
