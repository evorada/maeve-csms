// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

type StopTransactionHandler struct {
	Clock            clock.PassiveClock
	TokenStore       store.TokenStore
	TransactionStore store.TransactionStore
}

func (s StopTransactionHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (response ocpp.Response, err error) {
	req := request.(*types.StopTransactionJson)

	reason := "*unknown*"
	if req.Reason != nil {
		reason = string(*req.Reason)
	}
	transactionId := ConvertToUUID(req.TransactionId)
	slog.Info("stopping transaction", slog.String("transactionId", transactionId), slog.String("reason", reason))

	var idTagInfo *types.StopTransactionResponseJsonIdTagInfo
	if req.IdTag != nil {
		status := types.StopTransactionResponseJsonIdTagInfoStatusInvalid
		tok, err := s.TokenStore.LookupToken(ctx, *req.IdTag)
		if err != nil {
			return nil, err
		}
		if tok != nil {
			status = types.StopTransactionResponseJsonIdTagInfoStatusAccepted
		}
		idTagInfo = &types.StopTransactionResponseJsonIdTagInfo{
			Status: status,
		}
	}

	transaction, err := s.TransactionStore.FindTransaction(ctx, chargeStationId, transactionId)
	if err != nil {
		return nil, err
	}
	seqNo := -1
	if transaction != nil {
		seqNo = transaction.StartSeqNo + transaction.UpdatedSeqNoCount + 1
	}

	var idToken, tokenType string
	if req.IdTag != nil {
		idToken = *req.IdTag
		tokenType = "ISO14443"
	}

	meterValues := convertTransactionData(req.TransactionData)

	var previousMeterValues []store.MeterValue
	if transaction != nil {
		previousMeterValues = transaction.MeterValues
	}
	meterValues = calculateTransactionEndOutletEnergy(s.Clock, meterValues, previousMeterValues, req.MeterStop)

	err = s.TransactionStore.EndTransaction(ctx, chargeStationId, transactionId, idToken, tokenType, meterValues, seqNo)
	if err != nil {
		return nil, err
	}

	return &types.StopTransactionResponseJson{
		IdTagInfo: idTagInfo,
	}, nil
}

func calculateTransactionEndOutletEnergy(clock clock.PassiveClock, transactionValues []store.MeterValue, previousValues []store.MeterValue, meterStop int) []store.MeterValue {
	if findOutletEnergyReading(transactionValues) {
		return transactionValues
	}

	if meterStart, ok := findTransactionBeginMeterValues(previousValues); ok {
		energyUsed := meterStop - meterStart
		transactionEndContext := "Transaction.End"
		outletLocation := "Outlet"
		energyRegisteredMeasurand := "Energy.Active.Import.Register"

		transactionValues = append(transactionValues, store.MeterValue{
			SampledValues: []store.SampledValue{
				{
					Context:   &transactionEndContext,
					Location:  &outletLocation,
					Measurand: &energyRegisteredMeasurand,
					Value:     float64(energyUsed),
				},
			},
			Timestamp: clock.Now().Format(time.RFC3339),
		})
	}

	return transactionValues
}

func findOutletEnergyReading(values []store.MeterValue) bool {
	for _, value := range values {
		for _, sv := range value.SampledValues {
			if sv.Context != nil && *sv.Context == "Transaction.End" &&
				sv.Measurand != nil && *sv.Measurand == "Energy.Active.Import.Register" &&
				sv.Location != nil && *sv.Location == "Outlet" {
				return true
			}
		}
	}
	return false
}

func findTransactionBeginMeterValues(values []store.MeterValue) (int, bool) {
	for _, value := range values {
		for _, sv := range value.SampledValues {
			if sv.Context != nil && *sv.Context == "Transaction.Begin" &&
				sv.Measurand != nil && *sv.Measurand == "MeterValue" &&
				sv.Location != nil && *sv.Location == "Outlet" {
				return int(sv.Value), true
			}
		}
	}

	return 0, false
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
