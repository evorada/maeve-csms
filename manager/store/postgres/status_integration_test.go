// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func TestConnectorStatus_SetAndGet(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	status := &store.ConnectorStatus{
		ChargeStationId: "cs001",
		ConnectorId:     1,
		Status:          store.ConnectorStatusAvailable,
		ErrorCode:       store.ConnectorErrorCodeNoError,
		UpdatedAt:       time.Now().UTC().Truncate(time.Second),
	}

	err = testStore.SetConnectorStatus(ctx, "cs001", 1, status)
	require.NoError(t, err)

	got, err := testStore.GetConnectorStatus(ctx, "cs001", 1)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, store.ConnectorStatusAvailable, got.Status)
}

func TestConnectorStatus_List(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	for i := 1; i <= 3; i++ {
		err := testStore.SetConnectorStatus(ctx, "cs001", i, &store.ConnectorStatus{
			ChargeStationId: "cs001",
			ConnectorId:     i,
			Status:          store.ConnectorStatusAvailable,
			ErrorCode:       store.ConnectorErrorCodeNoError,
		})
		require.NoError(t, err)
	}

	results, err := testStore.ListConnectorStatuses(ctx, "cs001")
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestChargeStationStatus_SetAndGet(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	status := &store.ChargeStationStatus{
		ChargeStationId: "cs001",
		Connected:       true,
		UpdatedAt:       time.Now().UTC().Truncate(time.Second),
	}

	err = testStore.SetChargeStationStatus(ctx, "cs001", status)
	require.NoError(t, err)

	got, err := testStore.GetChargeStationStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.Connected)
}

func TestUpdateHeartbeat(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	err = testStore.SetChargeStationStatus(ctx, "cs001", &store.ChargeStationStatus{
		ChargeStationId: "cs001",
		Connected:       true,
	})
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	err = testStore.UpdateHeartbeat(ctx, "cs001", now)
	require.NoError(t, err)

	got, err := testStore.GetChargeStationStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.LastHeartbeat)
	assert.Equal(t, now, got.LastHeartbeat.Truncate(time.Second))
}
