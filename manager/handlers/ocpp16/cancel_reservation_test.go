// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	clock "k8s.io/utils/clock"
)

func createTestReservation(t *testing.T, memStore *inmemory.Store, reservationId int, chargeStationId string) {
	t.Helper()
	err := memStore.CreateReservation(context.Background(), &store.Reservation{
		ReservationId:   reservationId,
		ChargeStationId: chargeStationId,
		ConnectorId:     1,
		IdTag:           "TAG001",
		ExpiryDate:      time.Now().Add(1 * time.Hour),
		Status:          store.ReservationStatusAccepted,
		CreatedAt:       time.Now(),
	})
	require.NoError(t, err)
}

func TestCancelReservationHandler_Accepted(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := handlers.CancelReservationHandler{
		ReservationStore: memStore,
	}
	ctx := context.Background()
	chargeStationId := "cs001"

	// Create a reservation first
	createTestReservation(t, memStore, 42, chargeStationId)

	req := &types.CancelReservationJson{
		ReservationId: 42,
	}
	resp := &types.CancelReservationResponseJson{
		Status: types.CancelReservationResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(ctx, chargeStationId, req, resp, nil)
	require.NoError(t, err)

	// Verify reservation was cancelled in the store
	reservation, err := memStore.GetReservation(ctx, 42)
	require.NoError(t, err)
	require.NotNil(t, reservation)
	assert.Equal(t, store.ReservationStatusCancelled, reservation.Status)
}

func TestCancelReservationHandler_Rejected(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := handlers.CancelReservationHandler{
		ReservationStore: memStore,
	}
	ctx := context.Background()
	chargeStationId := "cs001"

	// Create a reservation first
	createTestReservation(t, memStore, 43, chargeStationId)

	req := &types.CancelReservationJson{
		ReservationId: 43,
	}
	resp := &types.CancelReservationResponseJson{
		Status: types.CancelReservationResponseJsonStatusRejected,
	}

	err := handler.HandleCallResult(ctx, chargeStationId, req, resp, nil)
	require.NoError(t, err)

	// Verify reservation was NOT cancelled (still Accepted)
	reservation, err := memStore.GetReservation(ctx, 43)
	require.NoError(t, err)
	require.NotNil(t, reservation)
	assert.Equal(t, store.ReservationStatusAccepted, reservation.Status)
}

func TestCancelReservationHandler_AcceptedNonExistentReservation(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := handlers.CancelReservationHandler{
		ReservationStore: memStore,
	}
	ctx := context.Background()

	req := &types.CancelReservationJson{
		ReservationId: 999,
	}
	resp := &types.CancelReservationResponseJson{
		Status: types.CancelReservationResponseJsonStatusAccepted,
	}

	// CancelReservation on a non-existent reservation should return an error
	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancelling reservation 999")
}

func TestCancelReservationHandler_MultipleReservations(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := handlers.CancelReservationHandler{
		ReservationStore: memStore,
	}
	ctx := context.Background()
	chargeStationId := "cs001"

	// Create two reservations
	createTestReservation(t, memStore, 50, chargeStationId)
	createTestReservation(t, memStore, 51, chargeStationId)

	// Cancel only reservation 50
	req := &types.CancelReservationJson{
		ReservationId: 50,
	}
	resp := &types.CancelReservationResponseJson{
		Status: types.CancelReservationResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(ctx, chargeStationId, req, resp, nil)
	require.NoError(t, err)

	// Reservation 50 should be cancelled
	r50, err := memStore.GetReservation(ctx, 50)
	require.NoError(t, err)
	assert.Equal(t, store.ReservationStatusCancelled, r50.Status)

	// Reservation 51 should still be accepted
	r51, err := memStore.GetReservation(ctx, 51)
	require.NoError(t, err)
	assert.Equal(t, store.ReservationStatusAccepted, r51.Status)
}
