// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	clock "k8s.io/utils/clock"
)

func createTestReservation201(t *testing.T, memStore *inmemory.Store, reservationId int, chargeStationId string) {
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

func TestCancelReservationResultHandler_Accepted(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := handlers.CancelReservationResultHandler{ReservationStore: memStore}
	ctx := context.Background()
	chargeStationId := "cs001"

	createTestReservation201(t, memStore, 42, chargeStationId)

	req := &types.CancelReservationRequestJson{ReservationId: 42}
	resp := &types.CancelReservationResponseJson{Status: types.CancelReservationStatusEnumTypeAccepted}

	err := handler.HandleCallResult(ctx, chargeStationId, req, resp, nil)
	require.NoError(t, err)

	reservation, err := memStore.GetReservation(ctx, 42)
	require.NoError(t, err)
	require.NotNil(t, reservation)
	assert.Equal(t, store.ReservationStatusCancelled, reservation.Status)
}

func TestCancelReservationResultHandler_Rejected(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := handlers.CancelReservationResultHandler{ReservationStore: memStore}
	ctx := context.Background()
	chargeStationId := "cs001"

	createTestReservation201(t, memStore, 43, chargeStationId)

	req := &types.CancelReservationRequestJson{ReservationId: 43}
	resp := &types.CancelReservationResponseJson{Status: types.CancelReservationStatusEnumTypeRejected}

	err := handler.HandleCallResult(ctx, chargeStationId, req, resp, nil)
	require.NoError(t, err)

	reservation, err := memStore.GetReservation(ctx, 43)
	require.NoError(t, err)
	require.NotNil(t, reservation)
	assert.Equal(t, store.ReservationStatusAccepted, reservation.Status)
}

func TestCancelReservationResultHandler_AcceptedNonExistentReservation(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := handlers.CancelReservationResultHandler{ReservationStore: memStore}

	req := &types.CancelReservationRequestJson{ReservationId: 999}
	resp := &types.CancelReservationResponseJson{Status: types.CancelReservationStatusEnumTypeAccepted}

	err := handler.HandleCallResult(context.Background(), "cs001", req, resp, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancelling reservation 999")
}
