// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	clock "k8s.io/utils/clock"
)

func TestReserveNowHandler_Accepted(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := handlers.ReserveNowHandler{
		ReservationStore: memStore,
	}
	ctx := context.Background()
	chargeStationId := "cs001"

	req := &types.ReserveNowJson{
		ConnectorId:   1,
		ExpiryDate:    "2026-02-13T10:00:00Z",
		IdTag:         "TAG001",
		ReservationId: 42,
	}
	resp := &types.ReserveNowResponseJson{
		Status: types.ReserveNowResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(ctx, chargeStationId, req, resp, nil)
	require.NoError(t, err)

	// Verify reservation was stored
	reservation, err := memStore.GetReservation(ctx, 42)
	require.NoError(t, err)
	require.NotNil(t, reservation)
	assert.Equal(t, 42, reservation.ReservationId)
	assert.Equal(t, chargeStationId, reservation.ChargeStationId)
	assert.Equal(t, 1, reservation.ConnectorId)
	assert.Equal(t, "TAG001", reservation.IdTag)
	assert.Equal(t, store.ReservationStatusAccepted, reservation.Status)
	assert.Nil(t, reservation.ParentIdTag)
}

func TestReserveNowHandler_AcceptedWithParentIdTag(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := handlers.ReserveNowHandler{
		ReservationStore: memStore,
	}
	ctx := context.Background()

	parentTag := "PARENT001"
	req := &types.ReserveNowJson{
		ConnectorId:   2,
		ExpiryDate:    "2026-02-13T12:00:00Z",
		IdTag:         "TAG002",
		ParentIdTag:   &parentTag,
		ReservationId: 43,
	}
	resp := &types.ReserveNowResponseJson{
		Status: types.ReserveNowResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(ctx, "cs002", req, resp, nil)
	require.NoError(t, err)

	reservation, err := memStore.GetReservation(ctx, 43)
	require.NoError(t, err)
	require.NotNil(t, reservation)
	assert.Equal(t, &parentTag, reservation.ParentIdTag)
}

func TestReserveNowHandler_Faulted(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := handlers.ReserveNowHandler{
		ReservationStore: memStore,
	}

	req := &types.ReserveNowJson{
		ConnectorId:   1,
		ExpiryDate:    "2026-02-13T10:00:00Z",
		IdTag:         "TAG001",
		ReservationId: 44,
	}
	resp := &types.ReserveNowResponseJson{
		Status: types.ReserveNowResponseJsonStatusFaulted,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", req, resp, nil)
	require.NoError(t, err)

	// Verify reservation was NOT stored
	reservation, err := memStore.GetReservation(context.Background(), 44)
	require.NoError(t, err)
	assert.Nil(t, reservation)
}

func TestReserveNowHandler_Occupied(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := handlers.ReserveNowHandler{
		ReservationStore: memStore,
	}

	req := &types.ReserveNowJson{
		ConnectorId:   1,
		ExpiryDate:    "2026-02-13T10:00:00Z",
		IdTag:         "TAG001",
		ReservationId: 45,
	}
	resp := &types.ReserveNowResponseJson{
		Status: types.ReserveNowResponseJsonStatusOccupied,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", req, resp, nil)
	require.NoError(t, err)

	reservation, err := memStore.GetReservation(context.Background(), 45)
	require.NoError(t, err)
	assert.Nil(t, reservation)
}

func TestReserveNowHandler_Rejected(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := handlers.ReserveNowHandler{
		ReservationStore: memStore,
	}

	req := &types.ReserveNowJson{
		ConnectorId:   1,
		ExpiryDate:    "2026-02-13T10:00:00Z",
		IdTag:         "TAG001",
		ReservationId: 46,
	}
	resp := &types.ReserveNowResponseJson{
		Status: types.ReserveNowResponseJsonStatusRejected,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", req, resp, nil)
	require.NoError(t, err)

	reservation, err := memStore.GetReservation(context.Background(), 46)
	require.NoError(t, err)
	assert.Nil(t, reservation)
}

func TestReserveNowHandler_Unavailable(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := handlers.ReserveNowHandler{
		ReservationStore: memStore,
	}

	req := &types.ReserveNowJson{
		ConnectorId:   1,
		ExpiryDate:    "2026-02-13T10:00:00Z",
		IdTag:         "TAG001",
		ReservationId: 47,
	}
	resp := &types.ReserveNowResponseJson{
		Status: types.ReserveNowResponseJsonStatusUnavailable,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", req, resp, nil)
	require.NoError(t, err)

	reservation, err := memStore.GetReservation(context.Background(), 47)
	require.NoError(t, err)
	assert.Nil(t, reservation)
}

func TestReserveNowHandler_InvalidExpiryDate(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := handlers.ReserveNowHandler{
		ReservationStore: memStore,
	}

	req := &types.ReserveNowJson{
		ConnectorId:   1,
		ExpiryDate:    "not-a-date",
		IdTag:         "TAG001",
		ReservationId: 48,
	}
	resp := &types.ReserveNowResponseJson{
		Status: types.ReserveNowResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", req, resp, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing expiry date")
}

func TestReserveNowHandler_AllRejectionStatuses(t *testing.T) {
	rejectionStatuses := []struct {
		name   string
		status types.ReserveNowResponseJsonStatus
	}{
		{"Faulted", types.ReserveNowResponseJsonStatusFaulted},
		{"Occupied", types.ReserveNowResponseJsonStatusOccupied},
		{"Rejected", types.ReserveNowResponseJsonStatusRejected},
		{"Unavailable", types.ReserveNowResponseJsonStatusUnavailable},
	}

	for _, tt := range rejectionStatuses {
		t.Run(tt.name, func(t *testing.T) {
			memStore := inmemory.NewStore(clock.RealClock{})
			handler := handlers.ReserveNowHandler{
				ReservationStore: memStore,
			}

			req := &types.ReserveNowJson{
				ConnectorId:   1,
				ExpiryDate:    "2026-02-13T10:00:00Z",
				IdTag:         "TAG001",
				ReservationId: 99,
			}
			resp := &types.ReserveNowResponseJson{
				Status: tt.status,
			}

			err := handler.HandleCallResult(context.Background(), "cs001", req, resp, nil)
			require.NoError(t, err)
		})
	}
}

func TestReserveNowHandler_ConnectorZero(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := handlers.ReserveNowHandler{
		ReservationStore: memStore,
	}

	// ConnectorId 0 means the whole charge point
	req := &types.ReserveNowJson{
		ConnectorId:   0,
		ExpiryDate:    "2026-02-13T10:00:00Z",
		IdTag:         "TAG001",
		ReservationId: 50,
	}
	resp := &types.ReserveNowResponseJson{
		Status: types.ReserveNowResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", req, resp, nil)
	require.NoError(t, err)

	reservation, err := memStore.GetReservation(context.Background(), 50)
	require.NoError(t, err)
	assert.Equal(t, 0, reservation.ConnectorId)
}
