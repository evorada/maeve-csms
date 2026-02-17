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

func TestClearedChargingLimitWithSource(t *testing.T) {
	handler := ocpp201.ClearedChargingLimitHandler{}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.ClearedChargingLimitRequestJson{
			ChargingLimitSource: types.ChargingLimitSourceEnumTypeEMS,
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		assert.Equal(t, &types.ClearedChargingLimitResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"cleared_charging_limit.charging_limit_source": "EMS",
	})
}

func TestClearedChargingLimitWithSourceAndEvseId(t *testing.T) {
	handler := ocpp201.ClearedChargingLimitHandler{}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		evseId := 1
		req := &types.ClearedChargingLimitRequestJson{
			ChargingLimitSource: types.ChargingLimitSourceEnumTypeCSO,
			EvseId:              &evseId,
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		assert.Equal(t, &types.ClearedChargingLimitResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"cleared_charging_limit.charging_limit_source": "CSO",
		"cleared_charging_limit.evse_id":               1,
	})
}

func TestClearedChargingLimitWithSOSource(t *testing.T) {
	handler := ocpp201.ClearedChargingLimitHandler{}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.ClearedChargingLimitRequestJson{
			ChargingLimitSource: types.ChargingLimitSourceEnumTypeSO,
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		assert.Equal(t, &types.ClearedChargingLimitResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"cleared_charging_limit.charging_limit_source": "SO",
	})
}
