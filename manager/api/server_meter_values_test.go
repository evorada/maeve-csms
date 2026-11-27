// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
)

func meterValuesStr(s string) *string {
	return &s
}

// TestGetMeterValuesReturnsOcpp16MeterValues checks that what the OCPP 1.6 MeterValues
// handler stores can be read back through the API.
func TestGetMeterValuesReturnsOcpp16MeterValues(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()
	registerStation(t, engine, "cs001")

	handler := ocpp16.MeterValuesHandler{
		TransactionStore: engine,
		MeterValuesStore: engine,
		Tx:               engine,
		Events:           events.NopEmitter{},
	}

	energy := types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandEnergyActiveImportRegister
	power := types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandPowerActiveImport
	soc := types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandSoC
	wattHours := types.MeterValuesJsonMeterValueElemSampledValueElemUnitWh
	watts := types.MeterValuesJsonMeterValueElemSampledValueElemUnitW
	percent := types.MeterValuesJsonMeterValueElemSampledValueElemUnitPercent
	ev := types.MeterValuesJsonMeterValueElemSampledValueElemLocationEV

	transactionId := 42
	_, err := handler.HandleCall(context.Background(), "cs001", &types.MeterValuesJson{
		ConnectorId:   2,
		TransactionId: &transactionId,
		MeterValue: []types.MeterValuesJsonMeterValueElem{
			{
				Timestamp: "2026-11-26T10:05:00Z",
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{
					{Value: "1500", Measurand: &energy, Unit: &wattHours},
					{Value: "7400.5", Measurand: &power, Unit: &watts},
					{Value: "55", Measurand: &soc, Unit: &percent, Location: &ev},
				},
			},
		},
	})
	require.NoError(t, err)

	// meter values of the main meter, without a transaction: not part of the answer
	_, err = handler.HandleCall(context.Background(), "cs001", &types.MeterValuesJson{
		ConnectorId: 0,
		MeterValue: []types.MeterValuesJsonMeterValueElem{
			{
				Timestamp:    "2026-11-26T10:06:00Z",
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{{Value: "99000"}},
			},
		},
	})
	require.NoError(t, err)

	wantTransactionId := ocpp16.ConvertToUUID(42)
	req := httptest.NewRequest(http.MethodGet, "/cs/cs001/meter-values?connectorId=2&transactionId="+wantTransactionId, nil)
	req.Header.Set("accept", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	b, err := io.ReadAll(rr.Result().Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rr.Result().StatusCode, string(b))

	got := new(api.MeterValuesResponse)
	require.NoError(t, json.Unmarshal(b, got))

	connectorId := 2
	periodic := "Sample.Periodic"
	outlet := "Outlet"
	want := &api.MeterValuesResponse{
		Limit:  100,
		Offset: 0,
		Total:  1,
		MeterValues: []api.MeterValue{
			{
				ConnectorId:   &connectorId,
				EvseId:        &connectorId,
				Timestamp:     time.Date(2026, 11, 26, 10, 5, 0, 0, time.UTC),
				TransactionId: &wantTransactionId,
				SampledValue: []api.MeterValuesSampledValue{
					{Value: "1500.000000", Context: &periodic, Location: &outlet, Measurand: meterValuesStr("Energy.Active.Import.Register"), Unit: meterValuesStr("Wh")},
					{Value: "7400.500000", Context: &periodic, Location: &outlet, Measurand: meterValuesStr("Power.Active.Import"), Unit: meterValuesStr("W")},
					{Value: "55.000000", Context: &periodic, Location: meterValuesStr("EV"), Measurand: meterValuesStr("SoC"), Unit: meterValuesStr("Percent")},
				},
			},
		},
	}
	assert.Equal(t, want, got)

	// without filters the main meter reading is returned as well, newest first
	req = httptest.NewRequest(http.MethodGet, "/cs/cs001/meter-values", nil)
	req.Header.Set("accept", "application/json")
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	b, err = io.ReadAll(rr.Result().Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rr.Result().StatusCode, string(b))

	got = new(api.MeterValuesResponse)
	require.NoError(t, json.Unmarshal(b, got))
	assert.Equal(t, 2, got.Total)
	require.Len(t, got.MeterValues, 2)
	require.NotNil(t, got.MeterValues[0].ConnectorId)
	assert.Equal(t, 0, *got.MeterValues[0].ConnectorId)
	assert.Nil(t, got.MeterValues[0].TransactionId)
	require.Len(t, got.MeterValues[0].SampledValue, 1)
	assert.Equal(t, api.MeterValuesSampledValue{
		Value:     "99000.000000",
		Context:   &periodic,
		Location:  &outlet,
		Measurand: meterValuesStr("Energy.Active.Import.Register"),
		Unit:      meterValuesStr("Wh"),
	}, got.MeterValues[0].SampledValue[0])
}
