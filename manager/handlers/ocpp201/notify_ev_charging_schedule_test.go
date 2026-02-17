// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
)

func TestNotifyEVChargingScheduleBasic(t *testing.T) {
	handler := ocpp201.NotifyEVChargingScheduleHandler{}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.NotifyEVChargingScheduleRequestJson{
			TimeBase: "2026-02-15T03:00:00Z",
			EvseId:   1,
			ChargingSchedule: types.ChargingScheduleType{
				Id:               1,
				ChargingRateUnit: types.ChargingRateUnitEnumTypeW,
				ChargingSchedulePeriod: []types.ChargingSchedulePeriodType{
					{StartPeriod: 0, Limit: 11000},
				},
			},
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		got := resp.(*types.NotifyEVChargingScheduleResponseJson)
		assert.Equal(t, types.GenericStatusEnumTypeAccepted, got.Status)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"notify_ev_charging_schedule.evse_id":            1,
		"notify_ev_charging_schedule.time_base":          "2026-02-15T03:00:00Z",
		"notify_ev_charging_schedule.schedule_id":        1,
		"notify_ev_charging_schedule.charging_rate_unit": "W",
		"notify_ev_charging_schedule.period_count":       1,
	})
}

func TestNotifyEVChargingScheduleWithDurationAndStartSchedule(t *testing.T) {
	handler := ocpp201.NotifyEVChargingScheduleHandler{}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	duration := 3600
	startSchedule := "2026-02-15T03:00:00Z"

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.NotifyEVChargingScheduleRequestJson{
			TimeBase: "2026-02-15T03:00:00Z",
			EvseId:   2,
			ChargingSchedule: types.ChargingScheduleType{
				Id:               42,
				ChargingRateUnit: types.ChargingRateUnitEnumTypeA,
				Duration:         &duration,
				StartSchedule:    &startSchedule,
				ChargingSchedulePeriod: []types.ChargingSchedulePeriodType{
					{StartPeriod: 0, Limit: 16},
					{StartPeriod: 1800, Limit: 32},
				},
			},
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		got := resp.(*types.NotifyEVChargingScheduleResponseJson)
		assert.Equal(t, types.GenericStatusEnumTypeAccepted, got.Status)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"notify_ev_charging_schedule.evse_id":            2,
		"notify_ev_charging_schedule.time_base":          "2026-02-15T03:00:00Z",
		"notify_ev_charging_schedule.schedule_id":        42,
		"notify_ev_charging_schedule.charging_rate_unit": "A",
		"notify_ev_charging_schedule.period_count":       2,
		"notify_ev_charging_schedule.duration":           3600,
		"notify_ev_charging_schedule.start_schedule":     "2026-02-15T03:00:00Z",
	})
}

func TestNotifyEVChargingScheduleWithMinChargingRate(t *testing.T) {
	handler := ocpp201.NotifyEVChargingScheduleHandler{}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	minRate := 6.0

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.NotifyEVChargingScheduleRequestJson{
			TimeBase: "2026-02-15T03:00:00Z",
			EvseId:   1,
			ChargingSchedule: types.ChargingScheduleType{
				Id:               5,
				ChargingRateUnit: types.ChargingRateUnitEnumTypeA,
				MinChargingRate:  &minRate,
				ChargingSchedulePeriod: []types.ChargingSchedulePeriodType{
					{StartPeriod: 0, Limit: 32},
				},
			},
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		got := resp.(*types.NotifyEVChargingScheduleResponseJson)
		assert.Equal(t, types.GenericStatusEnumTypeAccepted, got.Status)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"notify_ev_charging_schedule.evse_id":            1,
		"notify_ev_charging_schedule.time_base":          "2026-02-15T03:00:00Z",
		"notify_ev_charging_schedule.schedule_id":        5,
		"notify_ev_charging_schedule.charging_rate_unit": "A",
		"notify_ev_charging_schedule.period_count":       1,
		"notify_ev_charging_schedule.min_charging_rate":  6.0,
	})
}

func TestNotifyEVChargingScheduleMultiplePeriods(t *testing.T) {
	handler := ocpp201.NotifyEVChargingScheduleHandler{}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.NotifyEVChargingScheduleRequestJson{
			TimeBase: "2026-02-15T22:00:00Z",
			EvseId:   1,
			ChargingSchedule: types.ChargingScheduleType{
				Id:               10,
				ChargingRateUnit: types.ChargingRateUnitEnumTypeW,
				ChargingSchedulePeriod: []types.ChargingSchedulePeriodType{
					{StartPeriod: 0, Limit: 7400},
					{StartPeriod: 3600, Limit: 11000},
					{StartPeriod: 7200, Limit: 22000},
				},
			},
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		got := resp.(*types.NotifyEVChargingScheduleResponseJson)
		assert.Equal(t, types.GenericStatusEnumTypeAccepted, got.Status)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"notify_ev_charging_schedule.evse_id":            1,
		"notify_ev_charging_schedule.time_base":          "2026-02-15T22:00:00Z",
		"notify_ev_charging_schedule.schedule_id":        10,
		"notify_ev_charging_schedule.charging_rate_unit": "W",
		"notify_ev_charging_schedule.period_count":       3,
	})
}

func TestNotifyEVChargingScheduleHandlerInterface(t *testing.T) {
	handler := ocpp201.NotifyEVChargingScheduleHandler{}
	req := &types.NotifyEVChargingScheduleRequestJson{
		TimeBase: "2026-02-15T03:00:00Z",
		EvseId:   1,
		ChargingSchedule: types.ChargingScheduleType{
			Id:               1,
			ChargingRateUnit: types.ChargingRateUnitEnumTypeW,
			ChargingSchedulePeriod: []types.ChargingSchedulePeriodType{
				{StartPeriod: 0, Limit: 11000},
			},
		},
	}

	resp, err := handler.HandleCall(context.Background(), "cs001", req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
}
