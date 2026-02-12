// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"time"
)

type ReservationStatus string

const (
	ReservationStatusAccepted    ReservationStatus = "Accepted"
	ReservationStatusFaulted     ReservationStatus = "Faulted"
	ReservationStatusOccupied    ReservationStatus = "Occupied"
	ReservationStatusRejected    ReservationStatus = "Rejected"
	ReservationStatusUnavailable ReservationStatus = "Unavailable"
	ReservationStatusCancelled   ReservationStatus = "Cancelled"
	ReservationStatusExpired     ReservationStatus = "Expired"
)

type Reservation struct {
	ReservationId   int               `firestore:"reservationId" json:"reservation_id"`
	ChargeStationId string            `firestore:"chargeStationId" json:"charge_station_id"`
	ConnectorId     int               `firestore:"connectorId" json:"connector_id"`
	IdTag           string            `firestore:"idTag" json:"id_tag"`
	ParentIdTag     *string           `firestore:"parentIdTag" json:"parent_id_tag"`
	ExpiryDate      time.Time         `firestore:"expiryDate" json:"expiry_date"`
	Status          ReservationStatus `firestore:"status" json:"status"`
	CreatedAt       time.Time         `firestore:"createdAt" json:"created_at"`
}

type ReservationStore interface {
	CreateReservation(ctx context.Context, reservation *Reservation) error
	GetReservation(ctx context.Context, reservationId int) (*Reservation, error)
	CancelReservation(ctx context.Context, reservationId int) error
	GetActiveReservations(ctx context.Context, chargeStationId string) ([]*Reservation, error)
	GetReservationByConnector(ctx context.Context, chargeStationId string, connectorId int) (*Reservation, error)
	ExpireReservations(ctx context.Context) (int, error)
}
