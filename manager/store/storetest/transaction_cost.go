// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/money"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The time of the ratings of these tests, in whole seconds: PostgreSQL keeps
// microseconds.
var costTime = time.Date(2026, 11, 28, 10, 15, 0, 0, time.UTC)

// TestTransactionCost is the contract of TransactionStore.SetTransactionCost. newStore
// returns an empty store.
func TestTransactionCost(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()
	// 3.80 without and 4.60 with VAT, the cost so far of a transaction that runs
	running := store.TransactionCost{
		Currency: "EUR",
		Total:    money.Price{ExclVat: 3_8000, InclVat: 4_6000},
		RatedAt:  costTime,
	}
	// 5.80 and 7.02, the cost of the transaction after it ended
	final := store.TransactionCost{
		Currency: "EUR",
		Total:    money.Price{ExclVat: 5_8000, InclVat: 7_0200},
		Final:    true,
		RatedAt:  costTime.Add(75 * time.Minute),
	}
	cost := func(t *testing.T, s store.Engine, transactionId string) *store.TransactionCost {
		t.Helper()
		return findTransaction(t, s, transactionId).Cost
	}

	t.Run("a transaction gets its cost", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, analysisStation, orgA)
		endedTransaction(t, s, "tx-1")
		before := *findTransaction(t, s, "tx-1")
		assert.Nil(t, before.Cost, "a transaction has no cost until it is given one")
		assert.Nil(t, before.LastCost)

		require.NoError(t, s.SetTransactionCost(ctx, analysisStation, "tx-1", running))

		got := findTransaction(t, s, "tx-1")
		require.NotNil(t, got.Cost)
		assert.Equal(t, running, *got.Cost)
		assert.Equal(t, time.UTC, got.Cost.RatedAt.Location())
		assert.Equal(t, ptr(4.6), got.LastCost, "the amount with VAT is the last cost as well")
		// the rest of the transaction is as it was
		before.Cost, before.LastCost = got.Cost, got.LastCost
		assert.Equal(t, before, *got)

		summary, err := s.FindTransactionSummary(ctx, analysisStation, "tx-1")
		require.NoError(t, err)
		require.NotNil(t, summary.Cost)
		assert.Equal(t, running, *summary.Cost)

		listed, total, err := s.ListTransactionsForChargeStation(ctx, analysisStation, "all", nil, nil, 10, 0, nil)
		require.NoError(t, err)
		require.Equal(t, int64(1), total)
		require.NotNil(t, listed[0].Cost)
		assert.Equal(t, running, *listed[0].Cost)

		all, err := s.Transactions(ctx)
		require.NoError(t, err)
		require.Len(t, all, 1)
		require.NotNil(t, all[0].Cost)
		assert.Equal(t, running, *all[0].Cost)
	})

	t.Run("a later cost replaces the earlier one", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, analysisStation, orgA)
		endedTransaction(t, s, "tx-1")
		endedTransaction(t, s, "tx-2")
		require.NoError(t, s.SetTransactionCost(ctx, analysisStation, "tx-1", running))
		require.NoError(t, s.SetTransactionCost(ctx, analysisStation, "tx-2", running))

		// the cost so far, a minute later
		later := store.TransactionCost{Currency: "EUR", Total: money.Price{ExclVat: 4_3000, InclVat: 5_2000}, RatedAt: costTime.Add(time.Minute)}
		require.NoError(t, s.SetTransactionCost(ctx, analysisStation, "tx-1", later))
		assert.Equal(t, &later, cost(t, s, "tx-1"))
		assert.Equal(t, ptr(5.2), findTransaction(t, s, "tx-1").LastCost)

		// the final cost
		require.NoError(t, s.SetTransactionCost(ctx, analysisStation, "tx-1", final))
		assert.Equal(t, &final, cost(t, s, "tx-1"), "nothing is left of the cost so far")
		assert.Equal(t, ptr(7.02), findTransaction(t, s, "tx-1").LastCost)

		// and a final cost that was worked out again, in another currency
		again := store.TransactionCost{Currency: "PLN", Total: money.Price{ExclVat: 21_5000, InclVat: 26_4500}, Final: true, RatedAt: costTime.Add(2 * time.Hour)}
		require.NoError(t, s.SetTransactionCost(ctx, analysisStation, "tx-1", again))
		assert.Equal(t, &again, cost(t, s, "tx-1"))
		assert.Equal(t, ptr(26.45), findTransaction(t, s, "tx-1").LastCost)

		assert.Equal(t, &running, cost(t, s, "tx-2"), "the other transaction keeps its own")
		assert.Equal(t, ptr(4.6), findTransaction(t, s, "tx-2").LastCost)
	})

	t.Run("a transaction that has its final cost keeps it when a cost so far comes late", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, analysisStation, orgA)
		endedTransaction(t, s, "tx-1")
		require.NoError(t, s.SetTransactionCost(ctx, analysisStation, "tx-1", final))

		late := running
		late.RatedAt = final.RatedAt.Add(time.Second)
		require.NoError(t, s.SetTransactionCost(ctx, analysisStation, "tx-1", late), "nothing is written, and that is no error")

		assert.Equal(t, &final, cost(t, s, "tx-1"))
		assert.Equal(t, ptr(7.02), findTransaction(t, s, "tx-1").LastCost)
	})

	t.Run("a session that costs nothing has a cost", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, analysisStation, orgA)
		endedTransaction(t, s, "tx-1")
		free := store.TransactionCost{Currency: "EUR", Final: true, RatedAt: costTime}

		require.NoError(t, s.SetTransactionCost(ctx, analysisStation, "tx-1", free))

		assert.Equal(t, &free, cost(t, s, "tx-1"))
		assert.Equal(t, ptr(0.0), findTransaction(t, s, "tx-1").LastCost)

		// a cost so far of nothing replaces a cost so far
		require.NoError(t, s.CreateTransaction(ctx, analysisStation, "tx-2", "TAG1", "ISO14443", nil, 0, false))
		require.NoError(t, s.SetTransactionCost(ctx, analysisStation, "tx-2", running))
		nothing := store.TransactionCost{Currency: "EUR", RatedAt: costTime.Add(time.Minute)}
		require.NoError(t, s.SetTransactionCost(ctx, analysisStation, "tx-2", nothing))
		assert.Equal(t, &nothing, cost(t, s, "tx-2"))
	})

	t.Run("the time of another zone is read back as the same instant in UTC", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, analysisStation, orgA)
		endedTransaction(t, s, "tx-1")
		given := running
		given.RatedAt = costTime.In(time.FixedZone("CET", 3600))

		require.NoError(t, s.SetTransactionCost(ctx, analysisStation, "tx-1", given))

		assert.Equal(t, &running, cost(t, s, "tx-1"))
	})

	t.Run("the transaction goes on with its tariff, its details and its analysis", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, analysisStation, orgA)
		endedTransaction(t, s, "tx-1")
		analysis := store.TransactionAnalysis{IdleStartedAt: &analysisIdle, IdleS: 5400, ChargingS: 3600, AnalysedAt: analysisTime}
		require.NoError(t, s.SetTransactionTariff(ctx, analysisStation, "tx-1", ptr(testAssetId(50)), ptr(3), "connector"))
		require.NoError(t, s.SetTransactionAnalysis(ctx, analysisStation, "tx-1", analysis))
		require.NoError(t, s.SetTransactionCost(ctx, analysisStation, "tx-1", final))
		// what is written after the cost leaves it as it is
		require.NoError(t, s.UpdateTransactionDetails(ctx, analysisStation, "tx-1", &store.TransactionDetails{StopReason: ptr("EVDisconnected")}))
		require.NoError(t, s.SetTransactionAnalysis(ctx, analysisStation, "tx-1", analysis))

		got := findTransaction(t, s, "tx-1")
		assert.Equal(t, &final, got.Cost)
		assert.Equal(t, ptr(7.02), got.LastCost)
		assert.Equal(t, &analysis, got.Analysis)
		assert.Equal(t, ptr(testAssetId(50)), got.TariffId)
		assert.Equal(t, ptr(3), got.TariffVersion)
		assert.Equal(t, ptr("connector"), got.TariffSource)
		assert.Equal(t, ptr("EVDisconnected"), got.StopReason)
		assert.Equal(t, ptr(int64(111_000)), got.MeterStopWh)
		assert.Equal(t, orgA, got.OrgId)
		assert.Len(t, got.MeterValues, 2)

		// the cost that was communicated to the charge station is the last cost, and the
		// cost of the transaction stays
		require.NoError(t, s.UpdateTransactionCost(ctx, analysisStation, "tx-1", 7.5))
		got = findTransaction(t, s, "tx-1")
		assert.Equal(t, ptr(7.5), got.LastCost)
		assert.Equal(t, &final, got.Cost)
	})

	t.Run("what is refused", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, analysisStation, orgA)
		endedTransaction(t, s, "tx-1")
		require.NoError(t, s.CreateTransaction(ctx, "k7f2.CP002", "tx-2", "TAG1", "ISO14443", nil, 0, false))

		assert.ErrorIs(t, s.SetTransactionCost(ctx, analysisStation, "tx-9", running), store.ErrNotFound, "no such transaction")
		assert.ErrorIs(t, s.SetTransactionCost(ctx, analysisStation, "tx-2", running), store.ErrNotFound,
			"the transaction of another charge station")
		assert.ErrorIs(t, s.SetTransactionCost(ctx, analysisStation, "tx-9", final), store.ErrNotFound)

		with := func(change func(c *store.TransactionCost)) store.TransactionCost {
			changed := final
			change(&changed)
			return changed
		}
		for name, refused := range map[string]store.TransactionCost{
			"no currency":                       with(func(c *store.TransactionCost) { c.Currency = "" }),
			"a currency in small letters":       with(func(c *store.TransactionCost) { c.Currency = "eur" }),
			"a currency of four letters":        with(func(c *store.TransactionCost) { c.Currency = "EURO" }),
			"a currency of two letters":         with(func(c *store.TransactionCost) { c.Currency = "EU" }),
			"a negative cost without VAT":       with(func(c *store.TransactionCost) { c.Total.ExclVat = -1 }),
			"a negative cost with VAT":          with(func(c *store.TransactionCost) { c.Total.InclVat = -1 }),
			"a cost without VAT at the limit":   with(func(c *store.TransactionCost) { c.Total.ExclVat = store.MaxTransactionCost }),
			"a cost with VAT at the limit":      with(func(c *store.TransactionCost) { c.Total.InclVat = store.MaxTransactionCost }),
			"a cost without the time of it":     with(func(c *store.TransactionCost) { c.RatedAt = time.Time{} }),
			"more than one thing that is wrong": {Total: money.Price{ExclVat: -5, InclVat: -5}},
		} {
			err := s.SetTransactionCost(ctx, analysisStation, "tx-1", refused)
			assert.Error(t, err, name)
			assert.NotErrorIs(t, err, store.ErrNotFound, name)
		}
		got := findTransaction(t, s, "tx-1")
		assert.Nil(t, got.Cost, "a refused call writes nothing")
		assert.Nil(t, got.LastCost)

		// the highest cost that is kept, to the last decimal
		highest := with(func(c *store.TransactionCost) {
			c.Total = money.Price{ExclVat: store.MaxTransactionCost - 1, InclVat: store.MaxTransactionCost - 1}
		})
		require.NoError(t, s.SetTransactionCost(ctx, analysisStation, "tx-1", highest))
		assert.Equal(t, &highest, cost(t, s, "tx-1"))
		assert.Equal(t, ptr(99_999_999.9999), findTransaction(t, s, "tx-1").LastCost)
	})
}
