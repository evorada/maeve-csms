// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

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
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	types201 "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/rating"
	"github.com/thoughtworks/maeve-csms/manager/services"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/storetest"
)

func TestTransactionAnalysis(t *testing.T) {
	storetest.TestTransactionAnalysis(t, emptyTestStore)
}

func TestTransactionSummary(t *testing.T) {
	storetest.TestTransactionSummary(t, emptyTestStore)
}

// analysisColumns is the analysis of a transaction as it is stored.
type analysisColumns struct {
	idleStartedAt, analysedAt *time.Time
	idleS, chargingS          *int64
	averagePowerW             *string
}

func readAnalysisColumns(t *testing.T, stationKey, transactionId string) analysisColumns {
	t.Helper()
	var c analysisColumns
	require.NoError(t, testPool.QueryRow(context.Background(), `
		SELECT idle_started_at, idle_s, charging_s, average_power_w::text, analysed_at
		FROM transactions WHERE charge_station_id = $1 AND id = $2`, stationKey, transactionId).
		Scan(&c.idleStartedAt, &c.idleS, &c.chargingS, &c.averagePowerW, &c.analysedAt))
	return c
}

// The analysis is stored in the columns of the transaction: the times as instants, the
// average power with one decimal.
func TestSetTransactionAnalysis_StoredShape(t *testing.T) {
	ctx := context.Background()
	emptyTestStore(t)
	const station = "k7f2.cs001"
	require.NoError(t, testStore.CreateTransaction(ctx, station, "tx-1", "TAG1", "ISO14443", nil, 0, false))
	assert.Equal(t, analysisColumns{}, readAnalysisColumns(t, station, "tx-1"), "a transaction has no analysis until it is given one")
	var updatedBefore time.Time
	require.NoError(t, testPool.QueryRow(ctx, `SELECT updated_at FROM transactions WHERE id = 'tx-1'`).Scan(&updatedBefore))

	// the times are given in another zone, to the microsecond that the columns keep
	madrid := time.FixedZone("CET", 3600)
	idleStartedAt := time.Date(2026, 11, 28, 11, 0, 0, 123456000, madrid)
	analysedAt := time.Date(2026, 11, 28, 12, 30, 2, 987654000, madrid)
	require.NoError(t, testStore.SetTransactionAnalysis(ctx, station, "tx-1", store.TransactionAnalysis{
		IdleStartedAt: &idleStartedAt, IdleS: 5400, ChargingS: 3600, AveragePowerW: ptr(8571.428571428572), AnalysedAt: analysedAt,
	}))

	got := readAnalysisColumns(t, station, "tx-1")
	require.NotNil(t, got.idleStartedAt)
	require.NotNil(t, got.analysedAt)
	assert.True(t, got.idleStartedAt.Equal(time.Date(2026, 11, 28, 10, 0, 0, 123456000, time.UTC)), "the instant, to the microsecond: %s", got.idleStartedAt)
	assert.True(t, got.analysedAt.Equal(time.Date(2026, 11, 28, 11, 30, 2, 987654000, time.UTC)), "%s", got.analysedAt)
	assert.Equal(t, ptr(int64(5400)), got.idleS)
	assert.Equal(t, ptr(int64(3600)), got.chargingS)
	assert.Equal(t, ptr("8571.4"), got.averagePowerW)
	var updatedAfter time.Time
	require.NoError(t, testPool.QueryRow(ctx, `SELECT updated_at FROM transactions WHERE id = 'tx-1'`).Scan(&updatedAfter))
	assert.False(t, updatedAfter.Before(updatedBefore))

	// the store reads back what the columns keep, in UTC
	transaction, err := testStore.FindTransaction(ctx, station, "tx-1")
	require.NoError(t, err)
	require.NotNil(t, transaction.Analysis)
	assert.Equal(t, store.TransactionAnalysis{
		IdleStartedAt: ptr(time.Date(2026, 11, 28, 10, 0, 0, 123456000, time.UTC)),
		IdleS:         5400,
		ChargingS:     3600,
		AveragePowerW: ptr(8571.4),
		AnalysedAt:    time.Date(2026, 11, 28, 11, 30, 2, 987654000, time.UTC),
	}, *transaction.Analysis)

	// an analysis without an idle start and an average power clears the columns
	require.NoError(t, testStore.SetTransactionAnalysis(ctx, station, "tx-1", store.TransactionAnalysis{ChargingS: 9000, AnalysedAt: analysedAt}))
	got = readAnalysisColumns(t, station, "tx-1")
	assert.Nil(t, got.idleStartedAt)
	assert.Nil(t, got.averagePowerW)
	assert.Equal(t, ptr(int64(0)), got.idleS)
	assert.Equal(t, ptr(int64(9000)), got.chargingS)

	// the highest power that the column keeps
	require.NoError(t, testStore.SetTransactionAnalysis(ctx, station, "tx-1", store.TransactionAnalysis{
		AveragePowerW: ptr(999_999_999.94), AnalysedAt: analysedAt,
	}))
	assert.Equal(t, ptr("999999999.9"), readAnalysisColumns(t, station, "tx-1").averagePowerW)
}

// The analysis of a transaction is written in a database transaction of the caller. A
// call that is refused there (an analysis that cannot be, or a transaction that is not
// known) sends no statement that fails, so the database transaction stays usable.
func TestSetTransactionAnalysis_JoinsTheTransaction(t *testing.T) {
	ctx := context.Background()
	emptyTestStore(t)
	const station = "k7f2.cs001"
	analysis := store.TransactionAnalysis{IdleS: 60, ChargingS: 3540, AveragePowerW: ptr(7400.0), AnalysedAt: time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)}

	analyse := func(ctx context.Context, transactionId string) {
		require.NoError(t, testStore.CreateTransaction(ctx, station, transactionId, "TAG1", "ISO14443", nil, 0, false))

		refused := analysis
		refused.AveragePowerW = ptr(2e9)
		require.Error(t, testStore.SetTransactionAnalysis(ctx, station, transactionId, refused))
		refused = analysis
		refused.IdleS = -1
		require.Error(t, testStore.SetTransactionAnalysis(ctx, station, transactionId, refused))
		require.ErrorIs(t, testStore.SetTransactionAnalysis(ctx, station, "tx-unknown", analysis), store.ErrNotFound)

		require.NoError(t, testStore.SetTransactionAnalysis(ctx, station, transactionId, analysis))
		got, err := testStore.FindTransaction(ctx, station, transactionId)
		require.NoError(t, err)
		require.NotNil(t, got, "the transaction is still usable, and reads what it wrote")
		require.NotNil(t, got.Analysis)
		require.Equal(t, analysis, *got.Analysis)
	}

	err := testStore.RunInTx(ctx, func(ctx context.Context) error {
		analyse(ctx, "tx-rolled-back")
		return errStopTx
	})
	require.ErrorIs(t, err, errStopTx)
	got, err := testStore.FindTransaction(ctx, station, "tx-rolled-back")
	require.NoError(t, err)
	assert.Nil(t, got, "rolled back with the transaction")

	require.NoError(t, testStore.RunInTx(ctx, func(ctx context.Context) error {
		analyse(ctx, "tx-committed")
		return nil
	}))
	got, err = testStore.FindTransaction(ctx, station, "tx-committed")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Analysis)
	assert.Equal(t, analysis, *got.Analysis)
	requireNoOpenTransaction(t)
}

// The summary of a transaction is read without its meter values: it is there also when a
// meter value of the transaction cannot be read.
func TestFindTransactionSummary_DoesNotReadTheMeterValues(t *testing.T) {
	ctx := context.Background()
	emptyTestStore(t)
	const station = "k7f2.cs001"
	require.NoError(t, testStore.CreateTransaction(ctx, station, "tx-1", "TAG1", "ISO14443", nil, 0, false))
	_, err := testPool.Exec(ctx, `
		INSERT INTO transaction_meter_values (charge_station_id, transaction_id, timestamp, sampled_values)
		VALUES ($1, 'tx-1', '2026-11-28 09:00:00', '"not a list of sampled values"')`, station)
	require.NoError(t, err)

	_, err = testStore.FindTransaction(ctx, station, "tx-1")
	require.Error(t, err, "the read with the meter values fails on it")

	summary, err := testStore.FindTransactionSummary(ctx, station, "tx-1")
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, "TAG1", summary.IdToken)
	assert.Nil(t, summary.MeterValues)

	// inside a database transaction it reads what the transaction wrote
	require.NoError(t, testStore.RunInTx(ctx, func(ctx context.Context) error {
		require.NoError(t, testStore.SetTransactionTariff(ctx, station, "tx-1", nil, nil, "none"))
		summary, err := testStore.FindTransactionSummary(ctx, station, "tx-1")
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.Equal(t, ptr("none"), summary.TariffSource)
		return nil
	}))
	requireNoOpenTransaction(t)
}

// Migration 000048 adds the analysis of a transaction. The transactions that are there
// have none; going down loses only the analyses.
func TestMigration_AddTransactionAnalysis(t *testing.T) {
	ctx := context.Background()

	const (
		versionBefore = 47
		versionAfter  = 48
		orgId         = "0193b2c4-0000-7000-8000-0000000000a1"
		tariffId      = "0193b2c5-0000-7000-8000-000000000021"
	)

	migrationConnString, pool := migrationTestDatabase(t, "maeve_migration_000048")

	exec := func(statement string, args ...any) {
		t.Helper()
		_, err := pool.Exec(ctx, statement, args...)
		require.NoError(t, err, statement)
	}
	added := func() []string {
		rows, err := pool.Query(ctx, `
			SELECT column_name || ' ' || data_type || COALESCE('(' || numeric_precision || ',' || numeric_scale || ')', '') || ' ' || is_nullable
			FROM information_schema.columns
			WHERE table_name = 'transactions' AND column_name IN ('idle_started_at', 'idle_s', 'charging_s', 'average_power_w', 'analysed_at')
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
		"analysed_at timestamp with time zone YES",
		"average_power_w numeric(10,1) YES",
		"charging_s bigint(64,0) YES",
		"idle_s bigint(64,0) YES",
		"idle_started_at timestamp with time zone YES",
	}
	type row struct {
		idleStartedAt, analysedAt *time.Time
		idleS, chargingS          *int64
		averagePowerW             *string
	}
	read := func(transactionId string) row {
		t.Helper()
		var r row
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT idle_started_at, idle_s, charging_s, average_power_w::text, analysed_at
			FROM transactions WHERE charge_station_id = 'k7f2.cs001' AND id = $1`, transactionId).
			Scan(&r.idleStartedAt, &r.idleS, &r.chargingS, &r.averagePowerW, &r.analysedAt))
		return r
	}
	// kept returns what the migration must leave as it is: the transactions with their
	// stop and their tariff, and the number of their meter values
	kept := func() (transactions []string, meterValues int) {
		t.Helper()
		rows, err := pool.Query(ctx, `
			SELECT id || ' ' || COALESCE(stop_timestamp::text, 'running') || ' ' || COALESCE(meter_stop::text, '-') || ' ' || COALESCE(tariff_source, '-')
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
	wantKept := []string{"tx-ended 2026-11-28 11:30:00 111000 connector", "tx-running running - -"}

	// 1. before the migration: an ended transaction with its tariff and its meter values,
	// and a running one
	migrateTo(t, migrationConnString, versionBefore)
	assert.Empty(t, added())
	exec(`INSERT INTO charge_stations (charge_station_id, security_profile, org_id) VALUES ('k7f2.cs001', 1, $1)`, orgId)
	exec(`INSERT INTO transactions (id, charge_station_id, token_uid, token_type, meter_start, meter_stop, start_timestamp, stop_timestamp,
	                                org_id, tariff_id, tariff_version, tariff_source)
	      VALUES ('tx-ended', 'k7f2.cs001', 'TAG1', 'ISO14443', 100000, 111000, '2026-11-28 09:00:00', '2026-11-28 11:30:00', $1, $2, 1, 'connector'),
	             ('tx-running', 'k7f2.cs001', 'TAG2', 'ISO14443', 111000, NULL, '2026-11-28 12:00:00', NULL, $1, NULL, NULL, NULL)`, orgId, tariffId)
	exec(`INSERT INTO transaction_meter_values (charge_station_id, transaction_id, timestamp, sampled_values)
	      VALUES ('k7f2.cs001', 'tx-ended', '2026-11-28 09:00:00', '[{"value": 100000}]'),
	             ('k7f2.cs001', 'tx-ended', '2026-11-28 10:00:00', '[{"value": 111000}]'),
	             ('k7f2.cs001', 'tx-running', '2026-11-28 12:00:00', '[{"value": 111000}]')`)

	// 2. up: the transactions that were there have no analysis
	migrateTo(t, migrationConnString, versionAfter)
	assert.Equal(t, wantAdded, added())
	assert.Equal(t, row{}, read("tx-ended"))
	assert.Equal(t, row{}, read("tx-running"))
	transactions, meterValues := kept()
	assert.Equal(t, wantKept, transactions)
	assert.Equal(t, 3, meterValues)

	// 3. the new columns take values: the times as instants, the power with one decimal
	exec(`UPDATE transactions SET idle_started_at = '2026-11-28 11:00:00+01', idle_s = 5400, charging_s = 3600,
	                              average_power_w = 11000.46, analysed_at = '2026-11-28 11:30:02Z'
	      WHERE id = 'tx-ended'`)
	want := row{
		idleStartedAt: ptr(time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)),
		analysedAt:    ptr(time.Date(2026, 11, 28, 11, 30, 2, 0, time.UTC)),
		idleS:         ptr(int64(5400)),
		chargingS:     ptr(int64(3600)),
		averagePowerW: ptr("11000.5"),
	}
	got := read("tx-ended")
	require.NotNil(t, got.idleStartedAt)
	require.NotNil(t, got.analysedAt)
	assert.True(t, want.idleStartedAt.Equal(*got.idleStartedAt), "%s", got.idleStartedAt)
	assert.True(t, want.analysedAt.Equal(*got.analysedAt), "%s", got.analysedAt)
	assert.Equal(t, want.idleS, got.idleS)
	assert.Equal(t, want.chargingS, got.chargingS)
	assert.Equal(t, want.averagePowerW, got.averagePowerW)
	for name, statement := range map[string]string{
		"an average power with ten digits before the point": `UPDATE transactions SET average_power_w = 1000000000 WHERE id = 'tx-ended'`,
		"an idle time that is not a number":                 `UPDATE transactions SET idle_s = 'long' WHERE id = 'tx-ended'`,
		"a charging time that is not a number":              `UPDATE transactions SET charging_s = 'short' WHERE id = 'tx-ended'`,
		"an idle start that is not a time":                  `UPDATE transactions SET idle_started_at = 'noon' WHERE id = 'tx-ended'`,
	} {
		_, err := pool.Exec(ctx, statement)
		assert.Error(t, err, name)
	}
	exec(`UPDATE transactions SET average_power_w = 999999999.94 WHERE id = 'tx-running'`)
	assert.Equal(t, ptr("999999999.9"), read("tx-running").averagePowerW)

	// 4. down, with analyses there: only the analyses go
	migrateTo(t, migrationConnString, versionBefore)
	assert.Empty(t, added())
	transactions, meterValues = kept()
	assert.Equal(t, wantKept, transactions, "the transactions stay, with their tariff")
	assert.Equal(t, 3, meterValues)

	// 5. up again: nothing is left of the analyses
	migrateTo(t, migrationConnString, versionAfter)
	assert.Equal(t, wantAdded, added())
	assert.Equal(t, row{}, read("tx-ended"))
	assert.Equal(t, row{}, read("tx-running"))
	transactions, meterValues = kept()
	assert.Equal(t, wantKept, transactions)
	assert.Equal(t, 3, meterValues)
}

// The tests below analyse the transactions that the OCPP handlers end, with the tables
// of PostgreSQL: the Analyzer reads the transaction, its meter values, its tariff version
// and the highest power of its connector, and writes the analysis.

var analysisTestStart = time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)

func analysisTestMinute(m int) string {
	return analysisTestStart.Add(time.Duration(m) * time.Minute).Format(time.RFC3339)
}

func newTestAnalyzer(clk *clockTest.FakePassiveClock) *rating.Analyzer {
	return &rating.Analyzer{
		Transactions: testStore, MeterValues: testStore, Assets: testStore, Registry: testStore, Tariffs: testStore,
		Defaults: rating.DefaultIdleSettings(), Clock: clk,
	}
}

// failingTariffVersions is a tariff store that cannot read a tariff version.
type failingTariffVersions struct {
	store.TariffStore
	err error
}

func (s failingTariffVersions) LookupTariffVersion(context.Context, string, int) (*store.TariffVersion, error) {
	return nil, s.err
}

// An OCPP 1.6 transaction is analysed when its StopTransaction is stored: from the meter
// start, the meter values that the station sent on their own, and the meter stop.
func TestIdleAnalysis_StopTransaction(t *testing.T) {
	ctx := context.Background()
	f := newTariffSelectionFixture(t)
	clk := clockTest.NewFakePassiveClock(time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC))
	fixture := eventstest.New(testStore, testOrgA, clk)
	fixture.Events.Emitter = &events.OutboxEmitter{Store: testStore, Orgs: f.orgs, Clock: clk, Product: events.Product}
	startHandler := ocpp16.StartTransactionHandler{
		Clock: clk, TokenStore: testStore, RegistryStore: testStore, OrgResolver: f.orgs, TransactionStore: testStore, ReservationStore: testStore,
		Tx: fixture.Tx, Events: fixture.Events, TariffSelector: f.selector,
	}
	meterValuesHandler := ocpp16.MeterValuesHandler{TransactionStore: testStore, MeterValuesStore: testStore, Tx: fixture.Tx, Events: fixture.Events}
	stopHandler := ocpp16.StopTransactionHandler{
		Clock: clk, TokenStore: testStore, RegistryStore: testStore, OrgResolver: f.orgs, TransactionStore: testStore,
		Tx: fixture.Tx, Events: fixture.Events, Analyzer: newTestAnalyzer(clk),
	}
	// charge starts a transaction with a register of 100 kWh and sends its meter values:
	// the minute of the transaction and the register in Wh, in pairs
	charge := func(connectorId int, minutesAndWh ...int) int {
		t.Helper()
		resp, err := startHandler.HandleCall(ctx, tariffSelectionStation, &types.StartTransactionJson{
			ConnectorId: connectorId, IdTag: "RFID0001", MeterStart: 100_000, Timestamp: analysisTestMinute(0),
		})
		require.NoError(t, err)
		transactionId := resp.(*types.StartTransactionResponseJson).TransactionId
		for i := 0; i+1 < len(minutesAndWh); i += 2 {
			wh := types.MeterValuesJsonMeterValueElemSampledValueElemUnitWh
			_, err := meterValuesHandler.HandleCall(ctx, tariffSelectionStation, &types.MeterValuesJson{
				ConnectorId: connectorId, TransactionId: &transactionId,
				MeterValue: []types.MeterValuesJsonMeterValueElem{{
					Timestamp:    analysisTestMinute(minutesAndWh[i]),
					SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{{Value: strconv.Itoa(minutesAndWh[i+1]), Unit: &wh}},
				}},
			})
			require.NoError(t, err)
		}
		return transactionId
	}
	stop := func(transactionId, minute, meterStop int) string {
		t.Helper()
		resp, err := stopHandler.HandleCall(ctx, tariffSelectionStation, &types.StopTransactionJson{
			TransactionId: transactionId, MeterStop: meterStop, Timestamp: analysisTestMinute(minute),
		})
		require.NoError(t, err)
		require.Equal(t, &types.StopTransactionResponseJson{}, resp)
		return ocpp16.ConvertToUUID(transactionId)
	}
	idleSince := func(transactionId string) time.Time {
		t.Helper()
		columns := readAnalysisColumns(t, tariffSelectionStation, transactionId)
		require.NotNil(t, columns.idleStartedAt)
		return columns.idleStartedAt.UTC()
	}
	afterAnHour, afterTwoHours := analysisTestStart.Add(time.Hour), analysisTestStart.Add(2*time.Hour)

	// 11 kWh in the first hour, then plugged in for an hour and a half
	running := charge(1, 30, 105_500, 60, 111_000, 120, 111_000)
	assert.Equal(t, analysisColumns{}, readAnalysisColumns(t, tariffSelectionStation, ocpp16.ConvertToUUID(running)))
	first := stop(running, 150, 111_000)

	got := readAnalysisColumns(t, tariffSelectionStation, first)
	require.NotNil(t, got.idleStartedAt)
	require.NotNil(t, got.analysedAt)
	assert.True(t, afterAnHour.Equal(*got.idleStartedAt), "%s", got.idleStartedAt)
	assert.True(t, clk.Now().Equal(*got.analysedAt), "%s", got.analysedAt)
	assert.Equal(t, ptr(int64(5400)), got.idleS)
	assert.Equal(t, ptr(int64(3600)), got.chargingS)
	assert.Equal(t, ptr("11000.0"), got.averagePowerW)
	stored, err := testStore.FindTransaction(ctx, tariffSelectionStation, first)
	require.NoError(t, err)
	assert.Equal(t, &store.TransactionAnalysis{
		IdleStartedAt: &afterAnHour, IdleS: 5400, ChargingS: 3600, AveragePowerW: ptr(11_000.0), AnalysedAt: clk.Now().UTC(),
	}, stored.Analysis)
	assert.Equal(t, testOrgA, stored.OrgId)
	evs := fixture.Take(t)
	assert.Equal(t, events.TypeSessionCompleted, evs[len(evs)-1].Type)
	assert.Zero(t, fixture.Events.OutsideTx())

	// an hour at 11 kW and an hour at 1 kW: charging with the settings of the manager
	assert.Equal(t, afterTwoHours, idleSince(stop(charge(1, 60, 111_000), 120, 112_000)))

	// the highest power of the connector: 1 kW is below 1 % of the 150 kW of connector 2,
	// once the OCPP version of the charge station is known
	connector, err := testStore.LookupAssetConnectorByOcpp(ctx, tariffSelectionStation, 2, 1)
	require.NoError(t, err)
	connector.MaxElectricPower = ptr(int32(150_000))
	require.NoError(t, testStore.UpdateAssetConnector(ctx, connector))
	assert.Equal(t, afterTwoHours, idleSince(stop(charge(2, 60, 111_000), 120, 112_000)))
	require.NoError(t, testStore.SetChargeStationRuntimeDetails(ctx, tariffSelectionStation, &store.ChargeStationRuntimeDetails{OcppVersion: "1.6"}))
	assert.Equal(t, afterAnHour, idleSince(stop(charge(2, 60, 111_000), 120, 112_000)))

	// the idle settings of the tariff version that the transaction started with
	v := tariffTestVersion(3000)
	v.IdleDetection = &store.IdleDetection{MinChargingPowerW: ptr(1500.0)}
	require.NoError(t, testStore.AddTariffVersion(ctx, f.regular.Id, 1, v))
	withSettings := charge(1, 60, 111_000)
	require.NoError(t, testStore.AddTariffVersion(ctx, f.regular.Id, 2, tariffTestVersion(3500)))
	assert.Equal(t, afterAnHour, idleSince(stop(withSettings, 120, 112_000)), "1 kW is below the 1.5 kW of version 2")
	assert.Equal(t, afterTwoHours, idleSince(stop(charge(1, 60, 111_000), 120, 112_000)), "version 3 has no idle settings")
	requireNoOpenTransaction(t)

	// an analysis that fails leaves the stop as it is stored
	stopHandler.Analyzer.Tariffs = failingTariffVersions{TariffStore: testStore, err: errors.New("the tariffs cannot be read")}
	unanalysed := stop(charge(1, 60, 111_000), 120, 112_000)
	assert.Equal(t, analysisColumns{}, readAnalysisColumns(t, tariffSelectionStation, unanalysed))
	stored, err = testStore.FindTransaction(ctx, tariffSelectionStation, unanalysed)
	require.NoError(t, err)
	assert.Equal(t, ptr(analysisTestStart.Add(2*time.Hour)), stored.StoppedAt)
	assert.Equal(t, ptr(int64(12_000)), stored.EnergyWh)
	assert.Nil(t, stored.Analysis)
	requireNoOpenTransaction(t)

	// it can be analysed later
	stopHandler.Analyzer.Tariffs = testStore
	analysis, err := stopHandler.Analyzer.AnalyseAndStore(ctx, tariffSelectionStation, unanalysed)
	require.NoError(t, err)
	require.NotNil(t, analysis)
	assert.Equal(t, afterTwoHours, idleSince(unanalysed))
}

// An OCPP 2.0.1 transaction is analysed when its Ended event is stored: from the meter
// values of its events.
func TestIdleAnalysis_TransactionEvent(t *testing.T) {
	ctx := context.Background()
	f := newTariffSelectionFixture(t)
	clk := clockTest.NewFakePassiveClock(time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC))
	handler := ocpp201.TransactionEventHandler{
		Store:            testStore,
		TokenAuthService: &services.OcppTokenAuthService{Clock: clk, TokenStore: testStore, OrgResolver: f.orgs, Registry: testStore},
		TariffService:    services.BasicKwhTariffService{},
		TariffSelector:   f.selector,
		OrgResolver:      f.orgs,
		Clock:            clk,
		Analyzer:         newTestAnalyzer(clk),
	}
	connector := 1
	// event sends an event of a transaction with a reading of the register in Wh at the
	// minute of the transaction
	event := func(eventType types201.TransactionEventEnumType, transactionId string, seqNo, minute int, registerWh float64) {
		t.Helper()
		sampled := types201.SampledValueType{Value: registerWh}
		switch eventType {
		case types201.TransactionEventEnumTypeStarted:
			sampled.Context = ptr(types201.ReadingContextEnumTypeTransactionBegin)
			sampled.Measurand = ptr(types201.MeasurandEnumTypeEnergyActiveImportRegister)
		case types201.TransactionEventEnumTypeEnded:
			sampled.Context = ptr(types201.ReadingContextEnumTypeTransactionEnd)
			sampled.Measurand = ptr(types201.MeasurandEnumTypeEnergyActiveImportRegister)
			sampled.Location = ptr(types201.LocationEnumTypeOutlet)
		}
		_, err := handler.HandleCall(ctx, tariffSelectionStation, &types201.TransactionEventRequestJson{
			EventType: eventType, TriggerReason: types201.TriggerReasonEnumTypeMeterValuePeriodic, Timestamp: analysisTestMinute(minute),
			SeqNo: seqNo, Evse: &types201.EVSEType{Id: 1, ConnectorId: &connector},
			TransactionInfo: types201.TransactionType{TransactionId: transactionId},
			MeterValue:      []types201.MeterValueType{{Timestamp: analysisTestMinute(minute), SampledValue: []types201.SampledValueType{sampled}}},
		})
		require.NoError(t, err)
	}

	// 11 kWh in the first hour, then plugged in for an hour and a half
	event(types201.TransactionEventEnumTypeStarted, "tx-1", 0, 0, 100_000)
	event(types201.TransactionEventEnumTypeUpdated, "tx-1", 1, 30, 105_500)
	event(types201.TransactionEventEnumTypeUpdated, "tx-1", 2, 60, 111_000)
	event(types201.TransactionEventEnumTypeUpdated, "tx-1", 3, 120, 111_000)
	assert.Equal(t, analysisColumns{}, readAnalysisColumns(t, tariffSelectionStation, "tx-1"), "a transaction that runs has no analysis")
	event(types201.TransactionEventEnumTypeEnded, "tx-1", 4, 150, 111_000)

	got := readAnalysisColumns(t, tariffSelectionStation, "tx-1")
	require.NotNil(t, got.idleStartedAt)
	require.NotNil(t, got.analysedAt)
	assert.True(t, analysisTestStart.Add(time.Hour).Equal(*got.idleStartedAt), "%s", got.idleStartedAt)
	assert.True(t, clk.Now().Equal(*got.analysedAt), "%s", got.analysedAt)
	assert.Equal(t, ptr(int64(5400)), got.idleS)
	assert.Equal(t, ptr(int64(3600)), got.chargingS)
	assert.Equal(t, ptr("11000.0"), got.averagePowerW)
	tariffId, version, source := tariffColumns(t, "tx-1")
	assert.Equal(t, &f.regular.Id, tariffId)
	assert.Equal(t, ptr(1), version)
	assert.Equal(t, ptr("connector"), source)

	// the idle settings of the tariff version of the transaction: 1 kW in the second hour
	// is below the 1.5 kW of version 2
	v := tariffTestVersion(3000)
	v.IdleDetection = &store.IdleDetection{MinChargingPowerW: ptr(1500.0)}
	require.NoError(t, testStore.AddTariffVersion(ctx, f.regular.Id, 1, v))
	event(types201.TransactionEventEnumTypeStarted, "tx-2", 0, 0, 100_000)
	event(types201.TransactionEventEnumTypeUpdated, "tx-2", 1, 60, 111_000)
	event(types201.TransactionEventEnumTypeEnded, "tx-2", 2, 120, 112_000)
	got = readAnalysisColumns(t, tariffSelectionStation, "tx-2")
	require.NotNil(t, got.idleStartedAt)
	assert.True(t, analysisTestStart.Add(time.Hour).Equal(*got.idleStartedAt), "%s", got.idleStartedAt)
	assert.Equal(t, ptr(int64(3600)), got.idleS)
	assert.Equal(t, ptr("11000.0"), got.averagePowerW, "the average of what the tariff calls charging")
	requireNoOpenTransaction(t)

	// an analysis that fails leaves the event answered and the end stored
	handler.Analyzer.Tariffs = failingTariffVersions{TariffStore: testStore, err: errors.New("the tariffs cannot be read")}
	event(types201.TransactionEventEnumTypeStarted, "tx-3", 0, 0, 100_000)
	event(types201.TransactionEventEnumTypeEnded, "tx-3", 1, 60, 111_000)
	assert.Equal(t, analysisColumns{}, readAnalysisColumns(t, tariffSelectionStation, "tx-3"))
	stored, err := testStore.FindTransaction(ctx, tariffSelectionStation, "tx-3")
	require.NoError(t, err)
	assert.Equal(t, ptr(analysisTestStart.Add(time.Hour)), stored.StoppedAt)
	requireNoOpenTransaction(t)
}
