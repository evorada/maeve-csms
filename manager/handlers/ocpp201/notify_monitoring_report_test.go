// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"k8s.io/utils/clock"
)

func TestNotifyMonitoringReportHandler(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.NotifyMonitoringReportHandler{Store: memStore}

	tracer, exporter := testutil.GetTracer()
	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.NotifyMonitoringReportRequestJson{
			RequestId:   42,
			SeqNo:       0,
			GeneratedAt: "2026-02-16T17:50:00Z",
			Monitor: []types.MonitoringDataType{
				{
					Component: types.ComponentType{Name: "EVSE"},
					Variable:  types.VariableType{Name: "Voltage"},
					VariableMonitoring: []types.VariableMonitoringType{
						{Id: 1001, Transaction: false, Value: 400, Type: types.MonitorEnumTypeUpperThreshold, Severity: 5},
					},
				},
			},
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)
		assert.Equal(t, &types.NotifyMonitoringReportResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"notify_monitoring_report.request_id":    42,
		"notify_monitoring_report.seq_no":        0,
		"notify_monitoring_report.tbc":           false,
		"notify_monitoring_report.generated_at":  "2026-02-16T17:50:00Z",
		"notify_monitoring_report.monitor_count": 1,
	})

	settings, err := memStore.LookupChargeStationSettings(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, settings)

	stored := settings.Settings["ocpp201.monitoring_report.42.0"]
	require.NotNil(t, stored)
	assert.Equal(t, store.ChargeStationSettingStatusAccepted, stored.Status)
	assert.Contains(t, stored.Value, "\"name\":\"EVSE\"")
	assert.Contains(t, stored.Value, "\"name\":\"Voltage\"")
}

func TestNotifyMonitoringReportHandlerMergesFragments(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.NotifyMonitoringReportHandler{Store: memStore}
	ctx := context.Background()

	_, err := handler.HandleCall(ctx, "cs001", &types.NotifyMonitoringReportRequestJson{
		RequestId:   7,
		SeqNo:       0,
		GeneratedAt: "2026-02-16T17:00:00Z",
		Monitor:     []types.MonitoringDataType{},
	})
	require.NoError(t, err)

	_, err = handler.HandleCall(ctx, "cs001", &types.NotifyMonitoringReportRequestJson{
		RequestId:   7,
		SeqNo:       1,
		GeneratedAt: "2026-02-16T17:01:00Z",
		Tbc:         false,
		Monitor: []types.MonitoringDataType{
			{
				Component:          types.ComponentType{Name: "Connector"},
				Variable:           types.VariableType{Name: "Current"},
				VariableMonitoring: []types.VariableMonitoringType{{Id: 2002, Transaction: true, Value: 32, Type: types.MonitorEnumTypePeriodic, Severity: 7}},
			},
		},
	})
	require.NoError(t, err)

	settings, err := memStore.LookupChargeStationSettings(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, settings)

	require.NotNil(t, settings.Settings["ocpp201.monitoring_report.7.0"])
	require.NotNil(t, settings.Settings["ocpp201.monitoring_report.7.1"])
}
