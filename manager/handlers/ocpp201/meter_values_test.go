// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

type meterValuesStoreStub struct {
	storeMeterValuesFn func(ctx context.Context, chargeStationId string, evseId int, transactionId string, meterValues []store.MeterValue) error
}

func (s meterValuesStoreStub) StoreMeterValues(ctx context.Context, chargeStationId string, evseId int, transactionId string, meterValues []store.MeterValue) error {
	if s.storeMeterValuesFn != nil {
		return s.storeMeterValuesFn(ctx, chargeStationId, evseId, transactionId, meterValues)
	}
	return nil
}

func TestMeterValuesHandler_StoresMeterValues(t *testing.T) {
	var gotChargeStationID string
	var gotEvseID int
	var gotTransactionID string
	var gotMeterValues []store.MeterValue

	handler := ocpp201.MeterValuesHandler{
		Store: meterValuesStoreStub{storeMeterValuesFn: func(_ context.Context, chargeStationId string, evseId int, transactionId string, meterValues []store.MeterValue) error {
			gotChargeStationID = chargeStationId
			gotEvseID = evseId
			gotTransactionID = transactionId
			gotMeterValues = meterValues
			return nil
		}},
	}

	req := &types.MeterValuesRequestJson{
		EvseId: 1,
		MeterValue: []types.MeterValueType{{
			Timestamp: "2026-02-16T18:30:00Z",
			SampledValue: []types.SampledValueType{{
				Value:     100,
				Measurand: makePtr(types.MeasurandEnumTypeEnergyActiveImportRegister),
				Phase:     makePtr(types.PhaseEnumTypeL1),
				Location:  makePtr(types.LocationEnumTypeOutlet),
				UnitOfMeasure: &types.UnitOfMeasureType{
					Unit:       "Wh",
					Multiplier: 0,
				},
			}},
		}},
	}

	resp, err := handler.HandleCall(context.Background(), "cs001", req)
	require.NoError(t, err)
	assert.Equal(t, &types.MeterValuesResponseJson{}, resp)

	assert.Equal(t, "cs001", gotChargeStationID)
	assert.Equal(t, 1, gotEvseID)
	assert.Equal(t, "", gotTransactionID)
	require.Len(t, gotMeterValues, 1)
	require.Len(t, gotMeterValues[0].SampledValues, 1)
	assert.Equal(t, 100.0, gotMeterValues[0].SampledValues[0].Value)
	require.NotNil(t, gotMeterValues[0].SampledValues[0].Measurand)
	assert.Equal(t, "Energy.Active.Import.Register", *gotMeterValues[0].SampledValues[0].Measurand)
}

func TestMeterValuesHandler_StoreErrorDoesNotFailRequest(t *testing.T) {
	handler := ocpp201.MeterValuesHandler{
		Store: meterValuesStoreStub{storeMeterValuesFn: func(_ context.Context, _ string, _ int, _ string, _ []store.MeterValue) error {
			return errors.New("db unavailable")
		}},
	}

	resp, err := handler.HandleCall(context.Background(), "cs001", &types.MeterValuesRequestJson{EvseId: 1})
	require.NoError(t, err)
	assert.Equal(t, &types.MeterValuesResponseJson{}, resp)
}
