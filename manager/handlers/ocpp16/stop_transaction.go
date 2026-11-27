// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"errors"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

// StopTransactionHandler records the end of a transaction. A StopTransaction.req is
// always acknowledged, also for a transaction that the CSMS does not know: the station
// retries a StopTransaction.req that fails.
type StopTransactionHandler struct {
	Clock            clock.PassiveClock
	TokenStore       store.TokenStore
	OrgResolver      tenancy.StationOrgResolver
	TransactionStore store.TransactionStore
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
		auth, err := authorizeIdTag(ctx, s.TokenStore, s.OrgResolver, s.Clock, chargeStationId, *req.IdTag)
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
		tokenType = "ISO14443"
	}

	transaction, err := s.TransactionStore.FindTransaction(ctx, chargeStationId, transactionId)
	if err != nil {
		return nil, err
	}
	if transaction == nil {
		// a station that restarted can stop a transaction that the CSMS never saw:
		// it is created so that the stop is recorded
		slog.Warn("stopping a transaction that is not known, recording it",
			slog.String("chargeStationId", chargeStationId), slog.String("transactionId", transactionId))
		err = s.TransactionStore.CreateTransaction(ctx, chargeStationId, transactionId, idToken, tokenType, nil, 0, false)
		if err != nil {
			return nil, err
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
		return nil, err
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
		return nil, err
	}

	return &types.StopTransactionResponseJson{
		IdTagInfo: idTagInfo,
	}, nil
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
