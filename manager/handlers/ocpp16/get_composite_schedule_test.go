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

func TestGetCompositeScheduleHandler_HandleCallResult(t *testing.T) {
	handler := ocpp16.GetCompositeScheduleHandler{}
	ctx := context.Background()
	chargeStationId := "cs001"

	chargingRateUnitW := types.GetCompositeScheduleJsonChargingRateUnitW
	chargingRateUnitA := types.GetCompositeScheduleJsonChargingRateUnitA

	tests := []struct {
		name     string
		request  ocpp.Request
		response ocpp.Response
	}{
		{
			name: "accepted with schedule in watts",
			request: &types.GetCompositeScheduleJson{
				ConnectorId:      1,
				Duration:         3600,
				ChargingRateUnit: &chargingRateUnitW,
			},
			response: &types.GetCompositeScheduleResponseJson{
				Status:      types.GetCompositeScheduleResponseJsonStatusAccepted,
				ConnectorId: intPtr(1),
				ChargingSchedule: &types.GetCompositeScheduleResponseJsonChargingSchedule{
					ChargingRateUnit: types.GetCompositeScheduleResponseJsonChargingRateUnitW,
					ChargingSchedulePeriod: []types.GetCompositeScheduleResponseJsonChargingSchedulePeriod{
						{StartPeriod: 0, Limit: 11000.0},
						{StartPeriod: 1800, Limit: 7400.0, NumberPhases: intPtr(3)},
					},
				},
			},
		},
		{
			name: "accepted with schedule in amps",
			request: &types.GetCompositeScheduleJson{
				ConnectorId:      0,
				Duration:         7200,
				ChargingRateUnit: &chargingRateUnitA,
			},
			response: &types.GetCompositeScheduleResponseJson{
				Status:      types.GetCompositeScheduleResponseJsonStatusAccepted,
				ConnectorId: intPtr(0),
				ChargingSchedule: &types.GetCompositeScheduleResponseJsonChargingSchedule{
					ChargingRateUnit: types.GetCompositeScheduleResponseJsonChargingRateUnitA,
					ChargingSchedulePeriod: []types.GetCompositeScheduleResponseJsonChargingSchedulePeriod{
						{StartPeriod: 0, Limit: 32.0, NumberPhases: intPtr(3)},
					},
				},
			},
		},
		{
			name: "accepted without charging rate unit in request",
			request: &types.GetCompositeScheduleJson{
				ConnectorId: 1,
				Duration:    3600,
			},
			response: &types.GetCompositeScheduleResponseJson{
				Status:      types.GetCompositeScheduleResponseJsonStatusAccepted,
				ConnectorId: intPtr(1),
				ChargingSchedule: &types.GetCompositeScheduleResponseJsonChargingSchedule{
					ChargingRateUnit: types.GetCompositeScheduleResponseJsonChargingRateUnitW,
					ChargingSchedulePeriod: []types.GetCompositeScheduleResponseJsonChargingSchedulePeriod{
						{StartPeriod: 0, Limit: 22000.0},
					},
				},
			},
		},
		{
			name: "accepted with schedule start and duration",
			request: &types.GetCompositeScheduleJson{
				ConnectorId: 1,
				Duration:    3600,
			},
			response: &types.GetCompositeScheduleResponseJson{
				Status:        types.GetCompositeScheduleResponseJsonStatusAccepted,
				ConnectorId:   intPtr(1),
				ScheduleStart: strPtr("2026-02-12T00:00:00Z"),
				ChargingSchedule: &types.GetCompositeScheduleResponseJsonChargingSchedule{
					Duration:         intPtr(3600),
					StartSchedule:    strPtr("2026-02-12T00:00:00Z"),
					ChargingRateUnit: types.GetCompositeScheduleResponseJsonChargingRateUnitW,
					ChargingSchedulePeriod: []types.GetCompositeScheduleResponseJsonChargingSchedulePeriod{
						{StartPeriod: 0, Limit: 11000.0},
					},
					MinChargingRate: float64Ptr(6.0),
				},
			},
		},
		{
			name: "accepted with empty schedule periods",
			request: &types.GetCompositeScheduleJson{
				ConnectorId: 1,
				Duration:    3600,
			},
			response: &types.GetCompositeScheduleResponseJson{
				Status:      types.GetCompositeScheduleResponseJsonStatusAccepted,
				ConnectorId: intPtr(1),
				ChargingSchedule: &types.GetCompositeScheduleResponseJsonChargingSchedule{
					ChargingRateUnit:       types.GetCompositeScheduleResponseJsonChargingRateUnitW,
					ChargingSchedulePeriod: []types.GetCompositeScheduleResponseJsonChargingSchedulePeriod{},
				},
			},
		},
		{
			name: "rejected",
			request: &types.GetCompositeScheduleJson{
				ConnectorId: 1,
				Duration:    3600,
			},
			response: &types.GetCompositeScheduleResponseJson{
				Status: types.GetCompositeScheduleResponseJsonStatusRejected,
			},
		},
		{
			name: "accepted without schedule body",
			request: &types.GetCompositeScheduleJson{
				ConnectorId: 1,
				Duration:    3600,
			},
			response: &types.GetCompositeScheduleResponseJson{
				Status: types.GetCompositeScheduleResponseJsonStatusAccepted,
			},
		},
		{
			name: "connector 0 (whole charge point)",
			request: &types.GetCompositeScheduleJson{
				ConnectorId: 0,
				Duration:    1800,
			},
			response: &types.GetCompositeScheduleResponseJson{
				Status:      types.GetCompositeScheduleResponseJsonStatusAccepted,
				ConnectorId: intPtr(0),
				ChargingSchedule: &types.GetCompositeScheduleResponseJsonChargingSchedule{
					ChargingRateUnit: types.GetCompositeScheduleResponseJsonChargingRateUnitA,
					ChargingSchedulePeriod: []types.GetCompositeScheduleResponseJsonChargingSchedulePeriod{
						{StartPeriod: 0, Limit: 16.0},
					},
				},
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

func strPtr(s string) *string {
	return &s
}

func float64Ptr(f float64) *float64 {
	return &f
}
