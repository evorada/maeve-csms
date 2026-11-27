// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/postgres"
)

func TestTransaction_CreateAndFind(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	// Create auth first (FK)
	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	meterValues := []store.MeterValue{
		{
			Timestamp: "2026-01-01T00:00:00Z",
			SampledValues: []store.SampledValue{
				{Value: 0},
			},
		},
	}

	err = testStore.CreateTransaction(ctx, "cs001", "tx001", "TOKEN001", "RFID", meterValues, 1, false)
	require.NoError(t, err)

	got, err := testStore.FindTransaction(ctx, "cs001", "tx001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "cs001", got.ChargeStationId)
	assert.Equal(t, "tx001", got.TransactionId)
	assert.Equal(t, "TOKEN001", got.IdToken)
	assert.False(t, got.Offline)
}

func TestTransaction_FindNotFound(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	got, err := testStore.FindTransaction(ctx, "cs001", "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestTransaction_UpdateMeterValues(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	err = testStore.CreateTransaction(ctx, "cs001", "tx001", "TOKEN001", "RFID", nil, 1, false)
	require.NoError(t, err)

	meterValues := []store.MeterValue{
		{
			Timestamp: "2026-01-01T01:00:00Z",
			SampledValues: []store.SampledValue{
				{Value: 10.5},
			},
		},
	}

	err = testStore.UpdateTransaction(ctx, "cs001", "tx001", meterValues)
	require.NoError(t, err)

	got, err := testStore.FindTransaction(ctx, "cs001", "tx001")
	require.NoError(t, err)
	require.NotNil(t, got)
}

func TestTransaction_End(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	err = testStore.CreateTransaction(ctx, "cs001", "tx001", "TOKEN001", "RFID", nil, 1, false)
	require.NoError(t, err)

	meterValues := []store.MeterValue{
		{
			Timestamp: "2026-01-01T02:00:00Z",
			SampledValues: []store.SampledValue{
				{Value: 25.0},
			},
		},
	}

	err = testStore.EndTransaction(ctx, "cs001", "tx001", "TOKEN001", "RFID", meterValues, 2)
	require.NoError(t, err)
}

func TestTransaction_UpdateCost(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	err = testStore.CreateTransaction(ctx, "cs001", "tx001", "TOKEN001", "RFID", nil, 1, false)
	require.NoError(t, err)

	err = testStore.UpdateTransactionCost(ctx, "cs001", "tx001", 15.50)
	require.NoError(t, err)

	got, err := testStore.FindTransaction(ctx, "cs001", "tx001")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.LastCost)
	assert.InDelta(t, 15.50, *got.LastCost, 0.01)
}

func TestTransaction_ListAll(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	err = testStore.CreateTransaction(ctx, "cs001", "tx001", "TOKEN001", "RFID", nil, 1, false)
	require.NoError(t, err)
	err = testStore.CreateTransaction(ctx, "cs001", "tx002", "TOKEN002", "RFID", nil, 1, false)
	require.NoError(t, err)

	results, err := testStore.Transactions(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 2)
}

func ptr[T any](v T) *T {
	return &v
}

// energyMeterValue returns a meter value with an energy register reading.
func energyMeterValue(timestamp string, wh float64) store.MeterValue {
	return store.MeterValue{
		Timestamp: timestamp,
		SampledValues: []store.SampledValue{
			{Measurand: ptr("Energy.Active.Import.Register"), Value: wh},
		},
	}
}

func allTransactionDetails() (*store.TransactionDetails, time.Time, time.Time) {
	startedAt := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	stoppedAt := time.Date(2026, 11, 26, 11, 30, 0, 0, time.UTC)
	return &store.TransactionDetails{
		EvseId:        ptr(1),
		ConnectorId:   ptr(2),
		StartedAt:     &startedAt,
		StoppedAt:     &stoppedAt,
		MeterStartWh:  ptr(int64(1000)),
		MeterStopWh:   ptr(int64(8500)),
		EnergyWh:      ptr(int64(7500)),
		StopReason:    ptr("Local"),
		IdTokenGroup:  ptr("GROUP001"),
		ReservationId: ptr(42),
	}, startedAt, stoppedAt
}

func assertAllTransactionDetails(t *testing.T, got *store.Transaction, startedAt, stoppedAt time.Time) {
	t.Helper()
	require.NotNil(t, got)
	assert.Equal(t, ptr(1), got.EvseId)
	assert.Equal(t, ptr(2), got.ConnectorId)
	require.NotNil(t, got.StartedAt)
	assert.True(t, startedAt.Equal(*got.StartedAt), "started at %s", got.StartedAt)
	require.NotNil(t, got.StoppedAt)
	assert.True(t, stoppedAt.Equal(*got.StoppedAt), "stopped at %s", got.StoppedAt)
	assert.Equal(t, ptr(int64(1000)), got.MeterStartWh)
	assert.Equal(t, ptr(int64(8500)), got.MeterStopWh)
	assert.Equal(t, ptr(int64(7500)), got.EnergyWh)
	assert.Equal(t, ptr("Local"), got.StopReason)
	assert.Equal(t, ptr("GROUP001"), got.IdTokenGroup)
	assert.Equal(t, ptr(42), got.ReservationId)
}

func TestTransaction_UpdateDetails_AllFieldsAreReadBack(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	err = testStore.CreateTransaction(ctx, "cs001", "tx001", "TOKEN001", "RFID",
		[]store.MeterValue{{Timestamp: "2026-11-26T09:59:00Z", SampledValues: []store.SampledValue{{Value: 0}}}}, 1, false)
	require.NoError(t, err)

	details, startedAt, stoppedAt := allTransactionDetails()
	err = testStore.UpdateTransactionDetails(ctx, "cs001", "tx001", details)
	require.NoError(t, err)

	got, err := testStore.FindTransaction(ctx, "cs001", "tx001")
	require.NoError(t, err)
	assertAllTransactionDetails(t, got, startedAt, stoppedAt)
	// the fields written by the other methods are untouched
	assert.Equal(t, "TOKEN001", got.IdToken)
	assert.Equal(t, "RFID", got.TokenType)
	assert.Len(t, got.MeterValues, 1)

	listed, total, err := testStore.ListTransactionsForChargeStation(ctx, "cs001", "all", nil, nil, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, listed, 1)
	assertAllTransactionDetails(t, listed[0], startedAt, stoppedAt)

	all, err := testStore.Transactions(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assertAllTransactionDetails(t, all[0], startedAt, stoppedAt)
}

func TestTransaction_UpdateDetails_PartialUpdateKeepsEarlierFields(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.CreateTransaction(ctx, "cs001", "tx001", "TOKEN001", "RFID", nil, 1, false)
	require.NoError(t, err)

	details, startedAt, stoppedAt := allTransactionDetails()
	err = testStore.UpdateTransactionDetails(ctx, "cs001", "tx001", details)
	require.NoError(t, err)

	// nil details and empty details change nothing
	require.NoError(t, testStore.UpdateTransactionDetails(ctx, "cs001", "tx001", nil))
	require.NoError(t, testStore.UpdateTransactionDetails(ctx, "cs001", "tx001", &store.TransactionDetails{}))

	got, err := testStore.FindTransaction(ctx, "cs001", "tx001")
	require.NoError(t, err)
	assertAllTransactionDetails(t, got, startedAt, stoppedAt)

	// a single field replaces only that field
	err = testStore.UpdateTransactionDetails(ctx, "cs001", "tx001", &store.TransactionDetails{
		StopReason: ptr("EVDisconnected"),
	})
	require.NoError(t, err)

	got, err = testStore.FindTransaction(ctx, "cs001", "tx001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, ptr("EVDisconnected"), got.StopReason)
	assert.Equal(t, ptr(1), got.EvseId)
	assert.Equal(t, ptr(2), got.ConnectorId)
	assert.Equal(t, ptr(int64(1000)), got.MeterStartWh)
	assert.Equal(t, ptr(int64(8500)), got.MeterStopWh)
	assert.Equal(t, ptr(int64(7500)), got.EnergyWh)
	assert.Equal(t, ptr("GROUP001"), got.IdTokenGroup)
	assert.Equal(t, ptr(42), got.ReservationId)
	require.NotNil(t, got.StartedAt)
	assert.True(t, startedAt.Equal(*got.StartedAt))
	require.NotNil(t, got.StoppedAt)
	assert.True(t, stoppedAt.Equal(*got.StoppedAt))
}

func TestTransaction_UpdateDetails_NotFound(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.CreateTransaction(ctx, "cs001", "tx001", "TOKEN001", "RFID", nil, 1, false)
	require.NoError(t, err)

	// unknown transaction
	err = testStore.UpdateTransactionDetails(ctx, "cs001", "unknown", &store.TransactionDetails{StopReason: ptr("Local")})
	require.Error(t, err)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// the transaction of another charge station
	err = testStore.UpdateTransactionDetails(ctx, "cs002", "tx001", &store.TransactionDetails{StopReason: ptr("Local")})
	require.Error(t, err)
	assert.ErrorIs(t, err, store.ErrNotFound)

	got, err := testStore.FindTransaction(ctx, "cs001", "tx001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Nil(t, got.StopReason)
}

func TestTransaction_CreateDerivesStartFromFirstMeterValue(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	// the timestamp has an offset: it must be stored as the same instant
	err := testStore.CreateTransaction(ctx, "cs001", "with-energy", "TOKEN001", "RFID",
		[]store.MeterValue{energyMeterValue("2026-11-26T11:00:00+01:00", 1234)}, 1, false)
	require.NoError(t, err)

	got, err := testStore.FindTransaction(ctx, "cs001", "with-energy")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, ptr(int64(1234)), got.MeterStartWh)
	require.NotNil(t, got.StartedAt)
	assert.True(t, time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC).Equal(*got.StartedAt), "started at %s", got.StartedAt)
	// nothing else is known yet
	assert.Nil(t, got.EvseId)
	assert.Nil(t, got.ConnectorId)
	assert.Nil(t, got.StoppedAt)
	assert.Nil(t, got.MeterStopWh)
	assert.Nil(t, got.EnergyWh)
	assert.Nil(t, got.StopReason)
	assert.Nil(t, got.IdTokenGroup)
	assert.Nil(t, got.ReservationId)

	// without an energy register reading the meter start is not known (not 0)
	err = testStore.CreateTransaction(ctx, "cs001", "without-energy", "TOKEN001", "RFID",
		[]store.MeterValue{{Timestamp: "2026-11-26T10:00:00Z", SampledValues: []store.SampledValue{{Measurand: ptr("MeterValue"), Value: 55}}}}, 1, false)
	require.NoError(t, err)

	got, err = testStore.FindTransaction(ctx, "cs001", "without-energy")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Nil(t, got.MeterStartWh)
}

func TestTransaction_SameIdOnTwoStations(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.CreateTransaction(ctx, "cs001", "1", "TOKEN001", "RFID",
		[]store.MeterValue{energyMeterValue("2026-11-26T10:00:00Z", 100)}, 1, false)
	require.NoError(t, err)
	err = testStore.CreateTransaction(ctx, "cs002", "1", "TOKEN002", "RFID",
		[]store.MeterValue{energyMeterValue("2026-11-26T10:05:00Z", 200)}, 1, false)
	require.NoError(t, err)

	// update, details and end on cs001 only
	err = testStore.UpdateTransaction(ctx, "cs001", "1", []store.MeterValue{energyMeterValue("2026-11-26T10:10:00Z", 150)})
	require.NoError(t, err)
	err = testStore.UpdateTransactionDetails(ctx, "cs001", "1", &store.TransactionDetails{
		ConnectorId: ptr(1),
		StopReason:  ptr("Local"),
	})
	require.NoError(t, err)
	err = testStore.EndTransaction(ctx, "cs001", "1", "TOKEN001", "RFID",
		[]store.MeterValue{energyMeterValue("2026-11-26T10:20:00Z", 180)}, 3)
	require.NoError(t, err)

	got1, err := testStore.FindTransaction(ctx, "cs001", "1")
	require.NoError(t, err)
	require.NotNil(t, got1)
	assert.Equal(t, "cs001", got1.ChargeStationId)
	assert.Equal(t, "TOKEN001", got1.IdToken)
	require.Len(t, got1.MeterValues, 3)
	assert.Equal(t, 100.0, got1.MeterValues[0].SampledValues[0].Value)
	assert.Equal(t, 150.0, got1.MeterValues[1].SampledValues[0].Value)
	assert.Equal(t, 180.0, got1.MeterValues[2].SampledValues[0].Value)
	assert.Equal(t, ptr(1), got1.ConnectorId)
	assert.Equal(t, ptr("Local"), got1.StopReason)
	assert.Equal(t, ptr(int64(100)), got1.MeterStartWh)
	assert.Equal(t, ptr(int64(180)), got1.MeterStopWh)
	assert.NotNil(t, got1.StoppedAt)
	assert.NotZero(t, got1.EndedSeqNo)

	// the transaction with the same id on cs002 is unchanged and has only its own meter values
	got2, err := testStore.FindTransaction(ctx, "cs002", "1")
	require.NoError(t, err)
	require.NotNil(t, got2)
	assert.Equal(t, "cs002", got2.ChargeStationId)
	assert.Equal(t, "TOKEN002", got2.IdToken)
	require.Len(t, got2.MeterValues, 1)
	assert.Equal(t, 200.0, got2.MeterValues[0].SampledValues[0].Value)
	assert.Nil(t, got2.ConnectorId)
	assert.Nil(t, got2.StopReason)
	assert.Equal(t, ptr(int64(200)), got2.MeterStartWh)
	assert.Nil(t, got2.MeterStopWh)
	assert.Nil(t, got2.StoppedAt)
	assert.Zero(t, got2.EndedSeqNo)
	assert.Zero(t, got2.UpdatedSeqNoCount)

	// each charge station lists its own transaction
	listed, total, err := testStore.ListTransactionsForChargeStation(ctx, "cs002", "active", nil, nil, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, listed, 1)
	assert.Equal(t, "cs002", listed[0].ChargeStationId)
	require.Len(t, listed[0].MeterValues, 1)
}

func TestTransaction_CreateDuplicate_AlreadyExists(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.CreateTransaction(ctx, "cs001", "tx001", "TOKEN001", "RFID",
		[]store.MeterValue{energyMeterValue("2026-11-26T10:00:00Z", 100)}, 1, false)
	require.NoError(t, err)

	err = testStore.CreateTransaction(ctx, "cs001", "tx001", "TOKEN002", "RFID",
		[]store.MeterValue{energyMeterValue("2026-11-26T10:05:00Z", 200)}, 1, false)
	require.Error(t, err)
	assert.ErrorIs(t, err, store.ErrAlreadyExists)

	// the existing transaction is unchanged: no token change, no extra meter values
	got, err := testStore.FindTransaction(ctx, "cs001", "tx001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "TOKEN001", got.IdToken)
	require.Len(t, got.MeterValues, 1)
	assert.Equal(t, 100.0, got.MeterValues[0].SampledValues[0].Value)
}

func TestTransaction_CreateDuplicate_DoesNotAbortDatabaseTransaction(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	tx, err := testPool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	queries := postgres.New(tx)
	params := postgres.CreateTransactionParams{
		ID:              "tx001",
		ChargeStationID: "cs001",
		TokenUid:        "TOKEN001",
		TokenType:       "RFID",
		StartTimestamp:  pgtype.Timestamp{Time: time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC), Valid: true},
	}

	_, err = queries.CreateTransaction(ctx, params)
	require.NoError(t, err)

	// the duplicate returns no row instead of raising a unique violation ...
	_, err = queries.CreateTransaction(ctx, params)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	// ... so the next statements in the same database transaction still succeed
	_, err = queries.GetTransaction(ctx, postgres.GetTransactionParams{ChargeStationID: "cs001", ID: "tx001"})
	require.NoError(t, err)
	params.ID = "tx002"
	_, err = queries.CreateTransaction(ctx, params)
	require.NoError(t, err)

	require.NoError(t, tx.Commit(ctx))

	for _, id := range []string{"tx001", "tx002"} {
		got, err := testStore.FindTransaction(ctx, "cs001", id)
		require.NoError(t, err)
		assert.NotNil(t, got, id)
	}
}

func TestTransaction_MeterValueTimestampWithOffsetIsStoredAsInstant(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	// three instants, one hour apart, written with different UTC offsets
	err := testStore.CreateTransaction(ctx, "cs001", "tx001", "TOKEN001", "RFID",
		[]store.MeterValue{energyMeterValue("2026-11-26T11:00:00+01:00", 100)}, 1, false)
	require.NoError(t, err)
	err = testStore.UpdateTransaction(ctx, "cs001", "tx001",
		[]store.MeterValue{energyMeterValue("2026-11-26T06:00:00-05:00", 200)})
	require.NoError(t, err)
	err = testStore.EndTransaction(ctx, "cs001", "tx001", "TOKEN001", "RFID",
		[]store.MeterValue{energyMeterValue("2026-11-26T12:00:00Z", 300)}, 2)
	require.NoError(t, err)

	got, err := testStore.FindTransaction(ctx, "cs001", "tx001")
	require.NoError(t, err)
	require.NotNil(t, got)

	// read back in the order of the instants
	want := []store.MeterValue{
		energyMeterValue("2026-11-26T10:00:00Z", 100),
		energyMeterValue("2026-11-26T11:00:00Z", 200),
		energyMeterValue("2026-11-26T12:00:00Z", 300),
	}
	assert.Equal(t, want, got.MeterValues)
}

func TestTransaction_End_StopReason(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	// never set: the end of the transaction does not invent a stop reason
	err := testStore.CreateTransaction(ctx, "cs001", "no-reason", "TOKEN001", "RFID",
		[]store.MeterValue{energyMeterValue("2026-11-26T10:00:00Z", 100)}, 1, false)
	require.NoError(t, err)
	err = testStore.EndTransaction(ctx, "cs001", "no-reason", "TOKEN001", "RFID",
		[]store.MeterValue{energyMeterValue("2026-11-26T11:00:00+01:00", 300)}, 2)
	require.NoError(t, err)

	got, err := testStore.FindTransaction(ctx, "cs001", "no-reason")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Nil(t, got.StopReason)
	assert.Equal(t, ptr(int64(300)), got.MeterStopWh)
	require.NotNil(t, got.StoppedAt)
	assert.True(t, time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC).Equal(*got.StoppedAt), "stopped at %s", got.StoppedAt)

	// set before the end: the end of the transaction keeps it
	err = testStore.CreateTransaction(ctx, "cs001", "with-reason", "TOKEN001", "RFID",
		[]store.MeterValue{energyMeterValue("2026-11-26T10:00:00Z", 100)}, 1, false)
	require.NoError(t, err)
	err = testStore.UpdateTransactionDetails(ctx, "cs001", "with-reason", &store.TransactionDetails{
		StopReason:  ptr("EmergencyStop"),
		MeterStopWh: ptr(int64(450)),
	})
	require.NoError(t, err)
	// no energy register reading at the end: the meter stop set before is kept as well
	err = testStore.EndTransaction(ctx, "cs001", "with-reason", "TOKEN001", "RFID", nil, 2)
	require.NoError(t, err)

	got, err = testStore.FindTransaction(ctx, "cs001", "with-reason")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, ptr("EmergencyStop"), got.StopReason)
	assert.Equal(t, ptr(int64(450)), got.MeterStopWh)
	assert.NotNil(t, got.StoppedAt)
}

func TestTransaction_MeterValuesAbove32Bit(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	// a lifetime energy register in Wh does not fit in 32 bits
	err := testStore.CreateTransaction(ctx, "cs001", "tx001", "TOKEN001", "RFID",
		[]store.MeterValue{energyMeterValue("2026-11-26T10:00:00Z", 3_000_000_000)}, 1, false)
	require.NoError(t, err)

	got, err := testStore.FindTransaction(ctx, "cs001", "tx001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, ptr(int64(3_000_000_000)), got.MeterStartWh)

	err = testStore.UpdateTransactionDetails(ctx, "cs001", "tx001", &store.TransactionDetails{
		MeterStartWh: ptr(int64(3_000_000_001)),
		MeterStopWh:  ptr(int64(3_000_050_001)),
		EnergyWh:     ptr(int64(50_000)),
	})
	require.NoError(t, err)

	got, err = testStore.FindTransaction(ctx, "cs001", "tx001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, ptr(int64(3_000_000_001)), got.MeterStartWh)
	assert.Equal(t, ptr(int64(3_000_050_001)), got.MeterStopWh)
	assert.Equal(t, ptr(int64(50_000)), got.EnergyWh)

	err = testStore.EndTransaction(ctx, "cs001", "tx001", "TOKEN001", "RFID",
		[]store.MeterValue{energyMeterValue("2026-11-26T11:00:00Z", 3_000_060_000)}, 2)
	require.NoError(t, err)

	got, err = testStore.FindTransaction(ctx, "cs001", "tx001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, ptr(int64(3_000_060_000)), got.MeterStopWh)
}

func TestTransaction_NextOcpp16TransactionId(t *testing.T) {
	ctx := context.Background()

	previous, err := testStore.NextOcpp16TransactionId(ctx)
	require.NoError(t, err)
	assert.Positive(t, previous)

	for i := 0; i < 5; i++ {
		next, err := testStore.NextOcpp16TransactionId(ctx)
		require.NoError(t, err)
		assert.Greater(t, next, previous)
		previous = next
	}
}

func TestTransaction_DeleteCascadesToMeterValues(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	for _, cs := range []string{"cs001", "cs002"} {
		err := testStore.CreateTransaction(ctx, cs, "1", "TOKEN001", "RFID", []store.MeterValue{
			energyMeterValue("2026-11-26T10:00:00Z", 100),
			energyMeterValue("2026-11-26T10:05:00Z", 200),
		}, 1, false)
		require.NoError(t, err)
	}

	countMeterValues := func(cs string) int {
		var count int
		err := testPool.QueryRow(ctx,
			`SELECT COUNT(*) FROM transaction_meter_values WHERE charge_station_id = $1 AND transaction_id = '1'`, cs).Scan(&count)
		require.NoError(t, err)
		return count
	}
	require.Equal(t, 2, countMeterValues("cs001"))
	require.Equal(t, 2, countMeterValues("cs002"))

	_, err := testPool.Exec(ctx, `DELETE FROM transactions WHERE charge_station_id = 'cs001' AND id = '1'`)
	require.NoError(t, err)

	assert.Equal(t, 0, countMeterValues("cs001"))
	assert.Equal(t, 2, countMeterValues("cs002"))
}
