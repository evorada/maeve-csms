// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	clockutil "k8s.io/utils/clock"
)

func TestReservationStatusUpdateHandler_Expired(t *testing.T) {
	engine := inmemory.NewStore(clockutil.RealClock{})
	handler := ocpp201.ReservationStatusUpdateHandler{ReservationStore: engine}

	err := engine.CreateReservation(context.Background(), &store.Reservation{
		ReservationId:   100,
		ChargeStationId: "cs001",
		ConnectorId:     1,
		IdTag:           "TAG-1",
		ExpiryDate:      time.Now().Add(10 * time.Minute),
		Status:          store.ReservationStatusAccepted,
		CreatedAt:       time.Now(),
	})
	require.NoError(t, err)

	resp, err := handler.HandleCall(context.Background(), "cs001", &types.ReservationStatusUpdateRequestJson{
		ReservationId:           100,
		ReservationUpdateStatus: types.ReservationUpdateStatusEnumTypeExpired,
	})
	require.NoError(t, err)
	assert.Equal(t, &types.ReservationStatusUpdateResponseJson{}, resp)

	reservation, err := engine.GetReservation(context.Background(), 100)
	require.NoError(t, err)
	require.NotNil(t, reservation)
	assert.Equal(t, store.ReservationStatusExpired, reservation.Status)
}

func TestReservationStatusUpdateHandler_RemovedMapsToCancelled(t *testing.T) {
	engine := inmemory.NewStore(clockutil.RealClock{})
	handler := ocpp201.ReservationStatusUpdateHandler{ReservationStore: engine}

	err := engine.CreateReservation(context.Background(), &store.Reservation{
		ReservationId:   101,
		ChargeStationId: "cs001",
		ConnectorId:     1,
		IdTag:           "TAG-2",
		ExpiryDate:      time.Now().Add(10 * time.Minute),
		Status:          store.ReservationStatusAccepted,
		CreatedAt:       time.Now(),
	})
	require.NoError(t, err)

	_, err = handler.HandleCall(context.Background(), "cs001", &types.ReservationStatusUpdateRequestJson{
		ReservationId:           101,
		ReservationUpdateStatus: types.ReservationUpdateStatusEnumTypeRemoved,
	})
	require.NoError(t, err)

	reservation, err := engine.GetReservation(context.Background(), 101)
	require.NoError(t, err)
	require.NotNil(t, reservation)
	assert.Equal(t, store.ReservationStatusCancelled, reservation.Status)
}
