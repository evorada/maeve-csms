// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

const analysisStation = "k7f2.CP001"

// The times of the transactions of these tests, in whole seconds: PostgreSQL keeps
// microseconds.
var (
	analysisStart     = time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)
	analysisIdle      = time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	analysisStop      = time.Date(2026, 11, 28, 11, 30, 0, 0, time.UTC)
	analysisTime      = time.Date(2026, 11, 28, 11, 30, 2, 0, time.UTC)
	analysisMeasurand = "Energy.Active.Import.Register"
)

func analysisMeterValue(at time.Time, wh float64) store.MeterValue {
	return store.MeterValue{
		Timestamp:     at.Format(time.RFC3339),
		SampledValues: []store.SampledValue{{Measurand: &analysisMeasurand, Value: wh}},
	}
}

// endedTransaction stores a transaction of analysisStation, which belongs to orgA, that
// has ended, with a meter value of its start and one of its end.
func endedTransaction(t *testing.T, s store.Engine, transactionId string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, s.CreateTransaction(ctx, analysisStation, transactionId, "TAG1", "ISO14443",
		[]store.MeterValue{analysisMeterValue(analysisStart, 100_000)}, 0, false))
	require.NoError(t, s.EndTransaction(ctx, analysisStation, transactionId, "TAG1", "ISO14443",
		[]store.MeterValue{analysisMeterValue(analysisStop, 111_000)}, 1))
	require.NoError(t, s.UpdateTransactionDetails(ctx, analysisStation, transactionId, &store.TransactionDetails{
		ConnectorId: ptr(2), StartedAt: &analysisStart, StoppedAt: &analysisStop, MeterStartWh: ptr(int64(100_000)), MeterStopWh: ptr(int64(111_000)),
	}))
}

func findTransaction(t *testing.T, s store.Engine, transactionId string) *store.Transaction {
	t.Helper()
	transaction, err := s.FindTransaction(context.Background(), analysisStation, transactionId)
	require.NoError(t, err)
	require.NotNil(t, transaction)
	return transaction
}

// TestTransactionAnalysis is the contract of TransactionStore.SetTransactionAnalysis.
// newStore returns an empty store.
func TestTransactionAnalysis(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()
	analysis := store.TransactionAnalysis{
		IdleStartedAt: &analysisIdle,
		IdleS:         5400,
		ChargingS:     3600,
		AveragePowerW: ptr(11_000.5),
		AnalysedAt:    analysisTime,
	}

	t.Run("an ended transaction gets its analysis", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, analysisStation, orgA)
		endedTransaction(t, s, "tx-1")
		ended := findTransaction(t, s, "tx-1")
		assert.Nil(t, ended.Analysis, "a transaction has no analysis until it is given one")

		require.NoError(t, s.SetTransactionAnalysis(ctx, analysisStation, "tx-1", analysis))

		got := findTransaction(t, s, "tx-1")
		require.NotNil(t, got.Analysis)
		assert.Equal(t, analysis, *got.Analysis)
		assert.Equal(t, time.UTC, got.Analysis.AnalysedAt.Location())
		assert.Equal(t, time.UTC, got.Analysis.IdleStartedAt.Location())
		// the rest of the transaction is as it was
		ended.Analysis = got.Analysis
		assert.Equal(t, ended, got)

		listed, total, err := s.ListTransactionsForChargeStation(ctx, analysisStation, "all", nil, nil, 10, 0, nil)
		require.NoError(t, err)
		require.Equal(t, int64(1), total)
		require.NotNil(t, listed[0].Analysis)
		assert.Equal(t, analysis, *listed[0].Analysis)

		all, err := s.Transactions(ctx)
		require.NoError(t, err)
		require.Len(t, all, 1)
		require.NotNil(t, all[0].Analysis)
		assert.Equal(t, analysis, *all[0].Analysis)
	})

	t.Run("an analysis without an idle start and an average power", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, analysisStation, orgA)
		endedTransaction(t, s, "tx-1")
		noReadings := store.TransactionAnalysis{ChargingS: 9000, AnalysedAt: analysisTime}

		require.NoError(t, s.SetTransactionAnalysis(ctx, analysisStation, "tx-1", noReadings))

		got := findTransaction(t, s, "tx-1")
		require.NotNil(t, got.Analysis)
		assert.Equal(t, noReadings, *got.Analysis)

		// zeros are values: idle for no time, at no power
		zeros := store.TransactionAnalysis{IdleStartedAt: &analysisStop, AveragePowerW: ptr(0.0), AnalysedAt: analysisTime}
		require.NoError(t, s.SetTransactionAnalysis(ctx, analysisStation, "tx-1", zeros))
		got = findTransaction(t, s, "tx-1")
		require.NotNil(t, got.Analysis)
		assert.Equal(t, zeros, *got.Analysis)
	})

	t.Run("times of another zone are read back as the same instants in UTC", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, analysisStation, orgA)
		endedTransaction(t, s, "tx-1")
		zone := time.FixedZone("CET", 3600)
		given := analysis
		given.IdleStartedAt = ptr(analysisIdle.In(zone))
		given.AnalysedAt = analysisTime.In(zone)

		require.NoError(t, s.SetTransactionAnalysis(ctx, analysisStation, "tx-1", given))

		got := findTransaction(t, s, "tx-1")
		require.NotNil(t, got.Analysis)
		assert.Equal(t, analysis, *got.Analysis)
	})

	t.Run("a later analysis replaces the earlier one", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, analysisStation, orgA)
		endedTransaction(t, s, "tx-1")
		endedTransaction(t, s, "tx-2")
		require.NoError(t, s.SetTransactionAnalysis(ctx, analysisStation, "tx-1", analysis))
		require.NoError(t, s.SetTransactionAnalysis(ctx, analysisStation, "tx-2", analysis))

		later := store.TransactionAnalysis{IdleS: 60, ChargingS: 8940, AnalysedAt: analysisTime.Add(time.Hour)}
		require.NoError(t, s.SetTransactionAnalysis(ctx, analysisStation, "tx-1", later))

		got := findTransaction(t, s, "tx-1")
		require.NotNil(t, got.Analysis)
		assert.Equal(t, later, *got.Analysis, "nothing is left of the first analysis")
		other := findTransaction(t, s, "tx-2")
		require.NotNil(t, other.Analysis)
		assert.Equal(t, analysis, *other.Analysis, "the other transaction keeps its own")
	})

	t.Run("the analysis is stored with the transaction, not with what the caller holds", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, analysisStation, orgA)
		endedTransaction(t, s, "tx-1")
		idleStartedAt, averagePowerW := analysisIdle, 11_000.5
		given := store.TransactionAnalysis{IdleStartedAt: &idleStartedAt, IdleS: 5400, ChargingS: 3600, AveragePowerW: &averagePowerW, AnalysedAt: analysisTime}
		require.NoError(t, s.SetTransactionAnalysis(ctx, analysisStation, "tx-1", given))

		idleStartedAt, averagePowerW = analysisStop, 1

		got := findTransaction(t, s, "tx-1")
		require.NotNil(t, got.Analysis)
		assert.Equal(t, analysis, *got.Analysis)
	})

	t.Run("the transaction goes on with its tariff and its details", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, analysisStation, orgA)
		endedTransaction(t, s, "tx-1")
		require.NoError(t, s.SetTransactionTariff(ctx, analysisStation, "tx-1", ptr(testAssetId(50)), ptr(3), "connector"))
		require.NoError(t, s.SetTransactionAnalysis(ctx, analysisStation, "tx-1", analysis))
		// what is written after the analysis leaves it as it is
		require.NoError(t, s.UpdateTransactionDetails(ctx, analysisStation, "tx-1", &store.TransactionDetails{StopReason: ptr("EVDisconnected")}))
		require.NoError(t, s.UpdateTransactionCost(ctx, analysisStation, "tx-1", 4.5))

		got := findTransaction(t, s, "tx-1")
		require.NotNil(t, got.Analysis)
		assert.Equal(t, analysis, *got.Analysis)
		assert.Equal(t, ptr(testAssetId(50)), got.TariffId)
		assert.Equal(t, ptr(3), got.TariffVersion)
		assert.Equal(t, ptr("connector"), got.TariffSource)
		assert.Equal(t, ptr(2), got.ConnectorId)
		assert.Equal(t, ptr("EVDisconnected"), got.StopReason)
		assert.Equal(t, ptr(int64(111_000)), got.MeterStopWh)
		assert.Equal(t, ptr(4.5), got.LastCost)
		assert.Equal(t, orgA, got.OrgId)
		assert.Len(t, got.MeterValues, 2)
	})

	t.Run("what is refused", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, analysisStation, orgA)
		endedTransaction(t, s, "tx-1")
		require.NoError(t, s.CreateTransaction(ctx, "k7f2.CP002", "tx-2", "TAG1", "ISO14443", nil, 0, false))

		assert.ErrorIs(t, s.SetTransactionAnalysis(ctx, analysisStation, "tx-9", analysis), store.ErrNotFound, "no such transaction")
		assert.ErrorIs(t, s.SetTransactionAnalysis(ctx, analysisStation, "tx-2", analysis), store.ErrNotFound,
			"the transaction of another charge station")

		with := func(change func(a *store.TransactionAnalysis)) store.TransactionAnalysis {
			changed := analysis
			change(&changed)
			return changed
		}
		for name, refused := range map[string]store.TransactionAnalysis{
			"a negative idle time":                with(func(a *store.TransactionAnalysis) { a.IdleS = -1 }),
			"a negative charging time":            with(func(a *store.TransactionAnalysis) { a.ChargingS = -1 }),
			"a negative average power":            with(func(a *store.TransactionAnalysis) { a.AveragePowerW = ptr(-0.1) }),
			"an average power that is no number":  with(func(a *store.TransactionAnalysis) { a.AveragePowerW = ptr(math.NaN()) }),
			"an average power without end":        with(func(a *store.TransactionAnalysis) { a.AveragePowerW = ptr(math.Inf(1)) }),
			"an average power beyond the limit":   with(func(a *store.TransactionAnalysis) { a.AveragePowerW = ptr(float64(store.MaxAveragePowerW)) }),
			"an average power that rounds to it":  with(func(a *store.TransactionAnalysis) { a.AveragePowerW = ptr(999_999_999.96) }),
			"an analysis without the time of it":  with(func(a *store.TransactionAnalysis) { a.AnalysedAt = time.Time{} }),
			"more than one thing that is refused": {IdleS: -5, ChargingS: -5},
		} {
			err := s.SetTransactionAnalysis(ctx, analysisStation, "tx-1", refused)
			assert.Error(t, err, name)
			assert.NotErrorIs(t, err, store.ErrNotFound, name)
		}
		assert.Nil(t, findTransaction(t, s, "tx-1").Analysis, "a refused call writes nothing")

		// the highest power that is kept, and a transaction that has not ended: the store
		// does not judge the analysis
		highest := with(func(a *store.TransactionAnalysis) { a.AveragePowerW = ptr(999_999_999.9) })
		require.NoError(t, s.SetTransactionAnalysis(ctx, analysisStation, "tx-1", highest))
		assert.Equal(t, highest, *findTransaction(t, s, "tx-1").Analysis)
		require.NoError(t, s.CreateTransaction(ctx, analysisStation, "tx-3", "TAG1", "ISO14443", nil, 0, false))
		require.NoError(t, s.SetTransactionAnalysis(ctx, analysisStation, "tx-3", analysis))
	})
}

// TestTransactionSummary is the contract of TransactionStore.FindTransactionSummary.
// newStore returns an empty store.
func TestTransactionSummary(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()

	t.Run("a transaction without its meter values", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, analysisStation, orgA)
		endedTransaction(t, s, "tx-1")
		require.NoError(t, s.UpdateTransaction(ctx, analysisStation, "tx-1", []store.MeterValue{analysisMeterValue(analysisIdle, 111_000)}))
		require.NoError(t, s.SetTransactionTariff(ctx, analysisStation, "tx-1", ptr(testAssetId(50)), ptr(3), "connector"))
		require.NoError(t, s.UpdateTransactionCost(ctx, analysisStation, "tx-1", 4.5))
		require.NoError(t, s.SetTransactionAnalysis(ctx, analysisStation, "tx-1", store.TransactionAnalysis{
			IdleStartedAt: &analysisIdle, IdleS: 5400, ChargingS: 3600, AveragePowerW: ptr(11_000.0), AnalysedAt: analysisTime,
		}))

		summary, err := s.FindTransactionSummary(ctx, analysisStation, "tx-1")
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.Nil(t, summary.MeterValues)

		// everything else is what FindTransaction returns
		full := findTransaction(t, s, "tx-1")
		assert.Len(t, full.MeterValues, 3, "the transaction keeps its meter values")
		want := *full
		want.MeterValues = nil
		assert.Equal(t, &want, summary)
		assert.Equal(t, orgA, summary.OrgId)
		assert.Equal(t, ptr("connector"), summary.TariffSource)
		assert.Equal(t, "TAG1", summary.IdToken)
		require.NotNil(t, summary.Analysis)
		assert.Equal(t, int64(5400), summary.Analysis.IdleS)
	})

	t.Run("a transaction that runs", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, analysisStation, orgA)
		require.NoError(t, s.CreateTransaction(ctx, analysisStation, "tx-1", "TAG1", "ISO14443",
			[]store.MeterValue{analysisMeterValue(analysisStart, 100_000)}, 0, false))

		summary, err := s.FindTransactionSummary(ctx, analysisStation, "tx-1")
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.Nil(t, summary.MeterValues)
		assert.Nil(t, summary.TariffSource)
		assert.Nil(t, summary.Analysis)
		assert.Zero(t, summary.EndedSeqNo)
		assert.Equal(t, "TAG1", summary.IdToken)
		assert.Equal(t, orgA, summary.OrgId)
	})

	t.Run("no such transaction", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, analysisStation, orgA)
		require.NoError(t, s.CreateTransaction(ctx, "k7f2.CP002", "tx-2", "TAG1", "ISO14443", nil, 0, false))

		summary, err := s.FindTransactionSummary(ctx, analysisStation, "tx-9")
		require.NoError(t, err)
		assert.Nil(t, summary)
		summary, err = s.FindTransactionSummary(ctx, analysisStation, "tx-2")
		require.NoError(t, err)
		assert.Nil(t, summary, "the transaction of another charge station")
	})
}
