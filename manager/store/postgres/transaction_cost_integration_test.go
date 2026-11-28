// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thoughtworks/maeve-csms/manager/money"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/storetest"
)

func TestTransactionCost(t *testing.T) {
	storetest.TestTransactionCost(t, emptyTestStore)
}

// costColumns is the cost of a transaction as it is stored.
type costColumns struct {
	currency, exclVat, inclVat, lastCost *string
	final                                *bool
	ratedAt                              *time.Time
}

func readCostColumns(t *testing.T, stationKey, transactionId string) costColumns {
	t.Helper()
	var c costColumns
	require.NoError(t, testPool.QueryRow(context.Background(), `
		SELECT cost_currency::text, cost_excl_vat::text, cost_incl_vat::text, last_cost::text, cost_final, cost_rated_at
		FROM transactions WHERE charge_station_id = $1 AND id = $2`, stationKey, transactionId).
		Scan(&c.currency, &c.exclVat, &c.inclVat, &c.lastCost, &c.final, &c.ratedAt))
	return c
}

// The cost is stored in the columns of the transaction: the amounts with four decimals,
// the time as an instant, and the amount with VAT in last_cost too.
func TestSetTransactionCost_StoredShape(t *testing.T) {
	ctx := context.Background()
	emptyTestStore(t)
	const station = "k7f2.cs001"
	require.NoError(t, testStore.CreateTransaction(ctx, station, "tx-1", "TAG1", "ISO14443", nil, 0, false))
	assert.Equal(t, costColumns{}, readCostColumns(t, station, "tx-1"), "a transaction has no cost until it is given one")
	var updatedBefore time.Time
	require.NoError(t, testPool.QueryRow(ctx, `SELECT updated_at FROM transactions WHERE id = 'tx-1'`).Scan(&updatedBefore))

	// the time is given in another zone, to the microsecond that the column keeps
	ratedAt := time.Date(2026, 11, 28, 12, 30, 2, 987654000, time.FixedZone("CET", 3600))
	require.NoError(t, testStore.SetTransactionCost(ctx, station, "tx-1", store.TransactionCost{
		Currency: "EUR", Total: money.Price{ExclVat: 5_8000, InclVat: 7_0155}, RatedAt: ratedAt,
	}))

	got := readCostColumns(t, station, "tx-1")
	assert.Equal(t, ptr("EUR"), got.currency)
	assert.Equal(t, ptr("5.8000"), got.exclVat)
	assert.Equal(t, ptr("7.0155"), got.inclVat)
	assert.Equal(t, ptr("7.0155"), got.lastCost)
	assert.Equal(t, ptr(false), got.final)
	require.NotNil(t, got.ratedAt)
	assert.True(t, got.ratedAt.Equal(time.Date(2026, 11, 28, 11, 30, 2, 987654000, time.UTC)), "the instant, to the microsecond: %s", got.ratedAt)
	var updatedAfter time.Time
	require.NoError(t, testPool.QueryRow(ctx, `SELECT updated_at FROM transactions WHERE id = 'tx-1'`).Scan(&updatedAfter))
	assert.False(t, updatedAfter.Before(updatedBefore))

	// the store reads back what the columns keep, in UTC
	transaction, err := testStore.FindTransaction(ctx, station, "tx-1")
	require.NoError(t, err)
	require.NotNil(t, transaction.Cost)
	assert.Equal(t, store.TransactionCost{
		Currency: "EUR", Total: money.Price{ExclVat: 5_8000, InclVat: 7_0155},
		RatedAt: time.Date(2026, 11, 28, 11, 30, 2, 987654000, time.UTC),
	}, *transaction.Cost)
	assert.Equal(t, ptr(7.0155), transaction.LastCost)

	// the final cost, and the highest amounts that the columns keep
	require.NoError(t, testStore.SetTransactionCost(ctx, station, "tx-1", store.TransactionCost{
		Currency: "PLN", Total: money.Price{ExclVat: store.MaxTransactionCost - 1, InclVat: store.MaxTransactionCost - 1}, Final: true, RatedAt: ratedAt,
	}))
	got = readCostColumns(t, station, "tx-1")
	assert.Equal(t, ptr("PLN"), got.currency)
	assert.Equal(t, ptr("99999999.9999"), got.exclVat)
	assert.Equal(t, ptr("99999999.9999"), got.inclVat)
	assert.Equal(t, ptr("99999999.9999"), got.lastCost)
	assert.Equal(t, ptr(true), got.final)

	// a cost so far that comes late changes no column
	require.NoError(t, testStore.SetTransactionCost(ctx, station, "tx-1", store.TransactionCost{
		Currency: "EUR", Total: money.Price{ExclVat: 1_0000, InclVat: 1_2100}, RatedAt: ratedAt.Add(time.Hour),
	}))
	assert.Equal(t, got, readCostColumns(t, station, "tx-1"))

	// the cost that was communicated to the charge station only changes last_cost
	require.NoError(t, testStore.UpdateTransactionCost(ctx, station, "tx-1", 12.5))
	late := readCostColumns(t, station, "tx-1")
	assert.Equal(t, ptr("12.5000"), late.lastCost)
	assert.Equal(t, got.inclVat, late.inclVat)
	assert.Equal(t, got.final, late.final)
}

// The cost of a transaction is written in a database transaction of the caller. A call
// that is refused there (a cost that cannot be, or a transaction that is not known) sends
// no statement that fails, so the database transaction stays usable.
func TestSetTransactionCost_JoinsTheTransaction(t *testing.T) {
	ctx := context.Background()
	emptyTestStore(t)
	const station = "k7f2.cs001"
	cost := store.TransactionCost{Currency: "EUR", Total: money.Price{ExclVat: 2_6200, InclVat: 3_1700}, Final: true, RatedAt: time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)}

	rate := func(ctx context.Context, transactionId string) {
		require.NoError(t, testStore.CreateTransaction(ctx, station, transactionId, "TAG1", "ISO14443", nil, 0, false))

		refused := cost
		refused.Total.InclVat = store.MaxTransactionCost
		require.Error(t, testStore.SetTransactionCost(ctx, station, transactionId, refused))
		refused = cost
		refused.Currency = "euro"
		require.Error(t, testStore.SetTransactionCost(ctx, station, transactionId, refused))
		require.ErrorIs(t, testStore.SetTransactionCost(ctx, station, "tx-unknown", cost), store.ErrNotFound)

		require.NoError(t, testStore.SetTransactionCost(ctx, station, transactionId, cost))
		// a cost so far after the final cost: nothing is written, and nothing fails
		soFar := cost
		soFar.Final = false
		soFar.Total = money.Price{ExclVat: 1_0000, InclVat: 1_2100}
		require.NoError(t, testStore.SetTransactionCost(ctx, station, transactionId, soFar))

		got, err := testStore.FindTransaction(ctx, station, transactionId)
		require.NoError(t, err)
		require.NotNil(t, got, "the transaction is still usable, and reads what it wrote")
		require.NotNil(t, got.Cost)
		require.Equal(t, cost, *got.Cost)
	}

	err := testStore.RunInTx(ctx, func(ctx context.Context) error {
		rate(ctx, "tx-rolled-back")
		return errStopTx
	})
	require.ErrorIs(t, err, errStopTx)
	got, err := testStore.FindTransaction(ctx, station, "tx-rolled-back")
	require.NoError(t, err)
	assert.Nil(t, got, "rolled back with the transaction")

	require.NoError(t, testStore.RunInTx(ctx, func(ctx context.Context) error {
		rate(ctx, "tx-committed")
		return nil
	}))
	got, err = testStore.FindTransaction(ctx, station, "tx-committed")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Cost)
	assert.Equal(t, cost, *got.Cost)
	requireNoOpenTransaction(t)
}

// The final cost of a transaction stays, however many costs so far are written at the
// same time: one statement decides, so no cost so far can be written over the final cost.
func TestSetTransactionCost_FinalCostStays(t *testing.T) {
	ctx := context.Background()
	emptyTestStore(t)
	const station = "k7f2.cs001"
	ratedAt := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	final := store.TransactionCost{Currency: "EUR", Total: money.Price{ExclVat: 5_8000, InclVat: 7_0200}, Final: true, RatedAt: ratedAt}

	for round := 0; round < 5; round++ {
		transactionId := "tx-" + string(rune('a'+round))
		require.NoError(t, testStore.CreateTransaction(ctx, station, transactionId, "TAG1", "ISO14443", nil, 0, false))

		const writers = 12
		errs := make([]error, writers)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < writers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				cost := store.TransactionCost{Currency: "EUR", Total: money.Price{ExclVat: money.Amount(i) * 1_0000, InclVat: money.Amount(i) * 1_2100}, RatedAt: ratedAt}
				if i == writers/2 {
					cost = final
				}
				errs[i] = testStore.SetTransactionCost(ctx, station, transactionId, cost)
			}(i)
		}
		close(start)
		wg.Wait()
		for i, err := range errs {
			require.NoError(t, err, "writer %d", i)
		}

		got, err := testStore.FindTransaction(ctx, station, transactionId)
		require.NoError(t, err)
		require.NotNil(t, got.Cost)
		assert.Equal(t, final, *got.Cost)
		assert.Equal(t, ptr(7.02), got.LastCost)
	}
}

// Migration 000049 adds the cost of a transaction. The transactions that are there have
// none, and keep their last cost; going down loses only the costs.
func TestMigration_AddTransactionRatedCost(t *testing.T) {
	ctx := context.Background()

	const (
		versionBefore = 48
		versionAfter  = 49
		orgId         = "0193b2c4-0000-7000-8000-0000000000a1"
		tariffId      = "0193b2c5-0000-7000-8000-000000000021"
	)

	migrationConnString, pool := migrationTestDatabase(t, "maeve_migration_000049")

	exec := func(statement string, args ...any) {
		t.Helper()
		_, err := pool.Exec(ctx, statement, args...)
		require.NoError(t, err, statement)
	}
	added := func() []string {
		rows, err := pool.Query(ctx, `
			SELECT column_name || ' ' || data_type || COALESCE('(' || numeric_precision || ',' || numeric_scale || ')', '') ||
			       COALESCE('(' || character_maximum_length || ')', '') || ' ' || is_nullable
			FROM information_schema.columns
			WHERE table_name = 'transactions' AND column_name LIKE 'cost\_%'
			ORDER BY column_name COLLATE "C"`)
		require.NoError(t, err)
		defer rows.Close()
		columns := []string{}
		for rows.Next() {
			var column string
			require.NoError(t, rows.Scan(&column))
			columns = append(columns, column)
		}
		require.NoError(t, rows.Err())
		return columns
	}
	wantAdded := []string{
		"cost_currency character(3) YES",
		"cost_excl_vat numeric(12,4) YES",
		"cost_final boolean YES",
		"cost_incl_vat numeric(12,4) YES",
		"cost_rated_at timestamp with time zone YES",
	}
	type row struct {
		currency, exclVat, inclVat *string
		final                      *bool
		ratedAt                    *time.Time
	}
	read := func(transactionId string) row {
		t.Helper()
		var r row
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT cost_currency::text, cost_excl_vat::text, cost_incl_vat::text, cost_final, cost_rated_at
			FROM transactions WHERE charge_station_id = 'k7f2.cs001' AND id = $1`, transactionId).
			Scan(&r.currency, &r.exclVat, &r.inclVat, &r.final, &r.ratedAt))
		return r
	}
	// kept returns what the migration must leave as it is: the transactions with their
	// stop, their tariff, their analysis and their last cost, and the number of their
	// meter values
	kept := func() (transactions []string, meterValues int) {
		t.Helper()
		rows, err := pool.Query(ctx, `
			SELECT id || ' ' || COALESCE(stop_timestamp::text, 'running') || ' ' || COALESCE(meter_stop::text, '-') || ' ' ||
			       COALESCE(tariff_source, '-') || ' ' || COALESCE(idle_s::text, '-') || ' ' || COALESCE(last_cost::text, '-')
			FROM transactions ORDER BY id`)
		require.NoError(t, err)
		defer rows.Close()
		for rows.Next() {
			var transaction string
			require.NoError(t, rows.Scan(&transaction))
			transactions = append(transactions, transaction)
		}
		require.NoError(t, rows.Err())
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM transaction_meter_values`).Scan(&meterValues))
		return transactions, meterValues
	}
	wantKept := []string{"tx-ended 2026-11-28 11:30:00 111000 connector 5400 6.0500", "tx-running running - - - -"}

	// 1. before the migration: an ended transaction with its tariff, its analysis, its
	// last cost and its meter values, and a running one
	migrateTo(t, migrationConnString, versionBefore)
	assert.Empty(t, added())
	exec(`INSERT INTO charge_stations (charge_station_id, security_profile, org_id) VALUES ('k7f2.cs001', 1, $1)`, orgId)
	exec(`INSERT INTO transactions (id, charge_station_id, token_uid, token_type, meter_start, meter_stop, start_timestamp, stop_timestamp,
	                                org_id, tariff_id, tariff_version, tariff_source, idle_s, charging_s, analysed_at, last_cost)
	      VALUES ('tx-ended', 'k7f2.cs001', 'TAG1', 'ISO14443', 100000, 111000, '2026-11-28 09:00:00', '2026-11-28 11:30:00', $1, $2, 1, 'connector',
	              5400, 3600, '2026-11-28 11:30:02Z', 6.05),
	             ('tx-running', 'k7f2.cs001', 'TAG2', 'ISO14443', 111000, NULL, '2026-11-28 12:00:00', NULL, $1, NULL, NULL, NULL,
	              NULL, NULL, NULL, NULL)`, orgId, tariffId)
	exec(`INSERT INTO transaction_meter_values (charge_station_id, transaction_id, timestamp, sampled_values)
	      VALUES ('k7f2.cs001', 'tx-ended', '2026-11-28 09:00:00', '[{"value": 100000}]'),
	             ('k7f2.cs001', 'tx-ended', '2026-11-28 10:00:00', '[{"value": 111000}]'),
	             ('k7f2.cs001', 'tx-running', '2026-11-28 12:00:00', '[{"value": 111000}]')`)

	// 2. up: the transactions that were there have no cost, and keep their last cost
	migrateTo(t, migrationConnString, versionAfter)
	assert.Equal(t, wantAdded, added())
	assert.Equal(t, row{}, read("tx-ended"))
	assert.Equal(t, row{}, read("tx-running"))
	transactions, meterValues := kept()
	assert.Equal(t, wantKept, transactions)
	assert.Equal(t, 3, meterValues)

	// 3. the new columns take values: the amounts with four decimals, the time as an instant
	exec(`UPDATE transactions SET cost_currency = 'EUR', cost_excl_vat = 5.8, cost_incl_vat = 7.02046, cost_final = true,
	                              cost_rated_at = '2026-11-28 12:30:02+01'
	      WHERE id = 'tx-ended'`)
	got := read("tx-ended")
	assert.Equal(t, ptr("EUR"), got.currency)
	assert.Equal(t, ptr("5.8000"), got.exclVat)
	assert.Equal(t, ptr("7.0205"), got.inclVat)
	assert.Equal(t, ptr(true), got.final)
	require.NotNil(t, got.ratedAt)
	assert.True(t, got.ratedAt.Equal(time.Date(2026, 11, 28, 11, 30, 2, 0, time.UTC)), "%s", got.ratedAt)
	for name, statement := range map[string]string{
		"a cost with nine digits before the point": `UPDATE transactions SET cost_excl_vat = 100000000 WHERE id = 'tx-ended'`,
		"a cost that is not a number":              `UPDATE transactions SET cost_incl_vat = 'a lot' WHERE id = 'tx-ended'`,
		"a currency of four letters":               `UPDATE transactions SET cost_currency = 'EURO' WHERE id = 'tx-ended'`,
		"a time that is not a time":                `UPDATE transactions SET cost_rated_at = 'noon' WHERE id = 'tx-ended'`,
	} {
		_, err := pool.Exec(ctx, statement)
		assert.Error(t, err, name)
	}
	exec(`UPDATE transactions SET cost_currency = 'EUR', cost_excl_vat = 99999999.9999, cost_incl_vat = 0, cost_final = false,
	                              cost_rated_at = '2026-11-28 12:30:02Z'
	      WHERE id = 'tx-running'`)
	assert.Equal(t, ptr("99999999.9999"), read("tx-running").exclVat)

	// 4. down, with costs there: only the costs go
	migrateTo(t, migrationConnString, versionBefore)
	assert.Empty(t, added())
	transactions, meterValues = kept()
	assert.Equal(t, wantKept, transactions, "the transactions stay, with their tariff, their analysis and their last cost")
	assert.Equal(t, 3, meterValues)

	// 5. up again: nothing is left of the costs
	migrateTo(t, migrationConnString, versionAfter)
	assert.Equal(t, wantAdded, added())
	assert.Equal(t, row{}, read("tx-ended"))
	assert.Equal(t, row{}, read("tx-running"))
	transactions, meterValues = kept()
	assert.Equal(t, wantKept, transactions)
	assert.Equal(t, 3, meterValues)
}
