// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func sampleStr(s string) *string {
	return &s
}

func TestConvertSampledValue(t *testing.T) {
	tests := []struct {
		name string
		raw  rawSampledValue
		want store.SampledValue
	}{
		{
			name: "defaults applied when measurand, context, location and unit are omitted",
			raw:  rawSampledValue{Value: "1234.5"},
			want: store.SampledValue{
				Context:       sampleStr("Sample.Periodic"),
				Location:      sampleStr("Outlet"),
				Measurand:     sampleStr("Energy.Active.Import.Register"),
				UnitOfMeasure: &store.UnitOfMeasure{Unit: "Wh", Multipler: 0},
				Value:         1234.5,
			},
		},
		{
			name: "explicit values are kept",
			raw: rawSampledValue{
				Value:     "7.4",
				Context:   sampleStr("Transaction.End"),
				Format:    sampleStr("Raw"),
				Location:  sampleStr("EV"),
				Measurand: sampleStr("Power.Active.Import"),
				Phase:     sampleStr("L1"),
				Unit:      sampleStr("kW"),
			},
			want: store.SampledValue{
				Context:       sampleStr("Transaction.End"),
				Location:      sampleStr("EV"),
				Measurand:     sampleStr("Power.Active.Import"),
				Phase:         sampleStr("L1"),
				UnitOfMeasure: &store.UnitOfMeasure{Unit: "kW", Multipler: 0},
				Value:         7.4,
			},
		},
		{
			name: "explicit unit of an energy measurand is kept",
			raw:  rawSampledValue{Value: "12.5", Measurand: sampleStr("Energy.Active.Import.Register"), Unit: sampleStr("kWh")},
			want: store.SampledValue{
				Context:       sampleStr("Sample.Periodic"),
				Location:      sampleStr("Outlet"),
				Measurand:     sampleStr("Energy.Active.Import.Register"),
				UnitOfMeasure: &store.UnitOfMeasure{Unit: "kWh", Multipler: 0},
				Value:         12.5,
			},
		},
		{
			name: "unit defaults to Wh for another energy measurand",
			raw:  rawSampledValue{Value: "10", Measurand: sampleStr("Energy.Active.Export.Interval")},
			want: store.SampledValue{
				Context:       sampleStr("Sample.Periodic"),
				Location:      sampleStr("Outlet"),
				Measurand:     sampleStr("Energy.Active.Export.Interval"),
				UnitOfMeasure: &store.UnitOfMeasure{Unit: "Wh", Multipler: 0},
				Value:         10,
			},
		},
		{
			name: "no default unit for a power measurand",
			raw:  rawSampledValue{Value: "7400", Measurand: sampleStr("Power.Active.Import")},
			want: store.SampledValue{
				Context:   sampleStr("Sample.Periodic"),
				Location:  sampleStr("Outlet"),
				Measurand: sampleStr("Power.Active.Import"),
				Value:     7400,
			},
		},
		{
			name: "no default unit for SoC",
			raw:  rawSampledValue{Value: "55", Measurand: sampleStr("SoC"), Location: sampleStr("EV")},
			want: store.SampledValue{
				Context:   sampleStr("Sample.Periodic"),
				Location:  sampleStr("EV"),
				Measurand: sampleStr("SoC"),
				Value:     55,
			},
		},
		{
			name: "negative and exponent values are numeric",
			raw:  rawSampledValue{Value: "-1.5e3", Measurand: sampleStr("Power.Active.Import"), Unit: sampleStr("W")},
			want: store.SampledValue{
				Context:       sampleStr("Sample.Periodic"),
				Location:      sampleStr("Outlet"),
				Measurand:     sampleStr("Power.Active.Import"),
				UnitOfMeasure: &store.UnitOfMeasure{Unit: "W", Multipler: 0},
				Value:         -1500,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, reason := convertSampledValue(tt.raw)
			require.True(t, ok, "sample was skipped: %s", reason)
			assert.Empty(t, reason)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertSampledValueSkipped(t *testing.T) {
	tests := []struct {
		name       string
		raw        rawSampledValue
		wantReason string
	}{
		{name: "signed data", raw: rawSampledValue{Value: "AP9FqGTVI0bVYXNpZ25lZA==", Format: sampleStr("SignedData")}, wantReason: "signed data not supported"},
		{name: "signed data with a numeric value", raw: rawSampledValue{Value: "100", Format: sampleStr("SignedData")}, wantReason: "signed data not supported"},
		{name: "text", raw: rawSampledValue{Value: "abc"}, wantReason: "non-numeric value"},
		{name: "empty", raw: rawSampledValue{Value: ""}, wantReason: "non-numeric value"},
		{name: "decimal comma", raw: rawSampledValue{Value: "1,5"}, wantReason: "non-numeric value"},
		{name: "raw format with text", raw: rawSampledValue{Value: "n/a", Format: sampleStr("Raw")}, wantReason: "non-numeric value"},
		{name: "not a number", raw: rawSampledValue{Value: "NaN"}, wantReason: "non-numeric value"},
		{name: "infinity", raw: rawSampledValue{Value: "Inf"}, wantReason: "non-numeric value"},
		{name: "negative infinity", raw: rawSampledValue{Value: "-Infinity"}, wantReason: "non-numeric value"},
		{name: "out of range", raw: rawSampledValue{Value: "1e999"}, wantReason: "non-numeric value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, reason := convertSampledValue(tt.raw)
			assert.False(t, ok)
			assert.Equal(t, tt.wantReason, reason)
			assert.Equal(t, store.SampledValue{}, got)
		})
	}
}

func TestConvertSampledValueDoesNotAliasTheRequest(t *testing.T) {
	raw := rawSampledValue{
		Value:     "1",
		Context:   sampleStr("Trigger"),
		Location:  sampleStr("Inlet"),
		Measurand: sampleStr("Voltage"),
		Phase:     sampleStr("L2"),
		Unit:      sampleStr("V"),
	}

	got, ok, _ := convertSampledValue(raw)
	require.True(t, ok)

	*raw.Context = "changed"
	*raw.Location = "changed"
	*raw.Measurand = "changed"
	*raw.Phase = "changed"
	*raw.Unit = "changed"

	assert.Equal(t, store.SampledValue{
		Context:       sampleStr("Trigger"),
		Location:      sampleStr("Inlet"),
		Measurand:     sampleStr("Voltage"),
		Phase:         sampleStr("L2"),
		UnitOfMeasure: &store.UnitOfMeasure{Unit: "V", Multipler: 0},
		Value:         1,
	}, got)
}

func TestConvertMeterValue(t *testing.T) {
	energy := rawSampledValue{Value: "100"}
	signed := rawSampledValue{Value: "c2lnbmVk", Format: sampleStr("SignedData")}
	text := rawSampledValue{Value: "abc", Measurand: sampleStr("Power.Active.Import")}

	wantEnergy := store.SampledValue{
		Context:       sampleStr("Sample.Periodic"),
		Location:      sampleStr("Outlet"),
		Measurand:     sampleStr("Energy.Active.Import.Register"),
		UnitOfMeasure: &store.UnitOfMeasure{Unit: "Wh", Multipler: 0},
		Value:         100,
	}

	tests := []struct {
		name      string
		timestamp string
		raws      []rawSampledValue
		want      store.MeterValue
		wantOk    bool
	}{
		{
			name:      "timestamp is kept as received",
			timestamp: "2026-11-26T10:00:00Z",
			raws:      []rawSampledValue{energy},
			want:      store.MeterValue{Timestamp: "2026-11-26T10:00:00Z", SampledValues: []store.SampledValue{wantEnergy}},
			wantOk:    true,
		},
		{
			name:      "timestamp with fractional seconds",
			timestamp: "2026-11-26T10:00:00.123Z",
			raws:      []rawSampledValue{energy},
			want:      store.MeterValue{Timestamp: "2026-11-26T10:00:00.123Z", SampledValues: []store.SampledValue{wantEnergy}},
			wantOk:    true,
		},
		{
			name:      "timestamp with a UTC offset",
			timestamp: "2026-11-26T11:00:00+01:00",
			raws:      []rawSampledValue{energy},
			want:      store.MeterValue{Timestamp: "2026-11-26T11:00:00+01:00", SampledValues: []store.SampledValue{wantEnergy}},
			wantOk:    true,
		},
		{
			name:      "samples that cannot be stored are skipped",
			timestamp: "2026-11-26T10:00:00Z",
			raws:      []rawSampledValue{signed, energy, text},
			want:      store.MeterValue{Timestamp: "2026-11-26T10:00:00Z", SampledValues: []store.SampledValue{wantEnergy}},
			wantOk:    true,
		},
		{name: "no storable sample", timestamp: "2026-11-26T10:00:00Z", raws: []rawSampledValue{signed, text}},
		{name: "no sample", timestamp: "2026-11-26T10:00:00Z", raws: nil},
		{name: "timestamp is not a date", timestamp: "yesterday", raws: []rawSampledValue{energy}},
		{name: "timestamp is empty", timestamp: "", raws: []rawSampledValue{energy}},
		{name: "timestamp without time zone", timestamp: "2026-11-26T10:00:00", raws: []rawSampledValue{energy}},
		{name: "timestamp with a space separator", timestamp: "2026-11-26 10:00:00Z", raws: []rawSampledValue{energy}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := convertMeterValue(tt.timestamp, tt.raws)
			assert.Equal(t, tt.wantOk, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEnergyRegisterWh(t *testing.T) {
	tests := []struct {
		name   string
		raw    rawSampledValue
		want   int64
		wantOk bool
	}{
		{name: "Wh is rounded", raw: rawSampledValue{Value: "1234.6", Measurand: sampleStr("Energy.Active.Import.Register"), Unit: sampleStr("Wh")}, want: 1235, wantOk: true},
		{name: "kWh is multiplied by 1000", raw: rawSampledValue{Value: "12.5", Measurand: sampleStr("Energy.Active.Import.Register"), Unit: sampleStr("kWh")}, want: 12500, wantOk: true},
		{name: "kWh is rounded after the multiplication", raw: rawSampledValue{Value: "1.0004", Measurand: sampleStr("Energy.Active.Import.Register"), Unit: sampleStr("kWh")}, want: 1000, wantOk: true},
		{name: "half is rounded away from zero", raw: rawSampledValue{Value: "0.5", Unit: sampleStr("Wh")}, want: 1, wantOk: true},
		{name: "measurand and unit omitted", raw: rawSampledValue{Value: "100"}, want: 100, wantOk: true},
		{name: "power is not an energy register", raw: rawSampledValue{Value: "7400", Measurand: sampleStr("Power.Active.Import"), Unit: sampleStr("W")}},
		{name: "interval energy is not the register", raw: rawSampledValue{Value: "10", Measurand: sampleStr("Energy.Active.Import.Interval")}},
		{name: "exported energy is not the imported register", raw: rawSampledValue{Value: "10", Measurand: sampleStr("Energy.Active.Export.Register")}},
		{name: "unit that is not an energy unit", raw: rawSampledValue{Value: "10", Measurand: sampleStr("Energy.Active.Import.Register"), Unit: sampleStr("Percent")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sv, ok, reason := convertSampledValue(tt.raw)
			require.True(t, ok, "sample was skipped: %s", reason)

			got, ok := energyRegisterWh(sv)
			assert.Equal(t, tt.wantOk, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEnergyRegisterWhOfStoredSamples(t *testing.T) {
	tests := []struct {
		name   string
		sv     store.SampledValue
		want   int64
		wantOk bool
	}{
		{name: "sample stored without measurand and unit is read with the defaults", sv: store.SampledValue{Value: 50}, want: 50, wantOk: true},
		{name: "multiplier is ignored", sv: store.SampledValue{Measurand: sampleStr("Energy.Active.Import.Register"), UnitOfMeasure: &store.UnitOfMeasure{Unit: "Wh", Multipler: 1}, Value: 50}, want: 50, wantOk: true},
		{name: "other measurand", sv: store.SampledValue{Measurand: sampleStr("MeterValue"), UnitOfMeasure: &store.UnitOfMeasure{Unit: "Wh"}, Value: 50}},
		{name: "reading too large for an integer", sv: store.SampledValue{Measurand: sampleStr("Energy.Active.Import.Register"), Value: 1e300}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := energyRegisterWh(tt.sv)
			assert.Equal(t, tt.wantOk, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRawMeterValuesSamples(t *testing.T) {
	context := types.MeterValuesJsonMeterValueElemSampledValueElemContextTransactionBegin
	format := types.MeterValuesJsonMeterValueElemSampledValueElemFormatRaw
	location := types.MeterValuesJsonMeterValueElemSampledValueElemLocationCable
	measurand := types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandVoltage
	phase := types.MeterValuesJsonMeterValueElemSampledValueElemPhaseL1N
	unit := types.MeterValuesJsonMeterValueElemSampledValueElemUnitV

	got := rawMeterValuesSamples([]types.MeterValuesJsonMeterValueElemSampledValueElem{
		{Value: "230.1", Context: &context, Format: &format, Location: &location, Measurand: &measurand, Phase: &phase, Unit: &unit},
		{Value: "5"},
	})

	want := []rawSampledValue{
		{
			Value:     "230.1",
			Context:   sampleStr("Transaction.Begin"),
			Format:    sampleStr("Raw"),
			Location:  sampleStr("Cable"),
			Measurand: sampleStr("Voltage"),
			Phase:     sampleStr("L1-N"),
			Unit:      sampleStr("V"),
		},
		{Value: "5"},
	}
	assert.Equal(t, want, got)
}

func TestRawTransactionDataSamples(t *testing.T) {
	context := types.StopTransactionJsonTransactionDataElemSampledValueElemContextTransactionEnd
	format := types.StopTransactionJsonTransactionDataElemSampledValueElemFormatSignedData
	location := types.StopTransactionJsonTransactionDataElemSampledValueElemLocationInlet
	measurand := types.StopTransactionJsonTransactionDataElemSampledValueElemMeasurandCurrentImport
	phase := types.StopTransactionJsonTransactionDataElemSampledValueElemPhaseL3
	unit := types.StopTransactionJsonTransactionDataElemSampledValueElemUnitA

	got := rawTransactionDataSamples([]types.StopTransactionJsonTransactionDataElemSampledValueElem{
		{Value: "16", Context: &context, Format: &format, Location: &location, Measurand: &measurand, Phase: &phase, Unit: &unit},
		{Value: "5"},
	})

	want := []rawSampledValue{
		{
			Value:     "16",
			Context:   sampleStr("Transaction.End"),
			Format:    sampleStr("SignedData"),
			Location:  sampleStr("Inlet"),
			Measurand: sampleStr("Current.Import"),
			Phase:     sampleStr("L3"),
			Unit:      sampleStr("A"),
		},
		{Value: "5"},
	}
	assert.Equal(t, want, got)
}
