package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

func TestTransactionStore_CreateAndFind(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	chargeStationId := "CS001"
	transactionId := "TXN001"
	idToken := "TOKEN123"
	tokenType := "RFID"

	// Create meter values with energy reading
	measurand := "Energy.Active.Import.Register"
	meterValues := []store.MeterValue{
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			SampledValues: []store.SampledValue{
				{
					Value:     1000.0,
					Measurand: &measurand,
				},
			},
		},
	}

	// Create a transaction
	err := db.store.CreateTransaction(ctx, chargeStationId, transactionId, idToken, tokenType, meterValues, 0, false)
	require.NoError(t, err)

	// Find transaction
	found, err := db.store.FindTransaction(ctx, chargeStationId, transactionId)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, transactionId, found.TransactionId)
	assert.Equal(t, chargeStationId, found.ChargeStationId)
	assert.Equal(t, idToken, found.IdToken)
	assert.Equal(t, tokenType, found.TokenType)
	assert.False(t, found.Offline)
	assert.Len(t, found.MeterValues, 1)

	// Verify meter values
	assert.Equal(t, 1000.0, found.MeterValues[0].SampledValues[0].Value)
}

func TestTransactionStore_FindNonExistent(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Try to find non-existent transaction
	found, err := db.store.FindTransaction(ctx, "CS999", "TXN999")
	require.NoError(t, err)
	assert.Nil(t, found)
}

func TestTransactionStore_UpdateTransaction(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	chargeStationId := "CS002"
	transactionId := "TXN002"
	idToken := "TOKEN456"
	tokenType := "RFID"

	// Create initial transaction
	measurand := "Energy.Active.Import.Register"
	initialMeterValues := []store.MeterValue{
		{
			Timestamp: time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339),
			SampledValues: []store.SampledValue{
				{
					Value:     2000.0,
					Measurand: &measurand,
				},
			},
		},
	}

	err := db.store.CreateTransaction(ctx, chargeStationId, transactionId, idToken, tokenType, initialMeterValues, 0, false)
	require.NoError(t, err)

	// Update with additional meter values
	updateMeterValues := []store.MeterValue{
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			SampledValues: []store.SampledValue{
				{
					Value:     3000.0,
					Measurand: &measurand,
				},
			},
		},
	}

	err = db.store.UpdateTransaction(ctx, chargeStationId, transactionId, updateMeterValues)
	require.NoError(t, err)

	// Verify update
	found, err := db.store.FindTransaction(ctx, chargeStationId, transactionId)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Len(t, found.MeterValues, 2)
	assert.Greater(t, found.UpdatedSeqNoCount, 0)
}

func TestTransactionStore_EndTransaction(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	chargeStationId := "CS003"
	transactionId := "TXN003"
	idToken := "TOKEN789"
	tokenType := "RFID"

	// Create transaction
	measurand := "Energy.Active.Import.Register"
	startMeterValues := []store.MeterValue{
		{
			Timestamp: time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339),
			SampledValues: []store.SampledValue{
				{
					Value:     1500.0,
					Measurand: &measurand,
				},
			},
		},
	}

	err := db.store.CreateTransaction(ctx, chargeStationId, transactionId, idToken, tokenType, startMeterValues, 0, false)
	require.NoError(t, err)

	// End transaction with final meter values
	endMeterValues := []store.MeterValue{
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			SampledValues: []store.SampledValue{
				{
					Value:     5000.0,
					Measurand: &measurand,
				},
			},
		},
	}

	err = db.store.EndTransaction(ctx, chargeStationId, transactionId, idToken, tokenType, endMeterValues, 1)
	require.NoError(t, err)

	// Verify transaction is ended
	found, err := db.store.FindTransaction(ctx, chargeStationId, transactionId)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Greater(t, found.EndedSeqNo, 0, "EndedSeqNo should be set")
	assert.Len(t, found.MeterValues, 2, "Should have start and end meter values")
}

func TestTransactionStore_OfflineTransaction(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	chargeStationId := "CS004"
	transactionId := "TXN004"
	idToken := "TOKEN111"
	tokenType := "RFID"

	// Create offline transaction
	measurand := "Energy.Active.Import.Register"
	meterValues := []store.MeterValue{
		{
			Timestamp: time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339),
			SampledValues: []store.SampledValue{
				{
					Value:     100.0,
					Measurand: &measurand,
				},
			},
		},
	}

	err := db.store.CreateTransaction(ctx, chargeStationId, transactionId, idToken, tokenType, meterValues, 0, true)
	require.NoError(t, err)

	// Verify offline flag
	found, err := db.store.FindTransaction(ctx, chargeStationId, transactionId)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.True(t, found.Offline)
}

func TestTransactionStore_MultipleMeterValues(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	chargeStationId := "CS005"
	transactionId := "TXN005"

	// Create transaction with multiple sampled values
	measurandEnergy := "Energy.Active.Import.Register"
	measurandCurrent := "Current.Import"
	measurandVoltage := "Voltage"
	contextPeriodic := "Sample.Periodic"
	unit := "Wh"

	meterValues := []store.MeterValue{
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			SampledValues: []store.SampledValue{
				{
					Value:     2000.0,
					Context:   &contextPeriodic,
					Measurand: &measurandEnergy,
					UnitOfMeasure: &store.UnitOfMeasure{
						Unit:      unit,
						Multipler: 0,
					},
				},
				{
					Value:     10.5,
					Context:   &contextPeriodic,
					Measurand: &measurandCurrent,
				},
				{
					Value:     230.0,
					Context:   &contextPeriodic,
					Measurand: &measurandVoltage,
				},
			},
		},
	}

	err := db.store.CreateTransaction(ctx, chargeStationId, transactionId, "TOKEN222", "RFID", meterValues, 0, false)
	require.NoError(t, err)

	// Verify all sampled values are stored
	found, err := db.store.FindTransaction(ctx, chargeStationId, transactionId)
	require.NoError(t, err)
	require.NotNil(t, found)
	require.Len(t, found.MeterValues, 1)
	require.Len(t, found.MeterValues[0].SampledValues, 3)

	// Verify energy reading
	assert.Equal(t, 2000.0, found.MeterValues[0].SampledValues[0].Value)
	require.NotNil(t, found.MeterValues[0].SampledValues[0].Measurand)
	assert.Equal(t, measurandEnergy, *found.MeterValues[0].SampledValues[0].Measurand)

	// Verify current reading
	assert.Equal(t, 10.5, found.MeterValues[0].SampledValues[1].Value)
	require.NotNil(t, found.MeterValues[0].SampledValues[1].Measurand)
	assert.Equal(t, measurandCurrent, *found.MeterValues[0].SampledValues[1].Measurand)

	// Verify voltage reading
	assert.Equal(t, 230.0, found.MeterValues[0].SampledValues[2].Value)
	require.NotNil(t, found.MeterValues[0].SampledValues[2].Measurand)
	assert.Equal(t, measurandVoltage, *found.MeterValues[0].SampledValues[2].Measurand)
}

func TestTransactionStore_MeterValuesWithOptionalFields(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	chargeStationId := "CS006"
	transactionId := "TXN006"

	// Create meter value with all optional fields
	measurand := "Power.Active.Import"
	context := "Sample.Clock"
	location := "Outlet"
	phase := "L1"

	meterValues := []store.MeterValue{
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			SampledValues: []store.SampledValue{
				{
					Value:     12345.67,
					Context:   &context,
					Location:  &location,
					Measurand: &measurand,
					Phase:     &phase,
					UnitOfMeasure: &store.UnitOfMeasure{
						Unit:      "W",
						Multipler: 0,
					},
				},
			},
		},
	}

	err := db.store.CreateTransaction(ctx, chargeStationId, transactionId, "TOKEN333", "RFID", meterValues, 0, false)
	require.NoError(t, err)

	// Verify all optional fields are preserved
	found, err := db.store.FindTransaction(ctx, chargeStationId, transactionId)
	require.NoError(t, err)
	require.NotNil(t, found)
	require.Len(t, found.MeterValues, 1)
	require.Len(t, found.MeterValues[0].SampledValues, 1)

	sv := found.MeterValues[0].SampledValues[0]
	assert.Equal(t, 12345.67, sv.Value)
	require.NotNil(t, sv.Context)
	assert.Equal(t, context, *sv.Context)
	require.NotNil(t, sv.Location)
	assert.Equal(t, location, *sv.Location)
	require.NotNil(t, sv.Measurand)
	assert.Equal(t, measurand, *sv.Measurand)
	require.NotNil(t, sv.Phase)
	assert.Equal(t, phase, *sv.Phase)
	require.NotNil(t, sv.UnitOfMeasure)
	assert.Equal(t, "W", sv.UnitOfMeasure.Unit)
}

func TestTransactionStore_ListAllTransactions(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Create multiple transactions
	measurand := "Energy.Active.Import.Register"
	for i := 1; i <= 3; i++ {
		chargeStationId := "CS_LIST"
		transactionId := "TXN_LIST_" + string(rune('0'+i))
		meterValues := []store.MeterValue{
			{
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				SampledValues: []store.SampledValue{
					{
						Value:     float64(i * 1000),
						Measurand: &measurand,
					},
				},
			},
		}

		err := db.store.CreateTransaction(ctx, chargeStationId, transactionId, "TOKEN", "RFID", meterValues, 0, false)
		require.NoError(t, err)
	}

	// List all transactions
	transactions, err := db.store.Transactions(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(transactions), 3, "Should have at least 3 transactions")

	// Verify we can find our transactions
	foundCount := 0
	for _, txn := range transactions {
		if txn.ChargeStationId == "CS_LIST" {
			foundCount++
		}
	}
	assert.Equal(t, 3, foundCount, "Should find all 3 created transactions")
}

func TestTransactionStore_WrongChargeStationLookup(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	chargeStationId := "CS_CORRECT"
	wrongStationId := "CS_WRONG"
	transactionId := "TXN_ISOLATION"

	// Create transaction
	measurand := "Energy.Active.Import.Register"
	meterValues := []store.MeterValue{
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			SampledValues: []store.SampledValue{
				{
					Value:     1000.0,
					Measurand: &measurand,
				},
			},
		},
	}

	err := db.store.CreateTransaction(ctx, chargeStationId, transactionId, "TOKEN", "RFID", meterValues, 0, false)
	require.NoError(t, err)

	// Try to find with correct charge station ID
	found, err := db.store.FindTransaction(ctx, chargeStationId, transactionId)
	require.NoError(t, err)
	require.NotNil(t, found)

	// Try to find with wrong charge station ID
	notFound, err := db.store.FindTransaction(ctx, wrongStationId, transactionId)
	require.NoError(t, err)
	assert.Nil(t, notFound, "Should not find transaction with wrong charge station ID")
}

func TestTransactionStore_UpdateWrongChargeStation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	correctStationId := "CS_UPDATE_CORRECT"
	wrongStationId := "CS_UPDATE_WRONG"
	transactionId := "TXN_UPDATE_ISOLATION"

	// Create transaction
	measurand := "Energy.Active.Import.Register"
	meterValues := []store.MeterValue{
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			SampledValues: []store.SampledValue{
				{
					Value:     1000.0,
					Measurand: &measurand,
				},
			},
		},
	}

	err := db.store.CreateTransaction(ctx, correctStationId, transactionId, "TOKEN", "RFID", meterValues, 0, false)
	require.NoError(t, err)

	// Try to update with wrong charge station ID
	updateMeterValues := []store.MeterValue{
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			SampledValues: []store.SampledValue{
				{
					Value:     2000.0,
					Measurand: &measurand,
				},
			},
		},
	}

	err = db.store.UpdateTransaction(ctx, wrongStationId, transactionId, updateMeterValues)
	assert.Error(t, err, "Should fail to update with wrong charge station ID")
	assert.Contains(t, err.Error(), "does not belong")
}

func TestTransactionStore_EndWrongChargeStation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	correctStationId := "CS_END_CORRECT"
	wrongStationId := "CS_END_WRONG"
	transactionId := "TXN_END_ISOLATION"

	// Create transaction
	measurand := "Energy.Active.Import.Register"
	meterValues := []store.MeterValue{
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			SampledValues: []store.SampledValue{
				{
					Value:     1000.0,
					Measurand: &measurand,
				},
			},
		},
	}

	err := db.store.CreateTransaction(ctx, correctStationId, transactionId, "TOKEN", "RFID", meterValues, 0, false)
	require.NoError(t, err)

	// Try to end with wrong charge station ID
	endMeterValues := []store.MeterValue{
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			SampledValues: []store.SampledValue{
				{
					Value:     5000.0,
					Measurand: &measurand,
				},
			},
		},
	}

	err = db.store.EndTransaction(ctx, wrongStationId, transactionId, "TOKEN", "RFID", endMeterValues, 1)
	assert.Error(t, err, "Should fail to end with wrong charge station ID")
	assert.Contains(t, err.Error(), "does not belong")
}

func TestTransactionStore_EmptyMeterValues(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	chargeStationId := "CS_EMPTY"
	transactionId := "TXN_EMPTY"

	// Create transaction with no meter values
	err := db.store.CreateTransaction(ctx, chargeStationId, transactionId, "TOKEN", "RFID", []store.MeterValue{}, 0, false)
	require.NoError(t, err)

	// Verify transaction can be retrieved
	found, err := db.store.FindTransaction(ctx, chargeStationId, transactionId)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Empty(t, found.MeterValues)
}

func TestTransactionStore_SequenceNumberIncrement(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	chargeStationId := "CS_SEQ"
	transactionId := "TXN_SEQ"

	// Create transaction
	measurand := "Energy.Active.Import.Register"
	meterValues := []store.MeterValue{
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			SampledValues: []store.SampledValue{
				{
					Value:     1000.0,
					Measurand: &measurand,
				},
			},
		},
	}

	err := db.store.CreateTransaction(ctx, chargeStationId, transactionId, "TOKEN", "RFID", meterValues, 0, false)
	require.NoError(t, err)

	// Update multiple times
	for i := 1; i <= 3; i++ {
		updateMV := []store.MeterValue{
			{
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				SampledValues: []store.SampledValue{
					{
						Value:     float64(1000 + i*500),
						Measurand: &measurand,
					},
				},
			},
		}

		err = db.store.UpdateTransaction(ctx, chargeStationId, transactionId, updateMV)
		require.NoError(t, err)
	}

	// Verify sequence number incremented
	found, err := db.store.FindTransaction(ctx, chargeStationId, transactionId)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, 3, found.UpdatedSeqNoCount, "Should have incremented 3 times")
}
