// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type MeterValuesHandler struct {
	Store store.Engine
}

func (h MeterValuesHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (response ocpp.Response, err error) {
	req := request.(*ocpp201.MeterValuesRequestJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("meter_values.evse_id", req.EvseId),
		attribute.Int("meter_values.count", len(req.MeterValue)),
	)

	// Convert OCPP 2.0.1 meter values to store format
	storeMeterValues := make([]store.MeterValue, len(req.MeterValue))
	for i, mv := range req.MeterValue {
		storeMeterValues[i] = convertOcpp201MeterValue(&mv)
	}

	// Store the meter values
	// Note: We don't have a transaction ID from a standalone MeterValues message
	// If the charge station wants to associate these with a transaction, it should
	// send them via TransactionEvent messages instead
	transactionId := ""
	err = h.Store.StoreMeterValues(ctx, chargeStationId, req.EvseId, transactionId, storeMeterValues)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return &ocpp201.MeterValuesResponseJson{}, nil
}

// convertOcpp201MeterValue converts an OCPP 2.0.1 MeterValueType to store.MeterValue
func convertOcpp201MeterValue(mv *ocpp201.MeterValueType) store.MeterValue {
	sampledValues := make([]store.SampledValue, len(mv.SampledValue))
	for i, sv := range mv.SampledValue {
		sampledValues[i] = convertOcpp201SampledValue(&sv)
	}

	return store.MeterValue{
		Timestamp:     mv.Timestamp,
		SampledValues: sampledValues,
	}
}

// convertOcpp201SampledValue converts an OCPP 2.0.1 SampledValueType to store.SampledValue
func convertOcpp201SampledValue(sv *ocpp201.SampledValueType) store.SampledValue {
	result := store.SampledValue{
		Value: sv.Value,
	}

	// Convert optional fields
	if sv.Context != nil {
		context := string(*sv.Context)
		result.Context = &context
	}
	if sv.Location != nil {
		location := string(*sv.Location)
		result.Location = &location
	}
	if sv.Measurand != nil {
		measurand := string(*sv.Measurand)
		result.Measurand = &measurand
	}
	if sv.Phase != nil {
		phase := string(*sv.Phase)
		result.Phase = &phase
	}

	// Convert unit of measure
	if sv.UnitOfMeasure != nil {
		result.UnitOfMeasure = &store.UnitOfMeasure{
			Unit:      sv.UnitOfMeasure.Unit,
			Multipler: sv.UnitOfMeasure.Multiplier,
		}
	}

	return result
}
