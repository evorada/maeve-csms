// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
)

// MeterValuesHandler stores the sampled values of a MeterValues.req against the
// connector and, when the request has one, the transaction. For a transaction that is
// known it emits session.updated with the newest readings, in the same transaction.
type MeterValuesHandler struct {
	TransactionStore store.TransactionStore
	MeterValuesStore store.MeterValuesStore
	Tx               store.Transactor
	Events           events.Emitter
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

	if len(meterValues) == 0 {
		return &types.MeterValuesResponseJson{}, nil
	}

	err = m.Tx.RunInTx(ctx, func(ctx context.Context) error {
		// connector 0 is the main power meter of the charge station
		err := m.MeterValuesStore.StoreMeterValues(ctx, chargeStationId, req.ConnectorId, transactionId, meterValues)
		if err != nil {
			return err
		}
		if req.TransactionId == nil {
			return nil
		}
		transaction := m.findTransaction(ctx, chargeStationId, transactionId)
		if transaction == nil {
			return nil
		}
		return m.Events.Emit(ctx, sessionUpdatedEvent(chargeStationId, *req.TransactionId, req.ConnectorId, transaction, meterValues))
	})
	if err != nil {
		return nil, err
	}

	return &types.MeterValuesResponseJson{}, nil
}

// findTransaction returns the transaction of the meter values, or nil when it is not
// known. It is only read for the event: when it cannot be read, the meter values are
// stored all the same, without an event.
func (m MeterValuesHandler) findTransaction(ctx context.Context, chargeStationId, transactionId string) *store.Transaction {
	transaction, err := m.TransactionStore.FindTransaction(ctx, chargeStationId, transactionId)
	if err != nil {
		slog.Warn("cannot read the transaction of the meter values, no session.updated event",
			slog.String("chargeStationId", chargeStationId), slog.String("transactionId", transactionId), "err", err)
		trace.SpanFromContext(ctx).RecordError(err)
		return nil
	}
	return transaction
}

// sessionUpdatedEvent is the session.updated event of the meter values of a known
// transaction.
func sessionUpdatedEvent(chargeStationId string, ocppTransactionId, connectorId int, transaction *store.Transaction, meterValues []store.MeterValue) events.Event {
	readings := newestReadings(meterValues)
	data := events.SessionUpdatedData{
		StationKey:        chargeStationId,
		StationId:         events.StationId(chargeStationId),
		TransactionId:     transaction.TransactionId,
		OcppTransactionId: strconv.Itoa(ocppTransactionId),
		EvseId:            transaction.EvseId,
		ConnectorId:       transaction.ConnectorId,
		SampledAt:         readings.sampledAt.UTC(),
		MeterWh:           readings.meterWh,
		PowerW:            readings.powerW,
		SocPercent:        readings.socPercent,
	}
	// OCPP 1.6 has no EVSEs: the connector id is used for both
	if data.ConnectorId == nil && connectorId > 0 {
		data.ConnectorId = &connectorId
		data.EvseId = &connectorId
	}
	if meterStart, ok := transactionMeterStartWh(transaction); ok && readings.meterWh != nil && *readings.meterWh >= meterStart {
		energy := *readings.meterWh - meterStart
		data.EnergyWh = &energy
	}
	return events.Event{
		Type:         events.TypeSessionUpdated,
		StationKey:   chargeStationId,
		ResourceType: events.ResourceSession,
		ResourceId:   transaction.TransactionId,
		Actor:        events.StationActor(chargeStationId),
		Data:         data,
	}
}

// sessionReadings are the newest readings of a connector in a MeterValues.req.
type sessionReadings struct {
	sampledAt  time.Time // time of the newest meter value
	meterWh    *int64    // energy register (Wh)
	powerW     *float64  // active power imported (W)
	socPercent *float64  // state of charge of the EV (%)
}

// newestReadings returns, for each reading, the value of the newest meter value that
// has it; of two meter values with the same time the later one in the message wins.
// The energy register and the power are the totals of the connector: samples at
// another location than the outlet or of one phase are not used. A reading that no
// meter value has, or not in a unit that can be converted, stays nil.
func newestReadings(meterValues []store.MeterValue) sessionReadings {
	var readings sessionReadings
	var meterAt, powerAt, socAt time.Time
	for _, mv := range meterValues {
		// convertMeterValue only keeps meter values whose time parses
		at, err := time.Parse(time.RFC3339, mv.Timestamp)
		if err != nil {
			continue
		}
		if !at.Before(readings.sampledAt) {
			readings.sampledAt = at
		}
		for _, sv := range mv.SampledValues {
			if wh, ok := connectorEnergyRegisterWh(sv); ok && !at.Before(meterAt) {
				meterAt, readings.meterWh = at, &wh
			}
			if w, ok := connectorPowerW(sv); ok && !at.Before(powerAt) {
				powerAt, readings.powerW = at, &w
			}
			if percent, ok := stateOfChargePercent(sv); ok && !at.Before(socAt) {
				socAt, readings.socPercent = at, &percent
			}
		}
	}
	return readings
}

// isConnectorTotal reports whether a sample is of the whole connector: at the outlet
// (the default location) and not of one phase.
func isConnectorTotal(sv store.SampledValue) bool {
	return sv.Phase == nil && (sv.Location == nil || *sv.Location == sampledValueDefaultLocation)
}

// connectorEnergyRegisterWh returns the energy register of the connector in Wh.
func connectorEnergyRegisterWh(sv store.SampledValue) (int64, bool) {
	if !isConnectorTotal(sv) {
		return 0, false
	}
	return energyRegisterWh(sv)
}

// connectorPowerW returns the active power imported by the connector in W.
func connectorPowerW(sv store.SampledValue) (float64, bool) {
	if !isConnectorTotal(sv) || sv.Measurand == nil || *sv.Measurand != sampledValueMeasurandPowerActiveImport {
		return 0, false
	}
	w := sv.Value
	if sv.UnitOfMeasure != nil {
		switch sv.UnitOfMeasure.Unit {
		case sampledValueUnitW:
		case sampledValueUnitKW:
			w *= 1000
		default:
			return 0, false
		}
	}
	// a reading too large for the conversion could not be encoded as JSON
	if math.IsInf(w, 0) {
		return 0, false
	}
	return w, true
}

// stateOfChargePercent returns the state of charge of the EV in percent.
func stateOfChargePercent(sv store.SampledValue) (float64, bool) {
	if sv.Measurand == nil || *sv.Measurand != sampledValueMeasurandSoC {
		return 0, false
	}
	if sv.UnitOfMeasure != nil && sv.UnitOfMeasure.Unit != sampledValueUnitPercent {
		return 0, false
	}
	return sv.Value, true
}
