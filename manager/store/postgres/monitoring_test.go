package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// Helper to create a charge station for FK constraints
func createTestChargeStation(t *testing.T, db *testDB, csId string) {
	t.Helper()
	ctx := context.Background()
	err := db.store.SetChargeStationAuth(ctx, csId, &store.ChargeStationAuth{
		SecurityProfile:      store.TLSWithBasicAuth,
		Base64SHA256Password: "dGVzdA==",
	})
	require.NoError(t, err)
}

// VariableMonitoringStore Tests

func TestVariableMonitoringStore_SetAndGet(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	createTestChargeStation(t, db, "CS001")

	instance := "1"
	config := &store.VariableMonitoringConfig{
		ComponentName:     "Connector",
		ComponentInstance: &instance,
		VariableName:      "CurrentImport",
		MonitorType:       store.MonitoringTypeUpperThreshold,
		Value:             32.0,
		Severity:          3,
		Transaction:       false,
	}

	err := db.store.SetVariableMonitoring(ctx, "CS001", config)
	require.NoError(t, err)
	assert.NotZero(t, config.Id)

	got, err := db.store.GetVariableMonitoring(ctx, "CS001", config.Id)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Connector", got.ComponentName)
	assert.Equal(t, &instance, got.ComponentInstance)
	assert.Equal(t, "CurrentImport", got.VariableName)
	assert.Equal(t, store.MonitoringTypeUpperThreshold, got.MonitorType)
	assert.Equal(t, 32.0, got.Value)
	assert.Equal(t, 3, got.Severity)
	assert.False(t, got.Transaction)
}

func TestVariableMonitoringStore_GetNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	got, err := db.store.GetVariableMonitoring(ctx, "CS001", 999)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestVariableMonitoringStore_Delete(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	createTestChargeStation(t, db, "CS001")

	config := &store.VariableMonitoringConfig{
		ComponentName: "EVSE",
		VariableName:  "Power",
		MonitorType:   store.MonitoringTypeDelta,
		Value:         1.5,
		Severity:      5,
	}

	err := db.store.SetVariableMonitoring(ctx, "CS001", config)
	require.NoError(t, err)

	err = db.store.DeleteVariableMonitoring(ctx, "CS001", config.Id)
	require.NoError(t, err)

	got, err := db.store.GetVariableMonitoring(ctx, "CS001", config.Id)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestVariableMonitoringStore_List(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	createTestChargeStation(t, db, "CS001")

	for i := 0; i < 5; i++ {
		config := &store.VariableMonitoringConfig{
			ComponentName: "Connector",
			VariableName:  "Voltage",
			MonitorType:   store.MonitoringTypePeriodic,
			Value:         float64(i * 10),
			Severity:      i,
		}
		err := db.store.SetVariableMonitoring(ctx, "CS001", config)
		require.NoError(t, err)
	}

	// First page
	results, err := db.store.ListVariableMonitoring(ctx, "CS001", 0, 3)
	require.NoError(t, err)
	assert.Len(t, results, 3)

	// Second page
	results, err = db.store.ListVariableMonitoring(ctx, "CS001", 3, 10)
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Empty for other CS
	results, err = db.store.ListVariableMonitoring(ctx, "CS999", 0, 10)
	require.NoError(t, err)
	assert.Empty(t, results)
}

// ChargeStationEventStore Tests

func TestChargeStationEventStore_AddAndList(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	createTestChargeStation(t, db, "CS001")

	techCode := "FW001"
	for i := 0; i < 3; i++ {
		event := &store.ChargeStationEvent{
			Timestamp: time.Now().Add(time.Duration(i) * time.Minute),
			EventType: "FirmwareUpdated",
			TechCode:  &techCode,
		}
		err := db.store.AddChargeStationEvent(ctx, "CS001", event)
		require.NoError(t, err)
		assert.NotZero(t, event.Id)
	}

	events, total, err := db.store.ListChargeStationEvents(ctx, "CS001", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, events, 3)
	assert.Equal(t, "FirmwareUpdated", events[0].EventType)
	assert.Equal(t, &techCode, events[0].TechCode)
}

func TestChargeStationEventStore_Pagination(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	createTestChargeStation(t, db, "CS001")

	for i := 0; i < 5; i++ {
		event := &store.ChargeStationEvent{
			Timestamp: time.Now().Add(time.Duration(i) * time.Minute),
			EventType: "Rebooted",
		}
		err := db.store.AddChargeStationEvent(ctx, "CS001", event)
		require.NoError(t, err)
	}

	events, total, err := db.store.ListChargeStationEvents(ctx, "CS001", 0, 2)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, events, 2)

	events, total, err = db.store.ListChargeStationEvents(ctx, "CS001", 4, 10)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, events, 1)
}

func TestChargeStationEventStore_Empty(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	events, total, err := db.store.ListChargeStationEvents(ctx, "CS999", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Empty(t, events)
}

// DeviceReportStore Tests

func TestDeviceReportStore_AddAndList(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	createTestChargeStation(t, db, "CS001")

	reportType := "ConfigurationInventory"
	reportData := `{"key":"value"}`
	for i := 0; i < 2; i++ {
		report := &store.DeviceReport{
			RequestId:   i + 1,
			GeneratedAt: time.Now(),
			ReportType:  &reportType,
			ReportData:  &reportData,
		}
		err := db.store.AddDeviceReport(ctx, "CS001", report)
		require.NoError(t, err)
		assert.NotZero(t, report.Id)
	}

	reports, total, err := db.store.ListDeviceReports(ctx, "CS001", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, reports, 2)
	assert.Equal(t, &reportType, reports[0].ReportType)
}

func TestDeviceReportStore_Empty(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	reports, total, err := db.store.ListDeviceReports(ctx, "CS999", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Empty(t, reports)
}
