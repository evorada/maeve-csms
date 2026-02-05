// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
)

func TestUnlockConnectorHandlerUnlocked(t *testing.T) {
	handler := handlers.UnlockConnectorHandler{}

	request := &types.UnlockConnectorJson{
		ConnectorId: 1,
	}

	response := &types.UnlockConnectorResponseJson{
		Status: types.UnlockConnectorResponseJsonStatusUnlocked,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)
}

func TestUnlockConnectorHandlerUnlockFailed(t *testing.T) {
	handler := handlers.UnlockConnectorHandler{}

	request := &types.UnlockConnectorJson{
		ConnectorId: 2,
	}

	response := &types.UnlockConnectorResponseJson{
		Status: types.UnlockConnectorResponseJsonStatusUnlockFailed,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)
}

func TestUnlockConnectorHandlerNotSupported(t *testing.T) {
	handler := handlers.UnlockConnectorHandler{}

	request := &types.UnlockConnectorJson{
		ConnectorId: 1,
	}

	response := &types.UnlockConnectorResponseJson{
		Status: types.UnlockConnectorResponseJsonStatusNotSupported,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)
}

func TestUnlockConnectorHandlerConnectorZero(t *testing.T) {
	handler := handlers.UnlockConnectorHandler{}

	request := &types.UnlockConnectorJson{
		ConnectorId: 0,
	}

	response := &types.UnlockConnectorResponseJson{
		Status: types.UnlockConnectorResponseJsonStatusUnlocked,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)
}
