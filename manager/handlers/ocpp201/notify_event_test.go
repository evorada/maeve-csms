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

func TestNotifyEventHandler(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.NotifyEventHandler{Store: memStore}

	tracer, exporter := testutil.GetTracer()
	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.NotifyEventRequestJson{
			GeneratedAt: "2026-02-16T18:05:00Z",
			SeqNo:       3,
			EventData: []types.EventDataType{
				{
					EventId:                9001,
					Timestamp:              "2026-02-16T18:04:59Z",
					Trigger:                types.EventTriggerEnumTypeAlerting,
					ActualValue:            "OverCurrent",
					EventNotificationType:  types.EventNotificationEnumTypeCustomMonitor,
					Component:              types.ComponentType{Name: "Connector"},
					Variable:               types.VariableType{Name: "Current"},
					VariableMonitoringId:   intPtr(120),
					TransactionId:          stringPtr("tx-123"),
				},
			},
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)
		assert.Equal(t, &types.NotifyEventResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"notify_event.seq_no":       3,
		"notify_event.tbc":          false,
		"notify_event.generated_at": "2026-02-16T18:05:00Z",
		"notify_event.event_count":  1,
	})

	settings, err := memStore.LookupChargeStationSettings(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, settings)

	stored := settings.Settings["ocpp201.notify_event.3"]
	require.NotNil(t, stored)
	assert.Equal(t, store.ChargeStationSettingStatusAccepted, stored.Status)
	assert.Contains(t, stored.Value, "\"eventId\":9001")
	assert.Contains(t, stored.Value, "\"name\":\"Connector\"")
}

func TestNotifyEventHandlerStoresFragmentsBySeqNo(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.NotifyEventHandler{Store: memStore}
	ctx := context.Background()

	_, err := handler.HandleCall(ctx, "cs001", &types.NotifyEventRequestJson{
		GeneratedAt: "2026-02-16T18:00:00Z",
		SeqNo:       0,
		Tbc:         true,
		EventData: []types.EventDataType{
			{
				EventId:               1,
				Timestamp:             "2026-02-16T18:00:00Z",
				Trigger:               types.EventTriggerEnumTypePeriodic,
				ActualValue:           "12",
				EventNotificationType: types.EventNotificationEnumTypePreconfiguredMonitor,
				Component:             types.ComponentType{Name: "EVSE"},
				Variable:              types.VariableType{Name: "Voltage"},
			},
		},
	})
	require.NoError(t, err)

	_, err = handler.HandleCall(ctx, "cs001", &types.NotifyEventRequestJson{
		GeneratedAt: "2026-02-16T18:00:01Z",
		SeqNo:       1,
		Tbc:         false,
		EventData: []types.EventDataType{
			{
				EventId:               2,
				Timestamp:             "2026-02-16T18:00:01Z",
				Trigger:               types.EventTriggerEnumTypeDelta,
				ActualValue:           "13",
				EventNotificationType: types.EventNotificationEnumTypePreconfiguredMonitor,
				Component:             types.ComponentType{Name: "EVSE"},
				Variable:              types.VariableType{Name: "Voltage"},
			},
		},
	})
	require.NoError(t, err)

	settings, err := memStore.LookupChargeStationSettings(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, settings)

	require.NotNil(t, settings.Settings["ocpp201.notify_event.0"])
	require.NotNil(t, settings.Settings["ocpp201.notify_event.1"])
}

func intPtr(v int) *int {
	return &v
}

func stringPtr(v string) *string {
	return &v
}
