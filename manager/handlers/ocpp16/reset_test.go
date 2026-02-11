// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
)

func TestResetHandlerAcceptedSoftReset(t *testing.T) {
	handler := handlers.ResetHandler{}

	request := &types.ResetJson{
		Type: types.ResetJsonTypeSoft,
	}

	response := &types.ResetResponseJson{
		Status: types.ResetResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)
}

func TestResetHandlerAcceptedHardReset(t *testing.T) {
	handler := handlers.ResetHandler{}

	request := &types.ResetJson{
		Type: types.ResetJsonTypeHard,
	}

	response := &types.ResetResponseJson{
		Status: types.ResetResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)
}

func TestResetHandlerRejectedSoftReset(t *testing.T) {
	handler := handlers.ResetHandler{}

	request := &types.ResetJson{
		Type: types.ResetJsonTypeSoft,
	}

	response := &types.ResetResponseJson{
		Status: types.ResetResponseJsonStatusRejected,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)
}

func TestResetHandlerRejectedHardReset(t *testing.T) {
	handler := handlers.ResetHandler{}

	request := &types.ResetJson{
		Type: types.ResetJsonTypeHard,
	}

	response := &types.ResetResponseJson{
		Status: types.ResetResponseJsonStatusRejected,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)
}
