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

func TestExtendedTriggerMessageResultHandler_HandleCallResult(t *testing.T) {
	handler := ocpp16.ExtendedTriggerMessageResultHandler{}
	ctx := context.Background()
	chargeStationId := "cs001"

	tests := []struct {
		name     string
		request  ocpp.Request
		response ocpp.Response
	}{
		{
			name: "accepted trigger for BootNotification",
			request: &types.ExtendedTriggerMessageJson{
				RequestedMessage: types.ExtendedTriggerMessageJsonRequestedMessageBootNotification,
			},
			response: &types.ExtendedTriggerMessageResponseJson{
				Status: types.ExtendedTriggerMessageResponseJsonStatusAccepted,
			},
		},
		{
			name: "accepted trigger for SignChargePointCertificate",
			request: &types.ExtendedTriggerMessageJson{
				RequestedMessage: types.ExtendedTriggerMessageJsonRequestedMessageSignChargePointCertificate,
			},
			response: &types.ExtendedTriggerMessageResponseJson{
				Status: types.ExtendedTriggerMessageResponseJsonStatusAccepted,
			},
		},
		{
			name: "accepted trigger for LogStatusNotification",
			request: &types.ExtendedTriggerMessageJson{
				RequestedMessage: types.ExtendedTriggerMessageJsonRequestedMessageLogStatusNotification,
			},
			response: &types.ExtendedTriggerMessageResponseJson{
				Status: types.ExtendedTriggerMessageResponseJsonStatusAccepted,
			},
		},
		{
			name: "accepted trigger for StatusNotification with connector",
			request: &types.ExtendedTriggerMessageJson{
				RequestedMessage: types.ExtendedTriggerMessageJsonRequestedMessageStatusNotification,
				ConnectorId:      intPtr(1),
			},
			response: &types.ExtendedTriggerMessageResponseJson{
				Status: types.ExtendedTriggerMessageResponseJsonStatusAccepted,
			},
		},
		{
			name: "accepted trigger for MeterValues with connector",
			request: &types.ExtendedTriggerMessageJson{
				RequestedMessage: types.ExtendedTriggerMessageJsonRequestedMessageMeterValues,
				ConnectorId:      intPtr(2),
			},
			response: &types.ExtendedTriggerMessageResponseJson{
				Status: types.ExtendedTriggerMessageResponseJsonStatusAccepted,
			},
		},
		{
			name: "accepted trigger for Heartbeat",
			request: &types.ExtendedTriggerMessageJson{
				RequestedMessage: types.ExtendedTriggerMessageJsonRequestedMessageHeartbeat,
			},
			response: &types.ExtendedTriggerMessageResponseJson{
				Status: types.ExtendedTriggerMessageResponseJsonStatusAccepted,
			},
		},
		{
			name: "accepted trigger for FirmwareStatusNotification",
			request: &types.ExtendedTriggerMessageJson{
				RequestedMessage: types.ExtendedTriggerMessageJsonRequestedMessageFirmwareStatusNotification,
			},
			response: &types.ExtendedTriggerMessageResponseJson{
				Status: types.ExtendedTriggerMessageResponseJsonStatusAccepted,
			},
		},
		{
			name: "rejected trigger",
			request: &types.ExtendedTriggerMessageJson{
				RequestedMessage: types.ExtendedTriggerMessageJsonRequestedMessageBootNotification,
			},
			response: &types.ExtendedTriggerMessageResponseJson{
				Status: types.ExtendedTriggerMessageResponseJsonStatusRejected,
			},
		},
		{
			name: "not implemented trigger",
			request: &types.ExtendedTriggerMessageJson{
				RequestedMessage: types.ExtendedTriggerMessageJsonRequestedMessageSignChargePointCertificate,
			},
			response: &types.ExtendedTriggerMessageResponseJson{
				Status: types.ExtendedTriggerMessageResponseJsonStatusNotImplemented,
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

func TestExtendedTriggerMessageResultHandler_AllMessageTypes(t *testing.T) {
	handler := ocpp16.ExtendedTriggerMessageResultHandler{}
	ctx := context.Background()

	validMessageTypes := []types.ExtendedTriggerMessageJsonRequestedMessage{
		types.ExtendedTriggerMessageJsonRequestedMessageBootNotification,
		types.ExtendedTriggerMessageJsonRequestedMessageLogStatusNotification,
		types.ExtendedTriggerMessageJsonRequestedMessageFirmwareStatusNotification,
		types.ExtendedTriggerMessageJsonRequestedMessageHeartbeat,
		types.ExtendedTriggerMessageJsonRequestedMessageMeterValues,
		types.ExtendedTriggerMessageJsonRequestedMessageSignChargePointCertificate,
		types.ExtendedTriggerMessageJsonRequestedMessageStatusNotification,
	}

	for _, msgType := range validMessageTypes {
		t.Run(string(msgType), func(t *testing.T) {
			request := &types.ExtendedTriggerMessageJson{
				RequestedMessage: msgType,
			}
			response := &types.ExtendedTriggerMessageResponseJson{
				Status: types.ExtendedTriggerMessageResponseJsonStatusAccepted,
			}

			err := handler.HandleCallResult(ctx, "cs001", request, response, nil)
			require.NoError(t, err)
		})
	}
}
