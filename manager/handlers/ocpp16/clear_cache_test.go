// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
)

func TestClearCacheHandlerAccepted(t *testing.T) {
	handler := handlers.ClearCacheHandler{}

	request := &types.ClearCacheJson{}

	response := &types.ClearCacheResponseJson{
		Status: types.ClearCacheResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)
}

func TestClearCacheHandlerRejected(t *testing.T) {
	handler := handlers.ClearCacheHandler{}

	request := &types.ClearCacheJson{}

	response := &types.ClearCacheResponseJson{
		Status: types.ClearCacheResponseJsonStatusRejected,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)
}
