// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	clockTest "k8s.io/utils/clock/testing"
)

// The org of the session events: a session stays with the org that owned it, also when
// its charge station moves to another org while it runs.

const (
	sessionOrgA       = "0193b2c4-0000-7000-8000-0000000000a1"
	sessionOrgB       = "0193b2c4-0000-7000-8000-0000000000b2"
	sessionOrgStation = "aaaa.CP001"
)

// sessionOrgFixture is the StatusNotification, StartTransaction, MeterValues and
// StopTransaction handlers on one in-memory store. Their events are recorded, and an
// event without an org gets the org that the charge station has at that moment, as in
// the manager.
type sessionOrgFixture struct {
	engine      *inmemory.Store
	events      *eventstest.Fixture
	status      handlers.StatusNotificationHandler
	start       handlers.StartTransactionHandler
	meterValues handlers.MeterValuesHandler
	stop        handlers.StopTransactionHandler
}

func newSessionOrgFixture(t *testing.T) *sessionOrgFixture {
	t.Helper()
	clk := clockTest.NewFakePassiveClock(time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC))
	engine := inmemory.NewStore(clk)
	orgs := &tenancy.Resolver{Stations: engine}
	domainEvents := eventstest.New(engine, sessionOrgA, clk)
	domainEvents.Events.Emitter = &events.OutboxEmitter{Store: engine, Orgs: orgs, Clock: clk, Product: events.Product}

	return &sessionOrgFixture{
		engine: engine,
		events: domainEvents,
		status: handlers.StatusNotificationHandler{Clock: clk, StatusStore: engine, Tx: domainEvents.Tx, Events: domainEvents.Events},
		start: handlers.StartTransactionHandler{
			Clock: clk, TokenStore: engine, OrgResolver: orgs, TransactionStore: engine, ReservationStore: engine,
			Tx: domainEvents.Tx, Events: domainEvents.Events,
		},
		meterValues: handlers.MeterValuesHandler{
			TransactionStore: engine, MeterValuesStore: engine, Tx: domainEvents.Tx, Events: domainEvents.Events,
		},
		stop: handlers.StopTransactionHandler{
			Clock: clk, TokenStore: engine, OrgResolver: orgs, TransactionStore: engine,
			Tx: domainEvents.Tx, Events: domainEvents.Events,
		},
	}
}

// stationIn makes orgId the org that owns the charge station.
func (f *sessionOrgFixture) stationIn(t *testing.T, orgId string) {
	t.Helper()
	require.NoError(t, f.engine.SetChargeStationAuth(context.Background(), sessionOrgStation,
		&store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgId}))
}

func (f *sessionOrgFixture) startTransaction(t *testing.T) int {
	t.Helper()
	return startTransaction(t, f.start, sessionOrgStation, &types.StartTransactionJson{
		ConnectorId: 1, IdTag: "MYRFIDTAG", MeterStart: 1000, Timestamp: "2026-11-28T11:00:00Z",
	}).TransactionId
}

func (f *sessionOrgFixture) sendMeterValues(t *testing.T, transactionId int) {
	t.Helper()
	_, err := f.meterValues.HandleCall(context.Background(), sessionOrgStation, &types.MeterValuesJson{
		ConnectorId: 1, TransactionId: &transactionId,
		MeterValue: []types.MeterValuesJsonMeterValueElem{meterValue("2026-11-28T11:15:00Z", meterValuesSample("1800", measurandEnergy, unitWh))},
	})
	require.NoError(t, err)
}

func (f *sessionOrgFixture) stopTransaction(t *testing.T, transactionId int) {
	t.Helper()
	_, err := f.stop.HandleCall(context.Background(), sessionOrgStation, &types.StopTransactionJson{
		TransactionId: transactionId, MeterStop: 2500, Timestamp: "2026-11-28T11:30:00Z",
	})
	require.NoError(t, err)
}

// takeOne returns the one event that was stored since the last take, after checking its
// type.
func (f *sessionOrgFixture) takeOne(t *testing.T, eventType string) *store.OutboxEvent {
	t.Helper()
	evs := f.events.Take(t)
	require.Equal(t, []string{eventType}, eventstest.Types(evs))
	return evs[0]
}

func TestSessionEventsStayWithTheOrgOfTheTransaction(t *testing.T) {
	f := newSessionOrgFixture(t)
	f.stationIn(t, sessionOrgA)
	transactionId := f.startTransaction(t)
	session := "session/" + handlers.ConvertToUUID(transactionId)

	started := f.takeOne(t, events.TypeSessionStarted)
	assert.Equal(t, sessionOrgA, started.OrgId)
	assert.Equal(t, "urn:cpms:"+sessionOrgA+":"+session, started.Subject)

	// the charge station moves to another org while the transaction runs
	f.stationIn(t, sessionOrgB)

	f.sendMeterValues(t, transactionId)
	updated := f.takeOne(t, events.TypeSessionUpdated)
	assert.Equal(t, sessionOrgA, updated.OrgId, "the session belongs to the org that started it")
	assert.Equal(t, "urn:cpms:"+sessionOrgA+":"+session, updated.Subject)
	// the payload is as before: the station of the session, with its key
	assert.Equal(t, sessionOrgStation, eventstest.Data[events.SessionUpdatedData](t, updated).StationKey)

	f.stopTransaction(t, transactionId)
	completed := f.takeOne(t, events.TypeSessionCompleted)
	assert.Equal(t, sessionOrgA, completed.OrgId, "the session belongs to the org that started it")
	assert.Equal(t, "urn:cpms:"+sessionOrgA+":"+session, completed.Subject)
	data := eventstest.Data[events.SessionCompletedData](t, completed)
	assert.Equal(t, sessionOrgStation, data.StationKey)
	assert.Equal(t, int64Ptr(1500), data.EnergyWh)

	// what is about the charge station itself goes to the org that owns it now
	_, err := f.status.HandleCall(context.Background(), sessionOrgStation,
		statusNotification(1, types.StatusNotificationJsonStatusAvailable, types.StatusNotificationJsonErrorCodeNoError))
	require.NoError(t, err)
	assert.Equal(t, sessionOrgB, f.takeOne(t, events.TypeConnectorStatusChanged).OrgId)

	// and so does the next session
	next := f.startTransaction(t)
	assert.Equal(t, sessionOrgB, f.takeOne(t, events.TypeSessionStarted).OrgId)
	f.sendMeterValues(t, next)
	assert.Equal(t, sessionOrgB, f.takeOne(t, events.TypeSessionUpdated).OrgId)
	f.stopTransaction(t, next)
	assert.Equal(t, sessionOrgB, f.takeOne(t, events.TypeSessionCompleted).OrgId)
}

// orglessTransactionStore reads transactions without their org, as a store that does
// not keep the org of a transaction does (Firestore).
type orglessTransactionStore struct {
	store.TransactionStore
}

func (s orglessTransactionStore) FindTransaction(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	found, err := s.TransactionStore.FindTransaction(ctx, chargeStationId, transactionId)
	if err != nil || found == nil {
		return found, err
	}
	withoutOrg := *found
	withoutOrg.OrgId = ""
	return &withoutOrg, nil
}

func TestSessionEventsWithoutAStoredOrgFollowTheStation(t *testing.T) {
	for name, prepare := range map[string]func(t *testing.T, f *sessionOrgFixture){
		"a store that does not keep the org of a transaction": func(t *testing.T, f *sessionOrgFixture) {
			f.stationIn(t, sessionOrgA)
			transactions := orglessTransactionStore{TransactionStore: f.engine}
			f.start.TransactionStore, f.meterValues.TransactionStore, f.stop.TransactionStore = transactions, transactions, transactions
		},
		// the transaction of a charge station that is not registered belongs to the legacy
		// org, which is not an org of its own
		"a transaction of the legacy org": func(*testing.T, *sessionOrgFixture) {},
	} {
		t.Run(name, func(t *testing.T) {
			f := newSessionOrgFixture(t)
			prepare(t, f)
			transactionId := f.startTransaction(t)
			f.events.Take(t)
			stored, err := f.engine.FindTransaction(context.Background(), sessionOrgStation, handlers.ConvertToUUID(transactionId))
			require.NoError(t, err)
			require.NotNil(t, stored)
			require.NotEqual(t, sessionOrgB, stored.OrgId)

			f.stationIn(t, sessionOrgB)

			f.sendMeterValues(t, transactionId)
			assert.Equal(t, sessionOrgB, f.takeOne(t, events.TypeSessionUpdated).OrgId)
			f.stopTransaction(t, transactionId)
			completed := f.takeOne(t, events.TypeSessionCompleted)
			assert.Equal(t, sessionOrgB, completed.OrgId)
			assert.Equal(t, "urn:cpms:"+sessionOrgB+":session/"+handlers.ConvertToUUID(transactionId), completed.Subject)
		})
	}
}

// The transaction that a stop creates for a transaction that is not known belongs to
// the org that owns the charge station at that moment.
func TestStopOfAnUnknownTransactionBelongsToTheOrgOfTheStation(t *testing.T) {
	f := newSessionOrgFixture(t)
	f.stationIn(t, sessionOrgA)
	f.stationIn(t, sessionOrgB)

	f.stopTransaction(t, 42)

	assert.Equal(t, sessionOrgB, f.takeOne(t, events.TypeSessionCompleted).OrgId)
	stored, err := f.engine.FindTransaction(context.Background(), sessionOrgStation, handlers.ConvertToUUID(42))
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, sessionOrgB, stored.OrgId)
}

// session.started is emitted in the org of the transaction as it is stored, not in the
// org that the charge station has when the event is emitted: the two differ when the
// charge station moves to another org in between.
func TestSessionStartedHasTheOrgOfTheStoredTransaction(t *testing.T) {
	f := newSessionOrgFixture(t)
	f.stationIn(t, sessionOrgA)
	// the org that the emitter finds for the charge station is already the new one
	f.events.Events.Emitter = &events.OutboxEmitter{
		Store: f.engine, Orgs: events.StaticOrgResolver{OrgId: sessionOrgB}, Product: events.Product,
	}

	transactionId := f.startTransaction(t)

	started := f.takeOne(t, events.TypeSessionStarted)
	stored, err := f.engine.FindTransaction(context.Background(), sessionOrgStation, handlers.ConvertToUUID(transactionId))
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, sessionOrgA, stored.OrgId)
	assert.Equal(t, stored.OrgId, started.OrgId, "the event and the stored transaction name the same org")
}
