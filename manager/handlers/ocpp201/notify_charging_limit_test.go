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

func TestNotifyChargingLimitWithSourceOnly(t *testing.T) {
	handler := ocpp201.NotifyChargingLimitHandler{}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.NotifyChargingLimitRequestJson{
			ChargingLimit: types.ChargingLimitType{
				ChargingLimitSource: types.ChargingLimitSourceEnumTypeEMS,
			},
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		assert.Equal(t, &types.NotifyChargingLimitResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"notify_charging_limit.charging_limit_source": "EMS",
	})
}

func TestNotifyChargingLimitWithGridCritical(t *testing.T) {
	handler := ocpp201.NotifyChargingLimitHandler{}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		isGridCritical := true
		req := &types.NotifyChargingLimitRequestJson{
			ChargingLimit: types.ChargingLimitType{
				ChargingLimitSource: types.ChargingLimitSourceEnumTypeCSO,
				IsGridCritical:      &isGridCritical,
			},
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		assert.Equal(t, &types.NotifyChargingLimitResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"notify_charging_limit.charging_limit_source": "CSO",
		"notify_charging_limit.is_grid_critical":      true,
	})
}

func TestNotifyChargingLimitWithEvseId(t *testing.T) {
	handler := ocpp201.NotifyChargingLimitHandler{}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		evseId := 1
		req := &types.NotifyChargingLimitRequestJson{
			ChargingLimit: types.ChargingLimitType{
				ChargingLimitSource: types.ChargingLimitSourceEnumTypeSO,
			},
			EvseId: &evseId,
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		assert.Equal(t, &types.NotifyChargingLimitResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"notify_charging_limit.charging_limit_source": "SO",
		"notify_charging_limit.evse_id":               1,
	})
}

func TestNotifyChargingLimitWithChargingSchedules(t *testing.T) {
	handler := ocpp201.NotifyChargingLimitHandler{}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		limit := 32.0
		req := &types.NotifyChargingLimitRequestJson{
			ChargingLimit: types.ChargingLimitType{
				ChargingLimitSource: types.ChargingLimitSourceEnumTypeEMS,
			},
			ChargingSchedule: []types.ChargingScheduleType{
				{
					Id:               1,
					ChargingRateUnit: types.ChargingRateUnitEnumTypeA,
					ChargingSchedulePeriod: []types.ChargingSchedulePeriodType{
						{
							StartPeriod: 0,
							Limit:       limit,
						},
					},
				},
			},
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		assert.Equal(t, &types.NotifyChargingLimitResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"notify_charging_limit.charging_limit_source":    "EMS",
		"notify_charging_limit.charging_schedule_count": 1,
	})
}
