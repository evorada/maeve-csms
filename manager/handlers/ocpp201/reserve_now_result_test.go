// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"k8s.io/utils/clock"
)

func TestReserveNowResultHandler_Accepted(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.ReserveNowResultHandler{ReservationStore: memStore}

	evseId := 2
	groupIdToken := types.IdTokenType{IdToken: "GROUP-123", Type: types.IdTokenEnumTypeLocal}
	req := &types.ReserveNowRequestJson{
		Id:             123,
		ExpiryDateTime: "2026-02-20T10:00:00Z",
		IdToken:        types.IdTokenType{IdToken: "TAG-42", Type: types.IdTokenEnumTypeISO14443},
		EvseId:         &evseId,
		GroupIdToken:   &groupIdToken,
	}
	resp := &types.ReserveNowResponseJson{Status: types.ReserveNowStatusEnumTypeAccepted}

	err := handler.HandleCallResult(context.Background(), "cs001", req, resp, nil)
	require.NoError(t, err)

	reservation, err := memStore.GetReservation(context.Background(), 123)
	require.NoError(t, err)
	require.NotNil(t, reservation)
	assert.Equal(t, "cs001", reservation.ChargeStationId)
	assert.Equal(t, 2, reservation.ConnectorId)
	assert.Equal(t, "TAG-42", reservation.IdTag)
	require.NotNil(t, reservation.ParentIdTag)
	assert.Equal(t, "GROUP-123", *reservation.ParentIdTag)
	assert.Equal(t, store.ReservationStatusAccepted, reservation.Status)
}

func TestReserveNowResultHandler_Rejected(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.ReserveNowResultHandler{ReservationStore: memStore}

	req := &types.ReserveNowRequestJson{
		Id:             124,
		ExpiryDateTime: "2026-02-20T10:00:00Z",
		IdToken:        types.IdTokenType{IdToken: "TAG-99", Type: types.IdTokenEnumTypeISO14443},
	}
	resp := &types.ReserveNowResponseJson{Status: types.ReserveNowStatusEnumTypeRejected}

	err := handler.HandleCallResult(context.Background(), "cs001", req, resp, nil)
	require.NoError(t, err)

	reservation, err := memStore.GetReservation(context.Background(), 124)
	require.NoError(t, err)
	assert.Nil(t, reservation)
}
