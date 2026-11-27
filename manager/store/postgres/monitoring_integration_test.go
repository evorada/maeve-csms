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

func TestChargeStationEvent_Category(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	for _, csId := range []string{"cs001", "cs002"} {
		require.NoError(t, testStore.SetChargeStationAuth(ctx, csId, &store.ChargeStationAuth{
			SecurityProfile: store.UnsecuredTransportWithBasicAuth,
		}))
	}

	timestamp := time.Now().UTC().Truncate(time.Second)
	for _, event := range []*store.ChargeStationEvent{
		{Timestamp: timestamp, EventType: "SecurityLogWasCleared", Category: store.ChargeStationEventCategorySecurity},
		{Timestamp: timestamp, EventType: "LogStatusNotification", Category: store.ChargeStationEventCategoryLogStatus},
		{Timestamp: timestamp, EventType: "Threshold", Category: store.ChargeStationEventCategoryDevice},
		{Timestamp: timestamp, EventType: "Alert"},
	} {
		require.NoError(t, testStore.AddChargeStationEvent(ctx, "cs001", event))
	}
	require.NoError(t, testStore.AddChargeStationEvent(ctx, "cs002", &store.ChargeStationEvent{
		Timestamp: timestamp, EventType: "FirmwareUpdated", Category: store.ChargeStationEventCategorySecurity,
	}))

	events, total, err := testStore.ListChargeStationEvents(ctx, "cs001", 0, 10)
	require.NoError(t, err)
	require.Equal(t, 4, total)
	got := map[string]store.ChargeStationEventCategory{}
	for _, event := range events {
		got[event.EventType] = event.Category
	}
	assert.Equal(t, map[string]store.ChargeStationEventCategory{
		"SecurityLogWasCleared": store.ChargeStationEventCategorySecurity,
		"LogStatusNotification": store.ChargeStationEventCategoryLogStatus,
		"Threshold":             store.ChargeStationEventCategoryDevice,
		// an event without a category is a device event
		"Alert": store.ChargeStationEventCategoryDevice,
	}, got)
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
