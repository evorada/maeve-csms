// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	clockTest "k8s.io/utils/clock/testing"
)

// failingEventStore fails every write of a charge station event.
type failingEventStore struct {
	store.ChargeStationEventStore
	err error
}

func (s failingEventStore) AddChargeStationEvent(context.Context, string, *store.ChargeStationEvent) error {
	return s.err
}

func newSecurityEventNotificationHandler(now time.Time) (SecurityEventNotificationHandler, *inmemory.Store) {
	clk := clockTest.NewFakePassiveClock(now)
	engine := inmemory.NewStore(clk)
	return SecurityEventNotificationHandler{Clock: clk, EventStore: engine}, engine
}

func TestSecurityEventNotificationHandler(t *testing.T) {
	handler, _ := newSecurityEventNotificationHandler(time.Now())

	now := time.Now().UTC().Format(time.RFC3339)

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, `test`)
		defer span.End()

		req := &ocpp201.SecurityEventNotificationRequestJson{
			Timestamp: now,
			Type:      "SomeSecurityEvent",
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		assert.Equal(t, &ocpp201.SecurityEventNotificationResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"security_event.timestamp": now,
		"security_event.type":      "SomeSecurityEvent",
	})
}

func TestSecurityEventNotificationHandlerStoresEvent(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	handler, engine := newSecurityEventNotificationHandler(now)
	ctx := context.Background()

	techInfo := "3 failed attempts from 10.0.0.7"
	resp, err := handler.HandleCall(ctx, "cs001", &ocpp201.SecurityEventNotificationRequestJson{
		Timestamp: "2026-11-27T09:15:30+01:00",
		Type:      "FailedToAuthenticateAtCsms",
		TechInfo:  &techInfo,
	})
	require.NoError(t, err)
	assert.Equal(t, &ocpp201.SecurityEventNotificationResponseJson{}, resp)

	events, total, err := engine.ListChargeStationEvents(ctx, "cs001", 0, 10)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, events, 1)
	got := events[0]
	assert.Equal(t, "cs001", got.ChargeStationId)
	assert.Equal(t, "FailedToAuthenticateAtCsms", got.EventType)
	assert.Equal(t, &techInfo, got.TechInfo)
	assert.Equal(t, store.ChargeStationEventCategorySecurity, got.Category)
	// the time the station reported, not the time of arrival
	assert.True(t, time.Date(2026, 11, 27, 8, 15, 30, 0, time.UTC).Equal(got.Timestamp), "timestamp: %s", got.Timestamp)
	assert.Nil(t, got.EventData)

	// events of other stations are not touched
	_, total, err = engine.ListChargeStationEvents(ctx, "cs002", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, 0, total)
}

func TestSecurityEventNotificationHandlerUsesCurrentTimeForInvalidTimestamp(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	handler, engine := newSecurityEventNotificationHandler(now)
	ctx := context.Background()

	resp, err := handler.HandleCall(ctx, "cs001", &ocpp201.SecurityEventNotificationRequestJson{
		Timestamp: "27/11/2026 09:15",
		Type:      "SettingSystemTime",
	})
	require.NoError(t, err)
	assert.Equal(t, &ocpp201.SecurityEventNotificationResponseJson{}, resp)

	events, _, err := engine.ListChargeStationEvents(ctx, "cs001", 0, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "SettingSystemTime", events[0].EventType)
	assert.True(t, now.Equal(events[0].Timestamp), "timestamp: %s", events[0].Timestamp)
	assert.Nil(t, events[0].TechInfo)
	assert.Equal(t, store.ChargeStationEventCategorySecurity, events[0].Category)
}

func TestSecurityEventNotificationHandlerReturnsStoreError(t *testing.T) {
	storeErr := errors.New("event store is down")
	handler := SecurityEventNotificationHandler{
		Clock:      clockTest.NewFakePassiveClock(time.Now()),
		EventStore: failingEventStore{err: storeErr},
	}

	resp, err := handler.HandleCall(context.Background(), "cs001", &ocpp201.SecurityEventNotificationRequestJson{
		Timestamp: "2026-11-27T09:15:30Z",
		Type:      "FirmwareUpdated",
	})
	assert.ErrorIs(t, err, storeErr)
	assert.Nil(t, resp)
}
