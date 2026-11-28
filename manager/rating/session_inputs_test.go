// SPDX-License-Identifier: Apache-2.0

package rating_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thoughtworks/maeve-csms/manager/rating"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// What the rating of a transaction takes from this package besides the analysis: the
// times of a transaction, its meter values and readings, and its idle time while it runs.

func TestTransactionTimes(t *testing.T) {
	at := func(m float64) store.MeterValue { return meterValueAt(m, sample(100_000)) }
	unreadable := store.MeterValue{Timestamp: "noon", SampledValues: []store.SampledValue{sample(100_000)}}

	for name, tc := range map[string]struct {
		tx        store.Transaction
		start     time.Time
		stop      *time.Time
		startKnow bool
	}{
		"the stored times": {
			tx:    store.Transaction{StartedAt: ptr(minute(1)), StoppedAt: ptr(minute(90)), MeterValues: []store.MeterValue{at(0), at(100)}, EndedSeqNo: 2},
			start: minute(1), stop: ptr(minute(90)), startKnow: true,
		},
		"a transaction that runs": {
			tx:    store.Transaction{StartedAt: ptr(minute(1)), MeterValues: []store.MeterValue{at(0), at(100)}},
			start: minute(1), startKnow: true,
		},
		"no stored times: the first and the last meter value, in whatever order they are": {
			tx:    store.Transaction{MeterValues: []store.MeterValue{at(30), unreadable, at(100), at(5)}, EndedSeqNo: 3},
			start: minute(5), stop: ptr(minute(100)), startKnow: true,
		},
		"no stored times, and it runs": {
			tx:    store.Transaction{MeterValues: []store.MeterValue{at(30), at(100), at(5)}},
			start: minute(5), startKnow: true,
		},
		"a stored stop and no stored start": {
			tx:    store.Transaction{StoppedAt: ptr(minute(90)), MeterValues: []store.MeterValue{at(10)}},
			start: minute(10), stop: ptr(minute(90)), startKnow: true,
		},
		"nothing to tell the start from": {
			tx:   store.Transaction{StoppedAt: ptr(minute(90)), MeterValues: []store.MeterValue{unreadable}},
			stop: ptr(minute(90)),
		},
		"nothing at all": {tx: store.Transaction{EndedSeqNo: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			start, stop, startKnown := rating.TransactionTimes(&tc.tx)
			assert.Equal(t, tc.startKnow, startKnown)
			assert.True(t, tc.start.Equal(start), "%s", start)
			if tc.stop == nil {
				assert.Nil(t, stop)
			} else {
				require.NotNil(t, stop)
				assert.True(t, tc.stop.Equal(*stop), "%s", stop)
			}
		})
	}
}

// LoadMeterValues returns the meter values that are stored with the transaction and those
// that the charge station sent on their own, and EnergyPoints the readings in them: the
// two together are LoadEnergyPoints.
func TestLoadMeterValuesAndEnergyPoints(t *testing.T) {
	ctx := context.Background()
	f := newAnalysisFixture(t)
	f.start(t, analysisStation, "tx-1", 1, 0)
	sent := []store.MeterValue{
		registerMeterValue(reading(30, 5.5), "Sample.Periodic"),
		// a meter value without a reading of the register
		meterValueAt(40, sample(16, measurand("Current.Import"))),
	}
	require.NoError(t, f.engine.StoreMeterValues(ctx, analysisStation, 1, "tx-1", sent))
	stored, err := f.engine.FindTransaction(ctx, analysisStation, "tx-1")
	require.NoError(t, err)
	tx := *stored
	// the meter values of the transaction in a slice with room to spare: nothing may be
	// written into it
	tx.MeterValues = append(make([]store.MeterValue, 0, 8), stored.MeterValues...)

	meterValues, err := rating.LoadMeterValues(ctx, &tx, f.engine)

	require.NoError(t, err)
	require.Len(t, meterValues, 3)
	assert.Equal(t, tx.MeterValues[0], meterValues[0], "the meter value of the start, which is stored with the transaction")
	assert.ElementsMatch(t, sent, meterValues[1:], "in the order in which the store gives them")
	assert.Len(t, tx.MeterValues, 1)
	assert.Empty(t, tx.MeterValues[:cap(tx.MeterValues)][1].Timestamp, "the slice of the transaction is not written to")

	points := rating.EnergyPoints(&tx, meterValues)
	assert.Equal(t, []rating.EnergyPoint{reading(0, 0), reading(0, 0), reading(30, 5.5)}, points,
		"the meter start, the reading of the start and the reading that was sent")
	loaded, err := rating.LoadEnergyPoints(ctx, &tx, f.engine)
	require.NoError(t, err)
	assert.Equal(t, points, loaded)

	// without a store of the meter values that were sent, only those of the transaction
	meterValues, err = rating.LoadMeterValues(ctx, &tx, nil)
	require.NoError(t, err)
	assert.Equal(t, tx.MeterValues, meterValues)
	// and a store that fails fails the load
	errDown := errors.New("the store is down")
	_, err = rating.LoadMeterValues(ctx, &tx, failingMeterValues{err: errDown})
	require.ErrorIs(t, err, errDown)
}

// AnalyseReadings is Analyse with the readings given, and IdleNow tells the idle time of
// a transaction that runs with the same thresholds.
func TestAnalyseReadingsAndIdleNow(t *testing.T) {
	ctx := context.Background()
	f := newAnalysisFixture(t)
	f.start(t, analysisStation, "tx-1", 1, 0)
	require.NoError(t, f.engine.StoreMeterValues(ctx, analysisStation, 1, "tx-1", []store.MeterValue{
		registerMeterValue(reading(30, 5.5), "Sample.Periodic"),
		registerMeterValue(reading(60, 11), "Sample.Periodic"),
		registerMeterValue(reading(90, 11), "Sample.Periodic"),
	}))
	running, err := f.engine.FindTransaction(ctx, analysisStation, "tx-1")
	require.NoError(t, err)
	points, err := rating.LoadEnergyPoints(ctx, running, f.engine)
	require.NoError(t, err)

	t.Run("idle since the last reading with energy", func(t *testing.T) {
		idle, err := f.analyzer.IdleNow(ctx, running, points, minute(105))
		require.NoError(t, err)
		assert.Equal(t, rating.IdleResult{IdleStartedAt: ptr(minute(60)), Idle: 45 * time.Minute}, idle)
	})

	t.Run("charging when it was last heard of", func(t *testing.T) {
		idle, err := f.analyzer.IdleNow(ctx, running, points[:4], minute(105))
		require.NoError(t, err)
		assert.Equal(t, rating.IdleResult{}, idle)
	})

	t.Run("with the thresholds of the connector and of the tariff", func(t *testing.T) {
		// an hour at 11 kW, then half an hour at 1 kW, at a connector of 150 kW: 1 kW is
		// below 1 % of its power
		f.start(t, analysisStation, "tx-2", 2, 0)
		trickle := []rating.EnergyPoint{reading(0, 0), reading(60, 11), reading(90, 11.5)}
		slow, err := f.engine.FindTransaction(ctx, analysisStation, "tx-2")
		require.NoError(t, err)
		idle, err := f.analyzer.IdleNow(ctx, slow, trickle, minute(95))
		require.NoError(t, err)
		assert.Equal(t, ptr(minute(60)), idle.IdleStartedAt)
		// at the connector of 22 kW it charges
		idle, err = f.analyzer.IdleNow(ctx, running, trickle, minute(95))
		require.NoError(t, err)
		assert.Nil(t, idle.IdleStartedAt)
	})

	t.Run("a transaction whose start is not known", func(t *testing.T) {
		_, err := f.analyzer.IdleNow(ctx, &store.Transaction{ChargeStationId: analysisStation, TransactionId: "tx-9"}, points, minute(105))
		require.ErrorIs(t, err, store.ErrInvalidState)
	})

	t.Run("a store that fails", func(t *testing.T) {
		errDown := errors.New("the store is down")
		broken := *f.analyzer
		broken.Registry = failingRegistry{ChargeStationRegistryStore: f.engine, err: errDown}
		_, err := broken.IdleNow(ctx, running, points, minute(105))
		require.ErrorIs(t, err, errDown)
	})

	t.Run("the analysis of the readings that are given", func(t *testing.T) {
		// the in-memory store hands out the transaction that it keeps: this is how it
		// was while it ran
		stillRunning := *running
		f.stop(t, analysisStation, "tx-1", reading(160, 11))
		ended, err := f.engine.FindTransaction(ctx, analysisStation, "tx-1")
		require.NoError(t, err)
		want, err := f.analyzer.Analyse(ctx, ended)
		require.NoError(t, err)
		require.Equal(t, int64(6000), want.IdleS)

		// the readings are those that it is given, not those of the store
		all, err := rating.LoadEnergyPoints(ctx, ended, f.engine)
		require.NoError(t, err)
		got, err := f.analyzer.AnalyseReadings(ctx, ended, all)
		require.NoError(t, err)
		assert.Equal(t, want, got)
		got, err = f.analyzer.AnalyseReadings(ctx, ended, all[:1])
		require.NoError(t, err)
		assert.Equal(t, int64(9600), got.ChargingS, "with the meter start alone it charged from there to its meter stop")
		got, err = f.analyzer.AnalyseReadings(ctx, ended, nil)
		require.NoError(t, err)
		assert.Equal(t, int64(9600), got.IdleS, "and without any reading but its meter stop it never charged")

		_, err = f.analyzer.AnalyseReadings(ctx, &stillRunning, points)
		require.ErrorIs(t, err, store.ErrInvalidState, "a transaction that has not ended")
	})
}

// failingRegistry is a registry that cannot be read.
type failingRegistry struct {
	store.ChargeStationRegistryStore
	err error
}

func (s failingRegistry) LookupChargeStation(context.Context, string) (*store.ChargeStation, error) {
	return nil, s.err
}
