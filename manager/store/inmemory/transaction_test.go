// SPDX-License-Identifier: Apache-2.0

package inmemory_test

// Test for transaction.go

import (
	"context"
	"testing"
	"time"

	"k8s.io/utils/clock"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
)

func makePtr[T any](t T) *T {
	v := t
	return &v
}

const idToken = "SOMERFID"
const tokenType = "ISO14443"

func NewMeterValues(energyReactiveExportValue float64) []store.MeterValue {
	return []store.MeterValue{
		{
			Timestamp: time.Now().Format(time.RFC3339),
			SampledValues: []store.SampledValue{
				{
					Measurand: makePtr("Energy.Active.Import.Register"),
					Value:     energyReactiveExportValue,
				},
			},
		},
	}
}

func TestFindTransactionDoesNotExist(t *testing.T) {
	ctx := context.Background()

	transactionStore := inmemory.NewStore(clock.RealClock{})

	got, err := transactionStore.FindTransaction(ctx, "unknown", "ids")
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestCreateAndFindTransaction(t *testing.T) {
	ctx := context.Background()

	transactionStore := inmemory.NewStore(clock.RealClock{})

	meterValues := NewMeterValues(100)

	err := transactionStore.CreateTransaction(ctx, "cs001", "1234", idToken, tokenType, meterValues, 0, false)
	assert.NoError(t, err)

	got, err := transactionStore.FindTransaction(ctx, "cs001", "1234")
	assert.NoError(t, err)

	want := &store.Transaction{
		OrgId:           store.LegacyOrgId, // the charge station is not registered
		ChargeStationId: "cs001",
		TransactionId:   "1234",
		IdToken:         idToken,
		TokenType:       tokenType,
		MeterValues:     meterValues,
		StartSeqNo:      0,
	}

	assert.Equal(t, want, got)
}

func TestCreateTransactionWithExistingTransaction(t *testing.T) {
	ctx := context.Background()

	transactionStore := inmemory.NewStore(clock.RealClock{})

	meterValues1 := NewMeterValues(100)

	err := transactionStore.CreateTransaction(ctx, "cs002", "1234", idToken, tokenType, meterValues1, 0, false)
	assert.NoError(t, err)

	meterValues2 := NewMeterValues(200)

	err = transactionStore.CreateTransaction(ctx, "cs002", "1234", idToken, tokenType, meterValues2, 0, false)
	assert.NoError(t, err)

	got, err := transactionStore.FindTransaction(ctx, "cs002", "1234")
	assert.NoError(t, err)

	want := &store.Transaction{
		OrgId:           store.LegacyOrgId, // the charge station is not registered
		ChargeStationId: "cs002",
		TransactionId:   "1234",
		IdToken:         idToken,
		TokenType:       tokenType,
		MeterValues:     append(meterValues1, meterValues2...),
		StartSeqNo:      0,
	}

	assert.Equal(t, want, got)
}

func TestTransactionStoreUpdateCreatedTransaction(t *testing.T) {
	ctx := context.Background()

	transactionStore := inmemory.NewStore(clock.RealClock{})

	meterValues1 := NewMeterValues(100)

	err := transactionStore.CreateTransaction(ctx, "cs003", "1234", idToken, tokenType, meterValues1, 0, false)
	assert.NoError(t, err)

	meterValues2 := NewMeterValues(200)

	err = transactionStore.UpdateTransaction(ctx, "cs003", "1234", meterValues2)
	assert.NoError(t, err)

	got, err := transactionStore.FindTransaction(ctx, "cs003", "1234")
	assert.NoError(t, err)

	want := &store.Transaction{
		OrgId:             store.LegacyOrgId, // the charge station is not registered
		ChargeStationId:   "cs003",
		TransactionId:     "1234",
		IdToken:           idToken,
		TokenType:         tokenType,
		MeterValues:       append(meterValues1, meterValues2...),
		UpdatedSeqNoCount: 1,
	}

	assert.Equal(t, want, got)
}

func TestTransactionStoreEndTransaction(t *testing.T) {
	ctx := context.Background()

	transactionStore := inmemory.NewStore(clock.RealClock{})

	meterValues1 := NewMeterValues(100)
	err := transactionStore.CreateTransaction(ctx, "cs004", "1234", idToken, tokenType, meterValues1, 0, false)
	assert.NoError(t, err)

	meterValues2 := NewMeterValues(200)
	err = transactionStore.UpdateTransaction(ctx, "cs004", "1234", meterValues2)
	assert.NoError(t, err)

	meterValues3 := NewMeterValues(200)
	err = transactionStore.EndTransaction(ctx, "cs004", "1234", idToken, tokenType, meterValues3, 2)
	assert.NoError(t, err)

	got, err := transactionStore.FindTransaction(ctx, "cs004", "1234")
	assert.NoError(t, err)

	want := &store.Transaction{
		OrgId:             store.LegacyOrgId, // the charge station is not registered
		ChargeStationId:   "cs004",
		TransactionId:     "1234",
		IdToken:           idToken,
		TokenType:         tokenType,
		MeterValues:       append(meterValues1, append(meterValues2, meterValues3...)...),
		StartSeqNo:        0,
		EndedSeqNo:        2,
		UpdatedSeqNoCount: 1,
		Offline:           false,
	}

	assert.Equal(t, want, got)
}

func TestTransactionStoreEndNonExistingTransaction(t *testing.T) {
	ctx := context.Background()

	transactionStore := inmemory.NewStore(clock.RealClock{})

	meterValues := NewMeterValues(100)
	err := transactionStore.EndTransaction(ctx, "cs005", "1234", idToken, tokenType, meterValues, 2)
	assert.NoError(t, err)

	got, err := transactionStore.FindTransaction(ctx, "cs005", "1234")
	assert.NoError(t, err)

	want := &store.Transaction{
		OrgId:             store.LegacyOrgId, // the charge station is not registered
		ChargeStationId:   "cs005",
		TransactionId:     "1234",
		IdToken:           idToken,
		TokenType:         tokenType,
		MeterValues:       meterValues,
		StartSeqNo:        0,
		EndedSeqNo:        2,
		UpdatedSeqNoCount: 0,
		Offline:           false,
	}

	assert.Equal(t, want, got)
}

func TestUpdateTransactionDetails_MergesNonNilFields(t *testing.T) {
	ctx := context.Background()

	transactionStore := inmemory.NewStore(clock.RealClock{})

	err := transactionStore.CreateTransaction(ctx, "cs001", "1234", idToken, tokenType, NewMeterValues(100), 0, false)
	require.NoError(t, err)

	startedAt := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	stoppedAt := time.Date(2026, 11, 26, 11, 30, 0, 0, time.UTC)

	err = transactionStore.UpdateTransactionDetails(ctx, "cs001", "1234", &store.TransactionDetails{
		EvseId:        makePtr(1),
		ConnectorId:   makePtr(2),
		StartedAt:     &startedAt,
		StoppedAt:     &stoppedAt,
		MeterStartWh:  makePtr(int64(1000)),
		MeterStopWh:   makePtr(int64(8500)),
		EnergyWh:      makePtr(int64(7500)),
		StopReason:    makePtr("Local"),
		IdTokenGroup:  makePtr("GROUP001"),
		ReservationId: makePtr(42),
	})
	require.NoError(t, err)

	// a second update with a single field must keep everything else
	err = transactionStore.UpdateTransactionDetails(ctx, "cs001", "1234", &store.TransactionDetails{
		StopReason: makePtr("EVDisconnected"),
	})
	require.NoError(t, err)

	got, err := transactionStore.FindTransaction(ctx, "cs001", "1234")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, makePtr(1), got.EvseId)
	assert.Equal(t, makePtr(2), got.ConnectorId)
	assert.Equal(t, &startedAt, got.StartedAt)
	assert.Equal(t, &stoppedAt, got.StoppedAt)
	assert.Equal(t, makePtr(int64(1000)), got.MeterStartWh)
	assert.Equal(t, makePtr(int64(8500)), got.MeterStopWh)
	assert.Equal(t, makePtr(int64(7500)), got.EnergyWh)
	assert.Equal(t, makePtr("EVDisconnected"), got.StopReason)
	assert.Equal(t, makePtr("GROUP001"), got.IdTokenGroup)
	assert.Equal(t, makePtr(42), got.ReservationId)

	// the fields written by the other methods are untouched
	assert.Equal(t, idToken, got.IdToken)
	assert.Equal(t, tokenType, got.TokenType)
	assert.Len(t, got.MeterValues, 1)
}

func TestUpdateTransactionDetails_NewTransactionHasNoDetails(t *testing.T) {
	ctx := context.Background()

	transactionStore := inmemory.NewStore(clock.RealClock{})

	err := transactionStore.CreateTransaction(ctx, "cs001", "1234", idToken, tokenType, NewMeterValues(100), 0, false)
	require.NoError(t, err)

	// nil details and empty details change nothing
	require.NoError(t, transactionStore.UpdateTransactionDetails(ctx, "cs001", "1234", nil))
	require.NoError(t, transactionStore.UpdateTransactionDetails(ctx, "cs001", "1234", &store.TransactionDetails{}))

	got, err := transactionStore.FindTransaction(ctx, "cs001", "1234")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Nil(t, got.EvseId)
	assert.Nil(t, got.ConnectorId)
	assert.Nil(t, got.StartedAt)
	assert.Nil(t, got.StoppedAt)
	assert.Nil(t, got.MeterStartWh)
	assert.Nil(t, got.MeterStopWh)
	assert.Nil(t, got.EnergyWh)
	assert.Nil(t, got.StopReason)
	assert.Nil(t, got.IdTokenGroup)
	assert.Nil(t, got.ReservationId)
}

func TestUpdateTransactionDetails_NotFound(t *testing.T) {
	ctx := context.Background()

	transactionStore := inmemory.NewStore(clock.RealClock{})

	err := transactionStore.CreateTransaction(ctx, "cs001", "1234", idToken, tokenType, NewMeterValues(100), 0, false)
	require.NoError(t, err)

	// unknown transaction
	err = transactionStore.UpdateTransactionDetails(ctx, "cs001", "unknown", &store.TransactionDetails{
		StopReason: makePtr("Local"),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// the transaction of another charge station
	err = transactionStore.UpdateTransactionDetails(ctx, "cs002", "1234", &store.TransactionDetails{
		StopReason: makePtr("Local"),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// the update did not create a transaction
	got, err := transactionStore.FindTransaction(ctx, "cs002", "1234")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestUpdateTransactionDetails_DoesNotAliasInput(t *testing.T) {
	ctx := context.Background()

	transactionStore := inmemory.NewStore(clock.RealClock{})

	err := transactionStore.CreateTransaction(ctx, "cs001", "1234", idToken, tokenType, NewMeterValues(100), 0, false)
	require.NoError(t, err)

	connectorId := 2
	meterStartWh := int64(1000)
	stopReason := "Local"
	startedAt := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)

	err = transactionStore.UpdateTransactionDetails(ctx, "cs001", "1234", &store.TransactionDetails{
		ConnectorId:  &connectorId,
		MeterStartWh: &meterStartWh,
		StopReason:   &stopReason,
		StartedAt:    &startedAt,
	})
	require.NoError(t, err)

	// changing the caller's values afterwards must not change the stored transaction
	connectorId = 99
	meterStartWh = 99
	stopReason = "changed"
	startedAt = startedAt.Add(time.Hour)

	got, err := transactionStore.FindTransaction(ctx, "cs001", "1234")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, makePtr(2), got.ConnectorId)
	assert.Equal(t, makePtr(int64(1000)), got.MeterStartWh)
	assert.Equal(t, makePtr("Local"), got.StopReason)
	assert.Equal(t, makePtr(time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)), got.StartedAt)
}

func TestNextOcpp16TransactionId_Increments(t *testing.T) {
	ctx := context.Background()

	transactionStore := inmemory.NewStore(clock.RealClock{})

	for want := 1; want <= 3; want++ {
		got, err := transactionStore.NextOcpp16TransactionId(ctx)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestSameTransactionIdOnTwoStations(t *testing.T) {
	ctx := context.Background()

	transactionStore := inmemory.NewStore(clock.RealClock{})

	err := transactionStore.CreateTransaction(ctx, "cs001", "tx-1", "TOKEN001", tokenType, NewMeterValues(100), 0, false)
	require.NoError(t, err)
	err = transactionStore.CreateTransaction(ctx, "cs002", "tx-1", "TOKEN002", tokenType, NewMeterValues(200), 0, false)
	require.NoError(t, err)

	// changes to the transaction of one charge station leave the other one alone
	err = transactionStore.UpdateTransaction(ctx, "cs001", "tx-1", NewMeterValues(150))
	require.NoError(t, err)
	err = transactionStore.UpdateTransactionDetails(ctx, "cs001", "tx-1", &store.TransactionDetails{
		ConnectorId: makePtr(1),
	})
	require.NoError(t, err)

	got1, err := transactionStore.FindTransaction(ctx, "cs001", "tx-1")
	require.NoError(t, err)
	require.NotNil(t, got1)
	assert.Equal(t, "cs001", got1.ChargeStationId)
	assert.Equal(t, "TOKEN001", got1.IdToken)
	require.Len(t, got1.MeterValues, 2)
	assert.Equal(t, 100.0, got1.MeterValues[0].SampledValues[0].Value)
	assert.Equal(t, 150.0, got1.MeterValues[1].SampledValues[0].Value)
	assert.Equal(t, makePtr(1), got1.ConnectorId)

	got2, err := transactionStore.FindTransaction(ctx, "cs002", "tx-1")
	require.NoError(t, err)
	require.NotNil(t, got2)
	assert.Equal(t, "cs002", got2.ChargeStationId)
	assert.Equal(t, "TOKEN002", got2.IdToken)
	require.Len(t, got2.MeterValues, 1)
	assert.Equal(t, 200.0, got2.MeterValues[0].SampledValues[0].Value)
	assert.Nil(t, got2.ConnectorId)
}
