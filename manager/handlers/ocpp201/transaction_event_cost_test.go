// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clockTest "k8s.io/utils/clock/testing"

	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	msghandlers "github.com/thoughtworks/maeve-csms/manager/handlers"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/money"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/sessions"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tariffs"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// The cost of a transaction: so far with the Updated events, final with the Ended event,
// whose answer has it, stored with the transaction, and never a reason to fail an event.

// fallbackSessions is a session service on the store that rates a transaction without a
// tariff at 0.55 per kWh.
func fallbackSessions(engine store.Engine) *sessions.Service {
	return &sessions.Service{
		Transactions: engine,
		MeterValues:  engine,
		Tariffs:      engine,
		Assets:       engine,
		Registry:     engine,
		Tx:           engine,
		Fallback:     &sessions.FallbackTariff{Currency: "EUR", PricePerKwh: 5500},
	}
}

// rated gives the handler of the fixture a session service on the store of the fixture,
// with the Analyzer and the clock of the handler. It rates with every Updated event.
func rated(f *tariffFixture) *tariffFixture {
	f.handler.Sessions = &sessions.Service{
		Analyzer:     f.handler.Analyzer,
		Transactions: f.engine,
		MeterValues:  f.engine,
		Tariffs:      f.engine,
		Assets:       f.engine,
		Registry:     f.engine,
		Tx:           f.engine,
		Clock:        f.handler.Clock,
	}
	return f
}

// newCostFixture is the tariff fixture with an Analyzer and a session service.
func newCostFixture(t *testing.T) *tariffFixture {
	t.Helper()
	return rated(newAnalysisFixture(t))
}

func (f *tariffFixture) setTime(at time.Time) {
	f.handler.Clock.(*clockTest.FakePassiveClock).SetTime(at)
}

// parkingTariffVersion is energy at 0.30 per kWh and parking at 0.50 per 15 minutes, the
// first 30 minutes free, VAT 21%.
func parkingTariffVersion(t *testing.T) *store.TariffVersion {
	t.Helper()
	vat := money.Percent(2100)
	v := &store.TariffVersion{Currency: "EUR", Elements: []store.TariffElement{{PriceComponents: []store.PriceComponent{
		{Type: store.PriceComponentEnergy, Price: 3000, Vat: &vat, StepSize: 1},
		{Type: store.PriceComponentParkingTime, Vat: &vat, StepSize: 900, GracePeriodS: makePtr(int32(1800)), PricePerStep: makePtr(money.Amount(5000))},
	}}}}
	require.Empty(t, tariffs.Validate(v))
	return v
}

// answer handles the event and returns the answer.
func (f *tariffFixture) answer(t *testing.T, req *types.TransactionEventRequestJson) *types.TransactionEventResponseJson {
	t.Helper()
	resp, err := f.handler.HandleCall(context.Background(), tariffStation, req)
	require.NoError(t, err)
	require.IsType(t, &types.TransactionEventResponseJson{}, resp)
	return resp.(*types.TransactionEventResponseJson)
}

func (f *tariffFixture) stored(t *testing.T, transactionId string) *store.Transaction {
	t.Helper()
	transaction, err := f.engine.FindTransaction(context.Background(), tariffStation, transactionId)
	require.NoError(t, err)
	require.NotNil(t, transaction)
	return transaction
}

// The session of the worked example: 11 kWh in an hour, then 100 minutes idle, of which
// 30 are free and 70 are charged as 5 steps: 5.80 without and 7.02 with VAT. The answer
// to the Ended event has the total with VAT.
func TestEndedEventAnswersWithTheTotalCost(t *testing.T) {
	f := newCostFixture(t)
	require.NoError(t, f.engine.AddTariffVersion(context.Background(), f.regular.Id, 1, parkingTariffVersion(t)))

	started := f.answer(t, analysisEvent(types.TransactionEventEnumTypeStarted, "tx-1", 0, 0, 100_000, 1, 1))
	assert.Nil(t, started.TotalCost)
	for seqNo, reading := range [][2]float64{{30, 105_500}, {60, 111_000}, {90, 111_000}, {120, 111_000}} {
		updated := f.answer(t, analysisEvent(types.TransactionEventEnumTypeUpdated, "tx-1", seqNo+1, int(reading[0]), reading[1], 1, 1))
		assert.Nil(t, updated.TotalCost, "only the answer to the end has a total cost")
	}

	ended := f.answer(t, analysisEvent(types.TransactionEventEnumTypeEnded, "tx-1", 5, 160, 111_000, 1, 1))

	assert.Equal(t, makePtr(7.02), ended.TotalCost)
	transaction := f.stored(t, "tx-1")
	assertTariff(t, f.regular, 2, "connector", transaction)
	assert.Equal(t, &store.TransactionCost{
		Currency: "EUR", Total: money.Price{ExclVat: 5_8000, InclVat: 7_0200}, Final: true, RatedAt: tariffClockTime,
	}, transaction.Cost)
	assert.Equal(t, makePtr(7.02), transaction.LastCost)
	require.NotNil(t, transaction.Analysis)
	assert.Equal(t, int64(6000), transaction.Analysis.IdleS)
}

// An Updated event works out the cost so far of the transaction and stores it, not more
// often than the interval of the session service allows. The Started event rates nothing.
func TestUpdatedEventUpdatesTheCostSoFar(t *testing.T) {
	f := newCostFixture(t)
	f.handler.Sessions.RunningEvery = time.Minute
	reads := &primaryReads{Engine: f.engine}
	f.handler.Sessions.Transactions = reads
	soFar := func(excl money.Amount, at time.Time) *store.TransactionCost {
		return &store.TransactionCost{Currency: "EUR", Total: money.Price{ExclVat: excl, InclVat: excl}, RatedAt: at}
	}

	f.setTime(analysisSessionStart)
	f.answer(t, analysisEvent(types.TransactionEventEnumTypeStarted, "tx-1", 0, 0, 100_000, 1, 1))
	assert.Nil(t, f.stored(t, "tx-1").Cost)
	assert.Zero(t, reads.finds+reads.withMeterValues, "the start of a transaction rates nothing")

	// 5.5 kWh at 0.30 after half an hour
	halfPast := analysisSessionStart.Add(30 * time.Minute)
	f.setTime(halfPast)
	f.answer(t, analysisEvent(types.TransactionEventEnumTypeUpdated, "tx-1", 1, 30, 105_500, 1, 1))
	assert.Equal(t, soFar(1_6500, halfPast), f.stored(t, "tx-1").Cost)
	assert.Equal(t, makePtr(1.65), f.stored(t, "tx-1").LastCost)
	assert.Equal(t, 1, reads.withMeterValues)

	// within the minute nothing is worked out, and the transaction is not read for it
	f.setTime(halfPast.Add(59 * time.Second))
	f.answer(t, analysisEvent(types.TransactionEventEnumTypeUpdated, "tx-1", 2, 31, 106_000, 1, 1))
	assert.Equal(t, soFar(1_6500, halfPast), f.stored(t, "tx-1").Cost)
	assert.Equal(t, 1, reads.finds)
	assert.Equal(t, 1, reads.withMeterValues)

	// after it: 7 kWh
	f.setTime(halfPast.Add(time.Minute))
	f.answer(t, analysisEvent(types.TransactionEventEnumTypeUpdated, "tx-1", 3, 32, 107_000, 1, 1))
	assert.Equal(t, soFar(2_1000, halfPast.Add(time.Minute)), f.stored(t, "tx-1").Cost)
	assert.Equal(t, 2, reads.withMeterValues)
}

// A transaction is rated with the tariff version that it started under.
func TestTheTotalCostIsThatOfTheVersionOfTheStart(t *testing.T) {
	f := newCostFixture(t)
	f.answer(t, analysisEvent(types.TransactionEventEnumTypeStarted, "tx-1", 0, 0, 100_000, 1, 1))

	// the price doubles during the session
	require.NoError(t, f.engine.AddTariffVersion(context.Background(), f.regular.Id, 1, tariffVersion(6000)))
	f.answer(t, analysisEvent(types.TransactionEventEnumTypeUpdated, "tx-1", 1, 30, 105_500, 1, 1))
	ended := f.answer(t, analysisEvent(types.TransactionEventEnumTypeEnded, "tx-1", 2, 60, 111_000, 1, 1))

	// 11 kWh at 0.30
	assert.Equal(t, makePtr(3.3), ended.TotalCost)
	assertTariff(t, f.regular, 1, "connector", f.stored(t, "tx-1"))
}

// A transaction without a tariff has no price, and the answer no total cost, unless the
// session service has a price for such transactions.
func TestEndedEventOfATransactionWithoutATariff(t *testing.T) {
	session := func(t *testing.T, f *tariffFixture) *types.TransactionEventResponseJson {
		t.Helper()
		// EVSE 2 has no tariff
		f.answer(t, analysisEvent(types.TransactionEventEnumTypeStarted, "tx-1", 0, 0, 100_000, 2, 1))
		f.answer(t, analysisEvent(types.TransactionEventEnumTypeUpdated, "tx-1", 1, 30, 105_500, 2, 1))
		return f.answer(t, analysisEvent(types.TransactionEventEnumTypeEnded, "tx-1", 2, 60, 111_000, 2, 1))
	}

	t.Run("no price", func(t *testing.T) {
		f := newCostFixture(t)
		ended := session(t, f)
		assert.Nil(t, ended.TotalCost)
		transaction := f.stored(t, "tx-1")
		assertTariff(t, nil, 0, "none", transaction)
		assert.Nil(t, transaction.Cost)
		assert.Nil(t, transaction.LastCost)
	})

	t.Run("the fallback price", func(t *testing.T) {
		f := newCostFixture(t)
		f.handler.Sessions.Fallback = &sessions.FallbackTariff{Currency: "EUR", PricePerKwh: 5500}
		ended := session(t, f)
		// 11 kWh at 0.55
		assert.Equal(t, makePtr(6.05), ended.TotalCost)
		assert.Equal(t, &store.TransactionCost{
			Currency: "EUR", Total: money.Price{ExclVat: 6_0500, InclVat: 6_0500}, Final: true, RatedAt: tariffClockTime,
		}, f.stored(t, "tx-1").Cost)
	})
}

func TestTransactionEventWithoutASessionServiceRatesNothing(t *testing.T) {
	f := newAnalysisFixture(t)

	ended := f.answer(t, chargeAndPark(t, f, "tx-1"))

	assert.Nil(t, ended.TotalCost)
	transaction := f.stored(t, "tx-1")
	assert.Nil(t, transaction.Cost)
	assert.Nil(t, transaction.LastCost)
}

// ratingBackend is the transaction store of a session service. It notes how the
// transaction is read for the final cost, and fails or panics when it is told to.
type ratingBackend struct {
	store.TransactionStore
	err       error
	panicWith any
	reads     int
	// of the last read: whether it was in a database transaction, the deadline of its
	// ctx, and whether the transaction had ended and had its analysis
	inTx        bool
	hasDeadline bool
	deadlineIn  time.Duration
	ended       bool
	analysed    bool
}

func (s *ratingBackend) FindTransactionSummary(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	s.reads++
	s.inTx = eventstest.InTx(ctx)
	var deadline time.Time
	if deadline, s.hasDeadline = ctx.Deadline(); s.hasDeadline {
		s.deadlineIn = time.Until(deadline)
	}
	if s.panicWith != nil {
		panic(s.panicWith)
	}
	if s.err != nil {
		return nil, s.err
	}
	transaction, err := s.TransactionStore.FindTransactionSummary(ctx, chargeStationId, transactionId)
	if transaction != nil {
		s.ended, s.analysed = transaction.EndedSeqNo > 0, transaction.Analysis != nil
	}
	return transaction, err
}

// The final cost is worked out once the end of the transaction and its analysis are
// stored, in a database transaction (on the primary, where they are), with a deadline.
func TestTheFinalCostComesAfterTheEndAndTheAnalysis(t *testing.T) {
	f := newCostFixture(t)
	fixture := eventstest.New(f.engine, tariffOrgA, f.handler.Clock)
	f.handler.Store = fixture.Engine(f.engine)
	f.handler.Sessions.Tx = fixture.Tx
	f.handler.Sessions.RunningEvery = time.Hour
	backend := &ratingBackend{TransactionStore: f.engine}
	f.handler.Sessions.Transactions = backend

	ended := chargeAndPark(t, f, "tx-1")
	require.Equal(t, 1, backend.reads, "the cost so far, once in the hour")
	assert.False(t, backend.inTx)
	assert.True(t, backend.hasDeadline)

	answer := f.answer(t, ended)

	assert.Equal(t, makePtr(3.3), answer.TotalCost)
	assert.Equal(t, 2, backend.reads)
	assert.True(t, backend.ended, "the rating reads the transaction after its end is stored")
	assert.True(t, backend.analysed, "and after its analysis is")
	assert.True(t, backend.inTx)
	require.True(t, backend.hasDeadline, "the answer does not wait long for the rating")
	assert.LessOrEqual(t, backend.deadlineIn, sessions.RatingTimeout)
	assert.Positive(t, backend.deadlineIn)
}

// An event is answered as ever when the rating of its transaction fails: an Ended event
// without a total cost.
func TestTransactionEventIsAnsweredWhenTheRatingFails(t *testing.T) {
	// the answers of the handler without a session service
	plain := newAnalysisFixture(t)
	wantEnded := plain.answer(t, chargeAndPark(t, plain, "tx-1"))
	require.Nil(t, wantEnded.TotalCost)

	for name, backend := range map[string]*ratingBackend{
		"the transaction cannot be read": {err: errors.New("the store is down")},
		"the rating panics":              {panicWith: "the rating broke"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newCostFixture(t)
			backend.TransactionStore = f.engine
			f.handler.Sessions.Transactions = backend

			ended := chargeAndPark(t, f, "tx-1")
			assert.Equal(t, 3, backend.reads, "the cost so far was tried with every Updated event")
			assert.Equal(t, wantEnded, f.answer(t, ended))
			assert.Equal(t, 4, backend.reads, "the final cost was tried")

			transaction := f.stored(t, "tx-1")
			assert.Equal(t, 4, transaction.EndedSeqNo, "the end of the transaction is stored")
			assert.NotNil(t, transaction.Analysis, "and so is its analysis")
			assert.Nil(t, transaction.Cost)
		})
	}
}

// The router gives its session service to the handler of the transaction events.
func TestNewRouterGivesTheSessionServiceToTheTransactionEventHandler(t *testing.T) {
	clk := clockTest.NewFakePassiveClock(tariffClockTime)
	engine := inmemory.NewStore(clk)
	service := fallbackSessions(engine)

	router, ok := handlers.NewRouter(&fakeEmitter{}, clk, engine, &tenancy.Resolver{Stations: engine}, nil,
		&fakeCertValidationService{}, &fakeChargeStationCertProvider{}, &fakeContractCertProvider{}, 5*time.Minute, schemas.OcppSchemas,
		nil, nil, nil, service).(*msghandlers.Router)
	require.True(t, ok)

	handler, ok := router.CallRoutes["TransactionEvent"].Handler.(handlers.TransactionEventHandler)
	require.True(t, ok)
	assert.Same(t, service, handler.Sessions)
}
