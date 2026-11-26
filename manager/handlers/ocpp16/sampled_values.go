// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"math"
	"strconv"
	"strings"
	"time"

	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"golang.org/x/exp/slog"
)

// Values of the OCPP 1.6 SampledValue fields (section 7.43) that the converter refers to.
const (
	sampledValueFormatSignedData = string(types.MeterValuesJsonMeterValueElemSampledValueElemFormatSignedData)

	// defaults of the optional fields
	sampledValueDefaultMeasurand = string(types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandEnergyActiveImportRegister)
	sampledValueDefaultContext   = string(types.MeterValuesJsonMeterValueElemSampledValueElemContextSamplePeriodic)
	sampledValueDefaultLocation  = string(types.MeterValuesJsonMeterValueElemSampledValueElemLocationOutlet)

	// the default unit applies to every "Energy" measurand
	sampledValueEnergyMeasurandPrefix = "Energy."
	sampledValueUnitWh                = string(types.MeterValuesJsonMeterValueElemSampledValueElemUnitWh)
	sampledValueUnitKWh               = string(types.MeterValuesJsonMeterValueElemSampledValueElemUnitKWh)
)

// Reasons for skipping a sampled value.
const (
	skipReasonSignedData = "signed data not supported"
	skipReasonNonNumeric = "non-numeric value"
)

// rawSampledValue is the version-neutral form of an OCPP 1.6 SampledValue.
type rawSampledValue struct {
	Value     string
	Context   *string
	Format    *string
	Location  *string
	Measurand *string
	Phase     *string
	Unit      *string
}

// convertSampledValue applies the OCPP 1.6 defaults and parses the value.
// ok=false means the sample is skipped (signed or non-numeric) and reason says why.
func convertSampledValue(raw rawSampledValue) (sv store.SampledValue, ok bool, reason string) {
	// signed values are not numeric
	if raw.Format != nil && *raw.Format == sampledValueFormatSignedData {
		return store.SampledValue{}, false, skipReasonSignedData
	}

	// NaN and infinities parse as floats, but are not meter readings and cannot be stored as JSON
	value, err := strconv.ParseFloat(raw.Value, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return store.SampledValue{}, false, skipReasonNonNumeric
	}

	measurand := sampledValueDefaultMeasurand
	if raw.Measurand != nil {
		measurand = *raw.Measurand
	}
	sampleContext := sampledValueDefaultContext
	if raw.Context != nil {
		sampleContext = *raw.Context
	}
	location := sampledValueDefaultLocation
	if raw.Location != nil {
		location = *raw.Location
	}

	// OCPP 1.6 has no multiplier: the stored multiplier is a power of ten, so 0 means x1
	var unitOfMeasure *store.UnitOfMeasure
	switch {
	case raw.Unit != nil:
		unitOfMeasure = &store.UnitOfMeasure{Unit: *raw.Unit, Multipler: 0}
	case strings.HasPrefix(measurand, sampledValueEnergyMeasurandPrefix):
		unitOfMeasure = &store.UnitOfMeasure{Unit: sampledValueUnitWh, Multipler: 0}
	}

	var phase *string
	if raw.Phase != nil {
		p := *raw.Phase
		phase = &p
	}

	return store.SampledValue{
		Context:       &sampleContext,
		Location:      &location,
		Measurand:     &measurand,
		Phase:         phase,
		UnitOfMeasure: unitOfMeasure,
		Value:         value,
	}, true, ""
}

// convertMeterValue converts one 1.6 MeterValue. Samples that cannot be stored are
// skipped and logged; a meter value with an unparseable timestamp or no storable
// samples returns ok=false.
func convertMeterValue(timestamp string, raws []rawSampledValue) (mv store.MeterValue, ok bool) {
	// the timestamp is kept as received, it only has to be one the stores can parse
	if _, err := time.Parse(time.RFC3339, timestamp); err != nil {
		slog.Warn("skipping meter value with invalid timestamp", slog.String("timestamp", timestamp), "err", err)
		return store.MeterValue{}, false
	}

	sampledValues := make([]store.SampledValue, 0, len(raws))
	for _, raw := range raws {
		sv, storable, reason := convertSampledValue(raw)
		if !storable {
			measurand := sampledValueDefaultMeasurand
			if raw.Measurand != nil {
				measurand = *raw.Measurand
			}
			slog.Warn("skipping sampled value", slog.String("reason", reason),
				slog.String("timestamp", timestamp), slog.String("measurand", measurand))
			continue
		}
		sampledValues = append(sampledValues, sv)
	}
	if len(sampledValues) == 0 {
		return store.MeterValue{}, false
	}

	return store.MeterValue{
		SampledValues: sampledValues,
		Timestamp:     timestamp,
	}, true
}

// energyRegisterWh returns the Energy.Active.Import.Register reading of sv in Wh
// (kWh is multiplied by 1000, result rounded). ok=false for any other measurand.
//
// A sample without measurand or unit is read with the OCPP defaults
// (Energy.Active.Import.Register, Wh). The multiplier is ignored: OCPP 1.6 has none.
func energyRegisterWh(sv store.SampledValue) (wh int64, ok bool) {
	if sv.Measurand != nil && *sv.Measurand != sampledValueDefaultMeasurand {
		return 0, false
	}

	reading := sv.Value
	if sv.UnitOfMeasure != nil {
		switch sv.UnitOfMeasure.Unit {
		case sampledValueUnitWh:
		case sampledValueUnitKWh:
			reading *= 1000
		default:
			return 0, false
		}
	}

	reading = math.Round(reading)
	if math.IsNaN(reading) || reading < math.MinInt64 || reading >= math.MaxInt64 {
		return 0, false
	}
	return int64(reading), true
}

// stringPointer converts a pointer to a generated OCPP enum into a pointer to a copy of its value.
func stringPointer[T ~string](p *T) *string {
	if p == nil {
		return nil
	}
	s := string(*p)
	return &s
}

// rawMeterValuesSamples adapts the sampled values of a MeterValues.req.
func rawMeterValuesSamples(sampledValues []types.MeterValuesJsonMeterValueElemSampledValueElem) []rawSampledValue {
	raws := make([]rawSampledValue, 0, len(sampledValues))
	for _, sv := range sampledValues {
		raws = append(raws, rawSampledValue{
			Value:     sv.Value,
			Context:   stringPointer(sv.Context),
			Format:    stringPointer(sv.Format),
			Location:  stringPointer(sv.Location),
			Measurand: stringPointer(sv.Measurand),
			Phase:     stringPointer(sv.Phase),
			Unit:      stringPointer(sv.Unit),
		})
	}
	return raws
}

// rawTransactionDataSamples adapts the sampled values of StopTransaction.req transactionData.
func rawTransactionDataSamples(sampledValues []types.StopTransactionJsonTransactionDataElemSampledValueElem) []rawSampledValue {
	raws := make([]rawSampledValue, 0, len(sampledValues))
	for _, sv := range sampledValues {
		raws = append(raws, rawSampledValue{
			Value:     sv.Value,
			Context:   stringPointer(sv.Context),
			Format:    stringPointer(sv.Format),
			Location:  stringPointer(sv.Location),
			Measurand: stringPointer(sv.Measurand),
			Phase:     stringPointer(sv.Phase),
			Unit:      stringPointer(sv.Unit),
		})
	}
	return raws
}
