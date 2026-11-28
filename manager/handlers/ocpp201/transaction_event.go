// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/rating"
	"github.com/thoughtworks/maeve-csms/manager/services"
	"github.com/thoughtworks/maeve-csms/manager/sessions"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tariffs"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

// TransactionEventHandler records the transactions that a charge station reports.
//
// The tariff of a transaction is chosen once, with the first event of the transaction
// that says at which EVSE it runs (normally the Started event): the version that the
// TariffSelector selects is stored with the transaction and stays, also when the tariff
// gets a new version during the session. It is selected for the org of the transaction.
// Charging never waits for a tariff and is never refused for want of one: when the
// selection fails or takes too long, that is logged and the event is answered as ever.
//
// When a transaction ends, the Analyzer (when there is one) works out from the meter
// values how long it charged and how long it was idle, and stores that with the
// transaction. The answer never depends on it: an analysis that fails or takes too long
// is logged, and the transaction is left without one.
//
// Sessions (when there is one) works out what the transaction costs with its tariff: the
// cost so far with an Updated event, not more often than its interval allows, and the
// final cost with the Ended event, whose answer has it as the total cost, with VAT. The
// cost is stored with the transaction. A rating that fails or takes too long is logged:
// the event is answered as ever, an Ended event without a total cost.
type TransactionEventHandler struct {
	Store            store.Engine
	TokenAuthService services.TokenAuthService
	// Sessions works out what a transaction costs. Nil: transactions are not rated.
	Sessions *sessions.Service
	// TariffSelector selects the tariff of a transaction. Nil: transactions get no tariff.
	TariffSelector *tariffs.Selector
	// OrgResolver gives the org of a charge station, for a transaction that the store
	// gave the legacy org. It is only used with a TariffSelector.
	OrgResolver tenancy.StationOrgResolver
	// Clock gives the time for an event whose timestamp cannot be read. Nil is the real
	// clock.
	Clock clock.PassiveClock
	// Analyzer works out the idle time of a transaction that ends. Nil: transactions are
	// not analysed.
	Analyzer *rating.Analyzer
}

func (t TransactionEventHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	req := request.(*types.TransactionEventRequestJson)
	slog.Info("transaction event", slog.String("chargeStationId", chargeStationId),
		slog.String("transactionId", req.TransactionInfo.TransactionId),
		slog.String("eventType", string(req.EventType)),
		slog.String("triggerReason", string(req.TriggerReason)),
		slog.Int("seqNo", req.SeqNo))
	response := &types.TransactionEventResponseJson{}

	var idToken string
	var tokenType string
	if req.IdToken != nil {
		idToken = req.IdToken.IdToken
		tokenType = string(req.IdToken.Type)
		idTokenInfo := t.TokenAuthService.Authorize(ctx, chargeStationId, *req.IdToken)
		response.IdTokenInfo = &idTokenInfo
	}

	var err error
	switch req.EventType {
	case types.TransactionEventEnumTypeStarted:
		err = t.Store.CreateTransaction(
			ctx,
			chargeStationId,
			req.TransactionInfo.TransactionId,
			idToken,
			tokenType,
			convertMeterValues(req.MeterValue),
			req.SeqNo,
			req.Offline)
	case types.TransactionEventEnumTypeUpdated:
		err = t.Store.UpdateTransaction(
			ctx,
			chargeStationId,
			req.TransactionInfo.TransactionId,
			convertMeterValues(req.MeterValue))
	case types.TransactionEventEnumTypeEnded:
		err = t.Store.EndTransaction(
			ctx,
			chargeStationId,
			req.TransactionInfo.TransactionId,
			idToken,
			tokenType,
			convertMeterValues(req.MeterValue),
			req.SeqNo)
	}

	if err != nil {
		return nil, err
	}

	t.pinTariff(ctx, chargeStationId, req, idToken)

	switch req.EventType {
	case types.TransactionEventEnumTypeUpdated:
		t.updateRunningCost(ctx, chargeStationId, req.TransactionInfo.TransactionId)
	case types.TransactionEventEnumTypeEnded:
		t.analyse(ctx, chargeStationId, req.TransactionInfo.TransactionId)
		response.TotalCost = t.finalCost(ctx, chargeStationId, req.TransactionInfo.TransactionId)
	}

	return response, nil
}

// updateRunningCost works out the cost so far of the transaction and stores it with the
// transaction, when the last time was long enough ago. Nothing here fails the event.
func (t TransactionEventHandler) updateRunningCost(ctx context.Context, chargeStationId, transactionId string) {
	if t.Sessions == nil {
		return
	}
	if _, err := t.Sessions.UpdateRunningCost(ctx, chargeStationId, transactionId); err != nil {
		slog.Error("failed to rate a running transaction, its cost so far is not updated",
			slog.String("chargeStationId", chargeStationId), slog.String("transactionId", transactionId), "err", err)
	}
}

// finalCost works out the cost of the transaction that has just ended, stores it with the
// transaction, and returns the total with VAT for the answer; nil for a transaction
// without a price, and when the cost cannot be worked out in time. Nothing here fails the
// event.
func (t TransactionEventHandler) finalCost(ctx context.Context, chargeStationId, transactionId string) *float64 {
	if t.Sessions == nil {
		return nil
	}
	result, err := t.Sessions.End(ctx, chargeStationId, transactionId)
	if err != nil {
		slog.Error("failed to rate an ended transaction, it has no final cost",
			slog.String("chargeStationId", chargeStationId), slog.String("transactionId", transactionId), "err", err)
		return nil
	}
	if result == nil {
		return nil
	}
	totalCost, _ := result.Breakdown.Total.InclVat.Rat().Float64()
	return &totalCost
}

// pinTariff selects the tariff of the transaction of the event and stores it with the
// transaction, when the event says at which EVSE the transaction runs and the tariff of
// the transaction was not chosen before. Nothing here fails the event.
func (t TransactionEventHandler) pinTariff(ctx context.Context, chargeStationId string, req *types.TransactionEventRequestJson, idToken string) {
	if t.TariffSelector == nil || req.Evse == nil {
		return
	}
	transactionId := req.TransactionInfo.TransactionId
	logAttrs := []any{slog.String("chargeStationId", chargeStationId), slog.String("transactionId", transactionId)}

	// the transaction is read in a database transaction, which is on the primary: a read
	// replica may not have what this event has just stored. Its meter values are not
	// needed here, and they are many by the end of a long session.
	var transaction *store.Transaction
	err := t.Store.RunInTx(ctx, func(ctx context.Context) (err error) {
		transaction, err = t.Store.FindTransactionSummary(ctx, chargeStationId, transactionId)
		return err
	})
	if err != nil {
		slog.Error("failed to read a transaction to select its tariff, it goes on without one", append(logAttrs, "err", err)...)
		return
	}
	if transaction == nil || transaction.TariffSource != nil {
		return
	}

	// the tariff is a tariff of the org of the transaction, which stays the same when the
	// charge station moves to another org
	orgId := transaction.OrgId
	switch {
	case orgId == "":
		// a backend that does not keep the org of a transaction has no tariffs
		return
	case orgId == store.LegacyOrgId && t.OrgResolver != nil:
		// not an org of its own: the transaction belongs to the org of its charge station
		if orgId, err = t.OrgResolver.OrgIdForStation(ctx, chargeStationId); err != nil {
			slog.Error("failed to select the tariff of a transaction, it goes on without one", append(logAttrs, "err", err)...)
			return
		}
	}

	in := tariffs.SelectInput{
		OrgId:       orgId,
		StationKey:  chargeStationId,
		OcppVersion: string(transport.OcppVersion201),
		EvseId:      req.Evse.Id,
		TokenUid:    idToken,
		At:          t.eventTime(req),
	}
	if req.Evse.ConnectorId != nil {
		in.ConnectorId = *req.Evse.ConnectorId
	}
	if in.TokenUid == "" {
		// the token that an earlier event of the transaction gave
		in.TokenUid = transaction.IdToken
	}

	choice, err := t.TariffSelector.SelectForStart(ctx, in)
	if err == nil {
		_, err = tariffs.Pin(ctx, t.Store, chargeStationId, transactionId, choice)
	}
	if err != nil {
		slog.Error("failed to select the tariff of a transaction, it goes on without one", append(logAttrs, "err", err)...)
	}
}

// analyse works out the idle time of the transaction that has just ended and stores it
// with the transaction. Nothing here fails the event.
func (t TransactionEventHandler) analyse(ctx context.Context, chargeStationId, transactionId string) {
	if t.Analyzer == nil {
		return
	}
	if _, err := t.Analyzer.AnalyseAtStop(ctx, t.Store, chargeStationId, transactionId); err != nil {
		slog.Error("failed to analyse an ended transaction, it has no idle time",
			slog.String("chargeStationId", chargeStationId), slog.String("transactionId", transactionId), "err", err)
	}
}

// eventTime returns the time that the charge station gave the event; the current time
// when that cannot be read.
func (t TransactionEventHandler) eventTime(req *types.TransactionEventRequestJson) time.Time {
	if at, err := time.Parse(time.RFC3339, req.Timestamp); err == nil {
		return at
	}
	if t.Clock != nil {
		return t.Clock.Now()
	}
	return time.Now()
}

func convertMeterValues(meterValues []types.MeterValueType) []store.MeterValue {
	var converted []store.MeterValue
	for _, meterValue := range meterValues {
		converted = append(converted, convertMeterValue(meterValue))
	}
	return converted
}

func convertMeterValue(meterValue types.MeterValueType) store.MeterValue {
	return store.MeterValue{
		SampledValues: convertSampledValues(meterValue.SampledValue),
		Timestamp:     meterValue.Timestamp,
	}
}

func convertSampledValues(sampledValues []types.SampledValueType) []store.SampledValue {
	var converted []store.SampledValue
	for _, sampledValue := range sampledValues {
		converted = append(converted, convertSampledValue(sampledValue))
	}
	return converted
}

func convertSampledValue(sampledValue types.SampledValueType) store.SampledValue {
	return store.SampledValue{
		Context:       (*string)(sampledValue.Context),
		Location:      (*string)(sampledValue.Location),
		Measurand:     (*string)(sampledValue.Measurand),
		Phase:         (*string)(sampledValue.Phase),
		UnitOfMeasure: convertUnitOfMeasure(sampledValue.UnitOfMeasure),
		Value:         sampledValue.Value,
	}
}

func convertUnitOfMeasure(unitOfMeasure *types.UnitOfMeasureType) *store.UnitOfMeasure {
	if unitOfMeasure == nil {
		return nil
	}

	return &store.UnitOfMeasure{
		Unit:      unitOfMeasure.Unit,
		Multipler: unitOfMeasure.Multiplier,
	}
}
