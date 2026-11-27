// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// MeterValuesHandler stores the sampled values of a MeterValues.req against the
// connector and, when the request has one, the transaction.
type MeterValuesHandler struct {
	TransactionStore store.TransactionStore // not used yet: the session.updated event will need it
	MeterValuesStore store.MeterValuesStore
}

func (m MeterValuesHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (response ocpp.Response, err error) {
	req := request.(*types.MeterValuesJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("meter_values.connector_id", req.ConnectorId),
		attribute.Int("meter_values.sample_count", len(req.MeterValue)))

	// The transaction is not looked up: the id is worth keeping even when the
	// transaction is not known (same id scheme as StartTransaction).
	transactionId := ""
	if req.TransactionId != nil {
		transactionId = ConvertToUUID(*req.TransactionId)
		span.SetAttributes(attribute.String("meter_values.transaction_id", transactionId))
	}

	meterValues := make([]store.MeterValue, 0, len(req.MeterValue))
	for _, meterValue := range req.MeterValue {
		if mv, ok := convertMeterValue(meterValue.Timestamp, rawMeterValuesSamples(meterValue.SampledValue)); ok {
			meterValues = append(meterValues, mv)
		}
	}

	// connector 0 is the main power meter of the charge station
	if len(meterValues) > 0 {
		err = m.MeterValuesStore.StoreMeterValues(ctx, chargeStationId, req.ConnectorId, transactionId, meterValues)
		if err != nil {
			return nil, err
		}
	}

	return &types.MeterValuesResponseJson{}, nil
}
