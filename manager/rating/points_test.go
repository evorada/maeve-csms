// SPDX-License-Identifier: Apache-2.0

package rating_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/clock"

	"github.com/thoughtworks/maeve-csms/manager/rating"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
)

const (
	pointsStation     = "aaaa.CP001"
	pointsTransaction = "tx-1"
)

// sample is a sampled value with the fields that the options set.
func sample(value float64, options ...func(*store.SampledValue)) store.SampledValue {
	sv := store.SampledValue{Value: value}
	for _, option := range options {
		option(&sv)
	}
	return sv
}

func measurand(m string) func(*store.SampledValue) {
	return func(sv *store.SampledValue) { sv.Measurand = &m }
}

func sampleContext(c string) func(*store.SampledValue) {
	return func(sv *store.SampledValue) { sv.Context = &c }
}

func location(l string) func(*store.SampledValue) {
	return func(sv *store.SampledValue) { sv.Location = &l }
}

func phase(p string) func(*store.SampledValue) {
	return func(sv *store.SampledValue) { sv.Phase = &p }
}

func unit(u string, multiplier int) func(*store.SampledValue) {
	return func(sv *store.SampledValue) { sv.UnitOfMeasure = &store.UnitOfMeasure{Unit: u, Multipler: multiplier} }
}

// meterValueAt is a meter value of the minute of the session.
func meterValueAt(m float64, samples ...store.SampledValue) store.MeterValue {
	return store.MeterValue{Timestamp: minute(m).Format(time.RFC3339), SampledValues: samples}
}

// pointsTransactionWith is a transaction without a start and a meter start, with the
// meter values.
func pointsTransactionWith(meterValues ...store.MeterValue) *store.Transaction {
	return &store.Transaction{ChargeStationId: pointsStation, TransactionId: pointsTransaction, MeterValues: meterValues}
}

func loadPoints(t *testing.T, tx *store.Transaction, mv store.MeterValuesStore) []rating.EnergyPoint {
	t.Helper()
	points, err := rating.LoadEnergyPoints(context.Background(), tx, mv)
	require.NoError(t, err)
	return points
}

// Which sampled values are readings of the energy register of the connector, and what
// they read in Wh.
func TestLoadEnergyPointsReadsTheEnergyRegister(t *testing.T) {
	const register = "Energy.Active.Import.Register"

	for name, tc := range map[string]struct {
		sample store.SampledValue
		wantWh float64
		skip   bool
	}{
		"only a value: the register in Wh, by the defaults of OCPP": {sample: sample(111_000), wantWh: 111_000},
		"the measurand of the register":                             {sample: sample(111_000, measurand(register)), wantWh: 111_000},
		"Wh":                                                        {sample: sample(111_000, measurand(register), unit("Wh", 0)), wantWh: 111_000},
		"kWh":                                                       {sample: sample(111, measurand(register), unit("kWh", 0)), wantWh: 111_000},
		"a fraction of a kWh":                                       {sample: sample(111.25, unit("kWh", 0)), wantWh: 111_250},
		"a unit without a name is Wh":                               {sample: sample(111_000, unit("", 0)), wantWh: 111_000},
		"Wh with the multiplier 3":                                  {sample: sample(111, unit("Wh", 3)), wantWh: 111_000},
		"Wh with the multiplier -1":                                 {sample: sample(1_110_000, unit("Wh", -1)), wantWh: 111_000},
		"kWh with the multiplier -3":                                {sample: sample(111_000, unit("kWh", -3)), wantWh: 111_000},
		"kWh with the multiplier 3":                                 {sample: sample(0.111, unit("kWh", 3)), wantWh: 111_000},
		"at the outlet":                                             {sample: sample(111_000, location("Outlet")), wantWh: 111_000},
		"with a context":                                            {sample: sample(111_000, sampleContext("Sample.Periodic")), wantWh: 111_000},

		"at the inlet":                 {sample: sample(111_000, location("Inlet")), skip: true},
		"at the EV":                    {sample: sample(111_000, location("EV")), skip: true},
		"of one phase":                 {sample: sample(37_000, phase("L1")), skip: true},
		"the power":                    {sample: sample(11_000, measurand("Power.Active.Import"), unit("W", 0)), skip: true},
		"the energy of an interval":    {sample: sample(500, measurand("Energy.Active.Import.Interval")), skip: true},
		"the register of the export":   {sample: sample(111_000, measurand("Energy.Active.Export.Register")), skip: true},
		"the reactive energy":          {sample: sample(111_000, measurand("Energy.Reactive.Import.Register"), unit("varh", 0)), skip: true},
		"a unit that is not of energy": {sample: sample(111_000, unit("varh", 0)), skip: true},
		"the state of charge":          {sample: sample(80, measurand("SoC"), unit("Percent", 0)), skip: true},
	} {
		t.Run(name, func(t *testing.T) {
			points := loadPoints(t, pointsTransactionWith(meterValueAt(30, tc.sample)), nil)
			if tc.skip {
				assert.Empty(t, points)
				return
			}
			require.Len(t, points, 1)
			assert.True(t, minute(30).Equal(points[0].At))
			assert.InDelta(t, tc.wantWh, points[0].RegisterWh, 1e-6)
		})
	}
}

func TestLoadEnergyPointsOfTheMeterValuesOfATransaction(t *testing.T) {
	tx := pointsTransactionWith(
		// not in the order of time
		meterValueAt(60, sample(111_000)),
		// a meter value with the readings of the phases, the power and the register
		meterValueAt(30, sample(1800, phase("L1")), sample(11_000, measurand("Power.Active.Import")), sample(105_500)),
		store.MeterValue{Timestamp: "half past ten", SampledValues: []store.SampledValue{sample(107_000)}},
		meterValueAt(45, sample(80, measurand("SoC"))),
	)

	assert.Equal(t, []rating.EnergyPoint{
		{At: minute(30), RegisterWh: 105_500},
		{At: minute(60), RegisterWh: 111_000},
	}, loadPoints(t, tx, nil), "in the order of time, without the meter value whose time cannot be read")
}

// The first reading is the meter start at the start of the transaction.
func TestLoadEnergyPointsStartsWithTheMeterStart(t *testing.T) {
	later := meterValueAt(30, sample(105_500))

	tx := pointsTransactionWith(later)
	tx.StartedAt, tx.MeterStartWh = ptr(sessionStart), ptr(int64(100_000))
	assert.Equal(t, []rating.EnergyPoint{
		{At: sessionStart, RegisterWh: 100_000},
		{At: minute(30), RegisterWh: 105_500},
	}, loadPoints(t, tx, nil))

	tx = pointsTransactionWith(later)
	tx.StartedAt = ptr(sessionStart)
	assert.Equal(t, []rating.EnergyPoint{{At: minute(30), RegisterWh: 105_500}}, loadPoints(t, tx, nil), "no meter start")

	tx = pointsTransactionWith(later)
	tx.MeterStartWh = ptr(int64(100_000))
	assert.Equal(t, []rating.EnergyPoint{{At: minute(30), RegisterWh: 105_500}}, loadPoints(t, tx, nil), "no start")
}

// Transactions of OCPP 1.6 were once stored with the energy of the transaction as the
// value of its end: that is not a reading of the register.
func TestLoadEnergyPointsDropsTheEnergyOfTheTransaction(t *testing.T) {
	end := sampleContext("Transaction.End")
	transaction := func(meterStartWh *int64, last store.SampledValue) *store.Transaction {
		tx := pointsTransactionWith(meterValueAt(30, sample(105_500)), meterValueAt(90, last))
		tx.StartedAt, tx.MeterStartWh = ptr(sessionStart), meterStartWh
		return tx
	}
	meterStart := ptr(int64(100_000))
	start := rating.EnergyPoint{At: sessionStart, RegisterWh: 100_000}
	periodic := rating.EnergyPoint{At: minute(30), RegisterWh: 105_500}

	assert.Equal(t, []rating.EnergyPoint{start, periodic},
		loadPoints(t, transaction(meterStart, sample(11_000, end, measurand("Energy.Active.Import.Register"), location("Outlet"))), nil),
		"11 kWh at the end of a transaction that started at 100 kWh is its energy")
	assert.Equal(t, []rating.EnergyPoint{start, periodic},
		loadPoints(t, transaction(meterStart, sample(11, end, unit("kWh", 0))), nil), "compared in Wh")
	assert.Equal(t, []rating.EnergyPoint{start, periodic, {At: minute(90), RegisterWh: 111_000}},
		loadPoints(t, transaction(meterStart, sample(111_000, end)), nil), "a reading of the register at the end")
	assert.Equal(t, []rating.EnergyPoint{start, periodic, {At: minute(90), RegisterWh: 100_000}},
		loadPoints(t, transaction(meterStart, sample(100_000, end)), nil), "a transaction without energy ends at its meter start")
	assert.Equal(t, []rating.EnergyPoint{start, periodic, {At: minute(90), RegisterWh: 11_000}},
		loadPoints(t, transaction(meterStart, sample(11_000, sampleContext("Sample.Periodic"))), nil),
		"a low value that is not of the end is a reading (the register was replaced)")
	assert.Equal(t, []rating.EnergyPoint{periodic, {At: minute(90), RegisterWh: 11_000}},
		loadPoints(t, transaction(nil, sample(11_000, end)), nil), "without a meter start there is nothing to compare with")
}

// pagedMeterValues is a meter values store that records the pages that it is asked for.
type pagedMeterValues struct {
	store.MeterValuesStore
	filters []store.MeterValuesFilter
}

func (s *pagedMeterValues) QueryMeterValues(ctx context.Context, filter store.MeterValuesFilter) (*store.MeterValuesResult, error) {
	s.filters = append(s.filters, filter)
	return s.MeterValuesStore.QueryMeterValues(ctx, filter)
}

// An OCPP 1.6 charge station sends the readings of a transaction in MeterValues messages:
// they are stored on their own, with the id of the transaction.
func TestLoadEnergyPointsOfTheMeterValuesSentOnTheirOwn(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	// 2500 readings, one every 10 seconds, 10 Wh each, in three messages
	const total = 2500
	var sent []store.MeterValue
	for i := 1; i <= total; i++ {
		sent = append(sent, store.MeterValue{
			Timestamp:     sessionStart.Add(time.Duration(i) * 10 * time.Second).Format(time.RFC3339),
			SampledValues: []store.SampledValue{sample(float64(100_000+10*i), measurand("Energy.Active.Import.Register"), unit("Wh", 0))},
		})
	}
	require.NoError(t, engine.StoreMeterValues(ctx, pointsStation, 1, pointsTransaction, sent[:900]))
	require.NoError(t, engine.StoreMeterValues(ctx, pointsStation, 1, pointsTransaction, sent[900:1700]))
	require.NoError(t, engine.StoreMeterValues(ctx, pointsStation, 1, pointsTransaction, sent[1700:]))
	// what is not of the transaction
	other := []store.MeterValue{meterValueAt(5, sample(999_999))}
	require.NoError(t, engine.StoreMeterValues(ctx, pointsStation, 1, "tx-2", other))
	require.NoError(t, engine.StoreMeterValues(ctx, pointsStation, 1, "", other))
	require.NoError(t, engine.StoreMeterValues(ctx, "aaaa.CP002", 1, pointsTransaction, other))

	tx := pointsTransactionWith(meterValueAt(0, sample(100_000, sampleContext("Transaction.Begin"))))
	tx.StartedAt, tx.MeterStartWh = ptr(sessionStart), ptr(int64(100_000))
	backend := &pagedMeterValues{MeterValuesStore: engine}

	points := loadPoints(t, tx, backend)

	require.Len(t, points, total+2, "the meter start, the reading of the start, and every reading that was sent")
	assert.Equal(t, rating.EnergyPoint{At: sessionStart, RegisterWh: 100_000}, points[0])
	assert.Equal(t, rating.EnergyPoint{At: sessionStart, RegisterWh: 100_000}, points[1])
	for i := 1; i <= total; i++ {
		want := rating.EnergyPoint{At: sessionStart.Add(time.Duration(i) * 10 * time.Second), RegisterWh: float64(100_000 + 10*i)}
		require.Equal(t, want, points[i+1], "reading %d", i)
	}

	var offsets []int
	for _, filter := range backend.filters {
		assert.Equal(t, pointsStation, filter.ChargeStationId)
		assert.Equal(t, ptr(pointsTransaction), filter.TransactionId)
		assert.Equal(t, 1000, filter.Limit)
		assert.Nil(t, filter.ConnectorId, "a transaction is at one connector, whatever the connector of its meter values")
		offsets = append(offsets, filter.Offset)
	}
	assert.Equal(t, []int{0, 1000, 2000}, offsets, "read in pages")
}

// failingMeterValues is a meter values store whose query fails.
type failingMeterValues struct {
	store.MeterValuesStore
	err error
}

func (s failingMeterValues) QueryMeterValues(context.Context, store.MeterValuesFilter) (*store.MeterValuesResult, error) {
	return nil, s.err
}

func TestLoadEnergyPointsWithoutTheMeterValuesSentOnTheirOwn(t *testing.T) {
	tx := pointsTransactionWith(meterValueAt(30, sample(105_500)))
	want := []rating.EnergyPoint{{At: minute(30), RegisterWh: 105_500}}

	t.Run("a backend without the query", func(t *testing.T) {
		backend := failingMeterValues{err: fmt.Errorf("QueryMeterValues: %w", store.ErrNotImplemented)}
		assert.Equal(t, want, loadPoints(t, tx, backend))
	})

	t.Run("no store", func(t *testing.T) {
		assert.Equal(t, want, loadPoints(t, tx, nil))
	})

	t.Run("a store that fails", func(t *testing.T) {
		errDown := errors.New("the store is down")
		points, err := rating.LoadEnergyPoints(context.Background(), tx, failingMeterValues{err: errDown})
		require.ErrorIs(t, err, errDown)
		assert.Nil(t, points, "no readings rather than some of them")
	})
}

// A charge station that reports its register in kWh: 100 kWh at the start, 111 kWh an
// hour later and at the stop, half an hour after that.
func TestIdleTimeFromReadingsInKWh(t *testing.T) {
	kWh := func(value float64) store.SampledValue {
		return sample(value, measurand("Energy.Active.Import.Register"), unit("kWh", 0))
	}
	tx := pointsTransactionWith(meterValueAt(0, kWh(100.0)), meterValueAt(60, kWh(111.0)), meterValueAt(90, kWh(111.0)))
	stop := minute(90)

	got := rating.DetectIdle(loadPoints(t, tx, nil), sessionStart, &stop, stop, nil, maxPowerW, rating.DefaultIdleSettings())

	assert.Equal(t, idleAt(60, 30), got)
}
