// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	clockTest "k8s.io/utils/clock/testing"
)

const (
	stopTransactionChargeStationId = "cs001"
	stopTransactionIdTag           = "MYRFIDTAG"

	// the time of the handler's clock: no stored time may be equal to it,
	// unless the request has no usable timestamp
	stopTransactionClockTime = "2023-06-15T16:10:00+01:00"
	// the times that the station reports
	stopTransactionStartTime = "2023-06-15T14:58:30Z"
	stopTransactionStopTime  = "2023-06-15T15:06:00Z"
)

// newStopTransactionHandler returns a handler that stores in a new in-memory store.
// The store has one token, stopTransactionIdTag.
func newStopTransactionHandler(t *testing.T) (handlers.StopTransactionHandler, *inmemory.Store) {
	t.Helper()

	now, err := time.Parse(time.RFC3339, stopTransactionClockTime)
	require.NoError(t, err)
	clk := clockTest.NewFakePassiveClock(now)
	engine := inmemory.NewStore(clk)

	err = engine.SetToken(context.Background(), &store.Token{
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         stopTransactionIdTag,
		ContractId:  "GBTWK012345678V",
		Issuer:      "Thoughtworks",
		Valid:       true,
		CacheMode:   "NEVER",
	})
	require.NoError(t, err)

	return handlers.StopTransactionHandler{
		Clock:            clk,
		TokenStore:       engine,
		TransactionStore: engine,
	}, engine
}

// startedTransaction starts a transaction with the StartTransaction handler, with a
// meter start of 50 Wh, and returns its OCPP 1.6 transaction id.
func startedTransaction(t *testing.T, engine *inmemory.Store) int {
	t.Helper()

	startHandler := handlers.StartTransactionHandler{
		Clock:            clockTest.NewFakePassiveClock(time.Now()),
		TokenStore:       engine,
		TransactionStore: engine,
		ReservationStore: engine,
	}
	resp := startTransaction(t, startHandler, stopTransactionChargeStationId, &types.StartTransactionJson{
		ConnectorId: 1,
		IdTag:       stopTransactionIdTag,
		MeterStart:  50,
		Timestamp:   stopTransactionStartTime,
	})
	return resp.TransactionId
}

func stopTransactionRequest(transactionId int) *types.StopTransactionJson {
	return &types.StopTransactionJson{
		MeterStop:     200,
		Timestamp:     stopTransactionStopTime,
		TransactionId: transactionId,
	}
}

// stopTransaction calls the handler and returns the stored transaction.
func stopTransaction(t *testing.T, handler handlers.StopTransactionHandler, engine store.TransactionStore, req *types.StopTransactionJson) *store.Transaction {
	t.Helper()

	_, err := handler.HandleCall(context.Background(), stopTransactionChargeStationId, req)
	require.NoError(t, err)

	found, err := engine.FindTransaction(context.Background(), stopTransactionChargeStationId, handlers.ConvertToUUID(req.TransactionId))
	require.NoError(t, err)
	require.NotNil(t, found)
	return found
}

// endMeterValue is the meter value that the handler stores for the meter stop.
func endMeterValue(timestamp string, wh float64) store.MeterValue {
	mv := beginMeterValue(timestamp, wh)
	sampleContext := "Transaction.End"
	mv.SampledValues[0].Context = &sampleContext
	return mv
}

// transactionDataSample is a sample of the transactionData of a StopTransaction.req as it is stored.
func transactionDataSample(sampleContext string, value float64, unit string) store.SampledValue {
	location := "Outlet"
	measurand := "Energy.Active.Import.Register"
	return store.SampledValue{
		Context:       &sampleContext,
		Location:      &location,
		Measurand:     &measurand,
		UnitOfMeasure: &store.UnitOfMeasure{Unit: unit, Multipler: 0},
		Value:         value,
	}
}

func TestStopTransactionHandler(t *testing.T) {
	handler, engine := newStopTransactionHandler(t)
	transactionId := startedTransaction(t, engine)

	idTag := stopTransactionIdTag
	reason := types.StopTransactionJsonReasonEVDisconnected
	periodicSampleContext := types.StopTransactionJsonTransactionDataElemSampledValueElemContextSamplePeriodic
	energyRegisterMeasurand := types.StopTransactionJsonTransactionDataElemSampledValueElemMeasurandEnergyActiveImportRegister
	outletLocation := types.StopTransactionJsonTransactionDataElemSampledValueElemLocationOutlet
	req := stopTransactionRequest(transactionId)
	req.IdTag = &idTag
	req.Reason = &reason
	req.TransactionData = []types.StopTransactionJsonTransactionDataElem{
		{
			SampledValue: []types.StopTransactionJsonTransactionDataElemSampledValueElem{
				{
					Context:   &periodicSampleContext,
					Measurand: &energyRegisterMeasurand,
					Location:  &outletLocation,
					Value:     "100",
				},
			},
			Timestamp: "2023-06-15T15:00:00Z",
		},
	}

	got, err := handler.HandleCall(context.Background(), stopTransactionChargeStationId, req)
	require.NoError(t, err)

	// the token must not be cached: it expires at the time of the handler's clock
	expiryDate := "2023-06-15T15:10:00Z"
	want := &types.StopTransactionResponseJson{
		IdTagInfo: &types.StopTransactionResponseJsonIdTagInfo{
			Status:     types.StopTransactionResponseJsonIdTagInfoStatusAccepted,
			ExpiryDate: &expiryDate,
		},
	}
	assert.Equal(t, want, got)

	found, err := engine.FindTransaction(context.Background(), stopTransactionChargeStationId, handlers.ConvertToUUID(transactionId))
	require.NoError(t, err)

	// the times of the station, not of the handler's clock
	startedAt := time.Date(2023, 6, 15, 14, 58, 30, 0, time.UTC)
	stoppedAt := time.Date(2023, 6, 15, 15, 6, 0, 0, time.UTC)
	connectorId := 1
	meterStart := int64(50)
	meterStop := int64(200)
	energy := int64(150)
	stopReason := "EVDisconnected"
	expected := &store.Transaction{
		ChargeStationId: stopTransactionChargeStationId,
		TransactionId:   handlers.ConvertToUUID(transactionId),
		IdToken:         stopTransactionIdTag,
		TokenType:       "ISO14443",
		MeterValues: []store.MeterValue{
			beginMeterValue(stopTransactionStartTime, 50),
			{
				Timestamp: "2023-06-15T15:00:00Z",
				SampledValues: []store.SampledValue{
					// OCPP 1.6 default unit of an energy measurand
					transactionDataSample("Sample.Periodic", 100, "Wh"),
				},
			},
			// the reading of the register at the end, not the energy of the transaction
			endMeterValue(stopTransactionStopTime, 200),
		},
		StartSeqNo:        0,
		EndedSeqNo:        1,
		UpdatedSeqNoCount: 0,
		Offline:           false,
		ConnectorId:       &connectorId,
		EvseId:            &connectorId,
		StartedAt:         &startedAt,
		StoppedAt:         &stoppedAt,
		MeterStartWh:      &meterStart,
		MeterStopWh:       &meterStop,
		EnergyWh:          &energy,
		StopReason:        &stopReason,
	}

	assert.Equal(t, expected, found)
}

func TestStopTransactionHandlerWithoutReasonStoresLocal(t *testing.T) {
	handler, engine := newStopTransactionHandler(t)
	transactionId := startedTransaction(t, engine)

	// OCPP 1.6: the reason may only be omitted when it is Local
	found := stopTransaction(t, handler, engine, stopTransactionRequest(transactionId))

	require.NotNil(t, found.StopReason)
	assert.Equal(t, "Local", *found.StopReason)
}

func TestStopTransactionHandlerWithoutIdTag(t *testing.T) {
	handler, engine := newStopTransactionHandler(t)
	transactionId := startedTransaction(t, engine)

	got, err := handler.HandleCall(context.Background(), stopTransactionChargeStationId, stopTransactionRequest(transactionId))
	require.NoError(t, err)

	// no idTag in the request, no idTagInfo in the response
	assert.Equal(t, &types.StopTransactionResponseJson{}, got)

	// the idTag that started the transaction is kept
	found, err := engine.FindTransaction(context.Background(), stopTransactionChargeStationId, handlers.ConvertToUUID(transactionId))
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, stopTransactionIdTag, found.IdToken)
}

func TestStopTransactionHandlerWithUnknownIdTag(t *testing.T) {
	handler, engine := newStopTransactionHandler(t)
	transactionId := startedTransaction(t, engine)

	idTag := "BADRFIDTAG"
	req := stopTransactionRequest(transactionId)
	req.IdTag = &idTag
	got, err := handler.HandleCall(context.Background(), stopTransactionChargeStationId, req)
	require.NoError(t, err)

	want := &types.StopTransactionResponseJson{
		IdTagInfo: &types.StopTransactionResponseJsonIdTagInfo{
			Status: types.StopTransactionResponseJsonIdTagInfoStatusInvalid,
		},
	}
	assert.Equal(t, want, got)

	// the transaction is stopped all the same
	found, err := engine.FindTransaction(context.Background(), stopTransactionChargeStationId, handlers.ConvertToUUID(transactionId))
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.NotNil(t, found.StoppedAt)
	assert.Equal(t, 1, found.EndedSeqNo)
}

func TestStopTransactionHandlerWithTokenThatIsNotAccepted(t *testing.T) {
	// one second before the time of the handler's clock
	expiredAt := time.Date(2023, 6, 15, 15, 9, 59, 0, time.UTC)
	groupId := "MYGROUP"

	tests := []struct {
		name string
		// change makes the token of the store unacceptable
		change     func(tok *store.Token)
		wantStatus types.StopTransactionResponseJsonIdTagInfoStatus
	}{
		{
			name:       "blocked token",
			change:     func(tok *store.Token) { tok.Valid = false },
			wantStatus: types.StopTransactionResponseJsonIdTagInfoStatusBlocked,
		},
		{
			name:       "expired token",
			change:     func(tok *store.Token) { tok.ExpiresAt = &expiredAt },
			wantStatus: types.StopTransactionResponseJsonIdTagInfoStatusExpired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, engine := newStopTransactionHandler(t)
			ctx := context.Background()
			transactionId := startedTransaction(t, engine)

			// the token stops being acceptable during the transaction
			tok, err := engine.LookupToken(ctx, stopTransactionIdTag)
			require.NoError(t, err)
			require.NotNil(t, tok)
			tok.GroupId = &groupId
			tt.change(tok)
			require.NoError(t, engine.SetToken(ctx, tok))

			idTag := stopTransactionIdTag
			req := stopTransactionRequest(transactionId)
			req.IdTag = &idTag
			got, err := handler.HandleCall(ctx, stopTransactionChargeStationId, req)
			require.NoError(t, err)

			// no expiry date: the token is not accepted
			want := &types.StopTransactionResponseJson{
				IdTagInfo: &types.StopTransactionResponseJsonIdTagInfo{
					Status:      tt.wantStatus,
					ParentIdTag: &groupId,
				},
			}
			assert.Equal(t, want, got)

			// the transaction is stopped all the same
			found, err := engine.FindTransaction(ctx, stopTransactionChargeStationId, handlers.ConvertToUUID(transactionId))
			require.NoError(t, err)
			require.NotNil(t, found)
			assert.NotNil(t, found.StoppedAt)
			assert.Equal(t, 1, found.EndedSeqNo)
		})
	}
}

func TestStopTransactionHandlerKeepsTheOffsetAndFractionOfTheStationTime(t *testing.T) {
	handler, engine := newStopTransactionHandler(t)
	transactionId := startedTransaction(t, engine)

	req := stopTransactionRequest(transactionId)
	req.Timestamp = "2023-06-15T17:06:00.75+02:00"
	found := stopTransaction(t, handler, engine, req)

	want := time.Date(2023, 6, 15, 15, 6, 0, 750_000_000, time.UTC)
	require.NotNil(t, found.StoppedAt)
	assert.True(t, want.Equal(*found.StoppedAt), "stopped at %s, want %s", found.StoppedAt, want)

	// the end sample has the same instant
	require.Len(t, found.MeterValues, 2)
	sampleTime, err := time.Parse(time.RFC3339, found.MeterValues[1].Timestamp)
	require.NoError(t, err)
	assert.True(t, want.Equal(sampleTime), "end sample at %s, want %s", sampleTime, want)
}

func TestStopTransactionHandlerWithInvalidTimestampUsesTheClock(t *testing.T) {
	handler, engine := newStopTransactionHandler(t)
	transactionId := startedTransaction(t, engine)

	req := stopTransactionRequest(transactionId)
	req.Timestamp = "today"
	found := stopTransaction(t, handler, engine, req)

	clockTime, err := time.Parse(time.RFC3339, stopTransactionClockTime)
	require.NoError(t, err)
	require.NotNil(t, found.StoppedAt)
	assert.True(t, clockTime.Equal(*found.StoppedAt), "stopped at %s, want %s", found.StoppedAt, clockTime)
	require.Len(t, found.MeterValues, 2)
	assert.Equal(t, endMeterValue(stopTransactionClockTime, 200), found.MeterValues[1])
}

func TestStopTransactionHandlerTransactionEndSample(t *testing.T) {
	transactionEnd := types.StopTransactionJsonTransactionDataElemSampledValueElemContextTransactionEnd
	transactionBegin := types.StopTransactionJsonTransactionDataElemSampledValueElemContextTransactionBegin
	energyRegister := types.StopTransactionJsonTransactionDataElemSampledValueElemMeasurandEnergyActiveImportRegister
	power := types.StopTransactionJsonTransactionDataElemSampledValueElemMeasurandPowerActiveImport
	kWh := types.StopTransactionJsonTransactionDataElemSampledValueElemUnitKWh
	const sampleTime = "2023-06-15T15:05:59Z"

	powerMeasurand := "Power.Active.Import"
	storedPowerSample := transactionDataSample("Transaction.End", 7400, "W")
	storedPowerSample.Measurand = &powerMeasurand
	storedPowerSample.UnitOfMeasure = nil

	tests := []struct {
		name            string
		transactionData []types.StopTransactionJsonTransactionDataElemSampledValueElem
		// the meter values that are stored after the begin sample
		want []store.MeterValue
	}{
		{
			name: "no transaction data: end sample appended",
			want: []store.MeterValue{endMeterValue(stopTransactionStopTime, 200)},
		},
		{
			name: "transaction end register in the transaction data: nothing appended",
			transactionData: []types.StopTransactionJsonTransactionDataElemSampledValueElem{
				{Context: &transactionEnd, Measurand: &energyRegister, Unit: &kWh, Value: "0.2"},
			},
			want: []store.MeterValue{
				{
					Timestamp:     sampleTime,
					SampledValues: []store.SampledValue{transactionDataSample("Transaction.End", 0.2, "kWh")},
				},
			},
		},
		{
			name: "transaction end sample without measurand is the register: nothing appended",
			transactionData: []types.StopTransactionJsonTransactionDataElemSampledValueElem{
				{Context: &transactionEnd, Value: "200"},
			},
			want: []store.MeterValue{
				{
					Timestamp:     sampleTime,
					SampledValues: []store.SampledValue{transactionDataSample("Transaction.End", 200, "Wh")},
				},
			},
		},
		{
			name: "transaction end sample of another measurand: end sample appended",
			transactionData: []types.StopTransactionJsonTransactionDataElemSampledValueElem{
				{Context: &transactionEnd, Measurand: &power, Value: "7400"},
			},
			want: []store.MeterValue{
				{
					Timestamp:     sampleTime,
					SampledValues: []store.SampledValue{storedPowerSample},
				},
				endMeterValue(stopTransactionStopTime, 200),
			},
		},
		{
			name: "register of another context: end sample appended",
			transactionData: []types.StopTransactionJsonTransactionDataElemSampledValueElem{
				{Context: &transactionBegin, Measurand: &energyRegister, Value: "50"},
			},
			want: []store.MeterValue{
				{
					Timestamp:     sampleTime,
					SampledValues: []store.SampledValue{transactionDataSample("Transaction.Begin", 50, "Wh")},
				},
				endMeterValue(stopTransactionStopTime, 200),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, engine := newStopTransactionHandler(t)
			transactionId := startedTransaction(t, engine)

			req := stopTransactionRequest(transactionId)
			if tt.transactionData != nil {
				req.TransactionData = []types.StopTransactionJsonTransactionDataElem{
					{Timestamp: sampleTime, SampledValue: tt.transactionData},
				}
			}
			found := stopTransaction(t, handler, engine, req)

			want := append([]store.MeterValue{beginMeterValue(stopTransactionStartTime, 50)}, tt.want...)
			assert.Equal(t, want, found.MeterValues)

			// the meter stop and the energy come from the request, whatever the transaction data
			require.NotNil(t, found.MeterStopWh)
			assert.Equal(t, int64(200), *found.MeterStopWh)
			require.NotNil(t, found.EnergyWh)
			assert.Equal(t, int64(150), *found.EnergyWh)
		})
	}
}

func TestStopTransactionHandlerSkipsSignedTransactionData(t *testing.T) {
	handler, engine := newStopTransactionHandler(t)
	transactionId := startedTransaction(t, engine)

	signedFormat := types.StopTransactionJsonTransactionDataElemSampledValueElemFormatSignedData
	transactionEndContext := types.StopTransactionJsonTransactionDataElemSampledValueElemContextTransactionEnd
	// the value is a hex encoded signature that happens to have only decimal digits,
	// so the sample must be skipped because of its format
	signedSample := types.StopTransactionJsonTransactionDataElemSampledValueElem{
		Context: &transactionEndContext,
		Format:  &signedFormat,
		Value:   "3045022100",
	}
	req := stopTransactionRequest(transactionId)
	req.TransactionData = []types.StopTransactionJsonTransactionDataElem{
		{
			// only the raw sample of this meter value can be stored
			SampledValue: []types.StopTransactionJsonTransactionDataElemSampledValueElem{
				signedSample,
				{Value: "100"},
			},
			Timestamp: "2023-06-15T15:00:00Z",
		},
		{
			// nothing of this meter value can be stored
			SampledValue: []types.StopTransactionJsonTransactionDataElemSampledValueElem{signedSample},
			Timestamp:    "2023-06-15T15:05:00Z",
		},
	}

	got, err := handler.HandleCall(context.Background(), stopTransactionChargeStationId, req)
	require.NoError(t, err)
	assert.Equal(t, &types.StopTransactionResponseJson{}, got)

	found, err := engine.FindTransaction(context.Background(), stopTransactionChargeStationId, handlers.ConvertToUUID(transactionId))
	require.NoError(t, err)
	require.NotNil(t, found)

	// the signed Transaction.End sample is not stored, so the end sample is appended
	expectedMeterValues := []store.MeterValue{
		beginMeterValue(stopTransactionStartTime, 50),
		{
			Timestamp:     "2023-06-15T15:00:00Z",
			SampledValues: []store.SampledValue{transactionDataSample("Sample.Periodic", 100, "Wh")},
		},
		endMeterValue(stopTransactionStopTime, 200),
	}
	assert.Equal(t, expectedMeterValues, found.MeterValues)
	assert.Equal(t, 1, found.EndedSeqNo)
}

func TestStopTransactionHandlerForUnknownTransaction(t *testing.T) {
	handler, engine := newStopTransactionHandler(t)

	idTag := stopTransactionIdTag
	reason := types.StopTransactionJsonReasonPowerLoss
	req := stopTransactionRequest(4711)
	req.IdTag = &idTag
	req.Reason = &reason

	got, err := handler.HandleCall(context.Background(), stopTransactionChargeStationId, req)
	require.NoError(t, err, "the station retries a StopTransaction.req that fails")

	// the token must not be cached: it expires at the time of the handler's clock
	expiryDate := "2023-06-15T15:10:00Z"
	want := &types.StopTransactionResponseJson{
		IdTagInfo: &types.StopTransactionResponseJsonIdTagInfo{
			Status:     types.StopTransactionResponseJsonIdTagInfoStatusAccepted,
			ExpiryDate: &expiryDate,
		},
	}
	assert.Equal(t, want, got)

	found, err := engine.FindTransaction(context.Background(), stopTransactionChargeStationId, handlers.ConvertToUUID(4711))
	require.NoError(t, err)

	stoppedAt := time.Date(2023, 6, 15, 15, 6, 0, 0, time.UTC)
	meterStop := int64(200)
	stopReason := "PowerLoss"
	expected := &store.Transaction{
		ChargeStationId: stopTransactionChargeStationId,
		TransactionId:   handlers.ConvertToUUID(4711),
		IdToken:         stopTransactionIdTag,
		TokenType:       "ISO14443",
		MeterValues: []store.MeterValue{
			endMeterValue(stopTransactionStopTime, 200),
		},
		// ended
		EndedSeqNo:  1,
		StoppedAt:   &stoppedAt,
		MeterStopWh: &meterStop,
		// the meter start is not known, so the energy is not known either
		EnergyWh:   nil,
		StopReason: &stopReason,
	}
	assert.Equal(t, expected, found)

	// it is listed as a completed transaction
	completed, total, err := engine.ListTransactionsForChargeStation(context.Background(), stopTransactionChargeStationId, "completed", nil, nil, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, completed, 1)
}

func TestStopTransactionHandlerForUnknownTransactionWithoutIdTag(t *testing.T) {
	handler, engine := newStopTransactionHandler(t)

	found := stopTransaction(t, handler, engine, stopTransactionRequest(4711))

	assert.Empty(t, found.IdToken)
	assert.Equal(t, 1, found.EndedSeqNo)
	require.NotNil(t, found.StopReason)
	assert.Equal(t, "Local", *found.StopReason)
}

func TestStopTransactionHandlerWithMeterStopBelowMeterStart(t *testing.T) {
	handler, engine := newStopTransactionHandler(t)
	transactionId := startedTransaction(t, engine)

	// the meter was replaced or reset during the transaction
	req := stopTransactionRequest(transactionId)
	req.MeterStop = 49
	found := stopTransaction(t, handler, engine, req)

	assert.Nil(t, found.EnergyWh)
	require.NotNil(t, found.MeterStopWh)
	assert.Equal(t, int64(49), *found.MeterStopWh)
	require.NotNil(t, found.StoppedAt)
	require.NotNil(t, found.StopReason)
}

func TestStopTransactionHandlerWithMeterStopEqualToMeterStart(t *testing.T) {
	handler, engine := newStopTransactionHandler(t)
	transactionId := startedTransaction(t, engine)

	req := stopTransactionRequest(transactionId)
	req.MeterStop = 50
	found := stopTransaction(t, handler, engine, req)

	require.NotNil(t, found.EnergyWh, "no energy was delivered: that is known")
	assert.Equal(t, int64(0), *found.EnergyWh)
}

// TestStopTransactionHandlerReadsMeterStartFromTheBeginSample covers a transaction
// whose meter start is not stored with the transaction details.
func TestStopTransactionHandlerReadsMeterStartFromTheBeginSample(t *testing.T) {
	kWhBeginSample := beginMeterValue(stopTransactionStartTime, 1.5)
	kWhBeginSample.SampledValues[0].UnitOfMeasure = &store.UnitOfMeasure{Unit: "kWh", Multipler: 0}

	// what StartTransaction stored before the energy register was used for the meter start
	legacyBeginSample := beginMeterValue(stopTransactionStartTime, 50)
	legacyMeasurand := "MeterValue"
	legacyBeginSample.SampledValues[0].Measurand = &legacyMeasurand

	periodicSample := beginMeterValue(stopTransactionStartTime, 50)
	periodicContext := "Sample.Periodic"
	periodicSample.SampledValues[0].Context = &periodicContext

	tests := []struct {
		name        string
		meterValues []store.MeterValue
		meterStop   int
		wantEnergy  *int64
	}{
		{
			name:        "begin sample in Wh",
			meterValues: []store.MeterValue{beginMeterValue(stopTransactionStartTime, 50)},
			meterStop:   200,
			wantEnergy:  int64Ptr(150),
		},
		{
			name:        "begin sample in kWh",
			meterValues: []store.MeterValue{kWhBeginSample},
			meterStop:   2000,
			wantEnergy:  int64Ptr(500),
		},
		{
			name:        "begin sample after another sample",
			meterValues: []store.MeterValue{periodicSample, beginMeterValue(stopTransactionStartTime, 80)},
			meterStop:   200,
			wantEnergy:  int64Ptr(120),
		},
		{
			name:        "begin sample is not an energy register",
			meterValues: []store.MeterValue{legacyBeginSample},
			meterStop:   200,
			wantEnergy:  nil,
		},
		{
			name:        "no begin sample",
			meterValues: []store.MeterValue{periodicSample},
			meterStop:   200,
			wantEnergy:  nil,
		},
		{
			name:        "no samples",
			meterValues: nil,
			meterStop:   200,
			wantEnergy:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, engine := newStopTransactionHandler(t)
			err := engine.CreateTransaction(context.Background(), stopTransactionChargeStationId, handlers.ConvertToUUID(42),
				stopTransactionIdTag, "ISO14443", tt.meterValues, 0, false)
			require.NoError(t, err)

			req := stopTransactionRequest(42)
			req.MeterStop = tt.meterStop
			found := stopTransaction(t, handler, engine, req)

			assert.Equal(t, tt.wantEnergy, found.EnergyWh)
			require.NotNil(t, found.MeterStopWh)
			assert.Equal(t, int64(tt.meterStop), *found.MeterStopWh)
		})
	}
}

// TestStopTransactionHandlerWithStoreWithoutDetails covers the Firestore behaviour:
// the details are not stored, the transaction is ended all the same.
func TestStopTransactionHandlerWithStoreWithoutDetails(t *testing.T) {
	handler, engine := newStopTransactionHandler(t)
	transactionId := startedTransaction(t, engine)
	handler.TransactionStore = &faultyTransactionStore{
		TransactionStore: engine,
		updateDetailsErr: fmt.Errorf("UpdateTransactionDetails: %w", store.ErrNotImplemented),
	}

	found := stopTransaction(t, handler, engine, stopTransactionRequest(transactionId))

	assert.Equal(t, 1, found.EndedSeqNo)
	assert.Equal(t, []store.MeterValue{
		beginMeterValue(stopTransactionStartTime, 50),
		endMeterValue(stopTransactionStopTime, 200),
	}, found.MeterValues)
	assert.Nil(t, found.StoppedAt)
	assert.Nil(t, found.StopReason)
}

// endFailingTransactionStore fails EndTransaction.
type endFailingTransactionStore struct {
	store.TransactionStore
	err error
}

func (s endFailingTransactionStore) EndTransaction(context.Context, string, string, string, string, []store.MeterValue, int) error {
	return s.err
}

func TestStopTransactionHandlerReturnsStoreErrors(t *testing.T) {
	storeErr := errors.New("store failed")

	tests := []struct {
		name string
		// known says whether the transaction that is stopped exists
		known bool
		store func(engine *inmemory.Store) store.TransactionStore
	}{
		{
			name:  "ending the transaction fails",
			known: true,
			store: func(engine *inmemory.Store) store.TransactionStore {
				return endFailingTransactionStore{TransactionStore: engine, err: storeErr}
			},
		},
		{
			name:  "transaction details update fails",
			known: true,
			store: func(engine *inmemory.Store) store.TransactionStore {
				return &faultyTransactionStore{TransactionStore: engine, updateDetailsErr: storeErr}
			},
		},
		{
			name:  "recording an unknown transaction fails",
			known: false,
			store: func(engine *inmemory.Store) store.TransactionStore {
				return &faultyTransactionStore{TransactionStore: engine, createErrs: []error{storeErr}}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, engine := newStopTransactionHandler(t)
			transactionId := 4711
			if tt.known {
				transactionId = startedTransaction(t, engine)
			}
			handler.TransactionStore = tt.store(engine)

			resp, err := handler.HandleCall(context.Background(), stopTransactionChargeStationId, stopTransactionRequest(transactionId))

			require.ErrorIs(t, err, storeErr)
			assert.Nil(t, resp)
		})
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}
