// SPDX-License-Identifier: Apache-2.0

package sync_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/sync"
	"go.opentelemetry.io/otel/trace/noop"
	"k8s.io/utils/clock"
)

// triggerCycleCountingEngine counts the sync cycles: the sync loop lists the trigger
// messages once at the start of every cycle.
type triggerCycleCountingEngine struct {
	store.Engine
	cycle atomic.Int32
}

func (e *triggerCycleCountingEngine) ListChargeStationTriggerMessages(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationTriggerMessage, error) {
	e.cycle.Add(1)
	return e.Engine.ListChargeStationTriggerMessages(ctx, pageSize, previousChargeStationId)
}

func setTrigger(t *testing.T, engine store.Engine, chargeStationId, ocppVersion string, trigger store.ChargeStationTriggerMessage) {
	t.Helper()
	err := engine.SetChargeStationRuntimeDetails(context.Background(), chargeStationId, &store.ChargeStationRuntimeDetails{
		OcppVersion: ocppVersion,
	})
	require.NoError(t, err)
	trigger.ChargeStationId = chargeStationId
	err = engine.SetChargeStationTriggerMessage(context.Background(), chargeStationId, &trigger)
	require.NoError(t, err)
}

func TestSyncTriggersDoesNotSendRefusedTriggers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	engine := &triggerCycleCountingEngine{Engine: inmemory.NewStore(clock.RealClock{})}

	// the result handlers store the final status without a time to send the trigger after
	setTrigger(t, engine, "cs001", "1.6", store.ChargeStationTriggerMessage{
		TriggerMessage: store.TriggerMessageStatusNotification,
		TriggerStatus:  store.TriggerStatusRejected,
	})
	setTrigger(t, engine, "cs002", "1.6", store.ChargeStationTriggerMessage{
		TriggerMessage: store.TriggerMessageSignChargingStationCertificate,
		TriggerStatus:  store.TriggerStatusNotImplemented,
	})
	setTrigger(t, engine, "cs003", "2.0.1", store.ChargeStationTriggerMessage{
		TriggerMessage: store.TriggerMessageBootNotification,
		TriggerStatus:  store.TriggerStatusNotImplemented,
	})

	v16CallMaker := &mockCallMaker{}
	dataTransferCallMaker := &mockCallMaker{}
	v201CallMaker := &mockCallMaker{}
	sync.SyncTriggers(ctx, noop.NewTracerProvider().Tracer(""), engine, clock.RealClock{},
		v16CallMaker, dataTransferCallMaker, v201CallMaker, 50*time.Millisecond, time.Minute)

	require.Positive(t, engine.cycle.Load(), "the sync loop did not run")
	assert.Empty(t, v16CallMaker.callEvents)
	assert.Empty(t, dataTransferCallMaker.callEvents)
	assert.Empty(t, v201CallMaker.callEvents)

	// the final status is kept: the loop must not make the trigger pending again
	for chargeStationId, want := range map[string]store.TriggerStatus{
		"cs001": store.TriggerStatusRejected,
		"cs002": store.TriggerStatusNotImplemented,
		"cs003": store.TriggerStatusNotImplemented,
	} {
		got, err := engine.LookupChargeStationTriggerMessage(context.Background(), chargeStationId)
		require.NoError(t, err)
		require.NotNil(t, got, chargeStationId)
		assert.Equal(t, want, got.TriggerStatus, chargeStationId)
	}
}

func TestSyncTriggersSendsPendingTriggerPastSendAfter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	engine := &triggerCycleCountingEngine{Engine: inmemory.NewStore(clock.RealClock{})}

	connectorId := 1
	// a refused trigger does not stop the triggers that follow it from being sent
	setTrigger(t, engine, "cs001", "1.6", store.ChargeStationTriggerMessage{
		TriggerMessage: store.TriggerMessageHeartbeat,
		TriggerStatus:  store.TriggerStatusRejected,
	})
	setTrigger(t, engine, "cs002", "1.6", store.ChargeStationTriggerMessage{
		TriggerMessage: store.TriggerMessageStatusNotification,
		ConnectorId:    &connectorId,
		TriggerStatus:  store.TriggerStatusPending,
		SendAfter:      time.Now().Add(-time.Minute),
	})
	setTrigger(t, engine, "cs003", "1.6", store.ChargeStationTriggerMessage{
		TriggerMessage: store.TriggerMessageBootNotification,
		TriggerStatus:  store.TriggerStatusPending,
		SendAfter:      time.Now().Add(time.Hour),
	})

	v16CallMaker := &mockCallMaker{}
	dataTransferCallMaker := &mockCallMaker{}
	v201CallMaker := &mockCallMaker{}
	sync.SyncTriggers(ctx, noop.NewTracerProvider().Tracer(""), engine, clock.RealClock{},
		v16CallMaker, dataTransferCallMaker, v201CallMaker, 50*time.Millisecond, time.Minute)

	require.Positive(t, engine.cycle.Load(), "the sync loop did not run")
	require.Len(t, v16CallMaker.callEvents, 1)
	assert.Equal(t, "cs002", v16CallMaker.callEvents[0].chargeStationId)
	assert.Equal(t, &ocpp16.TriggerMessageJson{
		RequestedMessage: ocpp16.TriggerMessageJsonRequestedMessageStatusNotification,
		ConnectorId:      &connectorId,
	}, v16CallMaker.callEvents[0].request)
	assert.Empty(t, dataTransferCallMaker.callEvents)
	assert.Empty(t, v201CallMaker.callEvents)

	// the trigger stays pending until the charge station answers and is retried later
	got, err := engine.LookupChargeStationTriggerMessage(context.Background(), "cs002")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, store.TriggerStatusPending, got.TriggerStatus)
	assert.True(t, got.SendAfter.After(time.Now()), "the trigger must not be sent again before the retry time")
}
