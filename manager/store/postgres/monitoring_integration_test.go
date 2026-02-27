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

func TestVariableMonitoring_SetAndGet(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	config := &store.VariableMonitoringConfig{
		ChargeStationId: "cs001",
		ComponentName:   "Connector",
		VariableName:    "Temperature",
		MonitorType:     store.MonitoringTypeUpperThreshold,
		Value:           80.0,
		Severity:        5,
		Transaction:     false,
	}

	err = testStore.SetVariableMonitoring(ctx, "cs001", config)
	require.NoError(t, err)
}

func TestVariableMonitoring_List(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		err := testStore.SetVariableMonitoring(ctx, "cs001", &store.VariableMonitoringConfig{
			ChargeStationId: "cs001",
			ComponentName:   "Connector",
			VariableName:    "Temperature",
			MonitorType:     store.MonitoringTypeUpperThreshold,
			Value:           float64(70 + i*10),
			Severity:        5,
		})
		require.NoError(t, err)
	}

	results, err := testStore.ListVariableMonitoring(ctx, "cs001", 0, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 3)
}

func TestChargeStationEvent_AddAndList(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	event := &store.ChargeStationEvent{
		ChargeStationId: "cs001",
		Timestamp:       time.Now().UTC().Truncate(time.Second),
		EventType:       "Alert",
		Cleared:         false,
	}

	err = testStore.AddChargeStationEvent(ctx, "cs001", event)
	require.NoError(t, err)

	results, total, err := testStore.ListChargeStationEvents(ctx, "cs001", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, results, 1)
	assert.Equal(t, "Alert", results[0].EventType)
}

func TestDeviceReport_AddAndList(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	reportData := `{"key": "value"}`
	report := &store.DeviceReport{
		ChargeStationId: "cs001",
		RequestId:       1,
		GeneratedAt:     time.Now().UTC().Truncate(time.Second),
		ReportData:      &reportData,
	}

	err = testStore.AddDeviceReport(ctx, "cs001", report)
	require.NoError(t, err)

	results, total, err := testStore.ListDeviceReports(ctx, "cs001", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, results, 1)
}
