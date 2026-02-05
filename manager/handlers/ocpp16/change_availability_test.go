// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
)

func TestChangeAvailabilityHandlerAcceptedInoperative(t *testing.T) {
	handler := handlers.ChangeAvailabilityHandler{}

	request := &types.ChangeAvailabilityJson{
		ConnectorId: 1,
		Type:        types.ChangeAvailabilityJsonTypeInoperative,
	}

	response := &types.ChangeAvailabilityResponseJson{
		Status: types.ChangeAvailabilityResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)
}

func TestChangeAvailabilityHandlerAcceptedOperative(t *testing.T) {
	handler := handlers.ChangeAvailabilityHandler{}

	request := &types.ChangeAvailabilityJson{
		ConnectorId: 1,
		Type:        types.ChangeAvailabilityJsonTypeOperative,
	}

	response := &types.ChangeAvailabilityResponseJson{
		Status: types.ChangeAvailabilityResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)
}

func TestChangeAvailabilityHandlerScheduled(t *testing.T) {
	handler := handlers.ChangeAvailabilityHandler{}

	request := &types.ChangeAvailabilityJson{
		ConnectorId: 0,
		Type:        types.ChangeAvailabilityJsonTypeInoperative,
	}

	response := &types.ChangeAvailabilityResponseJson{
		Status: types.ChangeAvailabilityResponseJsonStatusScheduled,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)
}

func TestChangeAvailabilityHandlerRejected(t *testing.T) {
	handler := handlers.ChangeAvailabilityHandler{}

	request := &types.ChangeAvailabilityJson{
		ConnectorId: 2,
		Type:        types.ChangeAvailabilityJsonTypeOperative,
	}

	response := &types.ChangeAvailabilityResponseJson{
		Status: types.ChangeAvailabilityResponseJsonStatusRejected,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)
}

func TestChangeAvailabilityHandlerAllConnectors(t *testing.T) {
	handler := handlers.ChangeAvailabilityHandler{}

	request := &types.ChangeAvailabilityJson{
		ConnectorId: 0, // 0 means all connectors
		Type:        types.ChangeAvailabilityJsonTypeInoperative,
	}

	response := &types.ChangeAvailabilityResponseJson{
		Status: types.ChangeAvailabilityResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)
}
