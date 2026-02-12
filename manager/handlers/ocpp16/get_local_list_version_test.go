// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
)

func TestGetLocalListVersionHandler_HandleCallResult(t *testing.T) {
	handler := ocpp16.GetLocalListVersionHandler{}
	ctx := context.Background()
	chargeStationId := "cs001"

	tests := []struct {
		name     string
		request  ocpp.Request
		response ocpp.Response
	}{
		{
			name:    "version zero (no list set)",
			request: &types.GetLocalListVersionJson{},
			response: &types.GetLocalListVersionResponseJson{
				ListVersion: 0,
			},
		},
		{
			name:    "positive version",
			request: &types.GetLocalListVersionJson{},
			response: &types.GetLocalListVersionResponseJson{
				ListVersion: 5,
			},
		},
		{
			name:    "negative version indicates no list support",
			request: &types.GetLocalListVersionJson{},
			response: &types.GetLocalListVersionResponseJson{
				ListVersion: -1,
			},
		},
		{
			name:    "large version number",
			request: &types.GetLocalListVersionJson{},
			response: &types.GetLocalListVersionResponseJson{
				ListVersion: 99999,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handler.HandleCallResult(ctx, chargeStationId, tt.request, tt.response, nil)
			require.NoError(t, err)
		})
	}
}
