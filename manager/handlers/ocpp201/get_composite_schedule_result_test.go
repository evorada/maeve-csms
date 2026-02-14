// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
)

func TestGetCompositeScheduleResultHandler_AcceptedWithSchedule(t *testing.T) {
	ctx := context.Background()
	handler := ocpp201.GetCompositeScheduleResultHandler{}

	unitW := types.ChargingRateUnitEnumTypeW
	req := &types.GetCompositeScheduleRequestJson{
		EvseId:           1,
		Duration:         3600,
		ChargingRateUnit: &unitW,
	}
	resp := &types.GetCompositeScheduleResponseJson{
		Status: types.GenericStatusEnumTypeAccepted,
		Schedule: &types.CompositeScheduleType{
			EvseId:           1,
			Duration:         3600,
			ScheduleStart:    "2026-02-14T23:00:00Z",
			ChargingRateUnit: types.ChargingRateUnitEnumTypeW,
			ChargingSchedulePeriod: []types.ChargingSchedulePeriodType{
				{StartPeriod: 0, Limit: 11000.0},
				{StartPeriod: 1800, Limit: 7400.0},
			},
		},
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	assert.NoError(t, err)
}

func TestGetCompositeScheduleResultHandler_AcceptedInAmps(t *testing.T) {
	ctx := context.Background()
	handler := ocpp201.GetCompositeScheduleResultHandler{}

	unitA := types.ChargingRateUnitEnumTypeA
	req := &types.GetCompositeScheduleRequestJson{
		EvseId:           0,
		Duration:         7200,
		ChargingRateUnit: &unitA,
	}
	resp := &types.GetCompositeScheduleResponseJson{
		Status: types.GenericStatusEnumTypeAccepted,
		Schedule: &types.CompositeScheduleType{
			EvseId:           0,
			Duration:         7200,
			ScheduleStart:    "2026-02-14T23:00:00Z",
			ChargingRateUnit: types.ChargingRateUnitEnumTypeA,
			ChargingSchedulePeriod: []types.ChargingSchedulePeriodType{
				{StartPeriod: 0, Limit: 32.0},
			},
		},
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	assert.NoError(t, err)
}

func TestGetCompositeScheduleResultHandler_AcceptedNoSchedule(t *testing.T) {
	// The CS accepted but returned no schedule body (non-conformant but we handle gracefully)
	ctx := context.Background()
	handler := ocpp201.GetCompositeScheduleResultHandler{}

	req := &types.GetCompositeScheduleRequestJson{
		EvseId:   1,
		Duration: 3600,
	}
	resp := &types.GetCompositeScheduleResponseJson{
		Status: types.GenericStatusEnumTypeAccepted,
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	assert.NoError(t, err)
}

func TestGetCompositeScheduleResultHandler_Rejected(t *testing.T) {
	ctx := context.Background()
	handler := ocpp201.GetCompositeScheduleResultHandler{}

	req := &types.GetCompositeScheduleRequestJson{
		EvseId:   1,
		Duration: 3600,
	}
	resp := &types.GetCompositeScheduleResponseJson{
		Status: types.GenericStatusEnumTypeRejected,
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	assert.NoError(t, err)
}

func TestGetCompositeScheduleResultHandler_RejectedWithStatusInfo(t *testing.T) {
	ctx := context.Background()
	handler := ocpp201.GetCompositeScheduleResultHandler{}

	additionalInfo := "EVSE not found"
	req := &types.GetCompositeScheduleRequestJson{
		EvseId:   5,
		Duration: 3600,
	}
	resp := &types.GetCompositeScheduleResponseJson{
		Status: types.GenericStatusEnumTypeRejected,
		StatusInfo: &types.StatusInfoType{
			ReasonCode:     "EVSENotFound",
			AdditionalInfo: &additionalInfo,
		},
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	assert.NoError(t, err)
}

func TestGetCompositeScheduleResultHandler_GridConnection(t *testing.T) {
	// evseId=0 represents the grid connection
	ctx := context.Background()
	handler := ocpp201.GetCompositeScheduleResultHandler{}

	req := &types.GetCompositeScheduleRequestJson{
		EvseId:   0,
		Duration: 1800,
	}
	resp := &types.GetCompositeScheduleResponseJson{
		Status: types.GenericStatusEnumTypeAccepted,
		Schedule: &types.CompositeScheduleType{
			EvseId:           0,
			Duration:         1800,
			ScheduleStart:    "2026-02-14T23:00:00Z",
			ChargingRateUnit: types.ChargingRateUnitEnumTypeA,
			ChargingSchedulePeriod: []types.ChargingSchedulePeriodType{
				{StartPeriod: 0, Limit: 63.0},
			},
		},
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	assert.NoError(t, err)
}

func TestGetCompositeScheduleResultHandler_InterfaceCheck(t *testing.T) {
	// Verify that GetCompositeScheduleResultHandler implements the expected interface shape
	handler := ocpp201.GetCompositeScheduleResultHandler{}

	unitW := types.ChargingRateUnitEnumTypeW
	req := &types.GetCompositeScheduleRequestJson{
		EvseId:           1,
		Duration:         3600,
		ChargingRateUnit: &unitW,
	}
	resp := &types.GetCompositeScheduleResponseJson{
		Status: types.GenericStatusEnumTypeAccepted,
		Schedule: &types.CompositeScheduleType{
			EvseId:           1,
			Duration:         3600,
			ScheduleStart:    "2026-02-14T23:07:00Z",
			ChargingRateUnit: types.ChargingRateUnitEnumTypeW,
			ChargingSchedulePeriod: []types.ChargingSchedulePeriodType{
				{StartPeriod: 0, Limit: 22000.0},
			},
		},
	}

	ctx := context.Background()
	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	assert.NoError(t, err)
}
