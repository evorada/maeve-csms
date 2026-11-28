// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/rating"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

// StopTransactionHandler records the end of a transaction. A StopTransaction.req is
// always acknowledged, also for a transaction that the CSMS does not know: the station
// retries a StopTransaction.req that fails.
//
// The end of the transaction, its details and the session.completed event are stored in
// one transaction. Once it is stored, the end is given to the CommandTracker (when there
// is one), which records it on the remote stop that asked for it; nothing the tracker
// does changes the answer.
//
// After that the Analyzer (when there is one) works out from the meter values how long
// the transaction charged and how long it was idle, and stores that with the
// transaction. The answer never depends on it: an analysis that fails or takes too long
// is logged, and the transaction is left without one.
type StopTransactionHandler struct {
	Clock      clock.PassiveClock
	TokenStore store.TokenStore
	// RegistryStore holds allow_any_token of the charge station. The idTagInfo of the
	// response says Accepted for an unknown idTag when it is set, as Authorize and
	// StartTransaction did, so that the station does not cache the idTag as Invalid.
	// When RegistryStore is nil, an unknown idTag is Invalid.
	RegistryStore    store.ChargeStationRegistryStore
	OrgResolver      tenancy.StationOrgResolver
	TransactionStore store.TransactionStore
	Tx               store.Transactor
	Events           events.Emitter
	CommandTracker   CommandTracker // nil: commands are not followed
	// Analyzer works out the idle time of the transaction. Nil: transactions are not
	// analysed.
	Analyzer *rating.Analyzer
}

func (s StopTransactionHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (response ocpp.Response, err error) {
	req := request.(*types.StopTransactionJson)

	// OCPP 1.6: the reason may only be omitted when it is Local
	reason := string(types.StopTransactionJsonReasonLocal)
	if req.Reason != nil {
		reason = string(*req.Reason)
	}
	transactionId := ConvertToUUID(req.TransactionId)
	slog.Info("stopping transaction", slog.String("transactionId", transactionId), slog.String("reason", reason))

	stoppedAt, err := time.Parse(time.RFC3339, req.Timestamp)
	if err != nil {
		stoppedAt = s.Clock.Now()
		slog.Warn("invalid stop transaction timestamp, using the current time",
			slog.String("chargeStationId", chargeStationId), slog.String("timestamp", req.Timestamp), "err", err)
	}

	var idTagInfo *types.StopTransactionResponseJsonIdTagInfo
	if req.IdTag != nil {
		auth, err := authorizeIdTag(ctx, s.TokenStore, s.RegistryStore, s.OrgResolver, s.Clock, chargeStationId, *req.IdTag)
		if err != nil {
			return nil, err
		}
		idTagInfo = &types.StopTransactionResponseJsonIdTagInfo{
			Status:      types.StopTransactionResponseJsonIdTagInfoStatus(auth.Status),
			ExpiryDate:  auth.ExpiryDate,
			ParentIdTag: auth.ParentIdTag,
		}
	}

	var idToken, tokenType string
	if req.IdTag != nil {
		idToken = *req.IdTag
		tokenType = idTagTokenType
	}

	err = s.Tx.RunInTx(ctx, func(ctx context.Context) error {
		return s.endTransaction(ctx, chargeStationId, req, transactionId, idToken, tokenType, stoppedAt, reason)
	})
	if err != nil {
		return nil, err
	}

	trackCommand(ctx, s.CommandTracker, chargeStationId, "StopTransaction", func(tracker CommandTracker) {
		tracker.OnTransactionStopped(ctx, chargeStationId, req.TransactionId, reason)
	})

	s.analyse(ctx, chargeStationId, transactionId)

	return &types.StopTransactionResponseJson{
		IdTagInfo: idTagInfo,
	}, nil
}

// analyse works out the idle time of the transaction that has just ended and stores it
// with the transaction, in a database transaction of its own, after the one that stored
// the end. Nothing here fails the stop.
func (s StopTransactionHandler) analyse(ctx context.Context, chargeStationId, transactionId string) {
	if s.Analyzer == nil {
		return
	}
	if _, err := s.Analyzer.AnalyseAtStop(ctx, s.Tx, chargeStationId, transactionId); err != nil {
		slog.Error("failed to analyse an ended transaction, it has no idle time",
			slog.String("chargeStationId", chargeStationId), slog.String("transactionId", transactionId), "err", err)
		trace.SpanFromContext(ctx).RecordError(err)
	}
}

// endTransaction records the end of a transaction (creating the transaction when it is
// not known) and emits session.completed.
func (s StopTransactionHandler) endTransaction(ctx context.Context, chargeStationId string, req *types.StopTransactionJson,
	transactionId, idToken, tokenType string, stoppedAt time.Time, reason string) error {
	transaction, err := s.TransactionStore.FindTransaction(ctx, chargeStationId, transactionId)
	if err != nil {
		return err
	}
	if transaction == nil {
		// a station that restarted can stop a transaction that the CSMS never saw:
		// it is created so that the stop is recorded
		slog.Warn("stopping a transaction that is not known, recording it",
			slog.String("chargeStationId", chargeStationId), slog.String("transactionId", transactionId))
		err = s.TransactionStore.CreateTransaction(ctx, chargeStationId, transactionId, idToken, tokenType, nil, 0, false)
		if err != nil {
			return err
		}
		transaction = &store.Transaction{
			ChargeStationId: chargeStationId,
			TransactionId:   transactionId,
			IdToken:         idToken,
			TokenType:       tokenType,
		}
	}
	seqNo := transaction.StartSeqNo + transaction.UpdatedSeqNoCount + 1

	meterValues := convertTransactionData(req.TransactionData)
	if !hasTransactionEndEnergyRegister(meterValues) {
		// meterStop is the reading of the register, not the energy of the transaction
		meterValues = append(meterValues, energyRegisterMeterValue(stoppedAt, sampledValueContextTransactionEnd, req.MeterStop))
	}

	err = s.TransactionStore.EndTransaction(ctx, chargeStationId, transactionId, idToken, tokenType, meterValues, seqNo)
	if err != nil {
		return err
	}

	meterStop := int64(req.MeterStop)
	details := &store.TransactionDetails{
		StoppedAt:   &stoppedAt,
		MeterStopWh: &meterStop,
		StopReason:  &reason,
	}
	if meterStart, ok := transactionMeterStartWh(transaction); ok {
		if meterStop >= meterStart {
			energy := meterStop - meterStart
			details.EnergyWh = &energy
		} else {
			// the meter was replaced or reset during the transaction
			slog.Warn("meter stop is lower than meter start, the energy of the transaction is not known",
				slog.String("chargeStationId", chargeStationId), slog.String("transactionId", transactionId),
				slog.Int64("meterStart", meterStart), slog.Int64("meterStop", meterStop))
		}
	}
	err = s.TransactionStore.UpdateTransactionDetails(ctx, chargeStationId, transactionId, details)
	if err != nil && !errors.Is(err, store.ErrNotImplemented) {
		return err
	}

	// the event describes the transaction as it is stored now
	ended, err := s.TransactionStore.FindTransaction(ctx, chargeStationId, transactionId)
	if err != nil {
		return err
	}
	if ended == nil {
		ended = transaction
	}
	return s.Events.Emit(ctx, sessionCompletedEvent(chargeStationId, req.TransactionId, ended, details))
}

// sessionCompletedEvent is the session.completed event of an ended transaction, in the
// org of the transaction (sessionOrgId). The stop (time, meter, energy, reason) is what
// the handler wrote, details; the rest comes from the stored transaction. So a store
// that does not keep the details (Firestore) still gives the event its stop.
func sessionCompletedEvent(chargeStationId string, ocppTransactionId int, transaction *store.Transaction, details *store.TransactionDetails) events.Event {
	stoppedAt := details.StoppedAt.UTC()
	data := events.SessionCompletedData{
		StationKey:        chargeStationId,
		StationId:         events.StationId(chargeStationId),
		TransactionId:     transaction.TransactionId,
		OcppTransactionId: strconv.Itoa(ocppTransactionId),
		EvseId:            transaction.EvseId,
		ConnectorId:       transaction.ConnectorId,
		IdToken:           transaction.IdToken,
		TokenType:         transaction.TokenType,
		IdTokenGroup:      transaction.IdTokenGroup,
		StoppedAt:         stoppedAt,
		MeterStopWh:       details.MeterStopWh,
		EnergyWh:          details.EnergyWh,
		StopReason:        *details.StopReason,
	}
	if meterStart, ok := transactionMeterStartWh(transaction); ok {
		data.MeterStartWh = &meterStart
	}
	if transaction.StartedAt != nil {
		startedAt := transaction.StartedAt.UTC()
		data.StartedAt = &startedAt
		// a station clock that went backwards gives no duration
		if duration := int64(stoppedAt.Sub(startedAt) / time.Second); duration >= 0 {
			data.DurationS = &duration
		}
	}
	return events.Event{
		Type:         events.TypeSessionCompleted,
		OrgId:        sessionOrgId(transaction),
		StationKey:   chargeStationId,
		ResourceType: events.ResourceSession,
		ResourceId:   transaction.TransactionId,
		Actor:        events.StationActor(chargeStationId),
		Data:         data,
	}
}

// sessionOrgId returns the org of the session events of a transaction: the org that
// owns the transaction. A transaction keeps its org when its charge station moves to
// another org, so the events of a session that runs during the move still go to the org
// that the session belongs to.
//
// It is "" when the store gives the transaction no org: a store that does not keep the
// org of a transaction (Firestore), or the legacy org (store.LegacyOrgId), which is not
// an org of its own. The event then belongs to the org of the charge station, which the
// emitter resolves.
func sessionOrgId(transaction *store.Transaction) string {
	if transaction == nil || transaction.OrgId == store.LegacyOrgId {
		return ""
	}
	return transaction.OrgId
}

// transactionMeterStartWh returns the energy register (Wh) at the start of the
// transaction: the stored meter start or, when the store does not have it, the
// Transaction.Begin sample. ok=false when it is not known.
func transactionMeterStartWh(transaction *store.Transaction) (wh int64, ok bool) {
	if transaction.MeterStartWh != nil {
		return *transaction.MeterStartWh, true
	}
	for _, value := range transaction.MeterValues {
		for _, sv := range value.SampledValues {
			if sv.Context == nil || *sv.Context != sampledValueContextTransactionBegin {
				continue
			}
			if wh, ok := energyRegisterWh(sv); ok {
				return wh, true
			}
		}
	}
	return 0, false
}

// hasTransactionEndEnergyRegister reports whether the values have the reading of the
// energy register at the end of the transaction.
func hasTransactionEndEnergyRegister(values []store.MeterValue) bool {
	for _, value := range values {
		for _, sv := range value.SampledValues {
			if sv.Context != nil && *sv.Context == sampledValueContextTransactionEnd &&
				sv.Measurand != nil && *sv.Measurand == sampledValueDefaultMeasurand {
				return true
			}
		}
	}
	return false
}

// convertTransactionData converts the transactionData of a StopTransaction.req.
// Meter values and samples that cannot be stored are skipped (see convertMeterValue),
// so that they do not fail the message: the station retries a failed StopTransaction forever.
func convertTransactionData(transactionData []types.StopTransactionJsonTransactionDataElem) []store.MeterValue {
	var converted []store.MeterValue
	for _, meterValue := range transactionData {
		if mv, ok := convertMeterValue(meterValue.Timestamp, rawTransactionDataSamples(meterValue.SampledValue)); ok {
			converted = append(converted, mv)
		}
	}
	return converted
}
