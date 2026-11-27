// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	clockTest "k8s.io/utils/clock/testing"
)

// The tests in this file run the OCPP 1.6 StartTransaction and StopTransaction handlers
// against the PostgreSQL store: the store rejects a transaction id that is in use and
// does not end a transaction that it does not know, unlike the in-memory store that the
// handler tests use.

const ocpp16IdTag = "MYRFIDTAG"

// the clock of the CSMS is not the clock of the station
var ocpp16CsmsTime = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

func ocpp16StartHandler() ocpp16.StartTransactionHandler {
	return ocpp16.StartTransactionHandler{
		Clock:            clockTest.NewFakePassiveClock(ocpp16CsmsTime),
		TokenStore:       testStore,
		OrgResolver:      &tenancy.Resolver{Stations: testStore},
		TransactionStore: testStore,
		ReservationStore: testStore,
	}
}

func ocpp16StopHandler() ocpp16.StopTransactionHandler {
	return ocpp16.StopTransactionHandler{
		Clock:            clockTest.NewFakePassiveClock(ocpp16CsmsTime),
		TokenStore:       testStore,
		OrgResolver:      &tenancy.Resolver{Stations: testStore},
		TransactionStore: testStore,
	}
}

func ocpp16SetToken(t *testing.T, groupId *string) {
	t.Helper()
	require.NoError(t, testStore.SetToken(context.Background(), &store.Token{
		OrgId:       store.LegacyOrgId,
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         ocpp16IdTag,
		ContractId:  "GBTWK012345678V",
		Issuer:      "Thoughtworks",
		GroupId:     groupId,
		Valid:       true,
		CacheMode:   "ALWAYS",
		LastUpdated: time.Now().Format(time.RFC3339),
	}))
}

func ocpp16Start(t *testing.T, chargeStationId string, req *types.StartTransactionJson) *types.StartTransactionResponseJson {
	t.Helper()
	resp, err := ocpp16StartHandler().HandleCall(context.Background(), chargeStationId, req)
	require.NoError(t, err)
	return resp.(*types.StartTransactionResponseJson)
}

// ocpp16Sample is a sample of the energy register at the outlet, as the handlers store it.
func ocpp16Sample(sampleContext string, wh float64) store.SampledValue {
	return store.SampledValue{
		Context:       ptr(sampleContext),
		Location:      ptr("Outlet"),
		Measurand:     ptr("Energy.Active.Import.Register"),
		UnitOfMeasure: &store.UnitOfMeasure{Unit: "Wh", Multipler: 0},
		Value:         wh,
	}
}

func TestOcpp16Transaction_StartAndStop(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	ocpp16SetToken(t, ptr("FLEET1"))
	require.NoError(t, testStore.CreateReservation(ctx, &store.Reservation{
		ReservationId:   7,
		ChargeStationId: "cs001",
		ConnectorId:     2,
		IdTag:           ocpp16IdTag,
		ExpiryDate:      time.Date(2026, 11, 26, 11, 0, 0, 0, time.UTC),
		Status:          store.ReservationStatusAccepted,
		CreatedAt:       time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC),
	}))

	// the station reports its times with an offset and a fraction of a second
	started := ocpp16Start(t, "cs001", &types.StartTransactionJson{
		ConnectorId:   2,
		IdTag:         ocpp16IdTag,
		MeterStart:    3_000_000_000,
		ReservationId: ptr(7),
		Timestamp:     "2026-11-26T11:00:00.5+01:00",
	})
	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusAccepted, started.IdTagInfo.Status)
	assert.Positive(t, started.TransactionId)
	transactionId := ocpp16.ConvertToUUID(started.TransactionId)
	startedAt := time.Date(2026, 11, 26, 10, 0, 0, 500_000_000, time.UTC)

	got, err := testStore.FindTransaction(ctx, "cs001", transactionId)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, ocpp16IdTag, got.IdToken)
	assert.Equal(t, "ISO14443", got.TokenType)
	assert.Equal(t, 0, got.EndedSeqNo)
	assert.Equal(t, ptr(2), got.ConnectorId)
	assert.Equal(t, ptr(2), got.EvseId)
	require.NotNil(t, got.StartedAt)
	assert.True(t, startedAt.Equal(*got.StartedAt), "started at %s, want %s", got.StartedAt, startedAt)
	assert.Equal(t, ptr(int64(3_000_000_000)), got.MeterStartWh)
	assert.Equal(t, ptr("FLEET1"), got.IdTokenGroup)
	assert.Equal(t, ptr(7), got.ReservationId)
	assert.Nil(t, got.StoppedAt)
	assert.Nil(t, got.MeterStopWh)
	assert.Nil(t, got.EnergyWh)
	assert.Nil(t, got.StopReason)
	require.Len(t, got.MeterValues, 1)
	assert.Equal(t, []store.SampledValue{ocpp16Sample("Transaction.Begin", 3_000_000_000)}, got.MeterValues[0].SampledValues)

	// the reservation is consumed
	reservation, err := testStore.GetReservation(ctx, 7)
	require.NoError(t, err)
	require.NotNil(t, reservation)
	assert.Equal(t, store.ReservationStatusUsed, reservation.Status)

	// the transaction data has the register at the end in kWh: the meter stop is the one of the request
	idTag := ocpp16IdTag
	reason := types.StopTransactionJsonReasonEVDisconnected
	transactionEnd := types.StopTransactionJsonTransactionDataElemSampledValueElemContextTransactionEnd
	kWh := types.StopTransactionJsonTransactionDataElemSampledValueElemUnitKWh
	resp, err := ocpp16StopHandler().HandleCall(ctx, "cs001", &types.StopTransactionJson{
		TransactionId: started.TransactionId,
		IdTag:         &idTag,
		MeterStop:     3_000_007_500,
		Reason:        &reason,
		Timestamp:     "2026-11-26T11:30:00.25+01:00",
		TransactionData: []types.StopTransactionJsonTransactionDataElem{
			{
				Timestamp: "2026-11-26T10:15:00Z",
				SampledValue: []types.StopTransactionJsonTransactionDataElemSampledValueElem{
					{Value: "3000004000"},
				},
			},
			{
				Timestamp: "2026-11-26T10:30:00Z",
				SampledValue: []types.StopTransactionJsonTransactionDataElemSampledValueElem{
					{Value: "3000007.5", Context: &transactionEnd, Unit: &kWh},
				},
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, &types.StopTransactionResponseJson{
		IdTagInfo: &types.StopTransactionResponseJsonIdTagInfo{
			Status:      types.StopTransactionResponseJsonIdTagInfoStatusAccepted,
			ParentIdTag: ptr("FLEET1"),
		},
	}, resp)
	stoppedAt := time.Date(2026, 11, 26, 10, 30, 0, 250_000_000, time.UTC)

	got, err = testStore.FindTransaction(ctx, "cs001", transactionId)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Positive(t, got.EndedSeqNo)
	require.NotNil(t, got.StartedAt)
	assert.True(t, startedAt.Equal(*got.StartedAt), "started at %s, want %s", got.StartedAt, startedAt)
	require.NotNil(t, got.StoppedAt)
	assert.True(t, stoppedAt.Equal(*got.StoppedAt), "stopped at %s, want %s", got.StoppedAt, stoppedAt)
	assert.Equal(t, ptr(int64(3_000_000_000)), got.MeterStartWh)
	assert.Equal(t, ptr(int64(3_000_007_500)), got.MeterStopWh)
	assert.Equal(t, ptr(int64(7_500)), got.EnergyWh)
	assert.Equal(t, ptr("EVDisconnected"), got.StopReason)
	assert.Equal(t, ptr(2), got.ConnectorId)
	assert.Equal(t, ptr("FLEET1"), got.IdTokenGroup)
	assert.Equal(t, ptr(7), got.ReservationId)

	// the transaction data is stored and no end sample is added to it
	kWhEndSample := ocpp16Sample("Transaction.End", 3_000_007.5)
	kWhEndSample.UnitOfMeasure = &store.UnitOfMeasure{Unit: "kWh", Multipler: 0}
	require.Len(t, got.MeterValues, 3)
	assert.Equal(t, []store.SampledValue{ocpp16Sample("Transaction.Begin", 3_000_000_000)}, got.MeterValues[0].SampledValues)
	assert.Equal(t, []store.SampledValue{ocpp16Sample("Sample.Periodic", 3_000_004_000)}, got.MeterValues[1].SampledValues)
	assert.Equal(t, []store.SampledValue{kWhEndSample}, got.MeterValues[2].SampledValues)

	completed, total, err := testStore.ListTransactionsForChargeStation(ctx, "cs001", "completed", nil, nil, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, completed, 1)
}

func TestOcpp16Transaction_StopWithoutTransactionDataAddsTheEndSample(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	ocpp16SetToken(t, nil)
	started := ocpp16Start(t, "cs001", &types.StartTransactionJson{
		ConnectorId: 1,
		IdTag:       ocpp16IdTag,
		MeterStart:  1000,
		Timestamp:   "2026-11-26T10:00:00Z",
	})

	// no reason: Local
	_, err := ocpp16StopHandler().HandleCall(ctx, "cs001", &types.StopTransactionJson{
		TransactionId: started.TransactionId,
		MeterStop:     2500,
		Timestamp:     "2026-11-26T10:30:00Z",
	})
	require.NoError(t, err)

	got, err := testStore.FindTransaction(ctx, "cs001", ocpp16.ConvertToUUID(started.TransactionId))
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, ptr("Local"), got.StopReason)
	assert.Equal(t, ptr(int64(1500)), got.EnergyWh)
	assert.Nil(t, got.IdTokenGroup)
	assert.Nil(t, got.ReservationId)
	assert.Equal(t, []store.MeterValue{
		{Timestamp: "2026-11-26T10:00:00Z", SampledValues: []store.SampledValue{ocpp16Sample("Transaction.Begin", 1000)}},
		{Timestamp: "2026-11-26T10:30:00Z", SampledValues: []store.SampledValue{ocpp16Sample("Transaction.End", 2500)}},
	}, got.MeterValues)
}

func TestOcpp16Transaction_StartRetriesWhenTheIdIsInUse(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	ocpp16SetToken(t, nil)

	// the station already has a transaction with the id that the sequence returns next
	previousId, err := testStore.NextOcpp16TransactionId(ctx)
	require.NoError(t, err)
	usedId := previousId + 1
	err = testStore.CreateTransaction(ctx, "cs001", ocpp16.ConvertToUUID(usedId), "TOKEN001", "ISO14443",
		[]store.MeterValue{energyMeterValue("2026-11-26T09:00:00Z", 100)}, 0, false)
	require.NoError(t, err)

	started := ocpp16Start(t, "cs001", &types.StartTransactionJson{
		ConnectorId: 1,
		IdTag:       ocpp16IdTag,
		MeterStart:  1000,
		Timestamp:   "2026-11-26T10:00:00Z",
	})
	assert.Equal(t, usedId+1, started.TransactionId)

	// the new transaction has the details, the one that was there is unchanged
	got, err := testStore.FindTransaction(ctx, "cs001", ocpp16.ConvertToUUID(started.TransactionId))
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, ocpp16IdTag, got.IdToken)
	assert.Equal(t, ptr(1), got.ConnectorId)
	assert.Equal(t, ptr(int64(1000)), got.MeterStartWh)

	existing, err := testStore.FindTransaction(ctx, "cs001", ocpp16.ConvertToUUID(usedId))
	require.NoError(t, err)
	require.NotNil(t, existing)
	assert.Equal(t, "TOKEN001", existing.IdToken)
	assert.Nil(t, existing.ConnectorId)
	assert.Len(t, existing.MeterValues, 1)

	// another station can have a transaction with the same id
	err = testStore.CreateTransaction(ctx, "cs002", ocpp16.ConvertToUUID(started.TransactionId+1), "TOKEN001", "ISO14443",
		[]store.MeterValue{energyMeterValue("2026-11-26T09:00:00Z", 100)}, 0, false)
	require.NoError(t, err)
	next := ocpp16Start(t, "cs001", &types.StartTransactionJson{
		ConnectorId: 1,
		IdTag:       ocpp16IdTag,
		MeterStart:  1000,
		Timestamp:   "2026-11-26T10:00:00Z",
	})
	assert.Equal(t, started.TransactionId+1, next.TransactionId)
}

func TestOcpp16Transaction_UnknownIdTagStartsGetDifferentIds(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	req := &types.StartTransactionJson{
		ConnectorId: 1,
		IdTag:       "BADRFIDTAG",
		MeterStart:  1000,
		Timestamp:   "2026-11-26T10:00:00Z",
	}
	first := ocpp16Start(t, "cs001", req)
	second := ocpp16Start(t, "cs001", req)

	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusInvalid, first.IdTagInfo.Status)
	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusInvalid, second.IdTagInfo.Status)
	assert.Positive(t, first.TransactionId)
	assert.Greater(t, second.TransactionId, first.TransactionId)

	transactions, err := testStore.Transactions(ctx)
	require.NoError(t, err)
	assert.Len(t, transactions, 2)
}

func TestOcpp16Transaction_StopOfUnknownTransaction(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	ocpp16SetToken(t, nil)

	idTag := ocpp16IdTag
	reason := types.StopTransactionJsonReasonPowerLoss
	req := &types.StopTransactionJson{
		TransactionId: 4711,
		IdTag:         &idTag,
		MeterStop:     2500,
		Reason:        &reason,
		Timestamp:     "2026-11-26T10:30:00Z",
	}
	resp, err := ocpp16StopHandler().HandleCall(ctx, "cs001", req)
	require.NoError(t, err, "the station retries a StopTransaction.req that fails")
	assert.Equal(t, &types.StopTransactionResponseJson{
		IdTagInfo: &types.StopTransactionResponseJsonIdTagInfo{
			Status: types.StopTransactionResponseJsonIdTagInfoStatusAccepted,
		},
	}, resp)

	got, err := testStore.FindTransaction(ctx, "cs001", ocpp16.ConvertToUUID(4711))
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, ocpp16IdTag, got.IdToken)
	assert.Positive(t, got.EndedSeqNo)
	stoppedAt := time.Date(2026, 11, 26, 10, 30, 0, 0, time.UTC)
	require.NotNil(t, got.StoppedAt)
	assert.True(t, stoppedAt.Equal(*got.StoppedAt), "stopped at %s, want %s", got.StoppedAt, stoppedAt)
	assert.Equal(t, ptr(int64(2500)), got.MeterStopWh)
	assert.Equal(t, ptr("PowerLoss"), got.StopReason)
	// the meter start is not known, so the energy is not known either
	assert.Nil(t, got.MeterStartWh)
	assert.Nil(t, got.EnergyWh)
	assert.Equal(t, []store.MeterValue{
		{Timestamp: "2026-11-26T10:30:00Z", SampledValues: []store.SampledValue{ocpp16Sample("Transaction.End", 2500)}},
	}, got.MeterValues)

	active, total, err := testStore.ListTransactionsForChargeStation(ctx, "cs001", "active", nil, nil, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Empty(t, active)

	// a stop without idTag of a transaction that is not known
	_, err = ocpp16StopHandler().HandleCall(ctx, "cs001", &types.StopTransactionJson{
		TransactionId: 4712,
		MeterStop:     2600,
		Timestamp:     "2026-11-26T10:31:00Z",
	})
	require.NoError(t, err)
	got, err = testStore.FindTransaction(ctx, "cs001", ocpp16.ConvertToUUID(4712))
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Positive(t, got.EndedSeqNo)
	assert.Equal(t, ptr("Local"), got.StopReason)
}
