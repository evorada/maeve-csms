// SPDX-License-Identifier: Apache-2.0

package sessions_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clockTest "k8s.io/utils/clock/testing"

	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	"github.com/thoughtworks/maeve-csms/manager/money"
	"github.com/thoughtworks/maeve-csms/manager/rating"
	"github.com/thoughtworks/maeve-csms/manager/sessions"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tariffs"
)

const (
	testOrg = "0193b2c4-0000-7000-8000-0000000000a1"
	// an OCPP 1.6 charge station with a connector 1 of 22 kW at a location in
	// Europe/Madrid, and a connector 2 that the asset model does not have
	testStation = "aaaa.CP001"
	// a charge station of which nothing is known
	bareStation = "aaaa.CP002"
	// the register of the connector at the start of the sessions
	meterStartWh = 100_000
)

// sessionStart is the start of the sessions of these tests: a Thursday at 10:00 UTC,
// 12:00 in Madrid. A moment of a session is given by its minute.
var sessionStart = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

// ratingTime is the time of the manager: the next day, unless a test sets another.
var ratingTime = sessionStart.Add(24 * time.Hour)

func minute(m float64) time.Time {
	return sessionStart.Add(time.Duration(m * float64(time.Minute)))
}

func ptr[T any](v T) *T {
	return &v
}

func amount(t *testing.T, s string) money.Amount {
	t.Helper()
	a, err := money.ParseAmount(s)
	require.NoError(t, err)
	return a
}

func price(t *testing.T, excl, incl string) money.Price {
	t.Helper()
	return money.Price{ExclVat: amount(t, excl), InclVat: amount(t, incl)}
}

// fixture is a Service on an in-memory store with a clock that the test sets.
type fixture struct {
	engine  *inmemory.Store
	clock   *clockTest.FakePassiveClock
	service *sessions.Service
	site    *store.AssetLocation
	// parking is a tariff with energy at 0.30 per kWh and parking at 0.50 per 15 minutes,
	// the first 30 minutes free, VAT 21%
	parking *store.Tariff
}

func parkingVersion(t *testing.T) *store.TariffVersion {
	t.Helper()
	vat := money.Percent(2100)
	v := &store.TariffVersion{Currency: "EUR", Elements: []store.TariffElement{{PriceComponents: []store.PriceComponent{
		{Type: store.PriceComponentEnergy, Price: amount(t, "0.30"), Vat: &vat, StepSize: 1},
		{Type: store.PriceComponentParkingTime, Vat: &vat, StepSize: 900, GracePeriodS: ptr(int32(1800)), PricePerStep: ptr(amount(t, "0.50"))},
	}}}}
	require.Empty(t, tariffs.Validate(v))
	return v
}

func energyVersion(t *testing.T, perKWh string) *store.TariffVersion {
	t.Helper()
	return &store.TariffVersion{Currency: "EUR", Elements: []store.TariffElement{{PriceComponents: []store.PriceComponent{
		{Type: store.PriceComponentEnergy, Price: amount(t, perKWh), StepSize: 1},
	}}}}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	clk := clockTest.NewFakePassiveClock(ratingTime)
	engine := inmemory.NewStore(clk)

	require.NoError(t, engine.SetChargeStationAuth(ctx, testStation, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: testOrg}))
	require.NoError(t, engine.SetChargeStationRuntimeDetails(ctx, testStation, &store.ChargeStationRuntimeDetails{OcppVersion: "1.6"}))
	site := &store.AssetLocation{
		OrgId: testOrg, PartyRef: "party-a", Address: "Calle Mayor 1", City: "Madrid", Country: "ESP",
		Latitude: "40.416775", Longitude: "-3.703790", TimeZone: "Europe/Madrid",
	}
	require.NoError(t, engine.CreateAssetLocation(ctx, site))
	require.NoError(t, engine.CreateAssetEvse(ctx, &store.AssetEvse{
		OrgId: testOrg, LocationId: site.Id, StationKey: testStation, OcppEvseId: 1,
		Connectors: []*store.AssetConnector{{
			OcppConnectorId: 1, Standard: "IEC_62196_T2", Format: "SOCKET", PowerType: "AC_3_PHASE", MaxVoltage: 400, MaxAmperage: 32,
			MaxElectricPower: ptr(int32(22_000)),
		}},
	}))

	f := &fixture{engine: engine, clock: clk, site: site}
	f.parking = f.tariff(t, "Parking", parkingVersion(t))
	f.service = &sessions.Service{
		Analyzer: &rating.Analyzer{
			Transactions: engine, MeterValues: engine, Assets: engine, Registry: engine, Tariffs: engine,
			Defaults: rating.DefaultIdleSettings(), Clock: clk,
		},
		Transactions: engine,
		MeterValues:  engine,
		Tariffs:      engine,
		Assets:       engine,
		Registry:     engine,
		Tx:           engine,
		Clock:        clk,
	}
	return f
}

func (f *fixture) tariff(t *testing.T, name string, v *store.TariffVersion) *store.Tariff {
	t.Helper()
	tariff := &store.Tariff{OrgId: testOrg, PartyRef: "party-a", Name: name}
	require.NoError(t, f.engine.CreateTariff(context.Background(), tariff, v))
	return tariff
}

// registerAt is a meter value with a reading of the energy register, the energy of the
// session so far above the meter start, at the minute of the session.
func registerAt(m, sessionWh float64, readingContext string, more ...store.SampledValue) store.MeterValue {
	return store.MeterValue{
		Timestamp: minute(m).Format(time.RFC3339),
		SampledValues: append([]store.SampledValue{{
			Context:       &readingContext,
			Measurand:     ptr("Energy.Active.Import.Register"),
			Location:      ptr("Outlet"),
			UnitOfMeasure: &store.UnitOfMeasure{Unit: "Wh"},
			Value:         meterStartWh + sessionWh,
		}}, more...),
	}
}

// start stores the start of an OCPP 1.6 transaction as its handler does, at the minute 0
// of the session, with the tariff version that it is charged with (none without a
// tariff).
func (f *fixture) start(t *testing.T, station, transactionId string, connectorId int, tariff *store.Tariff, version int) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, f.engine.CreateTransaction(ctx, station, transactionId, "RFID0001", "ISO14443",
		[]store.MeterValue{registerAt(0, 0, "Transaction.Begin")}, 0, false))
	require.NoError(t, f.engine.UpdateTransactionDetails(ctx, station, transactionId, &store.TransactionDetails{
		ConnectorId: &connectorId, EvseId: &connectorId, StartedAt: &sessionStart, MeterStartWh: ptr(int64(meterStartWh)),
	}))
	if tariff == nil {
		require.NoError(t, f.engine.SetTransactionTariff(ctx, station, transactionId, nil, nil, "none"))
		return
	}
	require.NoError(t, f.engine.SetTransactionTariff(ctx, station, transactionId, &tariff.Id, &version, "connector"))
}

// readings stores meter values that the station sent on their own: the minute of the
// session and the energy of the session so far in Wh.
func (f *fixture) readings(t *testing.T, station, transactionId string, minutesAndWh ...float64) {
	t.Helper()
	var sent []store.MeterValue
	for i := 0; i+1 < len(minutesAndWh); i += 2 {
		sent = append(sent, registerAt(minutesAndWh[i], minutesAndWh[i+1], "Sample.Periodic"))
	}
	require.NoError(t, f.engine.StoreMeterValues(context.Background(), station, 1, transactionId, sent))
}

// stop stores the stop of an OCPP 1.6 transaction as its handler does.
func (f *fixture) stop(t *testing.T, station, transactionId string, m, sessionWh float64) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, f.engine.EndTransaction(ctx, station, transactionId, "RFID0001", "ISO14443",
		[]store.MeterValue{registerAt(m, sessionWh, "Transaction.End")}, 1))
	require.NoError(t, f.engine.UpdateTransactionDetails(ctx, station, transactionId, &store.TransactionDetails{
		StoppedAt: ptr(minute(m)), MeterStopWh: ptr(int64(meterStartWh + sessionWh)), EnergyWh: ptr(int64(sessionWh)),
	}))
}

// chargeAndPark stores a transaction with the parking tariff that charges 11 kWh in its
// first hour and is idle after it, with its readings up to 12:00 (minute 120).
func (f *fixture) chargeAndPark(t *testing.T, transactionId string) {
	t.Helper()
	f.start(t, testStation, transactionId, 1, f.parking, 1)
	f.readings(t, testStation, transactionId, 30, 5500, 60, 11000, 90, 11000, 120, 11000)
}

func (f *fixture) transaction(t *testing.T, station, transactionId string) *store.Transaction {
	t.Helper()
	transaction, err := f.engine.FindTransaction(context.Background(), station, transactionId)
	require.NoError(t, err)
	require.NotNil(t, transaction)
	return transaction
}

// The session of the worked example: 11 kWh in an hour, then 100 minutes idle, of which
// 30 are free and 70 are charged as 5 steps. 5.80 without and 7.02 with VAT.
func TestEndRatesAnEndedTransaction(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.chargeAndPark(t, "tx-1")
	f.stop(t, testStation, "tx-1", 160, 11000)
	_, err := f.service.Analyzer.AnalyseAndStore(ctx, testStation, "tx-1")
	require.NoError(t, err)

	result, err := f.service.End(ctx, testStation, "tx-1")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Final)
	assert.Equal(t, "EUR", result.Currency)
	assert.Equal(t, f.parking.Id, result.TariffId)
	assert.Equal(t, 1, result.TariffVersion)
	assert.Equal(t, price(t, "5.80", "7.02"), result.Breakdown.Total)
	assert.Equal(t, price(t, "3.30", "3.99"), result.Breakdown.Energy.Cost)
	assert.Equal(t, rating.TimeBreakdown{
		TotalS: 6000, GraceS: 1800, BillableS: 4200, StepSizeS: 900, Steps: 5, Minutes: 100, Cost: price(t, "2.50", "3.03"),
	}, result.Breakdown.Parking)
	assert.Equal(t, int64(11000), result.TotalEnergyWh)
	assert.Equal(t, int64(3600), result.ChargingTimeS)
	assert.Equal(t, int64(6000), result.ParkingTimeS)

	// the cost is stored with the transaction
	stored := f.transaction(t, testStation, "tx-1")
	assert.Equal(t, &store.TransactionCost{Currency: "EUR", Total: price(t, "5.80", "7.02"), Final: true, RatedAt: ratingTime}, stored.Cost)
	assert.Equal(t, ptr(7.02), stored.LastCost)

	// Rate gives the same price and the session that it was worked out from, and stores
	// nothing
	f.clock.SetTime(ratingTime.Add(time.Hour))
	rated, session, err := f.service.Rate(ctx, testStation, "tx-1", true)
	require.NoError(t, err)
	assert.Equal(t, result, rated)
	madrid, err := time.LoadLocation("Europe/Madrid")
	require.NoError(t, err)
	assert.Equal(t, &rating.Session{
		Start:    sessionStart,
		Stop:     ptr(minute(160)),
		Location: madrid,
		Samples: []rating.Sample{
			// the meter start and the reading of the start, which are the same
			{At: minute(0), EnergyWh: 0}, {At: minute(0), EnergyWh: 0},
			{At: minute(30), EnergyWh: 5500}, {At: minute(60), EnergyWh: 11000}, {At: minute(90), EnergyWh: 11000},
			{At: minute(120), EnergyWh: 11000}, {At: minute(160), EnergyWh: 11000},
		},
		TotalEnergyWh: ptr(11000.0),
		IdleStartedAt: ptr(minute(60)),
	}, session)
	assert.Equal(t, ratingTime, f.transaction(t, testStation, "tx-1").Cost.RatedAt)
}

// The session of the worked example while it runs, at 11:45 with the readings up to
// 11:30: idle since 11:00, one step of parking after the grace period.
func TestUpdateRunningCost(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.start(t, testStation, "tx-1", 1, f.parking, 1)
	f.readings(t, testStation, "tx-1", 30, 5500, 60, 11000, 90, 11000)
	f.clock.SetTime(minute(105))

	result, err := f.service.UpdateRunningCost(ctx, testStation, "tx-1")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Final)
	assert.Equal(t, price(t, "3.80", "4.60"), result.Breakdown.Total)
	assert.Equal(t, rating.TimeBreakdown{
		TotalS: 2700, GraceS: 1800, BillableS: 900, StepSizeS: 900, Steps: 1, Minutes: 45, Cost: price(t, "0.50", "0.61"),
	}, result.Breakdown.Parking)
	assert.Equal(t, int64(6300), result.TotalTimeS)

	stored := f.transaction(t, testStation, "tx-1")
	assert.Equal(t, &store.TransactionCost{Currency: "EUR", Total: price(t, "3.80", "4.60"), RatedAt: minute(105)}, stored.Cost)
	assert.Equal(t, ptr(4.6), stored.LastCost)

	// Rate without final gives the cost so far and the session up to the time of the
	// manager, and stores nothing
	f.clock.SetTime(minute(110))
	rated, session, err := f.service.Rate(ctx, testStation, "tx-1", false)
	require.NoError(t, err)
	// one more step of parking: 20 minutes after the grace period
	assert.Equal(t, price(t, "4.30", "5.20"), rated.Breakdown.Total)
	assert.Nil(t, session.Stop)
	assert.Equal(t, minute(110), session.Now)
	assert.Equal(t, ptr(minute(60)), session.IdleStartedAt)
	assert.Nil(t, session.TotalEnergyWh)
	assert.Equal(t, minute(105), f.transaction(t, testStation, "tx-1").Cost.RatedAt)
}

// A session that still charges has no idle time, and its end so far is never before its
// newest reading: the clock of a charge station can be ahead of the clock of the manager.
func TestRateARunningTransactionThatCharges(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.start(t, testStation, "tx-1", 1, f.parking, 1)
	f.readings(t, testStation, "tx-1", 30, 5500, 60, 11000)

	// the manager is two minutes behind the newest reading
	f.clock.SetTime(minute(58))
	result, session, err := f.service.Rate(ctx, testStation, "tx-1", false)
	require.NoError(t, err)
	assert.Equal(t, minute(60), session.Now)
	assert.Nil(t, session.IdleStartedAt)
	assert.Equal(t, price(t, "3.30", "3.99"), result.Breakdown.Total)
	assert.Equal(t, int64(3600), result.TotalTimeS)

	// a manager whose clock is behind the start of a session without readings
	f.start(t, testStation, "tx-2", 1, f.parking, 1)
	f.clock.SetTime(minute(-3))
	result, session, err = f.service.Rate(ctx, testStation, "tx-2", false)
	require.NoError(t, err)
	assert.Equal(t, sessionStart, session.Now)
	assert.Equal(t, money.Price{}, result.Breakdown.Total)

	// and of a session of which nothing but its start is stored
	require.NoError(t, f.engine.CreateTransaction(ctx, testStation, "tx-3", "RFID0001", "ISO14443", nil, 0, false))
	require.NoError(t, f.engine.UpdateTransactionDetails(ctx, testStation, "tx-3", &store.TransactionDetails{StartedAt: &sessionStart}))
	require.NoError(t, f.engine.SetTransactionTariff(ctx, testStation, "tx-3", &f.parking.Id, ptr(1), "connector"))
	result, session, err = f.service.Rate(ctx, testStation, "tx-3", false)
	require.NoError(t, err)
	assert.Equal(t, sessionStart, session.Now)
	assert.Empty(t, session.Samples)
	assert.Equal(t, int64(0), result.TotalTimeS)
}

// The energy of a session is counted from the meter start of the transaction, also when
// the first reading that is stored of it is a later one.
func TestTheEnergyIsCountedFromTheMeterStart(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	// the meter start is stored, the time of the start is not: the session starts with its
	// first meter value, five minutes in, when it has taken 0.5 kWh
	require.NoError(t, f.engine.CreateTransaction(ctx, testStation, "tx-1", "RFID0001", "ISO14443",
		[]store.MeterValue{registerAt(5, 500, "Sample.Periodic")}, 0, false))
	require.NoError(t, f.engine.UpdateTransactionDetails(ctx, testStation, "tx-1", &store.TransactionDetails{MeterStartWh: ptr(int64(meterStartWh))}))
	require.NoError(t, f.engine.SetTransactionTariff(ctx, testStation, "tx-1", &f.parking.Id, ptr(1), "connector"))
	require.NoError(t, f.engine.EndTransaction(ctx, testStation, "tx-1", "RFID0001", "ISO14443",
		[]store.MeterValue{registerAt(60, 11000, "Transaction.End")}, 1))

	result, session, err := f.service.Rate(ctx, testStation, "tx-1", true)

	require.NoError(t, err)
	assert.Equal(t, minute(5), session.Start)
	assert.Equal(t, []rating.Sample{{At: minute(5), EnergyWh: 500}, {At: minute(60), EnergyWh: 11000}}, session.Samples)
	assert.Equal(t, int64(11000), result.TotalEnergyWh)
	assert.Equal(t, price(t, "3.30", "3.99"), result.Breakdown.Energy.Cost)
}

// A transaction is rated with the tariff version that it started under, also after the
// tariff got a new version.
func TestRateUsesTheVersionOfTheStart(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	regular := f.tariff(t, "Regular", energyVersion(t, "0.30"))
	f.start(t, testStation, "tx-1", 1, regular, 1)
	f.stop(t, testStation, "tx-1", 60, 10000)

	// the price doubles during the session
	require.NoError(t, f.engine.AddTariffVersion(ctx, regular.Id, 1, energyVersion(t, "0.60")))

	result, _, err := f.service.Rate(ctx, testStation, "tx-1", true)
	require.NoError(t, err)
	assert.Equal(t, price(t, "3.00", "3.00"), result.Breakdown.Total)
	assert.Equal(t, regular.Id, result.TariffId)
	assert.Equal(t, 1, result.TariffVersion)

	// a transaction that starts after it has the new price
	f.start(t, testStation, "tx-2", 1, regular, 2)
	f.stop(t, testStation, "tx-2", 60, 10000)
	result, _, err = f.service.Rate(ctx, testStation, "tx-2", true)
	require.NoError(t, err)
	assert.Equal(t, price(t, "6.00", "6.00"), result.Breakdown.Total)
	assert.Equal(t, 2, result.TariffVersion)

	// a deleted tariff still rates the transactions that started under it
	require.NoError(t, f.engine.DeleteTariff(ctx, regular.Id))
	result, _, err = f.service.Rate(ctx, testStation, "tx-1", true)
	require.NoError(t, err)
	assert.Equal(t, price(t, "3.00", "3.00"), result.Breakdown.Total)
}

// countingTransactions counts the reads of the transactions.
type countingTransactions struct {
	store.TransactionStore
	summaries, withMeterValues int
}

func (s *countingTransactions) FindTransactionSummary(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	s.summaries++
	return s.TransactionStore.FindTransactionSummary(ctx, chargeStationId, transactionId)
}

func (s *countingTransactions) FindTransaction(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	s.withMeterValues++
	return s.TransactionStore.FindTransaction(ctx, chargeStationId, transactionId)
}

// A transaction without a tariff is rated with the fallback price, and has no price when
// there is none.
func TestRateATransactionWithoutATariff(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.start(t, testStation, "tx-1", 1, nil, 0)
	f.readings(t, testStation, "tx-1", 30, 5500)
	f.stop(t, testStation, "tx-1", 60, 11000)
	counting := &countingTransactions{TransactionStore: f.engine}
	f.service.Transactions = counting

	t.Run("without a fallback it has no price", func(t *testing.T) {
		result, session, err := f.service.Rate(ctx, testStation, "tx-1", true)
		require.NoError(t, err)
		assert.Nil(t, result)
		assert.Nil(t, session)
		assert.Zero(t, counting.withMeterValues, "a transaction without a price is not read with its meter values")

		result, err = f.service.End(ctx, testStation, "tx-1")
		require.NoError(t, err)
		assert.Nil(t, result)
		stored := f.transaction(t, testStation, "tx-1")
		assert.Nil(t, stored.Cost)
		assert.Nil(t, stored.LastCost)
	})

	t.Run("with the fallback", func(t *testing.T) {
		f.service.Fallback = &sessions.FallbackTariff{Currency: "PLN", PricePerKwh: amount(t, "0.55")}
		result, err := f.service.End(ctx, testStation, "tx-1")
		require.NoError(t, err)
		require.NotNil(t, result)
		// 11 kWh at 0.55, without VAT
		assert.Equal(t, price(t, "6.05", "6.05"), result.Breakdown.Total)
		assert.Equal(t, "PLN", result.Currency)
		assert.Empty(t, result.TariffId)
		assert.Zero(t, result.TariffVersion)
		assert.Equal(t, &store.TransactionCost{Currency: "PLN", Total: price(t, "6.05", "6.05"), Final: true, RatedAt: ratingTime},
			f.transaction(t, testStation, "tx-1").Cost)
	})

	t.Run("whatever the reason that it has none", func(t *testing.T) {
		// the tariff of the transaction was never chosen
		require.NoError(t, f.engine.CreateTransaction(ctx, testStation, "tx-2", "RFID0001", "ISO14443", nil, 0, false))
		require.NoError(t, f.engine.UpdateTransactionDetails(ctx, testStation, "tx-2", &store.TransactionDetails{
			StartedAt: &sessionStart, StoppedAt: ptr(minute(60)), MeterStartWh: ptr(int64(1000)), MeterStopWh: ptr(int64(3500)),
		}))
		result, _, err := f.service.Rate(ctx, testStation, "tx-2", true)
		require.NoError(t, err)
		require.NotNil(t, result)
		// 2.5 kWh, from the meter start and the meter stop alone, to the Wh: 1.375
		assert.Equal(t, rating.EnergyBreakdown{Wh: 2500, BilledWh: 2500, Cost: price(t, "1.38", "1.38")}, result.Breakdown.Energy)
		assert.Equal(t, price(t, "1.38", "1.38"), result.Breakdown.Total)
	})

	t.Run("a transaction with a tariff is not rated with the fallback", func(t *testing.T) {
		f.start(t, testStation, "tx-3", 1, f.parking, 1)
		f.stop(t, testStation, "tx-3", 60, 11000)
		result, _, err := f.service.Rate(ctx, testStation, "tx-3", true)
		require.NoError(t, err)
		assert.Equal(t, price(t, "3.30", "3.99"), result.Breakdown.Total)
		assert.Equal(t, "EUR", result.Currency)
	})
}

// The cost so far of a transaction is worked out at most once in the interval.
func TestRunningCostIsThrottled(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.service.RunningEvery = time.Minute
	counting := &countingTransactions{TransactionStore: f.engine}
	f.service.Transactions = counting
	f.start(t, testStation, "tx-1", 1, f.parking, 1)
	f.start(t, testStation, "tx-2", 1, f.parking, 1)
	f.readings(t, testStation, "tx-1", 10, 2000)
	f.clock.SetTime(minute(10))

	first, err := f.service.UpdateRunningCost(ctx, testStation, "tx-1")
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, price(t, "0.60", "0.73"), first.Breakdown.Total)
	assert.Equal(t, 1, counting.summaries)
	assert.Equal(t, 1, counting.withMeterValues)

	// within the minute: nothing is read, nothing is worked out, whichever method asks
	f.readings(t, testStation, "tx-1", 10.5, 3000)
	f.clock.SetTime(minute(10).Add(59 * time.Second))
	again, err := f.service.UpdateRunningCost(ctx, testStation, "tx-1")
	require.NoError(t, err)
	assert.Nil(t, again)
	again, err = f.service.RunningCost(ctx, testStation, "tx-1", nil)
	require.NoError(t, err)
	assert.Nil(t, again)
	assert.Equal(t, 1, counting.summaries)
	assert.Equal(t, 1, counting.withMeterValues)
	assert.Equal(t, price(t, "0.60", "0.73"), f.transaction(t, testStation, "tx-1").Cost.Total)

	// another transaction has its own time
	other, err := f.service.UpdateRunningCost(ctx, testStation, "tx-2")
	require.NoError(t, err)
	require.NotNil(t, other)
	assert.Equal(t, 2, counting.summaries)

	// after the minute
	f.clock.SetTime(minute(11))
	next, err := f.service.UpdateRunningCost(ctx, testStation, "tx-1")
	require.NoError(t, err)
	require.NotNil(t, next)
	assert.Equal(t, price(t, "0.90", "1.09"), next.Breakdown.Total)
	assert.Equal(t, price(t, "0.90", "1.09"), f.transaction(t, testStation, "tx-1").Cost.Total)

	// RunningCost counts for the interval too, and stores nothing
	f.clock.SetTime(minute(12))
	f.readings(t, testStation, "tx-1", 12, 4000)
	notStored, err := f.service.RunningCost(ctx, testStation, "tx-1", nil)
	require.NoError(t, err)
	require.NotNil(t, notStored)
	assert.Equal(t, price(t, "1.20", "1.45"), notStored.Breakdown.Total)
	assert.Equal(t, price(t, "0.90", "1.09"), f.transaction(t, testStation, "tx-1").Cost.Total)
	f.clock.SetTime(minute(12).Add(30 * time.Second))
	again, err = f.service.UpdateRunningCost(ctx, testStation, "tx-1")
	require.NoError(t, err)
	assert.Nil(t, again)

	// a clock that was set back does not hold the next rating up
	f.clock.SetTime(minute(5))
	back, err := f.service.RunningCost(ctx, testStation, "tx-1", nil)
	require.NoError(t, err)
	assert.NotNil(t, back)

	t.Run("a rating that fails counts for the interval", func(t *testing.T) {
		summaries := counting.summaries
		f.clock.SetTime(minute(20))
		_, err := f.service.UpdateRunningCost(ctx, testStation, "tx-unknown")
		require.ErrorIs(t, err, store.ErrNotFound)
		result, err := f.service.UpdateRunningCost(ctx, testStation, "tx-unknown")
		require.NoError(t, err)
		assert.Nil(t, result)
		assert.Equal(t, summaries+1, counting.summaries)
	})

	t.Run("without an interval every call rates", func(t *testing.T) {
		f.service.RunningEvery = 0
		summaries := counting.summaries
		for i := 0; i < 3; i++ {
			result, err := f.service.UpdateRunningCost(ctx, testStation, "tx-1")
			require.NoError(t, err)
			assert.NotNil(t, result)
		}
		assert.Equal(t, summaries+3, counting.summaries)
	})
}

// A transaction that has ended has no cost so far: its final cost stays.
func TestRunningCostOfAnEndedTransaction(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.chargeAndPark(t, "tx-1")
	f.stop(t, testStation, "tx-1", 160, 11000)
	final, err := f.service.End(ctx, testStation, "tx-1")
	require.NoError(t, err)
	require.NotNil(t, final)
	counting := &countingTransactions{TransactionStore: f.engine}
	f.service.Transactions = counting

	// a meter value of the transaction that comes after its stop
	result, err := f.service.UpdateRunningCost(ctx, testStation, "tx-1")
	require.NoError(t, err)
	assert.Nil(t, result)
	result, err = f.service.RunningCost(ctx, testStation, "tx-1", []store.MeterValue{registerAt(170, 12000, "Sample.Periodic")})
	require.NoError(t, err)
	assert.Nil(t, result)
	result, session, err := f.service.Rate(ctx, testStation, "tx-1", false)
	require.NoError(t, err)
	assert.Nil(t, result)
	assert.Nil(t, session)
	assert.Zero(t, counting.withMeterValues, "an ended transaction is not read with its meter values for a cost so far")

	stored := f.transaction(t, testStation, "tx-1")
	require.NotNil(t, stored.Cost)
	assert.True(t, stored.Cost.Final)
	assert.Equal(t, final.Breakdown.Total, stored.Cost.Total)

	// a transaction has ended when its record has a stop, or the number of the event
	// that ended it: a store may keep only one of the two
	f.start(t, testStation, "tx-stopped", 1, f.parking, 1)
	require.NoError(t, f.engine.UpdateTransactionDetails(ctx, testStation, "tx-stopped", &store.TransactionDetails{StoppedAt: ptr(minute(60))}))
	require.NoError(t, f.engine.CreateTransaction(ctx, testStation, "tx-ended", "RFID0001", "ISO14443",
		[]store.MeterValue{registerAt(0, 0, "Transaction.Begin")}, 0, false))
	require.NoError(t, f.engine.SetTransactionTariff(ctx, testStation, "tx-ended", &f.parking.Id, ptr(1), "connector"))
	require.NoError(t, f.engine.EndTransaction(ctx, testStation, "tx-ended", "RFID0001", "ISO14443",
		[]store.MeterValue{registerAt(60, 11000, "Transaction.End")}, 2))
	for _, transactionId := range []string{"tx-stopped", "tx-ended"} {
		result, err := f.service.UpdateRunningCost(ctx, testStation, transactionId)
		require.NoError(t, err, transactionId)
		assert.Nil(t, result, transactionId)
		assert.Nil(t, f.transaction(t, testStation, transactionId).Cost, transactionId)
		// and both have a final cost
		final, err := f.service.End(ctx, testStation, transactionId)
		require.NoError(t, err, transactionId)
		require.NotNil(t, final, transactionId)
		assert.True(t, final.Final, transactionId)
	}
	assert.Zero(t, counting.withMeterValues-2, "only the two final ratings read a transaction with its meter values")
}

func TestEnd(t *testing.T) {
	ctx := context.Background()

	t.Run("a transaction that has not ended", func(t *testing.T) {
		f := newFixture(t)
		f.chargeAndPark(t, "tx-1")
		result, err := f.service.End(ctx, testStation, "tx-1")
		require.ErrorIs(t, err, store.ErrInvalidState)
		assert.Nil(t, result)
		assert.Nil(t, f.transaction(t, testStation, "tx-1").Cost)

		_, _, err = f.service.Rate(ctx, testStation, "tx-1", true)
		require.ErrorIs(t, err, store.ErrInvalidState)
	})

	t.Run("a transaction that does not exist", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.service.End(ctx, testStation, "tx-9")
		require.ErrorIs(t, err, store.ErrNotFound)
		assert.ErrorContains(t, err, testStation+"/tx-9")
		_, _, err = f.service.Rate(ctx, testStation, "tx-9", false)
		require.ErrorIs(t, err, store.ErrNotFound)
		// the transaction of another charge station
		f.chargeAndPark(t, "tx-1")
		_, err = f.service.End(ctx, bareStation, "tx-1")
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("a transaction whose start is not known", func(t *testing.T) {
		f := newFixture(t)
		f.service.Fallback = &sessions.FallbackTariff{Currency: "EUR", PricePerKwh: amount(t, "0.55")}
		// a stop of a transaction that the manager never saw start
		require.NoError(t, f.engine.CreateTransaction(ctx, testStation, "tx-1", "", "", nil, 0, false))
		require.NoError(t, f.engine.EndTransaction(ctx, testStation, "tx-1", "", "", nil, 1))
		require.NoError(t, f.engine.UpdateTransactionDetails(ctx, testStation, "tx-1", &store.TransactionDetails{StoppedAt: ptr(minute(60))}))
		_, err := f.service.End(ctx, testStation, "tx-1")
		require.ErrorIs(t, err, store.ErrInvalidState)
		assert.Nil(t, f.transaction(t, testStation, "tx-1").Cost)
	})

	t.Run("a transaction that ends before it starts", func(t *testing.T) {
		f := newFixture(t)
		f.start(t, testStation, "tx-1", 1, f.parking, 1)
		f.stop(t, testStation, "tx-1", -5, 0)
		_, err := f.service.End(ctx, testStation, "tx-1")
		require.ErrorContains(t, err, "before its start")
		assert.Nil(t, f.transaction(t, testStation, "tx-1").Cost)
	})

	t.Run("rated again", func(t *testing.T) {
		f := newFixture(t)
		f.chargeAndPark(t, "tx-1")
		f.stop(t, testStation, "tx-1", 160, 11000)
		first, err := f.service.End(ctx, testStation, "tx-1")
		require.NoError(t, err)
		f.clock.SetTime(ratingTime.Add(time.Hour))
		second, err := f.service.End(ctx, testStation, "tx-1")
		require.NoError(t, err)
		assert.Equal(t, first, second)
		assert.Equal(t, ratingTime.Add(time.Hour), f.transaction(t, testStation, "tx-1").Cost.RatedAt)
	})

	t.Run("a transaction that runs can be rated for its cost so far once it was ended and forgotten", func(t *testing.T) {
		// End forgets when the cost so far of the transaction was last worked out
		f := newFixture(t)
		f.service.RunningEvery = time.Hour
		f.start(t, testStation, "tx-1", 1, f.parking, 1)
		f.clock.SetTime(minute(10))
		first, err := f.service.RunningCost(ctx, testStation, "tx-1", nil)
		require.NoError(t, err)
		require.NotNil(t, first)
		_, err = f.service.End(ctx, testStation, "tx-1")
		require.ErrorIs(t, err, store.ErrInvalidState)
		again, err := f.service.RunningCost(ctx, testStation, "tx-1", nil)
		require.NoError(t, err)
		assert.NotNil(t, again)
	})
}

// The idle time of an ended transaction is that of its stored analysis. Without one the
// Analyzer works it out, and nothing of that is stored.
func TestTheIdleTimeOfAnEndedTransaction(t *testing.T) {
	ctx := context.Background()

	t.Run("without a stored analysis", func(t *testing.T) {
		f := newFixture(t)
		f.chargeAndPark(t, "tx-1")
		f.stop(t, testStation, "tx-1", 160, 11000)

		result, err := f.service.End(ctx, testStation, "tx-1")
		require.NoError(t, err)
		assert.Equal(t, price(t, "5.80", "7.02"), result.Breakdown.Total)
		assert.Nil(t, f.transaction(t, testStation, "tx-1").Analysis)
	})

	t.Run("the stored analysis counts", func(t *testing.T) {
		f := newFixture(t)
		f.chargeAndPark(t, "tx-1")
		f.stop(t, testStation, "tx-1", 160, 11000)
		// an analysis that says that the session was idle from 11:55 on: 45 minutes, of
		// which 15 are charged
		require.NoError(t, f.engine.SetTransactionAnalysis(ctx, testStation, "tx-1", store.TransactionAnalysis{
			IdleStartedAt: ptr(minute(115)), IdleS: 2700, ChargingS: 6900, AnalysedAt: ratingTime,
		}))

		result, err := f.service.End(ctx, testStation, "tx-1")
		require.NoError(t, err)
		assert.Equal(t, int64(2700), result.ParkingTimeS)
		assert.Equal(t, price(t, "3.80", "4.60"), result.Breakdown.Total)
	})

	t.Run("an analysis without an idle start", func(t *testing.T) {
		f := newFixture(t)
		f.chargeAndPark(t, "tx-1")
		f.stop(t, testStation, "tx-1", 160, 11000)
		require.NoError(t, f.engine.SetTransactionAnalysis(ctx, testStation, "tx-1", store.TransactionAnalysis{ChargingS: 9600, AnalysedAt: ratingTime}))

		result, err := f.service.End(ctx, testStation, "tx-1")
		require.NoError(t, err)
		assert.Equal(t, int64(0), result.ParkingTimeS)
		assert.Equal(t, price(t, "3.30", "3.99"), result.Breakdown.Total)
	})

	t.Run("without an Analyzer a transaction without an analysis has no idle time", func(t *testing.T) {
		f := newFixture(t)
		f.service.Analyzer = nil
		f.chargeAndPark(t, "tx-1")
		running, _, err := f.service.Rate(ctx, testStation, "tx-1", false)
		require.NoError(t, err)
		assert.Equal(t, int64(0), running.ParkingTimeS)

		f.stop(t, testStation, "tx-1", 160, 11000)
		result, err := f.service.End(ctx, testStation, "tx-1")
		require.NoError(t, err)
		assert.Equal(t, int64(0), result.ParkingTimeS)
		assert.Equal(t, price(t, "3.30", "3.99"), result.Breakdown.Total)
	})
}

// nightVersion is energy at 0.20 per kWh between 22:00 and 06:00 and 0.30 otherwise.
func nightVersion(t *testing.T) *store.TariffVersion {
	t.Helper()
	v := &store.TariffVersion{Currency: "EUR", Elements: []store.TariffElement{
		{
			PriceComponents: []store.PriceComponent{{Type: store.PriceComponentEnergy, Price: amount(t, "0.20"), StepSize: 1}},
			Restrictions:    &store.TariffRestrictions{StartTime: ptr("22:00"), EndTime: ptr("06:00")},
		},
		{PriceComponents: []store.PriceComponent{{Type: store.PriceComponentEnergy, Price: amount(t, "0.30"), StepSize: 1}}},
	}}
	require.Empty(t, tariffs.Validate(v))
	return v
}

// The times of day of a tariff are those of the place of the transaction: the location of
// its EVSE, else the location of its charge station, else UTC.
func TestRateInTheTimeZoneOfTheLocation(t *testing.T) {
	ctx := context.Background()
	// a session from 19:00 to 21:00 UTC, which is 21:00 to 23:00 in Madrid: 7 kWh in each
	// hour
	const startMinute = 9 * 60
	const inMadrid, inUTC = "3.50", "4.20"

	session := func(t *testing.T, f *fixture, station string, connectorId int) *rating.Result {
		t.Helper()
		night := f.tariff(t, "Night", nightVersion(t))
		require.NoError(t, f.engine.CreateTransaction(ctx, station, "tx-1", "RFID0001", "ISO14443", nil, 0, false))
		details := &store.TransactionDetails{StartedAt: ptr(minute(startMinute)), MeterStartWh: ptr(int64(meterStartWh))}
		if connectorId > 0 {
			details.ConnectorId, details.EvseId = &connectorId, &connectorId
		}
		require.NoError(t, f.engine.UpdateTransactionDetails(ctx, station, "tx-1", details))
		require.NoError(t, f.engine.SetTransactionTariff(ctx, station, "tx-1", &night.Id, ptr(1), "connector"))
		f.readings(t, station, "tx-1", startMinute+30, 3500, startMinute+90, 10500)
		f.stop(t, station, "tx-1", startMinute+120, 14000)
		result, _, err := f.service.Rate(ctx, station, "tx-1", true)
		require.NoError(t, err)
		require.NotNil(t, result)
		return result
	}
	stationAt := func(t *testing.T, f *fixture, station string, location *store.AssetLocation) {
		t.Helper()
		record, err := f.engine.LookupChargeStation(ctx, station)
		require.NoError(t, err)
		require.NotNil(t, record)
		record.LocationId = &location.Id
		require.NoError(t, f.engine.UpdateChargeStation(ctx, record))
	}
	otherSite := func(t *testing.T, f *fixture, timeZone string) *store.AssetLocation {
		t.Helper()
		location := &store.AssetLocation{
			OrgId: testOrg, PartyRef: "party-a", Address: "1 Main Street", City: "Reykjavik", Country: "ISL",
			Latitude: "64.146600", Longitude: "-21.942600", TimeZone: timeZone,
		}
		require.NoError(t, f.engine.CreateAssetLocation(ctx, location))
		return location
	}

	t.Run("the location of the EVSE", func(t *testing.T) {
		f := newFixture(t)
		result := session(t, f, testStation, 1)
		assert.Equal(t, price(t, inMadrid, inMadrid), result.Breakdown.Total)
		require.Len(t, result.ChargingPeriods, 2)
		assert.True(t, result.ChargingPeriods[1].StartDateTime.Equal(minute(startMinute+60)), "22:00 in Madrid")
	})

	t.Run("the location of the EVSE, not that of the charge station", func(t *testing.T) {
		f := newFixture(t)
		stationAt(t, f, testStation, otherSite(t, f, "Atlantic/Reykjavik"))
		assert.Equal(t, price(t, inMadrid, inMadrid), session(t, f, testStation, 1).Breakdown.Total)
	})

	t.Run("an EVSE that the asset model does not have: the location of the charge station", func(t *testing.T) {
		f := newFixture(t)
		stationAt(t, f, testStation, f.site)
		assert.Equal(t, price(t, inMadrid, inMadrid), session(t, f, testStation, 2).Breakdown.Total)
	})

	t.Run("a transaction that does not say where it ran: the location of the charge station", func(t *testing.T) {
		f := newFixture(t)
		stationAt(t, f, testStation, f.site)
		assert.Equal(t, price(t, inMadrid, inMadrid), session(t, f, testStation, 0).Breakdown.Total)
	})

	t.Run("the location of the charge station is not that of the transaction when the EVSE has another", func(t *testing.T) {
		// the other way round: the EVSE in Reykjavik, the charge station in Madrid
		f := newFixture(t)
		stationAt(t, f, testStation, f.site)
		evse, err := f.engine.LookupAssetEvseByOcpp(ctx, testStation, 1)
		require.NoError(t, err)
		evse.LocationId = otherSite(t, f, "Atlantic/Reykjavik").Id
		require.NoError(t, f.engine.UpdateAssetEvse(ctx, evse))
		assert.Equal(t, price(t, inUTC, inUTC), session(t, f, testStation, 1).Breakdown.Total)
	})

	t.Run("a charge station that has not said which OCPP version it speaks", func(t *testing.T) {
		// the EVSE id of a transaction names the EVSE whatever the version
		f := newFixture(t)
		require.NoError(t, f.engine.SetChargeStationRuntimeDetails(ctx, testStation, &store.ChargeStationRuntimeDetails{}))
		assert.Equal(t, price(t, inMadrid, inMadrid), session(t, f, testStation, 1).Breakdown.Total)
	})

	t.Run("a backend with a registry and without assets: UTC", func(t *testing.T) {
		for _, method := range []string{"LookupAssetEvseByOcpp", "LookupAssetLocation"} {
			f := newFixture(t)
			stationAt(t, f, testStation, f.site)
			f.service.Assets = failingBackend{Engine: f.engine, fail: method, err: errNotImplemented}
			// with and without an EVSE: the second lookup is that of the location
			connectorId := map[string]int{"LookupAssetEvseByOcpp": 1, "LookupAssetLocation": 2}[method]
			assert.Equal(t, price(t, inUTC, inUTC), session(t, f, testStation, connectorId).Breakdown.Total, method)
		}
	})

	t.Run("no location at all: UTC", func(t *testing.T) {
		f := newFixture(t)
		assert.Equal(t, price(t, inUTC, inUTC), session(t, f, testStation, 2).Breakdown.Total)
		f = newFixture(t)
		assert.Equal(t, price(t, inUTC, inUTC), session(t, f, bareStation, 1).Breakdown.Total)
	})

	t.Run("without the stores that tell the place: UTC", func(t *testing.T) {
		f := newFixture(t)
		f.service.Assets = nil
		assert.Equal(t, price(t, inUTC, inUTC), session(t, f, testStation, 1).Breakdown.Total)
		f = newFixture(t)
		f.service.Registry = nil
		assert.Equal(t, price(t, inUTC, inUTC), session(t, f, testStation, 1).Breakdown.Total)
	})

	t.Run("a time zone that is not known fails the rating", func(t *testing.T) {
		for _, timeZone := range []string{"Mars/Olympus_Mons", "Local"} {
			f := newFixture(t)
			stationAt(t, f, testStation, otherSite(t, f, timeZone))
			night := f.tariff(t, "Night", nightVersion(t))
			require.NoError(t, f.engine.CreateTransaction(ctx, testStation, "tx-1", "RFID0001", "ISO14443", nil, 0, false))
			require.NoError(t, f.engine.UpdateTransactionDetails(ctx, testStation, "tx-1", &store.TransactionDetails{
				StartedAt: &sessionStart, StoppedAt: ptr(minute(60)),
			}))
			require.NoError(t, f.engine.SetTransactionTariff(ctx, testStation, "tx-1", &night.Id, ptr(1), "connector"))

			_, err := f.service.End(ctx, testStation, "tx-1")
			require.ErrorContains(t, err, timeZone)
			assert.Nil(t, f.transaction(t, testStation, "tx-1").Cost, "not rated with the times of day of another place")
		}
	})
}

// The current of a sample is what the charge station measured with the reading: the sum
// over the lines, or the value that it gave without a phase.
func TestRateWithTheMeasuredCurrent(t *testing.T) {
	ctx := context.Background()
	current := func(value float64, options ...func(*store.SampledValue)) store.SampledValue {
		sv := store.SampledValue{Measurand: ptr("Current.Import"), Value: value}
		for _, option := range options {
			option(&sv)
		}
		return sv
	}
	phase := func(p string) func(*store.SampledValue) {
		return func(sv *store.SampledValue) { sv.Phase = &p }
	}
	at := func(l string) func(*store.SampledValue) {
		return func(sv *store.SampledValue) { sv.Location = &l }
	}
	unit := func(u string, multiplier int) func(*store.SampledValue) {
		return func(sv *store.SampledValue) { sv.UnitOfMeasure = &store.UnitOfMeasure{Unit: u, Multipler: multiplier} }
	}

	t.Run("a price from 32 A", func(t *testing.T) {
		f := newFixture(t)
		fast := &store.TariffVersion{Currency: "EUR", Elements: []store.TariffElement{
			{
				PriceComponents: []store.PriceComponent{{Type: store.PriceComponentEnergy, Price: amount(t, "0.50"), StepSize: 1}},
				Restrictions:    &store.TariffRestrictions{MinCurrent: ptr(32.0)},
			},
			{PriceComponents: []store.PriceComponent{{Type: store.PriceComponentEnergy, Price: amount(t, "0.30"), StepSize: 1}}},
		}}
		f.start(t, testStation, "tx-1", 1, f.tariff(t, "Fast", fast), 1)
		// 3 x 10 A in the first half hour (5 kWh), 3 x 16 A in the second (7 kWh)
		require.NoError(t, f.engine.StoreMeterValues(ctx, testStation, 1, "tx-1", []store.MeterValue{
			registerAt(0, 0, "Sample.Periodic", current(10, phase("L1")), current(10, phase("L2")), current(10, phase("L3"))),
			registerAt(30, 5000, "Sample.Periodic", current(16, phase("L1")), current(16, phase("L2")), current(16, phase("L3"))),
		}))
		f.stop(t, testStation, "tx-1", 60, 12000)

		result, session, err := f.service.Rate(ctx, testStation, "tx-1", true)
		require.NoError(t, err)
		// 5 kWh at 0.30 and 7 kWh at 0.50
		assert.Equal(t, price(t, "5.00", "5.00"), result.Breakdown.Total)
		assert.Equal(t, []rating.Sample{
			// the meter start, the reading of the start and the meter value of that moment
			{At: minute(0), EnergyWh: 0, CurrentA: ptr(30.0)}, {At: minute(0), EnergyWh: 0, CurrentA: ptr(30.0)},
			{At: minute(0), EnergyWh: 0, CurrentA: ptr(30.0)},
			{At: minute(30), EnergyWh: 5000, CurrentA: ptr(48.0)},
			{At: minute(60), EnergyWh: 12000},
		}, session.Samples)
	})

	for name, tc := range map[string]struct {
		sampled []store.SampledValue
		want    *float64
	}{
		"no current":                        {nil, nil},
		"a value without a phase":           {[]store.SampledValue{current(25)}, ptr(25.0)},
		"a value without a phase and lines": {[]store.SampledValue{current(8, phase("L1")), current(25), current(8, phase("L2"))}, ptr(25.0)},
		"one line":                          {[]store.SampledValue{current(16, phase("L1"))}, ptr(16.0)},
		"lines to neutral":                  {[]store.SampledValue{current(16, phase("L1-N")), current(15, phase("L2-N")), current(14, phase("L3-N"))}, ptr(45.0)},
		"a line that is given twice":        {[]store.SampledValue{current(16, phase("L1")), current(16, phase("L1-N")), current(10, phase("L2"))}, ptr(26.0)},
		"the neutral conductor and between lines do not count": {
			[]store.SampledValue{current(16, phase("L1")), current(3, phase("N")), current(27, phase("L1-L2"))}, ptr(16.0)},
		"only the neutral conductor":          {[]store.SampledValue{current(3, phase("N"))}, nil},
		"in A, with a multiplier":             {[]store.SampledValue{current(16000, unit("A", -3))}, ptr(16.0)},
		"a unit that is not named":            {[]store.SampledValue{current(16, unit("", 0))}, ptr(16.0)},
		"another unit is not a current":       {[]store.SampledValue{current(16, unit("V", 0))}, nil},
		"at the outlet":                       {[]store.SampledValue{current(16, at("Outlet"))}, ptr(16.0)},
		"at another place":                    {[]store.SampledValue{current(16, at("Inlet"))}, nil},
		"the current that the EV may draw":    {[]store.SampledValue{{Measurand: ptr("Current.Offered"), Value: 32}}, nil},
		"a value without a measurand":         {[]store.SampledValue{{Value: 32, Phase: ptr("L1")}}, nil},
		"the first value without a phase":     {[]store.SampledValue{current(20), current(21)}, ptr(20.0)},
		"a current of nothing is a current":   {[]store.SampledValue{current(0, phase("L1")), current(0, phase("L2"))}, ptr(0.0)},
		"a current that is not a number":      {[]store.SampledValue{current(16, unit("A", 400))}, nil},
		"a line beside a value that is wrong": {[]store.SampledValue{current(16, phase("L1")), current(16, phase("L2"), unit("A", 400))}, ptr(16.0)},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.start(t, testStation, "tx-1", 1, f.parking, 1)
			require.NoError(t, f.engine.StoreMeterValues(ctx, testStation, 1, "tx-1", []store.MeterValue{registerAt(30, 5000, "Sample.Periodic", tc.sampled...)}))
			f.clock.SetTime(minute(31))

			_, session, err := f.service.Rate(ctx, testStation, "tx-1", false)
			require.NoError(t, err)
			require.Len(t, session.Samples, 3)
			assert.Nil(t, session.Samples[0].CurrentA)
			assert.Equal(t, tc.want, session.Samples[2].CurrentA)
		})
	}

	t.Run("a current without a reading of the register at that moment is not used", func(t *testing.T) {
		f := newFixture(t)
		f.start(t, testStation, "tx-1", 1, f.parking, 1)
		require.NoError(t, f.engine.StoreMeterValues(ctx, testStation, 1, "tx-1", []store.MeterValue{
			{Timestamp: minute(20).Format(time.RFC3339), SampledValues: []store.SampledValue{current(16)}},
			registerAt(30, 5000, "Sample.Periodic"),
		}))
		f.clock.SetTime(minute(31))

		_, session, err := f.service.Rate(ctx, testStation, "tx-1", false)
		require.NoError(t, err)
		assert.Equal(t, []rating.Sample{{At: minute(0)}, {At: minute(0)}, {At: minute(30), EnergyWh: 5000}}, session.Samples)
	})
}

// A transaction of a store that keeps neither its start nor its meter start (an OCPP
// 2.0.1 transaction in memory) is rated from its meter values: its times are those of the
// first and the last one, its energy is counted from its earliest reading.
func TestRateFromTheMeterValuesAlone(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.service.Fallback = &sessions.FallbackTariff{Currency: "EUR", PricePerKwh: amount(t, "0.55")}
	require.NoError(t, f.engine.CreateTransaction(ctx, bareStation, "tx-1", "RFID0001", "ISO14443",
		[]store.MeterValue{registerAt(0, 0, "Transaction.Begin")}, 0, false))
	require.NoError(t, f.engine.UpdateTransaction(ctx, bareStation, "tx-1", []store.MeterValue{registerAt(30, 4000, "Sample.Periodic")}))

	// while it runs
	f.clock.SetTime(minute(40))
	result, session, err := f.service.Rate(ctx, bareStation, "tx-1", false)
	require.NoError(t, err)
	assert.Equal(t, price(t, "2.20", "2.20"), result.Breakdown.Total)
	assert.Equal(t, sessionStart, session.Start)
	assert.Equal(t, minute(40), session.Now)
	assert.Equal(t, []rating.Sample{{At: minute(0)}, {At: minute(30), EnergyWh: 4000}}, session.Samples)

	// and after its end
	require.NoError(t, f.engine.EndTransaction(ctx, bareStation, "tx-1", "RFID0001", "ISO14443",
		[]store.MeterValue{registerAt(60, 10000, "Transaction.End")}, 2))
	result, err = f.service.End(ctx, bareStation, "tx-1")
	require.NoError(t, err)
	assert.True(t, result.Final)
	assert.Equal(t, price(t, "5.50", "5.50"), result.Breakdown.Total)
	assert.Equal(t, int64(3600), result.TotalTimeS)
	assert.Equal(t, int64(10000), result.TotalEnergyWh)
}

// What a message that is being handled tells about a transaction is rated with it, and
// the stored transaction is left as it is.
func TestRateWithWhatIsNotStoredYet(t *testing.T) {
	ctx := context.Background()

	t.Run("the meter values of the message", func(t *testing.T) {
		f := newFixture(t)
		f.start(t, testStation, "tx-1", 1, f.parking, 1)
		f.readings(t, testStation, "tx-1", 30, 5500)
		f.clock.SetTime(minute(60))
		before := *f.transaction(t, testStation, "tx-1")

		result, err := f.service.RunningCost(ctx, testStation, "tx-1", []store.MeterValue{registerAt(60, 11000, "Sample.Periodic")})

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, int64(11000), result.TotalEnergyWh)
		assert.Equal(t, price(t, "3.30", "3.99"), result.Breakdown.Total)
		after := f.transaction(t, testStation, "tx-1")
		assert.Equal(t, before, *after, "nothing is stored, and the stored transaction is not changed")
		assert.Len(t, after.MeterValues, 1)

		// without them the session has the energy of the stored readings
		f.clock.SetTime(minute(62))
		result, err = f.service.RunningCost(ctx, testStation, "tx-1", nil)
		require.NoError(t, err)
		assert.Equal(t, int64(5500), result.TotalEnergyWh)
	})

	t.Run("the end of the transaction", func(t *testing.T) {
		f := newFixture(t)
		f.chargeAndPark(t, "tx-1")
		before := *f.transaction(t, testStation, "tx-1")
		stoppedAt, meterStop := minute(160), int64(meterStartWh+11000)

		preview, err := f.service.PreviewEnd(ctx, testStation, "tx-1", sessions.Pending{
			MeterValues: []store.MeterValue{registerAt(160, 11000, "Transaction.End")},
			StoppedAt:   &stoppedAt,
			MeterStopWh: &meterStop,
		})

		require.NoError(t, err)
		require.NotNil(t, preview)
		assert.True(t, preview.Final)
		assert.Equal(t, price(t, "5.80", "7.02"), preview.Breakdown.Total)
		after := f.transaction(t, testStation, "tx-1")
		assert.Equal(t, before, *after, "nothing is stored, and the stored transaction is not changed")
		assert.Nil(t, after.StoppedAt)
		assert.Nil(t, after.Cost)

		// it is the cost that End works out once the end is stored
		f.stop(t, testStation, "tx-1", 160, 11000)
		final, err := f.service.End(ctx, testStation, "tx-1")
		require.NoError(t, err)
		assert.Equal(t, preview, final)
	})

	t.Run("an end with its meter stop alone", func(t *testing.T) {
		// 12.5 kWh by the meter stop, where the readings so far show 11 kWh
		f := newFixture(t)
		f.chargeAndPark(t, "tx-1")
		stoppedAt, meterStop := minute(160), int64(meterStartWh+12500)
		preview, err := f.service.PreviewEnd(ctx, testStation, "tx-1", sessions.Pending{StoppedAt: &stoppedAt, MeterStopWh: &meterStop})
		require.NoError(t, err)
		assert.Equal(t, int64(12500), preview.TotalEnergyWh)
		assert.Equal(t, price(t, "3.75", "4.54"), preview.Breakdown.Energy.Cost)
		// and by the meter stop it took 1.5 kWh in its last 40 minutes: it charged to its end
		assert.Equal(t, int64(0), preview.ParkingTimeS)
		assert.Equal(t, preview.Breakdown.Energy.Cost, preview.Breakdown.Total)
	})

	t.Run("an end with a meter stop below the meter start", func(t *testing.T) {
		// the meter was replaced during the session: its energy is that of the readings
		f := newFixture(t)
		f.chargeAndPark(t, "tx-1")
		stoppedAt, meterStop := minute(160), int64(500)
		preview, err := f.service.PreviewEnd(ctx, testStation, "tx-1", sessions.Pending{StoppedAt: &stoppedAt, MeterStopWh: &meterStop})
		require.NoError(t, err)
		assert.Equal(t, int64(11000), preview.TotalEnergyWh)
	})

	t.Run("a transaction that is not known, or has no price", func(t *testing.T) {
		f := newFixture(t)
		stoppedAt := minute(160)
		_, err := f.service.PreviewEnd(ctx, testStation, "tx-9", sessions.Pending{StoppedAt: &stoppedAt})
		require.ErrorIs(t, err, store.ErrNotFound)

		f.start(t, testStation, "tx-1", 1, nil, 0)
		preview, err := f.service.PreviewEnd(ctx, testStation, "tx-1", sessions.Pending{StoppedAt: &stoppedAt})
		require.NoError(t, err)
		assert.Nil(t, preview)

		// an end without its time is no end
		f.start(t, testStation, "tx-2", 1, f.parking, 1)
		_, err = f.service.PreviewEnd(ctx, testStation, "tx-2", sessions.Pending{})
		require.ErrorIs(t, err, store.ErrInvalidState)
	})
}

// bareBackend is a storage backend that keeps the transactions and their last cost, and
// nothing else: no tariffs, no assets, no registry, no meter values on their own, no
// analysis and no cost.
type bareBackend struct {
	store.Engine
}

var errNotImplemented = fmt.Errorf("not in this backend: %w", store.ErrNotImplemented)

func (bareBackend) LookupTariffVersion(context.Context, string, int) (*store.TariffVersion, error) {
	return nil, errNotImplemented
}

func (bareBackend) LookupChargeStation(context.Context, string) (*store.ChargeStation, error) {
	return nil, errNotImplemented
}

func (bareBackend) LookupAssetEvseByOcpp(context.Context, string, int) (*store.AssetEvse, error) {
	return nil, errNotImplemented
}

func (bareBackend) LookupAssetLocation(context.Context, string) (*store.AssetLocation, error) {
	return nil, errNotImplemented
}

func (bareBackend) QueryMeterValues(context.Context, store.MeterValuesFilter) (*store.MeterValuesResult, error) {
	return nil, errNotImplemented
}

func (bareBackend) SetTransactionAnalysis(context.Context, string, string, store.TransactionAnalysis) error {
	return errNotImplemented
}

func (bareBackend) SetTransactionCost(context.Context, string, string, store.TransactionCost) error {
	return errNotImplemented
}

// With a backend that has no tariffs a transaction is rated with the fallback price, in
// UTC, and the amount with VAT is kept as its last cost.
func TestRateWithABackendWithoutTariffs(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	backend := bareBackend{Engine: f.engine}
	f.service = &sessions.Service{
		Analyzer: &rating.Analyzer{
			Transactions: backend, MeterValues: backend, Assets: backend, Registry: backend, Tariffs: backend,
			Defaults: rating.DefaultIdleSettings(), Clock: f.clock,
		},
		Transactions: backend, MeterValues: backend, Tariffs: backend, Assets: backend, Registry: backend, Tx: backend,
		Clock:    f.clock,
		Fallback: &sessions.FallbackTariff{Currency: "EUR", PricePerKwh: amount(t, "0.55")},
	}
	// a transaction as OCPP 2.0.1 stores it: its meter values with it
	require.NoError(t, f.engine.CreateTransaction(ctx, testStation, "tx-1", "RFID0001", "ISO14443",
		[]store.MeterValue{registerAt(0, 0, "Transaction.Begin")}, 0, false))
	require.NoError(t, f.engine.UpdateTransaction(ctx, testStation, "tx-1", []store.MeterValue{registerAt(30, 4000, "Sample.Periodic")}))
	// the in-memory store has a tariff for it all the same, which this backend cannot read
	require.NoError(t, f.engine.SetTransactionTariff(ctx, testStation, "tx-1", &f.parking.Id, ptr(1), "connector"))

	f.clock.SetTime(minute(40))
	running, err := f.service.UpdateRunningCost(ctx, testStation, "tx-1")
	require.NoError(t, err)
	require.NotNil(t, running)
	assert.Equal(t, price(t, "2.20", "2.20"), running.Breakdown.Total)
	stored := f.transaction(t, testStation, "tx-1")
	assert.Equal(t, ptr(2.2), stored.LastCost)
	assert.Nil(t, stored.Cost)

	require.NoError(t, f.engine.EndTransaction(ctx, testStation, "tx-1", "RFID0001", "ISO14443",
		[]store.MeterValue{registerAt(60, 10000, "Transaction.End")}, 2))
	final, err := f.service.End(ctx, testStation, "tx-1")
	require.NoError(t, err)
	require.NotNil(t, final)
	assert.Equal(t, price(t, "5.50", "5.50"), final.Breakdown.Total)
	assert.Empty(t, final.TariffId)
	stored = f.transaction(t, testStation, "tx-1")
	assert.Equal(t, ptr(5.5), stored.LastCost)
	assert.Nil(t, stored.Cost)
	assert.Nil(t, stored.Analysis)

	// and without a fallback price nothing is rated
	f.service.Fallback = nil
	final, err = f.service.End(ctx, testStation, "tx-1")
	require.NoError(t, err)
	assert.Nil(t, final)
}

// failingBackend is a store of which one read or write fails.
type failingBackend struct {
	store.Engine
	fail string
	err  error
}

func (s failingBackend) failure(method string) error {
	if s.fail == method {
		return s.err
	}
	return nil
}

func (s failingBackend) FindTransactionSummary(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	if err := s.failure("FindTransactionSummary"); err != nil {
		return nil, err
	}
	return s.Engine.FindTransactionSummary(ctx, chargeStationId, transactionId)
}

func (s failingBackend) FindTransaction(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	if err := s.failure("FindTransaction"); err != nil {
		return nil, err
	}
	return s.Engine.FindTransaction(ctx, chargeStationId, transactionId)
}

func (s failingBackend) LookupTariffVersion(ctx context.Context, tariffId string, version int) (*store.TariffVersion, error) {
	if err := s.failure("LookupTariffVersion"); err != nil {
		return nil, err
	}
	return s.Engine.LookupTariffVersion(ctx, tariffId, version)
}

func (s failingBackend) LookupChargeStation(ctx context.Context, stationKey string) (*store.ChargeStation, error) {
	if err := s.failure("LookupChargeStation"); err != nil {
		return nil, err
	}
	return s.Engine.LookupChargeStation(ctx, stationKey)
}

func (s failingBackend) LookupAssetEvseByOcpp(ctx context.Context, stationKey string, ocppEvseId int) (*store.AssetEvse, error) {
	if err := s.failure("LookupAssetEvseByOcpp"); err != nil {
		return nil, err
	}
	return s.Engine.LookupAssetEvseByOcpp(ctx, stationKey, ocppEvseId)
}

func (s failingBackend) LookupAssetLocation(ctx context.Context, locationId string) (*store.AssetLocation, error) {
	if err := s.failure("LookupAssetLocation"); err != nil {
		return nil, err
	}
	return s.Engine.LookupAssetLocation(ctx, locationId)
}

func (s failingBackend) QueryMeterValues(ctx context.Context, filter store.MeterValuesFilter) (*store.MeterValuesResult, error) {
	if err := s.failure("QueryMeterValues"); err != nil {
		return nil, err
	}
	return s.Engine.QueryMeterValues(ctx, filter)
}

func (s failingBackend) SetTransactionCost(ctx context.Context, chargeStationId, transactionId string, cost store.TransactionCost) error {
	if err := s.failure("SetTransactionCost"); err != nil {
		return err
	}
	return s.Engine.SetTransactionCost(ctx, chargeStationId, transactionId, cost)
}

// A store that fails fails the rating, and nothing is stored: a transaction is not given
// a cost that what could not be read might have changed.
func TestRateFailsWhenAStoreFails(t *testing.T) {
	ctx := context.Background()
	errDown := errors.New("the store is down")

	for _, method := range []string{
		"FindTransactionSummary", "FindTransaction", "LookupTariffVersion", "LookupChargeStation", "LookupAssetEvseByOcpp",
		"LookupAssetLocation", "QueryMeterValues", "SetTransactionCost",
	} {
		t.Run(method, func(t *testing.T) {
			f := newFixture(t)
			f.chargeAndPark(t, "tx-1")
			backend := failingBackend{Engine: f.engine, fail: method, err: errDown}
			f.service.Transactions, f.service.MeterValues, f.service.Tariffs = backend, backend, backend
			f.service.Assets, f.service.Registry = backend, backend
			f.clock.SetTime(minute(105))

			running, err := f.service.UpdateRunningCost(ctx, testStation, "tx-1")
			require.ErrorIs(t, err, errDown)
			assert.Nil(t, running)
			assert.Nil(t, f.transaction(t, testStation, "tx-1").Cost)

			f.stop(t, testStation, "tx-1", 160, 11000)
			final, err := f.service.End(ctx, testStation, "tx-1")
			require.ErrorIs(t, err, errDown)
			assert.Nil(t, final)
			assert.Nil(t, f.transaction(t, testStation, "tx-1").Cost)

			if method != "SetTransactionCost" {
				_, _, err = f.service.Rate(ctx, testStation, "tx-1", true)
				require.ErrorIs(t, err, errDown)
				assert.ErrorContains(t, err, "rate transaction "+testStation+"/tx-1")
			}
		})
	}

	t.Run("the store of the Analyzer", func(t *testing.T) {
		f := newFixture(t)
		f.chargeAndPark(t, "tx-1")
		f.service.Analyzer.Registry = failingBackend{Engine: f.engine, fail: "LookupChargeStation", err: errDown}
		f.clock.SetTime(minute(105))
		_, err := f.service.UpdateRunningCost(ctx, testStation, "tx-1")
		require.ErrorIs(t, err, errDown)

		f.stop(t, testStation, "tx-1", 160, 11000)
		_, err = f.service.End(ctx, testStation, "tx-1")
		require.ErrorIs(t, err, errDown)
		assert.Nil(t, f.transaction(t, testStation, "tx-1").Cost)
	})

	t.Run("a tariff version that is gone", func(t *testing.T) {
		f := newFixture(t)
		f.service.Fallback = &sessions.FallbackTariff{Currency: "EUR", PricePerKwh: amount(t, "0.55")}
		f.start(t, testStation, "tx-1", 1, f.parking, 7)
		f.stop(t, testStation, "tx-1", 60, 11000)
		_, err := f.service.End(ctx, testStation, "tx-1")
		require.ErrorIs(t, err, store.ErrNotFound)
		assert.ErrorContains(t, err, "tariff version "+f.parking.Id+"/7")
		assert.Nil(t, f.transaction(t, testStation, "tx-1").Cost, "not rated with another price than its own")
	})
}

// observingTransactions notes how the transactions are read: in a database transaction or
// not, and with which deadline. It can also block a read until its ctx is done, or panic.
type observingTransactions struct {
	store.TransactionStore
	inTx        []bool
	hasDeadline bool
	deadlineIn  time.Duration
	block       bool
	panicWith   any
}

func (s *observingTransactions) FindTransactionSummary(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	s.inTx = append(s.inTx, eventstest.InTx(ctx))
	var deadline time.Time
	if deadline, s.hasDeadline = ctx.Deadline(); s.hasDeadline {
		s.deadlineIn = time.Until(deadline)
	}
	if s.panicWith != nil {
		panic(s.panicWith)
	}
	if s.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return s.TransactionStore.FindTransactionSummary(ctx, chargeStationId, transactionId)
}

func (s *observingTransactions) SetTransactionCost(ctx context.Context, chargeStationId, transactionId string, cost store.TransactionCost) error {
	s.inTx = append(s.inTx, eventstest.InTx(ctx))
	return s.TransactionStore.SetTransactionCost(ctx, chargeStationId, transactionId, cost)
}

// The ratings for the handlers of the messages of a charge station have a deadline, and
// those of the end of a transaction run in a database transaction: there they read from
// the primary, and what they write is stored together or not at all.
func TestTheRatingsOfTheHandlersAreBounded(t *testing.T) {
	ctx := context.Background()
	newObserved := func(t *testing.T) (*fixture, *observingTransactions) {
		f := newFixture(t)
		f.chargeAndPark(t, "tx-1")
		observed := &observingTransactions{TransactionStore: f.engine}
		f.service.Transactions = observed
		f.service.Tx = &eventstest.Transactor{Store: f.engine}
		return f, observed
	}
	bounded := func(t *testing.T, observed *observingTransactions) {
		t.Helper()
		require.True(t, observed.hasDeadline)
		assert.LessOrEqual(t, observed.deadlineIn, sessions.RatingTimeout)
		assert.Positive(t, observed.deadlineIn)
	}

	t.Run("the cost so far", func(t *testing.T) {
		f, observed := newObserved(t)
		f.clock.SetTime(minute(105))
		result, err := f.service.UpdateRunningCost(ctx, testStation, "tx-1")
		require.NoError(t, err)
		require.NotNil(t, result)
		bounded(t, observed)
		assert.Equal(t, []bool{false, false}, observed.inTx, "the read and the write")

		f, observed = newObserved(t)
		f.clock.SetTime(minute(105))
		_, err = f.service.RunningCost(ctx, testStation, "tx-1", nil)
		require.NoError(t, err)
		bounded(t, observed)
	})

	t.Run("the final cost", func(t *testing.T) {
		f, observed := newObserved(t)
		f.stop(t, testStation, "tx-1", 160, 11000)
		result, err := f.service.End(ctx, testStation, "tx-1")
		require.NoError(t, err)
		require.NotNil(t, result)
		bounded(t, observed)
		assert.Equal(t, []bool{true, true}, observed.inTx, "the read and the write, in one database transaction")
	})

	t.Run("the cost of a transaction that is about to end", func(t *testing.T) {
		f, observed := newObserved(t)
		stoppedAt := minute(160)
		result, err := f.service.PreviewEnd(ctx, testStation, "tx-1", sessions.Pending{StoppedAt: &stoppedAt})
		require.NoError(t, err)
		require.NotNil(t, result)
		bounded(t, observed)
		assert.Equal(t, []bool{true}, observed.inTx)
	})

	t.Run("Rate has the deadline of its caller", func(t *testing.T) {
		f, observed := newObserved(t)
		f.clock.SetTime(minute(105))
		_, _, err := f.service.Rate(ctx, testStation, "tx-1", false)
		require.NoError(t, err)
		assert.False(t, observed.hasDeadline)
		assert.Equal(t, []bool{false}, observed.inTx)
	})

	t.Run("without a transactor", func(t *testing.T) {
		f, observed := newObserved(t)
		f.service.Tx = nil
		f.stop(t, testStation, "tx-1", 160, 11000)
		result, err := f.service.End(ctx, testStation, "tx-1")
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, []bool{false, false}, observed.inTx)
		assert.NotNil(t, f.transaction(t, testStation, "tx-1").Cost)
	})

	t.Run("a rating that panics gives an error", func(t *testing.T) {
		f, observed := newObserved(t)
		observed.panicWith = "the rating broke"
		f.clock.SetTime(minute(105))
		for name, call := range map[string]func() (*rating.Result, error){
			"UpdateRunningCost": func() (*rating.Result, error) { return f.service.UpdateRunningCost(ctx, testStation, "tx-1") },
			"RunningCost":       func() (*rating.Result, error) { return f.service.RunningCost(ctx, testStation, "tx-1", nil) },
			"PreviewEnd": func() (*rating.Result, error) {
				return f.service.PreviewEnd(ctx, testStation, "tx-1", sessions.Pending{})
			},
			"End": func() (*rating.Result, error) { return f.service.End(ctx, testStation, "tx-1") },
		} {
			f.service.RunningEvery = 0
			result, err := call()
			require.ErrorContains(t, err, "the rating broke", name)
			assert.ErrorContains(t, err, testStation+"/tx-1", name)
			assert.Nil(t, result, name)
		}
	})

	t.Run("a rating that takes too long gives an error", func(t *testing.T) {
		f, observed := newObserved(t)
		observed.block = true
		f.stop(t, testStation, "tx-1", 160, 11000)
		started := time.Now()
		result, err := f.service.End(ctx, testStation, "tx-1")
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Nil(t, result)
		assert.Less(t, time.Since(started), sessions.RatingTimeout+time.Second)
		assert.GreaterOrEqual(t, time.Since(started), sessions.RatingTimeout)
		assert.Nil(t, f.transaction(t, testStation, "tx-1").Cost)
	})
}

// StoreCost stores the cost of a rating: nothing without a result.
func TestStoreCost(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.start(t, testStation, "tx-1", 1, f.parking, 1)

	require.NoError(t, f.service.StoreCost(ctx, testStation, "tx-1", nil))
	assert.Nil(t, f.transaction(t, testStation, "tx-1").Cost)

	result := &rating.Result{Currency: "EUR", Breakdown: rating.PriceBreakdown{Total: price(t, "3.80", "4.60")}}
	require.NoError(t, f.service.StoreCost(ctx, testStation, "tx-1", result))
	assert.Equal(t, &store.TransactionCost{Currency: "EUR", Total: price(t, "3.80", "4.60"), RatedAt: ratingTime},
		f.transaction(t, testStation, "tx-1").Cost)

	err := f.service.StoreCost(ctx, testStation, "tx-9", result)
	require.ErrorIs(t, err, store.ErrNotFound)
	assert.ErrorContains(t, err, testStation+"/tx-9")

	// a result that the store refuses
	err = f.service.StoreCost(ctx, testStation, "tx-1", &rating.Result{Currency: "euro"})
	require.Error(t, err)
	assert.Equal(t, price(t, "3.80", "4.60"), f.transaction(t, testStation, "tx-1").Cost.Total)
}

// The cost of a rating as the session events give it.
func TestEventCost(t *testing.T) {
	assert.Nil(t, sessions.EventCost(nil))

	result := &rating.Result{
		Currency: "EUR", TariffId: "0193b2c5-0000-7000-8000-000000000021", TariffVersion: 3, Final: true,
		Breakdown:     rating.PriceBreakdown{Total: price(t, "5.80", "7.02")},
		TotalEnergyWh: 11000, TotalTimeS: 9600, ChargingTimeS: 3600, ParkingTimeS: 6000,
	}
	assert.Equal(t, &events.SessionCost{
		Currency: "EUR", ExclVat: amount(t, "5.80"), InclVat: amount(t, "7.02"), Final: true,
		TariffId: ptr("0193b2c5-0000-7000-8000-000000000021"), TariffVersion: ptr(3),
		EnergyWh: 11000, ChargingS: 3600, ParkingS: 6000,
	}, sessions.EventCost(result))

	// a session without a tariff, rated with the fallback price, while it runs
	fallback := &rating.Result{Currency: "PLN", Breakdown: rating.PriceBreakdown{Total: price(t, "2.20", "2.20")}, TotalEnergyWh: 4000, ChargingTimeS: 1800}
	assert.Equal(t, &events.SessionCost{Currency: "PLN", ExclVat: amount(t, "2.20"), InclVat: amount(t, "2.20"), EnergyWh: 4000, ChargingS: 1800},
		sessions.EventCost(fallback))
}
