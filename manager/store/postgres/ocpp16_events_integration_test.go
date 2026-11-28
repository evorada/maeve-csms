// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	clockTest "k8s.io/utils/clock/testing"
)

// The tests in this file check that the OCPP 1.6 handlers store their domain events in
// the PostgreSQL transaction of their change: every store call joins it, and an event
// that cannot be stored undoes the change.

// ocpp16EventsFixture returns recorded events whose org is resolved like in the
// manager, from the charge station (an unregistered station has the legacy org).
func ocpp16EventsFixture() *eventstest.Fixture {
	clk := clockTest.NewFakePassiveClock(ocpp16CsmsTime)
	fixture := eventstest.New(testStore, store.LegacyOrgId, clk)
	fixture.Events.Emitter = &events.OutboxEmitter{
		Store:   testStore,
		Orgs:    &tenancy.Resolver{Stations: testStore},
		Clock:   clk,
		Product: events.Product,
	}
	return fixture
}

func ocpp16EventHandlers(fixture *eventstest.Fixture) (ocpp16.StatusNotificationHandler, ocpp16.StartTransactionHandler, ocpp16.MeterValuesHandler, ocpp16.StopTransactionHandler) {
	status := ocpp16.StatusNotificationHandler{
		Clock: clockTest.NewFakePassiveClock(ocpp16CsmsTime), StatusStore: testStore, Tx: fixture.Tx, Events: fixture.Events,
	}
	start := ocpp16StartHandler()
	start.Tx, start.Events = fixture.Tx, fixture.Events
	meterValues := ocpp16.MeterValuesHandler{
		TransactionStore: testStore, MeterValuesStore: testStore, Tx: fixture.Tx, Events: fixture.Events,
	}
	stop := ocpp16StopHandler()
	stop.Tx, stop.Events = fixture.Tx, fixture.Events
	return status, start, meterValues, stop
}

func TestOcpp16Events_Session(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()
	ocpp16SetToken(t, ptr("FLEET1"))
	fixture := ocpp16EventsFixture()
	status, start, meterValues, stop := ocpp16EventHandlers(fixture)

	for _, connectorStatus := range []types.StatusNotificationJsonStatus{
		types.StatusNotificationJsonStatusAvailable,
		types.StatusNotificationJsonStatusAvailable, // unchanged: no event
		types.StatusNotificationJsonStatusPreparing,
	} {
		_, err := status.HandleCall(ctx, "cs001", &types.StatusNotificationJson{
			ConnectorId: 1, ErrorCode: types.StatusNotificationJsonErrorCodeNoError, Status: connectorStatus,
		})
		require.NoError(t, err)
	}

	resp, err := start.HandleCall(ctx, "cs001", &types.StartTransactionJson{
		ConnectorId: 1, IdTag: ocpp16IdTag, MeterStart: 1000, Timestamp: "2026-11-26T10:00:00Z",
	})
	require.NoError(t, err)
	transactionId := resp.(*types.StartTransactionResponseJson).TransactionId

	_, err = meterValues.HandleCall(ctx, "cs001", &types.MeterValuesJson{
		ConnectorId: 1, TransactionId: &transactionId,
		MeterValue: []types.MeterValuesJsonMeterValueElem{{
			Timestamp:    "2026-11-26T10:15:00Z",
			SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{{Value: "1800"}},
		}},
	})
	require.NoError(t, err)

	_, err = stop.HandleCall(ctx, "cs001", &types.StopTransactionJson{
		TransactionId: transactionId, MeterStop: 2500, Timestamp: "2026-11-26T10:30:00Z", IdTag: ptr(ocpp16IdTag),
	})
	require.NoError(t, err)

	evs := fixture.Take(t)
	require.Equal(t, []string{"connector.status_changed", "connector.status_changed",
		"session.started", "session.updated", "session.completed"}, eventstest.Types(evs))
	for _, ev := range evs {
		assert.Equal(t, store.LegacyOrgId, ev.OrgId)
	}
	assert.Equal(t, "Available", eventstest.Data[events.ConnectorStatusChangedData](t, evs[1]).PreviousStatus)
	started := eventstest.Data[events.SessionStartedData](t, evs[2])
	assert.Equal(t, ptr("FLEET1"), started.IdTokenGroup)
	assert.Equal(t, "Accepted", started.AuthStatus)
	updated := eventstest.Data[events.SessionUpdatedData](t, evs[3])
	assert.Equal(t, ptr(int64(800)), updated.EnergyWh)
	completed := eventstest.Data[events.SessionCompletedData](t, evs[4])
	assert.Equal(t, ocpp16.ConvertToUUID(transactionId), completed.TransactionId)
	assert.Equal(t, ptr(int64(1000)), completed.MeterStartWh)
	assert.Equal(t, ptr(int64(1500)), completed.EnergyWh)
	assert.Equal(t, ptr(int64(1800)), completed.DurationS)
	assert.Equal(t, ptr("FLEET1"), completed.IdTokenGroup)
	requireNoOpenTransaction(t)
}

var errAppendFailed = errors.New("append failed")

// failingOutbox stores the events in the transaction and then fails, so the rollback
// has to undo them.
type failingOutbox struct {
	store.OutboxStore
}

func (o failingOutbox) AppendOutboxEvents(ctx context.Context, evs ...*store.OutboxEvent) error {
	if err := o.OutboxStore.AppendOutboxEvents(ctx, evs...); err != nil {
		return err
	}
	return errAppendFailed
}

// outboxRows counts the events in the outbox, published or not.
func outboxRows(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, testPool.QueryRow(context.Background(), `SELECT count(*) FROM outbox_events`).Scan(&n))
	return n
}

func failingOutboxFixture() *eventstest.Fixture {
	fixture := ocpp16EventsFixture()
	fixture.Events.Emitter = &events.OutboxEmitter{
		Store:   failingOutbox{OutboxStore: testStore},
		Orgs:    events.StaticOrgResolver{OrgId: store.LegacyOrgId},
		Product: events.Product,
	}
	return fixture
}

func TestOcpp16Events_StartWhoseEventFailsStoresNothing(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()
	ocpp16SetToken(t, nil)
	require.NoError(t, testStore.CreateReservation(ctx, &store.Reservation{
		ReservationId: 7, ChargeStationId: "cs001", ConnectorId: 1, IdTag: ocpp16IdTag,
		ExpiryDate: ocpp16CsmsTime, Status: store.ReservationStatusAccepted, CreatedAt: ocpp16CsmsTime,
	}))
	_, start, _, _ := ocpp16EventHandlers(failingOutboxFixture())

	_, err := start.HandleCall(ctx, "cs001", &types.StartTransactionJson{
		ConnectorId: 1, IdTag: ocpp16IdTag, MeterStart: 1000, ReservationId: ptr(7), Timestamp: "2026-11-26T10:00:00Z",
	})

	require.ErrorIs(t, err, errAppendFailed)
	transactions, total, err := testStore.ListTransactionsForChargeStation(ctx, "cs001", "", nil, nil, 10, 0)
	require.NoError(t, err)
	assert.Empty(t, transactions, "no transaction without its event")
	assert.Zero(t, total)
	assert.Zero(t, outboxRows(t), "no event without the transaction")
	reservation, err := testStore.GetReservation(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, store.ReservationStatusAccepted, reservation.Status, "the reservation is not used")
	requireNoOpenTransaction(t)
}

func TestOcpp16Events_StopWhoseEventFailsStoresNothing(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()
	ocpp16SetToken(t, nil)
	started := ocpp16Start(t, "cs001", &types.StartTransactionJson{
		ConnectorId: 1, IdTag: ocpp16IdTag, MeterStart: 1000, Timestamp: "2026-11-26T10:00:00Z",
	})
	_, _, _, stop := ocpp16EventHandlers(failingOutboxFixture())

	_, err := stop.HandleCall(ctx, "cs001", &types.StopTransactionJson{
		TransactionId: started.TransactionId, MeterStop: 2500, Timestamp: "2026-11-26T10:30:00Z",
	})

	require.ErrorIs(t, err, errAppendFailed)
	transaction, err := testStore.FindTransaction(ctx, "cs001", ocpp16.ConvertToUUID(started.TransactionId))
	require.NoError(t, err)
	require.NotNil(t, transaction)
	assert.Zero(t, transaction.EndedSeqNo, "the transaction is still active")
	assert.Nil(t, transaction.StoppedAt)
	assert.Nil(t, transaction.EnergyWh)
	assert.Len(t, transaction.MeterValues, 1, "only the begin sample")
	assert.Zero(t, outboxRows(t))
	requireNoOpenTransaction(t)
}

func TestOcpp16Events_StatusWhoseEventFailsStoresNothing(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()
	status, _, _, _ := ocpp16EventHandlers(failingOutboxFixture())

	_, err := status.HandleCall(ctx, "cs001", &types.StatusNotificationJson{
		ConnectorId: 1, ErrorCode: types.StatusNotificationJsonErrorCodeNoError, Status: types.StatusNotificationJsonStatusAvailable,
	})

	require.ErrorIs(t, err, errAppendFailed)
	statuses, err := testStore.ListConnectorStatuses(ctx, "cs001")
	require.NoError(t, err)
	assert.Empty(t, statuses)
	assert.Zero(t, outboxRows(t))
	requireNoOpenTransaction(t)
}
