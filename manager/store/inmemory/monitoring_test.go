// SPDX-License-Identifier: Apache-2.0

package inmemory_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	clock2 "k8s.io/utils/clock"
	testclock "k8s.io/utils/clock/testing"
)

func TestSetAndGetVariableMonitoring(t *testing.T) {
	clock := testclock.NewFakeClock(time.Now())
	s := inmemory.NewStore(clock)
	ctx := context.Background()

	config := &store.VariableMonitoringConfig{
		ComponentName: "Connector",
		VariableName:  "CurrentImport",
		MonitorType:   store.MonitoringTypeUpperThreshold,
		Value:         32.0,
		Severity:      3,
		Transaction:   false,
	}

	err := s.SetVariableMonitoring(ctx, "cs001", config)
	require.NoError(t, err)
	assert.NotZero(t, config.Id)

	got, err := s.GetVariableMonitoring(ctx, "cs001", config.Id)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Connector", got.ComponentName)
	assert.Equal(t, "CurrentImport", got.VariableName)
	assert.Equal(t, store.MonitoringTypeUpperThreshold, got.MonitorType)
	assert.Equal(t, 32.0, got.Value)
	assert.Equal(t, 3, got.Severity)
}

func TestDeleteVariableMonitoring(t *testing.T) {
	clock := testclock.NewFakeClock(time.Now())
	s := inmemory.NewStore(clock)
	ctx := context.Background()

	config := &store.VariableMonitoringConfig{
		ComponentName: "Connector",
		VariableName:  "CurrentImport",
		MonitorType:   store.MonitoringTypeDelta,
		Value:         5.0,
		Severity:      5,
	}

	err := s.SetVariableMonitoring(ctx, "cs001", config)
	require.NoError(t, err)

	err = s.DeleteVariableMonitoring(ctx, "cs001", config.Id)
	require.NoError(t, err)

	got, err := s.GetVariableMonitoring(ctx, "cs001", config.Id)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestListVariableMonitoring(t *testing.T) {
	clock := testclock.NewFakeClock(time.Now())
	s := inmemory.NewStore(clock)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		config := &store.VariableMonitoringConfig{
			ComponentName: "Connector",
			VariableName:  "Voltage",
			MonitorType:   store.MonitoringTypePeriodic,
			Value:         float64(i * 10),
			Severity:      i,
		}
		err := s.SetVariableMonitoring(ctx, "cs001", config)
		require.NoError(t, err)
	}

	results, err := s.ListVariableMonitoring(ctx, "cs001", 0, 3)
	require.NoError(t, err)
	assert.Len(t, results, 3)

	results, err = s.ListVariableMonitoring(ctx, "cs001", 3, 10)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestAddAndListChargeStationEvents(t *testing.T) {
	clock := testclock.NewFakeClock(time.Now())
	s := inmemory.NewStore(clock)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		event := &store.ChargeStationEvent{
			Timestamp: time.Now().Add(time.Duration(i) * time.Minute),
			EventType: "FirmwareUpdated",
		}
		err := s.AddChargeStationEvent(ctx, "cs001", event)
		require.NoError(t, err)
		assert.NotZero(t, event.Id)
	}

	events, total, err := s.ListChargeStationEvents(ctx, "cs001", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, events, 3)

	events, total, err = s.ListChargeStationEvents(ctx, "cs001", 1, 1)
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, events, 1)
}

func TestAddAndListDeviceReports(t *testing.T) {
	clock := testclock.NewFakeClock(time.Now())
	s := inmemory.NewStore(clock)
	ctx := context.Background()

	reportType := "ConfigurationInventory"
	for i := 0; i < 2; i++ {
		report := &store.DeviceReport{
			RequestId:   i + 1,
			GeneratedAt: time.Now(),
			ReportType:  &reportType,
		}
		err := s.AddDeviceReport(ctx, "cs001", report)
		require.NoError(t, err)
		assert.NotZero(t, report.Id)
	}

	reports, total, err := s.ListDeviceReports(ctx, "cs001", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, reports, 2)
}

var _ clock2.PassiveClock = testclock.NewFakeClock(time.Now())
