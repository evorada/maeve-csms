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
	ReservationStatusUsed        ReservationStatus = "Used" // consumed by the start of a transaction
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
	// OrgId is the org (UUID) that owns the reservation. It is read-only: the store
	// takes it from the charge station when the reservation is created (LegacyOrgId
	// when the charge station is not registered). Firestore does not store it: it
	// always reads as "".
	OrgId string `firestore:"-" json:"-"`
}

type ReservationStore interface {
	CreateReservation(ctx context.Context, reservation *Reservation) error
	GetReservation(ctx context.Context, reservationId int) (*Reservation, error)
	CancelReservation(ctx context.Context, reservationId int) error
	UpdateReservationStatus(ctx context.Context, reservationId int, status ReservationStatus) error
	GetActiveReservations(ctx context.Context, chargeStationId string) ([]*Reservation, error)
	GetReservationByConnector(ctx context.Context, chargeStationId string, connectorId int) (*Reservation, error)
	ExpireReservations(ctx context.Context) (int, error)
}
