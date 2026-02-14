// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"k8s.io/utils/clock"
	clockTest "k8s.io/utils/clock/testing"
)

func TestMeterValuesHandler(t *testing.T) {
	clk := clockTest.NewFakeClock(clock.RealClock{}.Now())
	store := inmemory.NewStore(clk)
	handler := ocpp201.MeterValuesHandler{Store: store}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.MeterValuesRequestJson{
			EvseId: 1,
			MeterValue: []types.MeterValueType{
				{
					SampledValue: []types.SampledValueType{
						{
							Measurand: makePtr(types.MeasurandEnumTypeEnergyActiveImportRegister),
							Location:  makePtr(types.LocationEnumTypeOutlet),
							Value:     100,
						},
					},
					Timestamp: "2023-06-15T15:05:00+01:00",
				},
			},
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		assert.Equal(t, &types.MeterValuesResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"meter_values.evse_id": 1,
		"meter_values.count":   1,
	})

	// Verify meter values were stored
	stored, err := store.GetMeterValues(ctx, "cs001", 1, 0)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	
	assert.Equal(t, "cs001", stored[0].ChargeStationId)
	assert.Equal(t, 1, stored[0].EvseId)
	assert.Equal(t, "", stored[0].TransactionId)
	assert.Equal(t, "2023-06-15T15:05:00+01:00", stored[0].MeterValue.Timestamp)
	require.Len(t, stored[0].MeterValue.SampledValues, 1)
	assert.Equal(t, 100.0, stored[0].MeterValue.SampledValues[0].Value)
	assert.NotNil(t, stored[0].MeterValue.SampledValues[0].Measurand)
	assert.Equal(t, "Energy.Active.Import.Register", *stored[0].MeterValue.SampledValues[0].Measurand)
	assert.NotNil(t, stored[0].MeterValue.SampledValues[0].Location)
	assert.Equal(t, "Outlet", *stored[0].MeterValue.SampledValues[0].Location)
}

func TestMeterValuesHandler_MultipleSampledValues(t *testing.T) {
	clk := clockTest.NewFakeClock(clock.RealClock{}.Now())
	store := inmemory.NewStore(clk)
	handler := ocpp201.MeterValuesHandler{Store: store}

	ctx := context.Background()

	phase := types.PhaseEnumTypeL1
	unit := "Wh"
	multiplier := 0

	req := &types.MeterValuesRequestJson{
		EvseId: 2,
		MeterValue: []types.MeterValueType{
			{
				SampledValue: []types.SampledValueType{
					{
						Measurand: makePtr(types.MeasurandEnumTypeEnergyActiveImportRegister),
						Value:     2000,
					},
					{
						Measurand: makePtr(types.MeasurandEnumTypeCurrentImport),
						Phase:     &phase,
						Value:     10.5,
					},
					{
						Measurand: makePtr(types.MeasurandEnumTypeVoltage),
						UnitOfMeasure: &types.UnitOfMeasureType{
							Unit:       unit,
							Multiplier: multiplier,
						},
						Value: 230.0,
					},
				},
				Timestamp: "2023-06-15T15:10:00+01:00",
			},
		},
	}

	resp, err := handler.HandleCall(ctx, "cs002", req)
	require.NoError(t, err)
	assert.Equal(t, &types.MeterValuesResponseJson{}, resp)

	// Verify storage
	stored, err := store.GetMeterValues(ctx, "cs002", 2, 0)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.Len(t, stored[0].MeterValue.SampledValues, 3)

	// Check first sampled value (Energy)
	sv1 := stored[0].MeterValue.SampledValues[0]
	assert.Equal(t, 2000.0, sv1.Value)
	require.NotNil(t, sv1.Measurand)
	assert.Equal(t, "Energy.Active.Import.Register", *sv1.Measurand)

	// Check second sampled value (Current with phase)
	sv2 := stored[0].MeterValue.SampledValues[1]
	assert.Equal(t, 10.5, sv2.Value)
	require.NotNil(t, sv2.Measurand)
	assert.Equal(t, "Current.Import", *sv2.Measurand)
	require.NotNil(t, sv2.Phase)
	assert.Equal(t, "L1", *sv2.Phase)

	// Check third sampled value (Voltage with unit)
	sv3 := stored[0].MeterValue.SampledValues[2]
	assert.Equal(t, 230.0, sv3.Value)
	require.NotNil(t, sv3.Measurand)
	assert.Equal(t, "Voltage", *sv3.Measurand)
	require.NotNil(t, sv3.UnitOfMeasure)
	assert.Equal(t, "Wh", sv3.UnitOfMeasure.Unit)
	assert.Equal(t, 0, sv3.UnitOfMeasure.Multipler)
}

func TestMeterValuesHandler_MultipleReadings(t *testing.T) {
	clk := clockTest.NewFakeClock(clock.RealClock{}.Now())
	store := inmemory.NewStore(clk)
	handler := ocpp201.MeterValuesHandler{Store: store}

	ctx := context.Background()

	// First reading
	req1 := &types.MeterValuesRequestJson{
		EvseId: 1,
		MeterValue: []types.MeterValueType{
			{
				SampledValue: []types.SampledValueType{
					{Value: 100},
				},
				Timestamp: "2023-06-15T15:00:00+01:00",
			},
		},
	}
	_, err := handler.HandleCall(ctx, "cs003", req1)
	require.NoError(t, err)

	// Second reading
	req2 := &types.MeterValuesRequestJson{
		EvseId: 1,
		MeterValue: []types.MeterValueType{
			{
				SampledValue: []types.SampledValueType{
					{Value: 200},
				},
				Timestamp: "2023-06-15T15:05:00+01:00",
			},
		},
	}
	_, err = handler.HandleCall(ctx, "cs003", req2)
	require.NoError(t, err)

	// Verify both readings are stored
	stored, err := store.GetMeterValues(ctx, "cs003", 1, 0)
	require.NoError(t, err)
	require.Len(t, stored, 2)

	// Should be sorted by timestamp descending
	assert.Equal(t, "2023-06-15T15:05:00+01:00", stored[0].MeterValue.Timestamp)
	assert.Equal(t, 200.0, stored[0].MeterValue.SampledValues[0].Value)
	assert.Equal(t, "2023-06-15T15:00:00+01:00", stored[1].MeterValue.Timestamp)
	assert.Equal(t, 100.0, stored[1].MeterValue.SampledValues[0].Value)
}

func TestMeterValuesHandler_QueryWithLimit(t *testing.T) {
	clk := clockTest.NewFakeClock(clock.RealClock{}.Now())
	store := inmemory.NewStore(clk)
	handler := ocpp201.MeterValuesHandler{Store: store}

	ctx := context.Background()

	// Store 5 readings
	for i := 0; i < 5; i++ {
		req := &types.MeterValuesRequestJson{
			EvseId: 1,
			MeterValue: []types.MeterValueType{
				{
					SampledValue: []types.SampledValueType{
						{Value: float64(i * 100)},
					},
					Timestamp: "2023-06-15T15:00:00+01:00",
				},
			},
		}
		_, err := handler.HandleCall(ctx, "cs004", req)
		require.NoError(t, err)
	}

	// Query with limit
	stored, err := store.GetMeterValues(ctx, "cs004", 1, 3)
	require.NoError(t, err)
	assert.Len(t, stored, 3)
}
