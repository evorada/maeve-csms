// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	clockTest "k8s.io/utils/clock/testing"
)

const meterValuesChargeStationId = "cs001"

// newMeterValuesHandler returns a handler that stores in a new in-memory store.
func newMeterValuesHandler(t *testing.T) (handlers.MeterValuesHandler, *inmemory.Store) {
	t.Helper()

	now, err := time.Parse(time.RFC3339, "2026-11-26T10:00:00Z")
	require.NoError(t, err)
	engine := inmemory.NewStore(clockTest.NewFakePassiveClock(now))

	return handlers.MeterValuesHandler{
		TransactionStore: engine,
		MeterValuesStore: engine,
		Tx:               engine,
		Events:           events.NopEmitter{},
	}, engine
}

// storedMeterValues returns every meter value stored for the test charge station, newest first.
func storedMeterValues(t *testing.T, engine store.MeterValuesStore) *store.MeterValuesResult {
	t.Helper()

	result, err := engine.QueryMeterValues(context.Background(), store.MeterValuesFilter{
		ChargeStationId: meterValuesChargeStationId,
		Limit:           100,
	})
	require.NoError(t, err)
	return result
}

// recordingMeterValuesStore records the calls to StoreMeterValues and returns err from them.
type recordingMeterValuesStore struct {
	store.MeterValuesStore
	err   error
	calls int
}

func (s *recordingMeterValuesStore) StoreMeterValues(context.Context, string, int, string, []store.MeterValue) error {
	s.calls++
	return s.err
}

func meterValuesSample(value string, measurand types.MeterValuesJsonMeterValueElemSampledValueElemMeasurand, unit types.MeterValuesJsonMeterValueElemSampledValueElemUnit) types.MeterValuesJsonMeterValueElemSampledValueElem {
	return types.MeterValuesJsonMeterValueElemSampledValueElem{
		Value:     value,
		Measurand: &measurand,
		Unit:      &unit,
	}
}

// signedMeterValuesSample returns a signed sample. Its value is a hex encoded signature
// that happens to have only decimal digits, so it must be skipped because of its format.
func signedMeterValuesSample() types.MeterValuesJsonMeterValueElemSampledValueElem {
	format := types.MeterValuesJsonMeterValueElemSampledValueElemFormatSignedData
	return types.MeterValuesJsonMeterValueElemSampledValueElem{
		Value:  "3045022100",
		Format: &format,
	}
}

// storedSample returns the sample the handler is expected to store for a sample that
// only has a value, a measurand and a unit: context and location get the OCPP 1.6 defaults.
func storedSample(value float64, measurand, unit string) store.SampledValue {
	periodic := "Sample.Periodic"
	outlet := "Outlet"
	return store.SampledValue{
		Context:       &periodic,
		Location:      &outlet,
		Measurand:     &measurand,
		UnitOfMeasure: &store.UnitOfMeasure{Unit: unit, Multipler: 0},
		Value:         value,
	}
}

func TestMeterValuesHandlerStoresSamplesOfATransaction(t *testing.T) {
	handler, engine := newMeterValuesHandler(t)

	transactionId := 42
	req := &types.MeterValuesJson{
		ConnectorId:   2,
		TransactionId: &transactionId,
		MeterValue: []types.MeterValuesJsonMeterValueElem{
			{
				Timestamp: "2026-11-26T10:05:00Z",
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{
					meterValuesSample("1500", types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandEnergyActiveImportRegister, types.MeterValuesJsonMeterValueElemSampledValueElemUnitWh),
					meterValuesSample("7400.5", types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandPowerActiveImport, types.MeterValuesJsonMeterValueElemSampledValueElemUnitW),
					meterValuesSample("55", types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandSoC, types.MeterValuesJsonMeterValueElemSampledValueElemUnitPercent),
				},
			},
		},
	}

	tracer, exporter := testutil.GetTracer()

	func() {
		ctx, span := tracer.Start(context.Background(), "test")
		defer span.End()

		got, err := handler.HandleCall(ctx, meterValuesChargeStationId, req)
		require.NoError(t, err)
		assert.Equal(t, &types.MeterValuesResponseJson{}, got)
	}()

	wantTransactionId := handlers.ConvertToUUID(42)
	result, err := engine.QueryMeterValues(context.Background(), store.MeterValuesFilter{
		ChargeStationId: meterValuesChargeStationId,
		TransactionId:   &wantTransactionId,
		Limit:           100,
	})
	require.NoError(t, err)

	want := []store.StoredMeterValue{
		{
			ChargeStationId: meterValuesChargeStationId,
			EvseId:          2,
			TransactionId:   wantTransactionId,
			MeterValue: store.MeterValue{
				Timestamp: "2026-11-26T10:05:00Z",
				SampledValues: []store.SampledValue{
					storedSample(1500, "Energy.Active.Import.Register", "Wh"),
					storedSample(7400.5, "Power.Active.Import", "W"),
					storedSample(55, "SoC", "Percent"),
				},
			},
		},
	}
	assert.Equal(t, want, result.MeterValues)
	assert.Equal(t, 1, result.Total)

	// the connector filter of the query is the connector of the request
	connectorId := 2
	result, err = engine.QueryMeterValues(context.Background(), store.MeterValuesFilter{
		ChargeStationId: meterValuesChargeStationId,
		ConnectorId:     &connectorId,
		Limit:           100,
	})
	require.NoError(t, err)
	assert.Equal(t, want, result.MeterValues)

	require.Len(t, exporter.GetSpans(), 1)
	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"meter_values.connector_id":   2,
		"meter_values.sample_count":   1,
		"meter_values.transaction_id": wantTransactionId,
	})
}

func TestMeterValuesHandlerStoresMainMeterSamplesWithoutTransaction(t *testing.T) {
	handler, engine := newMeterValuesHandler(t)

	req := &types.MeterValuesJson{
		ConnectorId: 0,
		MeterValue: []types.MeterValuesJsonMeterValueElem{
			{
				Timestamp: "2026-11-26T10:05:00Z",
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{
					meterValuesSample("12.5", types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandEnergyActiveImportRegister, types.MeterValuesJsonMeterValueElemSampledValueElemUnitKWh),
				},
			},
		},
	}

	tracer, exporter := testutil.GetTracer()

	func() {
		ctx, span := tracer.Start(context.Background(), "test")
		defer span.End()

		got, err := handler.HandleCall(ctx, meterValuesChargeStationId, req)
		require.NoError(t, err)
		assert.Equal(t, &types.MeterValuesResponseJson{}, got)
	}()

	want := []store.StoredMeterValue{
		{
			ChargeStationId: meterValuesChargeStationId,
			EvseId:          0,
			TransactionId:   "",
			MeterValue: store.MeterValue{
				Timestamp:     "2026-11-26T10:05:00Z",
				SampledValues: []store.SampledValue{storedSample(12.5, "Energy.Active.Import.Register", "kWh")},
			},
		},
	}
	result := storedMeterValues(t, engine)
	assert.Equal(t, want, result.MeterValues)
	assert.Equal(t, 1, result.Total)

	// no transaction id attribute without a transaction
	require.Len(t, exporter.GetSpans(), 1)
	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"meter_values.connector_id": 0,
		"meter_values.sample_count": 1,
	})
}

func TestMeterValuesHandlerAppliesOcpp16Defaults(t *testing.T) {
	handler, engine := newMeterValuesHandler(t)

	// a sample with only a value is an energy register reading in Wh
	req := &types.MeterValuesJson{
		ConnectorId: 1,
		MeterValue: []types.MeterValuesJsonMeterValueElem{
			{
				Timestamp:    "2026-11-26T10:05:00Z",
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{{Value: "1234"}},
			},
		},
	}

	_, err := handler.HandleCall(context.Background(), meterValuesChargeStationId, req)
	require.NoError(t, err)

	result := storedMeterValues(t, engine)
	require.Len(t, result.MeterValues, 1)
	assert.Equal(t, []store.SampledValue{storedSample(1234, "Energy.Active.Import.Register", "Wh")}, result.MeterValues[0].MeterValue.SampledValues)
}

func TestMeterValuesHandlerStoresEveryMeterValueOfARequest(t *testing.T) {
	handler, engine := newMeterValuesHandler(t)

	transactionId := 42
	req := &types.MeterValuesJson{
		ConnectorId:   1,
		TransactionId: &transactionId,
		MeterValue: []types.MeterValuesJsonMeterValueElem{
			{
				Timestamp: "2026-11-26T10:05:00Z",
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{
					meterValuesSample("1500", types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandEnergyActiveImportRegister, types.MeterValuesJsonMeterValueElemSampledValueElemUnitWh),
				},
			},
			{
				Timestamp: "2026-11-26T10:06:00Z",
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{
					meterValuesSample("1600", types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandEnergyActiveImportRegister, types.MeterValuesJsonMeterValueElemSampledValueElemUnitWh),
				},
			},
		},
	}

	_, err := handler.HandleCall(context.Background(), meterValuesChargeStationId, req)
	require.NoError(t, err)

	wantTransactionId := handlers.ConvertToUUID(42)
	want := []store.StoredMeterValue{
		{
			ChargeStationId: meterValuesChargeStationId,
			EvseId:          1,
			TransactionId:   wantTransactionId,
			MeterValue: store.MeterValue{
				Timestamp:     "2026-11-26T10:06:00Z",
				SampledValues: []store.SampledValue{storedSample(1600, "Energy.Active.Import.Register", "Wh")},
			},
		},
		{
			ChargeStationId: meterValuesChargeStationId,
			EvseId:          1,
			TransactionId:   wantTransactionId,
			MeterValue: store.MeterValue{
				Timestamp:     "2026-11-26T10:05:00Z",
				SampledValues: []store.SampledValue{storedSample(1500, "Energy.Active.Import.Register", "Wh")},
			},
		},
	}
	result := storedMeterValues(t, engine)
	assert.Equal(t, want, result.MeterValues)
	assert.Equal(t, 2, result.Total)
}

func TestMeterValuesHandlerSkipsSignedSamples(t *testing.T) {
	handler, engine := newMeterValuesHandler(t)

	req := &types.MeterValuesJson{
		ConnectorId: 1,
		MeterValue: []types.MeterValuesJsonMeterValueElem{
			{
				Timestamp: "2026-11-26T10:05:00Z",
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{
					signedMeterValuesSample(),
					meterValuesSample("1500", types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandEnergyActiveImportRegister, types.MeterValuesJsonMeterValueElemSampledValueElemUnitWh),
				},
			},
		},
	}

	got, err := handler.HandleCall(context.Background(), meterValuesChargeStationId, req)
	require.NoError(t, err)
	assert.Equal(t, &types.MeterValuesResponseJson{}, got)

	result := storedMeterValues(t, engine)
	require.Len(t, result.MeterValues, 1)
	assert.Equal(t, []store.SampledValue{storedSample(1500, "Energy.Active.Import.Register", "Wh")}, result.MeterValues[0].MeterValue.SampledValues)
}

func TestMeterValuesHandlerSkipsNonNumericSamples(t *testing.T) {
	handler, engine := newMeterValuesHandler(t)

	req := &types.MeterValuesJson{
		ConnectorId: 1,
		MeterValue: []types.MeterValuesJsonMeterValueElem{
			{
				Timestamp: "2026-11-26T10:05:00Z",
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{
					meterValuesSample("n/a", types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandTemperature, types.MeterValuesJsonMeterValueElemSampledValueElemUnitCelsius),
					meterValuesSample("230.1", types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandVoltage, types.MeterValuesJsonMeterValueElemSampledValueElemUnitV),
				},
			},
		},
	}

	got, err := handler.HandleCall(context.Background(), meterValuesChargeStationId, req)
	require.NoError(t, err)
	assert.Equal(t, &types.MeterValuesResponseJson{}, got)

	result := storedMeterValues(t, engine)
	require.Len(t, result.MeterValues, 1)
	assert.Equal(t, []store.SampledValue{storedSample(230.1, "Voltage", "V")}, result.MeterValues[0].MeterValue.SampledValues)
}

func TestMeterValuesHandlerStoresNothingWhenEverySampleIsSigned(t *testing.T) {
	handler, engine := newMeterValuesHandler(t)
	recorder := &recordingMeterValuesStore{MeterValuesStore: engine}
	handler.MeterValuesStore = recorder

	transactionId := 42
	req := &types.MeterValuesJson{
		ConnectorId:   1,
		TransactionId: &transactionId,
		MeterValue: []types.MeterValuesJsonMeterValueElem{
			{
				Timestamp:    "2026-11-26T10:05:00Z",
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{signedMeterValuesSample()},
			},
			{
				Timestamp:    "2026-11-26T10:06:00Z",
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{signedMeterValuesSample(), signedMeterValuesSample()},
			},
		},
	}

	got, err := handler.HandleCall(context.Background(), meterValuesChargeStationId, req)
	require.NoError(t, err)
	assert.Equal(t, &types.MeterValuesResponseJson{}, got)

	assert.Zero(t, recorder.calls, "the store must not be called without a storable meter value")
	result := storedMeterValues(t, engine)
	assert.Empty(t, result.MeterValues)
	assert.Equal(t, 0, result.Total)
}

func TestMeterValuesHandlerSkipsMeterValueWithInvalidTimestamp(t *testing.T) {
	handler, engine := newMeterValuesHandler(t)

	req := &types.MeterValuesJson{
		ConnectorId: 1,
		MeterValue: []types.MeterValuesJsonMeterValueElem{
			{
				Timestamp: "2026-11-26T10:05:00Z",
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{
					meterValuesSample("1500", types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandEnergyActiveImportRegister, types.MeterValuesJsonMeterValueElemSampledValueElemUnitWh),
				},
			},
			{
				Timestamp: "26/11/2026 10:06",
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{
					meterValuesSample("1600", types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandEnergyActiveImportRegister, types.MeterValuesJsonMeterValueElemSampledValueElemUnitWh),
				},
			},
			{
				Timestamp: "2026-11-26T10:07:00.250+01:00",
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{
					meterValuesSample("1700", types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandEnergyActiveImportRegister, types.MeterValuesJsonMeterValueElemSampledValueElemUnitWh),
				},
			},
		},
	}

	got, err := handler.HandleCall(context.Background(), meterValuesChargeStationId, req)
	require.NoError(t, err)
	assert.Equal(t, &types.MeterValuesResponseJson{}, got)

	result := storedMeterValues(t, engine)
	assert.Equal(t, 2, result.Total)

	// timestamps are stored as received
	var timestamps []string
	for _, mv := range result.MeterValues {
		timestamps = append(timestamps, mv.MeterValue.Timestamp)
	}
	assert.ElementsMatch(t, []string{"2026-11-26T10:05:00Z", "2026-11-26T10:07:00.250+01:00"}, timestamps)
}

func TestMeterValuesHandlerReturnsStoreError(t *testing.T) {
	handler, engine := newMeterValuesHandler(t)
	storeErr := errors.New("meter values store is down")
	recorder := &recordingMeterValuesStore{MeterValuesStore: engine, err: storeErr}
	handler.MeterValuesStore = recorder

	req := &types.MeterValuesJson{
		ConnectorId: 1,
		MeterValue: []types.MeterValuesJsonMeterValueElem{
			{
				Timestamp: "2026-11-26T10:05:00Z",
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{
					meterValuesSample("1500", types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandEnergyActiveImportRegister, types.MeterValuesJsonMeterValueElemSampledValueElemUnitWh),
				},
			},
		},
	}

	got, err := handler.HandleCall(context.Background(), meterValuesChargeStationId, req)
	assert.ErrorIs(t, err, storeErr)
	assert.Nil(t, got)
	assert.Equal(t, 1, recorder.calls)
}
