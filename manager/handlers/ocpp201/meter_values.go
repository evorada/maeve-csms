// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
)

type meterValuesStore interface {
	StoreMeterValues(ctx context.Context, chargeStationId string, evseId int, transactionId string, meterValues []store.MeterValue) error
}

type MeterValuesHandler struct {
	Store meterValuesStore
}

func (h MeterValuesHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (response ocpp.Response, err error) {
	req := request.(*ocpp201.MeterValuesRequestJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("meter_values.evse_id", req.EvseId),
		attribute.Int("meter_values.sample_count", len(req.MeterValue)),
	)

	meterValues := toStoreMeterValues(req.MeterValue)
	if h.Store != nil {
		if err := h.Store.StoreMeterValues(ctx, chargeStationId, req.EvseId, "", meterValues); err != nil {
			slog.Error("failed to store meter values", "charge_station_id", chargeStationId, "evse_id", req.EvseId, "error", err)
			span.AddEvent("failed to store meter values", trace.WithAttributes(attribute.String("error", err.Error())))
		}
	}

	return &ocpp201.MeterValuesResponseJson{}, nil
}

func toStoreMeterValues(values []ocpp201.MeterValueType) []store.MeterValue {
	result := make([]store.MeterValue, 0, len(values))

	for _, mv := range values {
		sampledValues := make([]store.SampledValue, 0, len(mv.SampledValue))
		for _, sv := range mv.SampledValue {
			var context, location, measurand, phase *string
			if sv.Context != nil {
				ctxValue := string(*sv.Context)
				context = &ctxValue
			}
			if sv.Location != nil {
				loc := string(*sv.Location)
				location = &loc
			}
			if sv.Measurand != nil {
				mea := string(*sv.Measurand)
				measurand = &mea
			}
			if sv.Phase != nil {
				ph := string(*sv.Phase)
				phase = &ph
			}

			var unit *store.UnitOfMeasure
			if sv.UnitOfMeasure != nil {
				unit = &store.UnitOfMeasure{
					Unit:      sv.UnitOfMeasure.Unit,
					Multipler: sv.UnitOfMeasure.Multiplier,
				}
			}

			sampledValues = append(sampledValues, store.SampledValue{
				Context:       context,
				Location:      location,
				Measurand:     measurand,
				Phase:         phase,
				UnitOfMeasure: unit,
				Value:         sv.Value,
			})
		}

		result = append(result, store.MeterValue{
			SampledValues: sampledValues,
			Timestamp:     mv.Timestamp,
		})
	}

	return result
}
