// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
)

func TestTriggerMessageResultHandler_HandleCallResult(t *testing.T) {
	handler := ocpp16.TriggerMessageResultHandler{}
	ctx := context.Background()
	chargeStationId := "cs001"

	tests := []struct {
		name          string
		request       ocpp.Request
		response      ocpp.Response
		expectedError error
	}{
		{
			name: "accepted trigger for BootNotification",
			request: &types.TriggerMessageJson{
				RequestedMessage: types.TriggerMessageJsonRequestedMessageBootNotification,
			},
			response: &types.TriggerMessageResponseJson{
				Status: types.TriggerMessageResponseJsonStatusAccepted,
			},
			expectedError: nil,
		},
		{
			name: "accepted trigger for StatusNotification with connector",
			request: &types.TriggerMessageJson{
				RequestedMessage: types.TriggerMessageJsonRequestedMessageStatusNotification,
				ConnectorId:      intPtr(1),
			},
			response: &types.TriggerMessageResponseJson{
				Status: types.TriggerMessageResponseJsonStatusAccepted,
			},
			expectedError: nil,
		},
		{
			name: "accepted trigger for MeterValues",
			request: &types.TriggerMessageJson{
				RequestedMessage: types.TriggerMessageJsonRequestedMessageMeterValues,
				ConnectorId:      intPtr(2),
			},
			response: &types.TriggerMessageResponseJson{
				Status: types.TriggerMessageResponseJsonStatusAccepted,
			},
			expectedError: nil,
		},
		{
			name: "accepted trigger for Heartbeat",
			request: &types.TriggerMessageJson{
				RequestedMessage: types.TriggerMessageJsonRequestedMessageHeartbeat,
			},
			response: &types.TriggerMessageResponseJson{
				Status: types.TriggerMessageResponseJsonStatusAccepted,
			},
			expectedError: nil,
		},
		{
			name: "accepted trigger for FirmwareStatusNotification",
			request: &types.TriggerMessageJson{
				RequestedMessage: types.TriggerMessageJsonRequestedMessageFirmwareStatusNotification,
			},
			response: &types.TriggerMessageResponseJson{
				Status: types.TriggerMessageResponseJsonStatusAccepted,
			},
			expectedError: nil,
		},
		{
			name: "accepted trigger for DiagnosticsStatusNotification",
			request: &types.TriggerMessageJson{
				RequestedMessage: types.TriggerMessageJsonRequestedMessageDiagnosticsStatusNotification,
			},
			response: &types.TriggerMessageResponseJson{
				Status: types.TriggerMessageResponseJsonStatusAccepted,
			},
			expectedError: nil,
		},
		{
			name: "rejected trigger - message not supported",
			request: &types.TriggerMessageJson{
				RequestedMessage: types.TriggerMessageJsonRequestedMessageBootNotification,
			},
			response: &types.TriggerMessageResponseJson{
				Status: types.TriggerMessageResponseJsonStatusRejected,
			},
			expectedError: nil,
		},
		{
			name: "not implemented trigger",
			request: &types.TriggerMessageJson{
				RequestedMessage: types.TriggerMessageJsonRequestedMessageDiagnosticsStatusNotification,
			},
			response: &types.TriggerMessageResponseJson{
				Status: types.TriggerMessageResponseJsonStatusNotImplemented,
			},
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handler.HandleCallResult(ctx, chargeStationId, tt.request, tt.response, nil)

			if tt.expectedError != nil {
				require.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTriggerMessageResultHandler_ValidRequestTypes(t *testing.T) {
	handler := ocpp16.TriggerMessageResultHandler{}
	ctx := context.Background()

	// Test all valid OCPP 1.6 trigger message types
	validMessageTypes := []types.TriggerMessageJsonRequestedMessage{
		types.TriggerMessageJsonRequestedMessageBootNotification,
		types.TriggerMessageJsonRequestedMessageDiagnosticsStatusNotification,
		types.TriggerMessageJsonRequestedMessageFirmwareStatusNotification,
		types.TriggerMessageJsonRequestedMessageHeartbeat,
		types.TriggerMessageJsonRequestedMessageMeterValues,
		types.TriggerMessageJsonRequestedMessageStatusNotification,
	}

	for _, msgType := range validMessageTypes {
		t.Run(string(msgType), func(t *testing.T) {
			request := &types.TriggerMessageJson{
				RequestedMessage: msgType,
			}
			response := &types.TriggerMessageResponseJson{
				Status: types.TriggerMessageResponseJsonStatusAccepted,
			}

			err := handler.HandleCallResult(ctx, "cs001", request, response, nil)
			require.NoError(t, err)
		})
	}
}

func intPtr(i int) *int {
	return &i
}
