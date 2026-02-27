// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
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
