// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"k8s.io/utils/clock"
)

// newPendingTrigger stores the pending trigger message that the sync loop sent as request.
func newPendingTrigger(t *testing.T, engine store.Engine, chargeStationId string, request *types.TriggerMessageJson) {
	t.Helper()
	err := engine.SetChargeStationTriggerMessage(context.Background(), chargeStationId, &store.ChargeStationTriggerMessage{
		ChargeStationId: chargeStationId,
		TriggerMessage:  store.TriggerMessage(request.RequestedMessage),
		ConnectorId:     request.ConnectorId,
		TriggerStatus:   store.TriggerStatusPending,
	})
	require.NoError(t, err)
}

func TestTriggerMessageResultHandler_HandleCallResult(t *testing.T) {
	ctx := context.Background()
	chargeStationId := "cs001"

	tests := []struct {
		name    string
		request *types.TriggerMessageJson
		status  types.TriggerMessageResponseJsonStatus
		// status of the trigger message in the store afterwards; empty when it must be deleted
		wantStatus store.TriggerStatus
	}{
		{
			name: "accepted trigger for BootNotification",
			request: &types.TriggerMessageJson{
				RequestedMessage: types.TriggerMessageJsonRequestedMessageBootNotification,
			},
			status: types.TriggerMessageResponseJsonStatusAccepted,
		},
		{
			name: "accepted trigger for StatusNotification with connector",
			request: &types.TriggerMessageJson{
				RequestedMessage: types.TriggerMessageJsonRequestedMessageStatusNotification,
				ConnectorId:      intPtr(1),
			},
			status: types.TriggerMessageResponseJsonStatusAccepted,
		},
		{
			name: "accepted trigger for MeterValues",
			request: &types.TriggerMessageJson{
				RequestedMessage: types.TriggerMessageJsonRequestedMessageMeterValues,
				ConnectorId:      intPtr(2),
			},
			status: types.TriggerMessageResponseJsonStatusAccepted,
		},
		{
			name: "accepted trigger for Heartbeat",
			request: &types.TriggerMessageJson{
				RequestedMessage: types.TriggerMessageJsonRequestedMessageHeartbeat,
			},
			status: types.TriggerMessageResponseJsonStatusAccepted,
		},
		{
			name: "accepted trigger for FirmwareStatusNotification",
			request: &types.TriggerMessageJson{
				RequestedMessage: types.TriggerMessageJsonRequestedMessageFirmwareStatusNotification,
			},
			status: types.TriggerMessageResponseJsonStatusAccepted,
		},
		{
			name: "accepted trigger for DiagnosticsStatusNotification",
			request: &types.TriggerMessageJson{
				RequestedMessage: types.TriggerMessageJsonRequestedMessageDiagnosticsStatusNotification,
			},
			status: types.TriggerMessageResponseJsonStatusAccepted,
		},
		{
			name: "rejected trigger",
			request: &types.TriggerMessageJson{
				RequestedMessage: types.TriggerMessageJsonRequestedMessageBootNotification,
			},
			status:     types.TriggerMessageResponseJsonStatusRejected,
			wantStatus: store.TriggerStatusRejected,
		},
		{
			name: "rejected trigger with connector",
			request: &types.TriggerMessageJson{
				RequestedMessage: types.TriggerMessageJsonRequestedMessageMeterValues,
				ConnectorId:      intPtr(2),
			},
			status:     types.TriggerMessageResponseJsonStatusRejected,
			wantStatus: store.TriggerStatusRejected,
		},
		{
			name: "not implemented trigger",
			request: &types.TriggerMessageJson{
				RequestedMessage: types.TriggerMessageJsonRequestedMessageDiagnosticsStatusNotification,
			},
			status:     types.TriggerMessageResponseJsonStatusNotImplemented,
			wantStatus: store.TriggerStatusNotImplemented,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := inmemory.NewStore(clock.RealClock{})
			handler := ocpp16.TriggerMessageResultHandler{Store: engine}
			newPendingTrigger(t, engine, chargeStationId, tt.request)

			err := handler.HandleCallResult(ctx, chargeStationId, tt.request, &types.TriggerMessageResponseJson{Status: tt.status}, nil)
			require.NoError(t, err)

			got, err := engine.LookupChargeStationTriggerMessage(ctx, chargeStationId)
			require.NoError(t, err)
			if tt.wantStatus == "" {
				assert.Nil(t, got, "an accepted trigger message must be deleted")
				return
			}
			assert.Equal(t, &store.ChargeStationTriggerMessage{
				ChargeStationId: chargeStationId,
				TriggerMessage:  store.TriggerMessage(tt.request.RequestedMessage),
				ConnectorId:     tt.request.ConnectorId,
				TriggerStatus:   tt.wantStatus,
			}, got)
		})
	}
}

func TestTriggerMessageResultHandler_ValidRequestTypes(t *testing.T) {
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
			handler := ocpp16.TriggerMessageResultHandler{Store: inmemory.NewStore(clock.RealClock{})}
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

func TestTriggerMessageResultHandler_HandleCallError(t *testing.T) {
	ctx := context.Background()
	chargeStationId := "cs001"

	tests := []struct {
		errorCode  transport.ErrorCode
		wantStatus store.TriggerStatus
	}{
		{errorCode: transport.ErrorNotSupported, wantStatus: store.TriggerStatusNotImplemented},
		{errorCode: transport.ErrorNotImplemented, wantStatus: store.TriggerStatusNotImplemented},
		{errorCode: transport.ErrorInternalError, wantStatus: store.TriggerStatusRejected},
		{errorCode: transport.ErrorPropertyConstraintViolation, wantStatus: store.TriggerStatusRejected},
	}

	for _, tt := range tests {
		t.Run(string(tt.errorCode), func(t *testing.T) {
			engine := inmemory.NewStore(clock.RealClock{})
			handler := ocpp16.TriggerMessageResultHandler{Store: engine}
			request := &types.TriggerMessageJson{
				RequestedMessage: types.TriggerMessageJsonRequestedMessageStatusNotification,
				ConnectorId:      intPtr(1),
			}
			newPendingTrigger(t, engine, chargeStationId, request)

			err := handler.HandleCallError(ctx, chargeStationId, request, &handlers.CallError{
				MessageId:        "msg-001",
				Action:           "TriggerMessage",
				ErrorCode:        tt.errorCode,
				ErrorDescription: "refused",
			}, nil)
			require.NoError(t, err)

			got, err := engine.LookupChargeStationTriggerMessage(ctx, chargeStationId)
			require.NoError(t, err)
			assert.Equal(t, &store.ChargeStationTriggerMessage{
				ChargeStationId: chargeStationId,
				TriggerMessage:  store.TriggerMessageStatusNotification,
				ConnectorId:     intPtr(1),
				TriggerStatus:   tt.wantStatus,
			}, got)
		})
	}
}

func TestTriggerMessageResultHandler_HandleCallErrorWithoutRequest(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp16.TriggerMessageResultHandler{Store: engine}
	pending := &types.TriggerMessageJson{
		RequestedMessage: types.TriggerMessageJsonRequestedMessageHeartbeat,
	}
	newPendingTrigger(t, engine, "cs001", pending)

	// the router passes a nil request when the payload of the original call cannot be decoded
	err := handler.HandleCallError(ctx, "cs001", nil, &handlers.CallError{
		MessageId:        "msg-001",
		Action:           "TriggerMessage",
		ErrorCode:        transport.ErrorInternalError,
		ErrorDescription: "refused",
	}, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "TriggerMessage msg-001: InternalError: refused")

	got, err := engine.LookupChargeStationTriggerMessage(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, store.TriggerStatusPending, got.TriggerStatus)
}

func intPtr(i int) *int {
	return &i
}
