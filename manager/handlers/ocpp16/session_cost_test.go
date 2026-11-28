// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clockTest "k8s.io/utils/clock/testing"

	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	msghandlers "github.com/thoughtworks/maeve-csms/manager/handlers"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/money"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/sessions"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tariffs"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// The cost of a transaction: so far with its meter values, final with its stop, in the
// session events and stored with the transaction, and never a reason to fail a message.

// newCostFixture is the analysis fixture with a session service on its store for the
// handlers of the meter values and of the stop. It rates with every MeterValues.req.
func newCostFixture(t *testing.T) *analysisFixture {
	t.Helper()
	f := newAnalysisFixture(t)
	service := &sessions.Service{
		Analyzer:     f.stop.Analyzer,
		Transactions: f.engine,
		MeterValues:  f.engine,
		Tariffs:      f.engine,
		Assets:       f.engine,
		Registry:     f.engine,
		Tx:           f.events.Tx,
		Clock:        f.start.Clock,
	}
	f.meterValues.Sessions, f.stop.Sessions = service, service
	return f
}

func (f *analysisFixture) setTime(at time.Time) {
	f.start.Clock.(*clockTest.FakePassiveClock).SetTime(at)
}

// parkingTariffVersion is energy at 0.30 per kWh and parking at 0.50 per 15 minutes, the
// first 30 minutes free, VAT 21%.
func parkingTariffVersion(t *testing.T) *store.TariffVersion {
	t.Helper()
	vat := money.Percent(2100)
	v := &store.TariffVersion{Currency: "EUR", Elements: []store.TariffElement{{PriceComponents: []store.PriceComponent{
		{Type: store.PriceComponentEnergy, Price: 3000, Vat: &vat, StepSize: 1},
		{Type: store.PriceComponentParkingTime, Vat: &vat, StepSize: 900, GracePeriodS: ptr(int32(1800)), PricePerStep: ptr(money.Amount(5000))},
	}}}}
	require.Empty(t, tariffs.Validate(v))
	return v
}

// reading sends a MeterValues.req of the transaction with a reading of the energy
// register in Wh at the minute of the transaction.
func (f *analysisFixture) reading(t *testing.T, transactionId, minute, registerWh int) {
	t.Helper()
	_, err := f.meterValues.HandleCall(context.Background(), sessionOrgStation, &types.MeterValuesJson{
		ConnectorId:   1,
		TransactionId: &transactionId,
		MeterValue: []types.MeterValuesJsonMeterValueElem{{
			Timestamp:    analysisMinute(minute),
			SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{{Value: strconv.Itoa(registerWh)}},
		}},
	})
	require.NoError(t, err)
}

// sentMeterValues returns the number of meter values that are stored for the charge
// station of the fixture: those that it sent on their own.
func (f *analysisFixture) sentMeterValues(t *testing.T) int {
	t.Helper()
	result, err := f.engine.QueryMeterValues(context.Background(), store.MeterValuesFilter{ChargeStationId: sessionOrgStation, Limit: 100})
	require.NoError(t, err)
	return result.Total
}

// updated returns the payloads of the session.updated events since the last call.
func (f *analysisFixture) updated(t *testing.T) []events.SessionUpdatedData {
	t.Helper()
	var payloads []events.SessionUpdatedData
	for _, ev := range f.events.Take(t) {
		if ev.Type == events.TypeSessionUpdated {
			payloads = append(payloads, eventstest.Data[events.SessionUpdatedData](t, ev))
		}
	}
	return payloads
}

// completed returns the payload of the one session.completed event since the last call.
func (f *analysisFixture) completed(t *testing.T) events.SessionCompletedData {
	t.Helper()
	var payloads []events.SessionCompletedData
	for _, ev := range f.events.Take(t) {
		if ev.Type == events.TypeSessionCompleted {
			payloads = append(payloads, eventstest.Data[events.SessionCompletedData](t, ev))
		}
	}
	require.Len(t, payloads, 1)
	return payloads[0]
}

// The session of the worked example: 11 kWh in an hour, then 100 minutes idle, of which
// 30 are free and 70 are charged as 5 steps: 5.80 without and 7.02 with VAT. The cost is
// in the session.completed event and is stored with the transaction.
func TestStopTransactionRatesTheTransaction(t *testing.T) {
	f := newCostFixture(t)
	require.NoError(t, f.engine.AddTariffVersion(context.Background(), f.regular.Id, 1, parkingTariffVersion(t)))
	transactionId := f.charge(t, 1, 30, 105_500, 60, 111_000, 90, 111_000, 120, 111_000)
	f.events.Take(t)

	transaction := f.stopAt(t, transactionId, 160, 111_000)

	assert.Equal(t, &events.SessionCost{
		Currency: "EUR", ExclVat: 5_8000, InclVat: 7_0200, Final: true,
		TariffId: &f.regular.Id, TariffVersion: ptr(2),
		EnergyWh: 11_000, ChargingS: 3600, ParkingS: 6000,
	}, f.completed(t).Cost)
	assert.Equal(t, &store.TransactionCost{
		Currency: "EUR", Total: money.Price{ExclVat: 5_8000, InclVat: 7_0200}, Final: true, RatedAt: tariffClockTime,
	}, transaction.Cost)
	assert.Equal(t, ptr(7.02), transaction.LastCost)
	require.NotNil(t, transaction.Analysis)
	assert.Equal(t, int64(6000), transaction.Analysis.IdleS)
}

// A MeterValues.req of a transaction works out its cost so far, with the readings of the
// request, not more often than the interval of the session service allows. The cost is in
// the session.updated event of the request and is stored with the transaction.
func TestMeterValuesUpdateTheCostSoFar(t *testing.T) {
	f := newCostFixture(t)
	f.meterValues.Sessions.RunningEvery = time.Minute
	reads := &ratingBackend{TransactionStore: f.engine}
	f.meterValues.Sessions.Transactions = reads
	f.setTime(analysisSessionStart)
	transactionId := f.charge(t, 1)
	f.events.Take(t)
	assert.Zero(t, reads.reads, "the start of a transaction rates nothing")
	soFar := func(excl money.Amount, energyWh, chargingS int64) *events.SessionCost {
		return &events.SessionCost{
			Currency: "EUR", ExclVat: excl, InclVat: excl, TariffId: &f.regular.Id, TariffVersion: ptr(1),
			EnergyWh: energyWh, ChargingS: chargingS,
		}
	}

	// 5.5 kWh at 0.30 after half an hour: the reading of the request counts
	halfPast := analysisSessionStart.Add(30 * time.Minute)
	f.setTime(halfPast)
	f.reading(t, transactionId, 30, 105_500)
	updated := f.updated(t)
	require.Len(t, updated, 1)
	assert.Equal(t, soFar(1_6500, 5500, 1800), updated[0].Cost)
	assert.Equal(t, ptr(int64(5500)), updated[0].EnergyWh)
	transaction := f.stored(t, transactionId)
	assert.Equal(t, &store.TransactionCost{Currency: "EUR", Total: money.Price{ExclVat: 1_6500, InclVat: 1_6500}, RatedAt: halfPast}, transaction.Cost)
	assert.Equal(t, ptr(1.65), transaction.LastCost)
	assert.Equal(t, 1, reads.reads)

	// within the minute nothing is worked out: the event has no cost, the transaction
	// keeps the one that it has, and nothing is read for it
	f.setTime(halfPast.Add(59 * time.Second))
	f.reading(t, transactionId, 31, 106_000)
	updated = f.updated(t)
	require.Len(t, updated, 1)
	assert.Nil(t, updated[0].Cost)
	assert.Equal(t, ptr(int64(6000)), updated[0].EnergyWh)
	assert.Equal(t, halfPast, f.stored(t, transactionId).Cost.RatedAt)
	assert.Equal(t, 1, reads.reads)

	// after it: 7 kWh
	f.setTime(halfPast.Add(time.Minute))
	f.reading(t, transactionId, 32, 107_000)
	updated = f.updated(t)
	require.Len(t, updated, 1)
	assert.Equal(t, soFar(2_1000, 7000, 1920), updated[0].Cost)
	assert.Equal(t, money.Price{ExclVat: 2_1000, InclVat: 2_1000}, f.stored(t, transactionId).Cost.Total)
	assert.Equal(t, 2, reads.reads)
}

// Meter values that are not of a transaction that can be rated are stored as ever.
func TestMeterValuesThatAreNotRated(t *testing.T) {
	ctx := context.Background()
	meterValue := []types.MeterValuesJsonMeterValueElem{{
		Timestamp:    analysisMinute(30),
		SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{{Value: "105500"}},
	}}

	t.Run("without a transaction", func(t *testing.T) {
		f := newCostFixture(t)
		reads := &ratingBackend{TransactionStore: f.engine}
		f.meterValues.Sessions.Transactions = reads
		_, err := f.meterValues.HandleCall(ctx, sessionOrgStation, &types.MeterValuesJson{ConnectorId: 0, MeterValue: meterValue})
		require.NoError(t, err)
		assert.Zero(t, reads.reads)
		assert.Equal(t, 1, f.sentMeterValues(t))
	})

	t.Run("of a transaction that is not known", func(t *testing.T) {
		f := newCostFixture(t)
		_, err := f.meterValues.HandleCall(ctx, sessionOrgStation, &types.MeterValuesJson{ConnectorId: 1, TransactionId: ptr(4711), MeterValue: meterValue})
		require.NoError(t, err)
		assert.Equal(t, 1, f.sentMeterValues(t))
		assert.Empty(t, f.events.Take(t))
	})

	t.Run("of a transaction without a price", func(t *testing.T) {
		f := newCostFixture(t)
		transactionId := f.charge(t, 2, 30, 105_500)
		updated := f.updated(t)
		require.Len(t, updated, 1)
		assert.Nil(t, updated[0].Cost)
		assert.Nil(t, f.stored(t, transactionId).Cost)
	})

	t.Run("of a transaction that has ended", func(t *testing.T) {
		f := newCostFixture(t)
		transactionId := f.charge(t, 1, 30, 105_500)
		final := f.stopAt(t, transactionId, 60, 111_000).Cost
		require.NotNil(t, final)
		require.True(t, final.Final)
		f.events.Take(t)

		// a reading that comes after the stop
		f.reading(t, transactionId, 59, 110_000)
		updated := f.updated(t)
		require.Len(t, updated, 1)
		assert.Nil(t, updated[0].Cost)
		assert.Equal(t, final, f.stored(t, transactionId).Cost, "the final cost stays")
	})

	t.Run("without a session service", func(t *testing.T) {
		f := newAnalysisFixture(t)
		transactionId := f.charge(t, 1, 30, 105_500)
		updated := f.updated(t)
		require.Len(t, updated, 1)
		assert.Nil(t, updated[0].Cost)
		transaction := f.stopAt(t, transactionId, 60, 111_000)
		assert.Nil(t, transaction.Cost)
		assert.Nil(t, f.completed(t).Cost)
	})
}

// A transaction without a tariff has no cost, unless the session service has a price for
// such transactions. Its events then name no tariff.
func TestStopTransactionOfATransactionWithoutATariff(t *testing.T) {
	t.Run("no price", func(t *testing.T) {
		f := newCostFixture(t)
		// connector 2 has no tariff
		transactionId := f.charge(t, 2, 30, 105_500)
		f.events.Take(t)
		transaction := f.stopAt(t, transactionId, 60, 111_000)
		assert.Nil(t, f.completed(t).Cost)
		assert.Nil(t, transaction.Cost)
		assert.Nil(t, transaction.LastCost)
	})

	t.Run("the fallback price", func(t *testing.T) {
		f := newCostFixture(t)
		f.stop.Sessions.Fallback = &sessions.FallbackTariff{Currency: "EUR", PricePerKwh: 5500}
		transactionId := f.charge(t, 2, 30, 105_500)
		updated := f.updated(t)
		require.Len(t, updated, 1)
		require.NotNil(t, updated[0].Cost)
		assert.Nil(t, updated[0].Cost.TariffId)
		assert.Nil(t, updated[0].Cost.TariffVersion)

		transaction := f.stopAt(t, transactionId, 60, 111_000)
		// 11 kWh at 0.55
		assert.Equal(t, &events.SessionCost{Currency: "EUR", ExclVat: 6_0500, InclVat: 6_0500, Final: true, EnergyWh: 11_000, ChargingS: 3600},
			f.completed(t).Cost)
		assert.Equal(t, money.Price{ExclVat: 6_0500, InclVat: 6_0500}, transaction.Cost.Total)
	})

	t.Run("a transaction that the manager never saw start", func(t *testing.T) {
		f := newCostFixture(t)
		f.stop.Sessions.Fallback = &sessions.FallbackTariff{Currency: "EUR", PricePerKwh: 5500}
		transaction := f.stopAt(t, 4711, 150, 111_000)
		// nothing is known of it but its stop: its event has no cost, and what is stored
		// of it afterwards has no energy to charge for
		assert.Nil(t, f.completed(t).Cost)
		assert.Equal(t, &store.TransactionCost{Currency: "EUR", Final: true, RatedAt: tariffClockTime}, transaction.Cost)
	})
}

// A transaction is rated with the tariff version that it started under.
func TestStopTransactionRatesWithTheVersionOfTheStart(t *testing.T) {
	f := newCostFixture(t)
	transactionId := f.charge(t, 1, 30, 105_500)
	// the price doubles during the session
	require.NoError(t, f.engine.AddTariffVersion(context.Background(), f.regular.Id, 1, tariffVersion(6000)))
	f.events.Take(t)

	transaction := f.stopAt(t, transactionId, 60, 111_000)

	// 11 kWh at 0.30
	cost := f.completed(t).Cost
	require.NotNil(t, cost)
	assert.Equal(t, money.Amount(3_3000), cost.ExclVat)
	assert.Equal(t, ptr(1), cost.TariffVersion)
	assert.Equal(t, money.Price{ExclVat: 3_3000, InclVat: 3_3000}, transaction.Cost.Total)
}

// ratingBackend is the transaction store of a session service. It notes what the reads of
// a transaction for a rating saw and the writes of a cost, and fails or panics when it is
// told to.
type ratingBackend struct {
	store.TransactionStore
	// failReads fails so many of the first reads with err
	failReads int
	err       error
	panicWith any
	reads     int
	costs     int
	// of each read: whether it was in a database transaction and whether the transaction
	// had ended and had its analysis
	inTx, ended, analysed []bool
	// of the last read: the deadline of its ctx
	hasDeadline bool
	deadlineIn  time.Duration
	// log, when set, is told of every read and of every write of a cost
	log func(string)
}

func (s *ratingBackend) FindTransactionSummary(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	s.reads++
	var deadline time.Time
	if deadline, s.hasDeadline = ctx.Deadline(); s.hasDeadline {
		s.deadlineIn = time.Until(deadline)
	}
	if s.log != nil {
		s.log("rating reads the transaction")
	}
	if s.panicWith != nil {
		panic(s.panicWith)
	}
	if s.reads <= s.failReads || (s.failReads == 0 && s.err != nil) {
		return nil, s.err
	}
	transaction, err := s.TransactionStore.FindTransactionSummary(ctx, chargeStationId, transactionId)
	s.inTx = append(s.inTx, eventstest.InTx(ctx))
	s.ended = append(s.ended, transaction != nil && transaction.StoppedAt != nil)
	s.analysed = append(s.analysed, transaction != nil && transaction.Analysis != nil)
	return transaction, err
}

func (s *ratingBackend) SetTransactionCost(ctx context.Context, chargeStationId, transactionId string, cost store.TransactionCost) error {
	s.costs++
	if s.log != nil {
		s.log("cost is stored")
	}
	return s.TransactionStore.SetTransactionCost(ctx, chargeStationId, transactionId, cost)
}

// The cost so far is worked out before the database transaction that stores the meter
// values and outside of it, and stored after it.
func TestMeterValuesAreRatedOutsideTheirDatabaseTransaction(t *testing.T) {
	f := newCostFixture(t)
	transactionId := f.charge(t, 1)
	var steps []string
	log := func(step string) { steps = append(steps, step) }
	backend := &ratingBackend{TransactionStore: f.engine, log: log}
	f.meterValues.Sessions.Transactions = backend
	f.meterValues.Tx = loggingTx{Transactor: f.events.Tx, log: log}

	f.reading(t, transactionId, 30, 105_500)

	assert.Equal(t, []string{"rating reads the transaction", "begin", "end", "cost is stored"}, steps)
	assert.Equal(t, []bool{false}, backend.inTx)
	require.True(t, backend.hasDeadline, "the answer does not wait long for the rating")
	assert.LessOrEqual(t, backend.deadlineIn, sessions.RatingTimeout)
	assert.Positive(t, backend.deadlineIn)
	assert.NotNil(t, f.stored(t, transactionId).Cost)
}

// failingMeterValues is a meter values store that cannot store.
type failingMeterValues struct {
	store.MeterValuesStore
	err error
}

func (s failingMeterValues) StoreMeterValues(context.Context, string, int, string, []store.MeterValue) error {
	return s.err
}

// Meter values are stored and their event is emitted when the rating fails, without a
// cost; and no cost is stored for meter values that could not be stored.
func TestMeterValuesAreStoredWhenTheRatingFails(t *testing.T) {
	for name, backend := range map[string]*ratingBackend{
		"the transaction cannot be read": {err: errors.New("the store is down")},
		"the rating panics":              {panicWith: "the rating broke"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newCostFixture(t)
			transactionId := f.charge(t, 1)
			f.events.Take(t)
			backend.TransactionStore = f.engine
			f.meterValues.Sessions.Transactions = backend

			f.reading(t, transactionId, 30, 105_500)

			assert.Equal(t, 1, backend.reads, "the rating was tried")
			updated := f.updated(t)
			require.Len(t, updated, 1)
			assert.Nil(t, updated[0].Cost)
			assert.Equal(t, ptr(int64(5500)), updated[0].EnergyWh)
			assert.Equal(t, 1, f.sentMeterValues(t))
			assert.Nil(t, f.stored(t, transactionId).Cost)
		})
	}

	t.Run("meter values that cannot be stored", func(t *testing.T) {
		f := newCostFixture(t)
		transactionId := f.charge(t, 1)
		backend := &ratingBackend{TransactionStore: f.engine}
		f.meterValues.Sessions.Transactions = backend
		errDown := errors.New("the store is down")
		f.meterValues.MeterValuesStore = failingMeterValues{MeterValuesStore: f.engine, err: errDown}

		_, err := f.meterValues.HandleCall(context.Background(), sessionOrgStation, &types.MeterValuesJson{
			ConnectorId: 1, TransactionId: &transactionId,
			MeterValue: []types.MeterValuesJsonMeterValueElem{{
				Timestamp:    analysisMinute(30),
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{{Value: "105500"}},
			}},
		})

		require.ErrorIs(t, err, errDown)
		assert.Equal(t, 1, backend.reads)
		assert.Zero(t, backend.costs, "the cost of readings that are not stored is not stored either")
		assert.Nil(t, f.stored(t, transactionId).Cost)
	})
}

// The transaction is rated twice at its stop. Before the stop is stored, in a database
// transaction of its own, for the event that is emitted with the stop; and once the stop
// and the analysis are stored, again in a database transaction of its own, for the cost
// that is stored: a rating that fails cannot undo the stop. The answer does not wait long
// for either.
func TestStopTransactionRatesBeforeAndAfterTheStopIsStored(t *testing.T) {
	f := newCostFixture(t)
	transactionId := f.charge(t, 1, 60, 111_000)
	f.events.Take(t)
	var steps []string
	log := func(step string) { steps = append(steps, step) }
	backend := &ratingBackend{TransactionStore: f.engine, log: log}
	f.stop.Sessions.Transactions = backend
	f.stop.Sessions.Tx = loggingTx{Transactor: f.events.Tx, log: log}
	f.stop.Analyzer.Transactions = &analysisBackend{Engine: f.engine, log: log}
	f.stop.Tx = loggingTx{Transactor: f.events.Tx, log: log}
	f.stop.CommandTracker = loggingTracker{log: log}

	transaction := f.stopAt(t, transactionId, 150, 111_000)

	assert.Equal(t, []string{
		"begin", "rating reads the transaction", "end",
		"begin", "end",
		"tracker hears of the stop",
		"begin", "analysis reads the transaction", "end",
		"begin", "rating reads the transaction", "cost is stored", "end",
	}, steps)
	assert.Equal(t, []bool{true, true}, backend.inTx)
	assert.Equal(t, []bool{false, true}, backend.ended, "the first rating is of the transaction before its stop is stored")
	assert.Equal(t, []bool{false, true}, backend.analysed, "the second one has the analysis")
	require.True(t, backend.hasDeadline, "the answer does not wait long for the rating")
	assert.LessOrEqual(t, backend.deadlineIn, sessions.RatingTimeout)
	assert.Positive(t, backend.deadlineIn)

	// both ratings give the same cost
	require.NotNil(t, transaction.Cost)
	cost := f.completed(t).Cost
	require.NotNil(t, cost)
	assert.Equal(t, transaction.Cost.Total, money.Price{ExclVat: cost.ExclVat, InclVat: cost.InclVat})
	assert.Equal(t, money.Amount(3_3000), cost.ExclVat)
	assert.Equal(t, int64(5400), cost.ParkingS, "the idle time of the event is that of the analysis")
}

// A stop is answered as ever when the rating of its transaction fails, and the
// transaction is stopped and analysed all the same.
func TestStopTransactionIsAnsweredWhenTheRatingFails(t *testing.T) {
	ctx := context.Background()
	errDown := errors.New("the store is down")

	// the answer and the event of the handler without a session service
	plain := newAnalysisFixture(t)
	plainId := plain.charge(t, 1, 60, 111_000)
	plain.events.Take(t)
	stop := analysisStopRequest(plainId, 150, 111_000)
	stop.IdTag = ptr("RFID0001")
	want, err := plain.stop.HandleCall(ctx, sessionOrgStation, stop)
	require.NoError(t, err)
	wantEvents := plain.events.Take(t)
	require.Equal(t, []string{events.TypeSessionCompleted}, eventstest.Types(wantEvents))

	for name, tc := range map[string]struct {
		backend  *ratingBackend
		wantCost bool
	}{
		"the transaction cannot be read": {backend: &ratingBackend{err: errDown}},
		"the rating panics":              {backend: &ratingBackend{panicWith: "the rating broke"}},
		// only the rating for the event fails: the event has no cost, the transaction has
		"the first rating fails": {backend: &ratingBackend{failReads: 1, err: errDown}, wantCost: true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newCostFixture(t)
			transactionId := f.charge(t, 1, 60, 111_000)
			require.Equal(t, plainId, transactionId)
			f.events.Take(t)
			tc.backend.TransactionStore = f.engine
			f.stop.Sessions.Transactions = tc.backend
			tracker := &recordingTracker{store: f.engine}
			f.stop.CommandTracker = tracker

			got, err := f.stop.HandleCall(ctx, sessionOrgStation, stop)

			require.NoError(t, err)
			assert.Equal(t, want, got)
			assert.Equal(t, 2, tc.backend.reads, "both ratings were tried")
			transaction := f.stored(t, transactionId)
			assert.Positive(t, transaction.EndedSeqNo)
			assert.Equal(t, ptr(analysisSessionStart.Add(150*time.Minute)), transaction.StoppedAt)
			assert.NotNil(t, transaction.Analysis)
			// the cost so far of the meter values is there, and stays what it was without a
			// final cost
			require.NotNil(t, transaction.Cost)
			assert.Equal(t, tc.wantCost, transaction.Cost.Final)
			gotEvents := f.events.Take(t)
			require.Equal(t, []string{events.TypeSessionCompleted}, eventstest.Types(gotEvents))
			assert.JSONEq(t, string(wantEvents[0].Data), string(gotEvents[0].Data), "the event is that of a stop without a cost")
			assert.Equal(t, []string{"stopped " + sessionOrgStation + " " + strconv.Itoa(transactionId) + " Local"}, tracker.calls)
		})
	}
}

// The router gives its session service to the handlers of the meter values and of the
// stop.
func TestNewRouterGivesTheSessionServiceToTheHandlers(t *testing.T) {
	engine := inmemory.NewStore(clockAt(t, "2026-11-28T10:00:00Z"))
	service := &sessions.Service{Transactions: engine}

	router, ok := handlers.NewRouter(&FakeEmitter{}, clockAt(t, "2026-11-28T10:00:00Z"), engine, events.NopEmitter{},
		&tenancy.Resolver{Stations: engine}, nil, nil, nil, nil, time.Minute, schemas.OcppSchemas, nil, nil, nil, nil, service).(*msghandlers.Router)
	require.True(t, ok)

	stop, ok := router.CallRoutes["StopTransaction"].Handler.(handlers.StopTransactionHandler)
	require.True(t, ok)
	assert.Same(t, service, stop.Sessions)
	meterValues, ok := router.CallRoutes["MeterValues"].Handler.(handlers.MeterValuesHandler)
	require.True(t, ok)
	assert.Same(t, service, meterValues.Sessions)
}
