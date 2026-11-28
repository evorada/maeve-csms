// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/postgres"
)

// migrationTestDatabase creates an empty database next to the one used by the other
// integration tests and returns its connection string and a pool connected to it.
func migrationTestDatabase(t *testing.T, name string) (string, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	_, err := testPool.Exec(ctx, `DROP DATABASE IF EXISTS `+name)
	require.NoError(t, err)
	_, err = testPool.Exec(ctx, `CREATE DATABASE `+name)
	require.NoError(t, err)

	u, err := url.Parse(connString)
	require.NoError(t, err)
	u.Path = "/" + name
	migrationConnString := u.String()

	pool, err := pgxpool.New(ctx, migrationConnString)
	require.NoError(t, err)

	t.Cleanup(func() {
		pool.Close()
		_, err := testPool.Exec(context.Background(), `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
		assert.NoError(t, err)
	})

	return migrationConnString, pool
}

// migrateTo migrates the database to the given version (up or down).
func migrateTo(t *testing.T, migrationConnString string, version uint) {
	t.Helper()

	m, err := migrate.New("file://./migrations", migrationConnString)
	require.NoError(t, err)
	defer func() {
		sourceErr, dbErr := m.Close()
		require.NoError(t, sourceErr)
		require.NoError(t, dbErr)
	}()

	err = m.Migrate(version)
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		require.NoError(t, err)
	}
}

// migrateUp applies every migration.
func migrateUp(t *testing.T, migrationConnString string) {
	t.Helper()

	m, err := migrate.New("file://./migrations", migrationConnString)
	require.NoError(t, err)
	defer func() {
		sourceErr, dbErr := m.Close()
		require.NoError(t, sourceErr)
		require.NoError(t, dbErr)
	}()

	err = m.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		require.NoError(t, err)
	}
}

// Migration 000031 changes the identity of a transaction from (id) to
// (charge_station_id, id). Transactions and meter values stored before the migration
// must still belong together afterwards, and the migration must be reversible.
func TestMigration_EnrichTransactions_KeepsExistingData(t *testing.T) {
	ctx := context.Background()

	const (
		versionBefore = 30
		versionAfter  = 31
	)

	migrationConnString, pool := migrationTestDatabase(t, "maeve_migration_000031")

	primaryKeyColumns := func(table string) []string {
		rows, err := pool.Query(ctx, `
			SELECT a.attname
			FROM pg_index i
			JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
			WHERE i.indrelid = $1::regclass AND i.indisprimary
			ORDER BY array_position(i.indkey, a.attnum)`, table)
		require.NoError(t, err)
		defer rows.Close()
		var columns []string
		for rows.Next() {
			var column string
			require.NoError(t, rows.Scan(&column))
			columns = append(columns, column)
		}
		require.NoError(t, rows.Err())
		return columns
	}

	columnType := func(table, column string) (dataType string, nullable string) {
		err := pool.QueryRow(ctx, `
			SELECT data_type, is_nullable FROM information_schema.columns
			WHERE table_name = $1 AND column_name = $2`, table, column).Scan(&dataType, &nullable)
		require.NoError(t, err)
		return dataType, nullable
	}

	hasColumn := func(table, column string) bool {
		var count int
		err := pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM information_schema.columns
			WHERE table_name = $1 AND column_name = $2`, table, column).Scan(&count)
		require.NoError(t, err)
		return count == 1
	}

	meterValueStations := func() map[string][]string {
		// the cast keeps the result type of the query the same when a later migration
		// changes the width of the column
		rows, err := pool.Query(ctx, `
			SELECT transaction_id, charge_station_id::text FROM transaction_meter_values ORDER BY transaction_id, timestamp`)
		require.NoError(t, err)
		defer rows.Close()
		stations := make(map[string][]string)
		for rows.Next() {
			var transactionId, chargeStationId string
			require.NoError(t, rows.Scan(&transactionId, &chargeStationId))
			stations[transactionId] = append(stations[transactionId], chargeStationId)
		}
		require.NoError(t, rows.Err())
		return stations
	}

	// 1. the schema before the migration, with two transactions and their meter values
	migrateTo(t, migrationConnString, versionBefore)
	require.Equal(t, []string{"id"}, primaryKeyColumns("transactions"))
	require.False(t, hasColumn("transaction_meter_values", "charge_station_id"))

	_, err := pool.Exec(ctx, `
		INSERT INTO transactions (id, charge_station_id, token_uid, token_type, meter_start, start_timestamp)
		VALUES ('tx-a', 'cs001', 'TOKEN001', 'RFID', 1000, '2026-11-26 10:00:00'),
		       ('tx-b', 'cs002', 'TOKEN002', 'RFID', 0, '2026-11-26 10:05:00')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO transaction_meter_values (transaction_id, timestamp, sampled_values)
		VALUES ('tx-a', '2026-11-26 10:00:00', '[{"Measurand": "Energy.Active.Import.Register", "Value": 1000}]'),
		       ('tx-a', '2026-11-26 10:15:00', '[{"Measurand": "Energy.Active.Import.Register", "Value": 4000}]'),
		       ('tx-b', '2026-11-26 10:05:00', '[{"Measurand": "Energy.Active.Import.Register", "Value": 0}]')`)
	require.NoError(t, err)

	// 2. up: per-station identity, the meter values carry the station of their transaction
	migrateTo(t, migrationConnString, versionAfter)
	assert.Equal(t, []string{"charge_station_id", "id"}, primaryKeyColumns("transactions"))
	assert.Equal(t, map[string][]string{
		"tx-a": {"cs001", "cs001"},
		"tx-b": {"cs002"},
	}, meterValueStations())

	dataType, nullable := columnType("transactions", "meter_start")
	assert.Equal(t, "bigint", dataType)
	assert.Equal(t, "YES", nullable)
	dataType, _ = columnType("transactions", "meter_stop")
	assert.Equal(t, "bigint", dataType)

	// the same transaction id can now be used by another charge station
	_, err = pool.Exec(ctx, `
		INSERT INTO transactions (id, charge_station_id, token_uid, token_type, start_timestamp)
		VALUES ('tx-a', 'cs003', 'TOKEN003', 'RFID', '2026-11-26 11:00:00')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM transactions WHERE charge_station_id = 'cs003'`)
	require.NoError(t, err)

	// the OCPP 1.6 transaction id allocator starts at 1
	var nextId int
	require.NoError(t, pool.QueryRow(ctx, `SELECT nextval('ocpp16_transaction_id_seq')`).Scan(&nextId))
	assert.Equal(t, 1, nextId)

	// 3. down: the old identity is back and no data is lost
	migrateTo(t, migrationConnString, versionBefore)
	assert.Equal(t, []string{"id"}, primaryKeyColumns("transactions"))
	assert.False(t, hasColumn("transaction_meter_values", "charge_station_id"))
	assert.False(t, hasColumn("transactions", "energy_wh"))
	dataType, nullable = columnType("transactions", "meter_start")
	assert.Equal(t, "integer", dataType)
	assert.Equal(t, "NO", nullable)

	var transactions, meterValues int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM transactions`).Scan(&transactions))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM transaction_meter_values`).Scan(&meterValues))
	assert.Equal(t, 2, transactions)
	assert.Equal(t, 3, meterValues)

	// 4. up again, to the latest version: the store reads the migrated data
	migrateUp(t, migrationConnString)
	assert.Equal(t, []string{"charge_station_id", "id"}, primaryKeyColumns("transactions"))
	assert.Equal(t, map[string][]string{
		"tx-a": {"cs001", "cs001"},
		"tx-b": {"cs002"},
	}, meterValueStations())

	migratedStore, err := postgres.NewStore(ctx, migrationConnString)
	require.NoError(t, err)
	defer migratedStore.Close()

	got, err := migratedStore.FindTransaction(ctx, "cs001", "tx-a")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "TOKEN001", got.IdToken)
	require.Len(t, got.MeterValues, 2)
	assert.Equal(t, 1000.0, got.MeterValues[0].SampledValues[0].Value)
	assert.Equal(t, 4000.0, got.MeterValues[1].SampledValues[0].Value)
	require.NotNil(t, got.MeterStartWh)
	assert.Equal(t, int64(1000), *got.MeterStartWh)
	require.NotNil(t, got.StartedAt)
	assert.Equal(t, "2026-11-26T10:00:00Z", got.StartedAt.Format("2006-01-02T15:04:05Z07:00"))

	got, err = migratedStore.FindTransaction(ctx, "cs002", "tx-b")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got.MeterValues, 1)

	// a transaction id of another charge station is not found
	got, err = migratedStore.FindTransaction(ctx, "cs002", "tx-a")
	require.NoError(t, err)
	assert.Nil(t, got)
}

// Migration 000032 adds the expiry time of a token. Tokens stored before the migration
// do not expire, and the migration must be reversible with tokens in the table.
func TestMigration_AddTokenExpiresAt_KeepsExistingTokens(t *testing.T) {
	ctx := context.Background()

	const (
		versionBefore = 31
		versionAfter  = 32
	)

	migrationConnString, pool := migrationTestDatabase(t, "maeve_migration_000032")

	hasExpiresAt := func() bool {
		var exists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'tokens' AND column_name = 'expires_at' AND data_type = 'timestamp with time zone'
			)`).Scan(&exists)
		require.NoError(t, err)
		return exists
	}
	tokenUids := func() []string {
		rows, err := pool.Query(ctx, `SELECT uid FROM tokens ORDER BY uid`)
		require.NoError(t, err)
		defer rows.Close()
		uids := []string{}
		for rows.Next() {
			var uid string
			require.NoError(t, rows.Scan(&uid))
			uids = append(uids, uid)
		}
		require.NoError(t, rows.Err())
		return uids
	}

	migrateTo(t, migrationConnString, versionBefore)
	require.False(t, hasExpiresAt())
	_, err := pool.Exec(ctx, `
		INSERT INTO tokens (country_code, party_id, type, uid, contract_id, issuer, valid, cache_mode, last_updated)
		VALUES ('GB', 'TWK', 'RFID', 'OLDTOKEN', 'GBTWK001', 'TestIssuer', true, 'ALWAYS', NOW())`)
	require.NoError(t, err)

	// up: the token that was there does not expire
	migrateTo(t, migrationConnString, versionAfter)
	require.True(t, hasExpiresAt())
	var expires bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT expires_at IS NOT NULL FROM tokens WHERE uid = 'OLDTOKEN'`).Scan(&expires))
	assert.False(t, expires)

	_, err = pool.Exec(ctx, `
		INSERT INTO tokens (country_code, party_id, type, uid, contract_id, issuer, valid, cache_mode, last_updated, expires_at)
		VALUES ('GB', 'TWK', 'RFID', 'NEWTOKEN', 'GBTWK002', 'TestIssuer', true, 'ALWAYS', NOW(), '2027-01-31T08:30:00Z')`)
	require.NoError(t, err)

	// down: both tokens stay, without their expiry time
	migrateTo(t, migrationConnString, versionBefore)
	require.False(t, hasExpiresAt())
	assert.Equal(t, []string{"NEWTOKEN", "OLDTOKEN"}, tokenUids())

	// up again with tokens in the table
	migrateUp(t, migrationConnString)
	require.True(t, hasExpiresAt())
	assert.Equal(t, []string{"NEWTOKEN", "OLDTOKEN"}, tokenUids())
	var expiring int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM tokens WHERE expires_at IS NOT NULL`).Scan(&expiring))
	assert.Equal(t, 0, expiring)
}

// Migration 000033 adds the category of a charge station event. Events stored before
// the migration are device events, and the migration must be reversible with events
// in the table.
func TestMigration_AddChargeStationEventCategory_KeepsExistingEvents(t *testing.T) {
	ctx := context.Background()

	const (
		versionBefore = 32
		versionAfter  = 33
	)

	migrationConnString, pool := migrationTestDatabase(t, "maeve_migration_000033")

	hasCategory := func() bool {
		var exists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'charge_station_event' AND column_name = 'category'
			)`).Scan(&exists)
		require.NoError(t, err)
		return exists
	}
	hasCategoryIndex := func() bool {
		var exists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes
				WHERE tablename = 'charge_station_event' AND indexname = 'idx_cs_event_category'
			)`).Scan(&exists)
		require.NoError(t, err)
		return exists
	}
	categories := func() map[string]string {
		rows, err := pool.Query(ctx, `SELECT event_type, category FROM charge_station_event`)
		require.NoError(t, err)
		defer rows.Close()
		got := map[string]string{}
		for rows.Next() {
			var eventType, category string
			require.NoError(t, rows.Scan(&eventType, &category))
			got[eventType] = category
		}
		require.NoError(t, rows.Err())
		return got
	}

	migrateTo(t, migrationConnString, versionBefore)
	require.False(t, hasCategory())
	_, err := pool.Exec(ctx, `INSERT INTO charge_stations (charge_station_id, security_profile) VALUES ('cs001', 0)`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO charge_station_event (charge_station_id, timestamp, event_type)
		VALUES ('cs001', '2026-11-27T08:30:00Z', 'OldEvent')`)
	require.NoError(t, err)

	// up: the event that was there is a device event
	migrateTo(t, migrationConnString, versionAfter)
	require.True(t, hasCategory())
	require.True(t, hasCategoryIndex())
	assert.Equal(t, map[string]string{"OldEvent": "device"}, categories())

	_, err = pool.Exec(ctx, `
		INSERT INTO charge_station_event (charge_station_id, timestamp, event_type, category)
		VALUES ('cs001', '2026-11-27T08:31:00Z', 'SecurityLogWasCleared', 'security')`)
	require.NoError(t, err)

	// down: both events stay, without their category
	migrateTo(t, migrationConnString, versionBefore)
	require.False(t, hasCategory())
	require.False(t, hasCategoryIndex())
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM charge_station_event`).Scan(&count))
	assert.Equal(t, 2, count)

	// up again with events in the table
	migrateUp(t, migrationConnString)
	require.True(t, hasCategory())
	require.True(t, hasCategoryIndex())
	assert.Equal(t, map[string]string{"OldEvent": "device", "SecurityLogWasCleared": "device"}, categories())
}

// Migration 000034 creates the tenants and their hostnames, with the path of a tenant
// as an ltree. It must apply on a database at the previous version, be reversible with
// tenants in the tables, and apply again while the ltree extension is still there.
func TestMigration_CreateTenants(t *testing.T) {
	ctx := context.Background()

	const (
		versionBefore = 33
		versionAfter  = 34
	)

	migrationConnString, pool := migrationTestDatabase(t, "maeve_migration_000034")

	hasTable := func(table string) bool {
		var exists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = 'public' AND table_name = $1
			)`, table).Scan(&exists)
		require.NoError(t, err)
		return exists
	}
	hasLtree := func() bool {
		var exists bool
		err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'ltree')`).Scan(&exists)
		require.NoError(t, err)
		return exists
	}
	insertTenants := func() {
		_, err := pool.Exec(ctx, `
			INSERT INTO tenants (org_id, parent_id, path, slug, org_key)
			VALUES ('0193b2c4-0000-7000-8000-00000000000b', '0193b2c4-0000-7000-8000-00000000000a',
			        '0193b2c400007000800000000000000a.0193b2c400007000800000000000000b', 'partner', 'part01'),
			       ('0193b2c4-0000-7000-8000-00000000000c', '0193b2c4-0000-7000-8000-00000000000b',
			        '0193b2c400007000800000000000000a.0193b2c400007000800000000000000b.0193b2c400007000800000000000000c', 'child', 'chld01')`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `
			INSERT INTO tenant_hostnames (hostname, org_id)
			VALUES ('partner.ocpp.example.com', '0193b2c4-0000-7000-8000-00000000000b')`)
		require.NoError(t, err)
	}

	migrateTo(t, migrationConnString, versionBefore)
	require.False(t, hasTable("tenants"))
	require.False(t, hasTable("tenant_hostnames"))
	require.False(t, hasLtree())

	// up: the tables are there, with their defaults, and paths can be searched by label
	migrateTo(t, migrationConnString, versionAfter)
	require.True(t, hasTable("tenants"))
	require.True(t, hasTable("tenant_hostnames"))
	insertTenants()

	var status, parties, entitlements, config string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, parties::text, entitlements::text, config::text FROM tenants WHERE slug = 'child'`).
		Scan(&status, &parties, &entitlements, &config))
	assert.Equal(t, "active", status)
	assert.Equal(t, "[]", parties)
	assert.Equal(t, "{}", entitlements)
	assert.Equal(t, "{}", config)

	var underPartner int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM tenants WHERE path ~ '*.0193b2c400007000800000000000000b.*'::lquery`).Scan(&underPartner))
	assert.Equal(t, 2, underPartner)

	_, err := pool.Exec(ctx, `UPDATE tenants SET status = 'deleted' WHERE slug = 'child'`)
	assert.Error(t, err, "status outside of active, suspended, cancelled")
	_, err = pool.Exec(ctx, `
		INSERT INTO tenant_hostnames (hostname, org_id)
		VALUES ('partner.ocpp.example.com', '0193b2c4-0000-7000-8000-00000000000c')`)
	assert.Error(t, err, "a hostname belongs to one tenant")

	// down: the tables go, with their rows; the extension stays
	migrateTo(t, migrationConnString, versionBefore)
	require.False(t, hasTable("tenants"))
	require.False(t, hasTable("tenant_hostnames"))
	assert.True(t, hasLtree())

	// up again, with the extension already installed
	migrateUp(t, migrationConnString)
	require.True(t, hasTable("tenants"))
	require.True(t, hasTable("tenant_hostnames"))
	insertTenants()

	// the hostnames of a tenant go with the tenant
	_, err = pool.Exec(ctx, `DELETE FROM tenants WHERE slug = 'partner'`)
	require.NoError(t, err)
	var hostnames int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM tenant_hostnames`).Scan(&hostnames))
	assert.Equal(t, 0, hostnames)
}

// Migration 000035 widens the charge station id columns to 80 characters and adds the
// org of a row to eight tables. The rows that are there before the migration belong to
// the legacy org, a new row must name its org, and the migration is reversible as long
// as no station key is longer than the old width.
func TestMigration_TenancyOrgColumns(t *testing.T) {
	ctx := context.Background()

	const (
		versionBefore = 34
		versionAfter  = 35
		legacyOrgId   = "00000000-0000-0000-0000-000000000000"
		otherOrgId    = "0193b2c4-0000-7000-8000-0000000000a1"
	)

	migrationConnString, pool := migrationTestDatabase(t, "maeve_migration_000035")

	// the widths before the migration
	oldWidths := map[string]int{
		"charge_stations":                      48,
		"charge_station_settings":              48,
		"charge_station_runtime":               48,
		"charge_station_certificates":          48,
		"charge_station_triggers":              48,
		"transactions":                         48,
		"transaction_meter_values":             48,
		"variable_monitoring":                  48,
		"charge_station_event":                 48,
		"device_report":                        48,
		"charge_station_status":                36,
		"connector_status":                     36,
		"remote_start_transaction_requests":    28,
		"remote_stop_transaction_requests":     28,
		"charge_station_certificate_queries":   28,
		"charge_station_certificate_deletions": 28,
	}
	newWidths := map[string]int{}
	for table := range oldWidths {
		newWidths[table] = 80
	}
	orgTables := []string{
		"charge_stations", "locations", "tokens", "transactions",
		"reservations", "charging_profiles", "ocpi_registrations", "ocpi_parties",
	}
	orgIndexes := []string{
		"idx_charge_stations_org", "idx_locations_org", "idx_transactions_org",
		"idx_reservations_org", "idx_charging_profiles_org",
	}

	// stationIdWidths returns the width of every charge_station_id column that has one
	// (the TEXT columns have none).
	stationIdWidths := func() map[string]int {
		rows, err := pool.Query(ctx, `
			SELECT table_name, character_maximum_length FROM information_schema.columns
			WHERE table_schema = 'public' AND column_name = 'charge_station_id'
			  AND character_maximum_length IS NOT NULL AND character_maximum_length < 255`)
		require.NoError(t, err)
		defer rows.Close()
		widths := map[string]int{}
		for rows.Next() {
			var table string
			var width int
			require.NoError(t, rows.Scan(&table, &width))
			widths[table] = width
		}
		require.NoError(t, rows.Err())
		return widths
	}
	// orgColumns returns the tables that have an org_id column of type UUID that is
	// NOT NULL and has no default.
	orgColumns := func() []string {
		rows, err := pool.Query(ctx, `
			SELECT table_name FROM information_schema.columns
			WHERE table_schema = 'public' AND column_name = 'org_id' AND table_name <> 'tenants'
			  AND table_name <> 'tenant_hostnames'
			  AND data_type = 'uuid' AND is_nullable = 'NO' AND column_default IS NULL`)
		require.NoError(t, err)
		defer rows.Close()
		tables := []string{}
		for rows.Next() {
			var table string
			require.NoError(t, rows.Scan(&table))
			tables = append(tables, table)
		}
		require.NoError(t, rows.Err())
		return tables
	}
	orgIndexCount := func() int {
		var count int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM pg_indexes WHERE indexname = ANY($1)`, orgIndexes).Scan(&count))
		return count
	}
	// rowsByOrg returns how many rows of a table each org owns.
	rowsByOrg := func(table string) map[string]int {
		rows, err := pool.Query(ctx, `SELECT org_id::text, COUNT(*) FROM `+table+` GROUP BY org_id`)
		require.NoError(t, err)
		defer rows.Close()
		counts := map[string]int{}
		for rows.Next() {
			var orgId string
			var count int
			require.NoError(t, rows.Scan(&orgId, &count))
			counts[orgId] = count
		}
		require.NoError(t, rows.Err())
		return counts
	}
	rowCount := func(table string) int {
		var count int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count))
		return count
	}

	migrateTo(t, migrationConnString, versionBefore)
	require.Equal(t, oldWidths, stationIdWidths())
	require.Empty(t, orgColumns())

	// one row in each table that gets an org, and in tables of each old width, with
	// the foreign keys to the charge station and to the transaction in use
	for _, statement := range []string{
		`INSERT INTO charge_stations (charge_station_id, security_profile) VALUES ('cs001', 0)`,
		`INSERT INTO charge_station_settings (charge_station_id, settings) VALUES ('cs001', '{}')`,
		`INSERT INTO charge_station_event (charge_station_id, timestamp, event_type) VALUES ('cs001', '2026-11-27T08:30:00Z', 'OldEvent')`,
		`INSERT INTO transactions (id, charge_station_id, token_uid, token_type, start_timestamp)
		 VALUES ('tx001', 'cs001', 'OLDTOKEN', 'ISO14443', '2026-11-27T08:30:00')`,
		`INSERT INTO transaction_meter_values (charge_station_id, transaction_id, timestamp, sampled_values)
		 VALUES ('cs001', 'tx001', '2026-11-27T08:31:00', '[]')`,
		`INSERT INTO charge_station_status (charge_station_id) VALUES ('cs001')`,
		`INSERT INTO remote_stop_transaction_requests (charge_station_id, transaction_id, status, request_type)
		 VALUES ('cs001', 'tx001', 'Pending', 'Stop')`,
		`INSERT INTO locations (id, country_code, party_id, location_data) VALUES ('loc001', 'BE', 'EVO', '{}')`,
		`INSERT INTO tokens (country_code, party_id, type, uid, contract_id, issuer, valid, cache_mode, last_updated)
		 VALUES ('GB', 'TWK', 'RFID', 'OLDTOKEN', 'GBTWK001', 'TestIssuer', true, 'ALWAYS', NOW())`,
		`INSERT INTO reservations (reservation_id, charge_station_id, connector_id, id_tag, expiry_date)
		 VALUES (1, 'cs001', 1, 'OLDTOKEN', '2026-11-28T08:30:00Z')`,
		`INSERT INTO charging_profiles (charge_station_id, charging_profile_id, charging_profile_purpose, charging_profile_kind, charging_rate_unit)
		 VALUES ('cs001', 1, 'TxDefaultProfile', 'Absolute', 'A')`,
		`INSERT INTO ocpi_registrations (token, status) VALUES ('token001', 'registered')`,
		`INSERT INTO ocpi_parties (role, country_code, party_id, url, token) VALUES ('EMSP', 'GB', 'TWK', 'https://example.com', 'token001')`,
	} {
		_, err := pool.Exec(ctx, statement)
		require.NoError(t, err, statement)
	}

	// up: wide station ids, and the rows that were there belong to the legacy org
	migrateTo(t, migrationConnString, versionAfter)
	assert.Equal(t, newWidths, stationIdWidths())
	assert.ElementsMatch(t, orgTables, orgColumns())
	assert.Equal(t, len(orgIndexes), orgIndexCount())
	for _, table := range orgTables {
		assert.Equal(t, map[string]int{legacyOrgId: 1}, rowsByOrg(table), table)
	}

	// a new row must name its org
	_, err := pool.Exec(ctx, `INSERT INTO charge_stations (charge_station_id, security_profile) VALUES ('cs002', 0)`)
	require.Error(t, err, "org_id has no default")
	_, err = pool.Exec(ctx, `INSERT INTO charge_stations (charge_station_id, security_profile, org_id) VALUES ('cs002', 0, $1)`, otherOrgId)
	require.NoError(t, err)

	// down: the old widths, no orgs, and every row is still there
	migrateTo(t, migrationConnString, versionBefore)
	assert.Equal(t, oldWidths, stationIdWidths())
	assert.Empty(t, orgColumns())
	assert.Equal(t, 0, orgIndexCount())
	assert.Equal(t, 2, rowCount("charge_stations"))
	for _, table := range []string{
		"charge_station_settings", "charge_station_event", "transactions", "transaction_meter_values",
		"charge_station_status", "remote_stop_transaction_requests", "locations", "tokens",
		"reservations", "charging_profiles", "ocpi_registrations", "ocpi_parties",
	} {
		assert.Equal(t, 1, rowCount(table), table)
	}

	// up again with rows in the tables: the org that a row had is not restored. Only to
	// this migration: later ones add tables with an org column of their own.
	migrateTo(t, migrationConnString, versionAfter)
	assert.Equal(t, newWidths, stationIdWidths())
	assert.ElementsMatch(t, orgTables, orgColumns())
	assert.Equal(t, map[string]int{legacyOrgId: 2}, rowsByOrg("charge_stations"))

	// down with a station key that is longer than the old width fails and changes nothing
	longKey := "abcd1234.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 57 characters
	_, err = pool.Exec(ctx, `INSERT INTO charge_stations (charge_station_id, security_profile, org_id) VALUES ($1, 0, $2)`, longKey, otherOrgId)
	require.NoError(t, err)

	m, err := migrate.New("file://./migrations", migrationConnString)
	require.NoError(t, err)
	err = m.Migrate(versionBefore)
	sourceErr, dbErr := m.Close()
	require.NoError(t, sourceErr)
	require.NoError(t, dbErr)
	require.Error(t, err, "the long station key does not fit the old width")

	assert.Equal(t, newWidths, stationIdWidths())
	assert.ElementsMatch(t, orgTables, orgColumns())
	assert.Equal(t, map[string]int{legacyOrgId: 2, otherOrgId: 1}, rowsByOrg("charge_stations"))
}

// Migration 000036 replaces "one token for each UID" with "one token for each org,
// country code, party id and UID". The tokens that are there must stay as they are,
// and the migration must be reversible as long as no UID is stored twice.
func TestMigration_TokensUniquePerOrg(t *testing.T) {
	ctx := context.Background()

	const (
		versionBefore = 35
		versionAfter  = 36
		legacyOrgId   = "00000000-0000-0000-0000-000000000000"
		otherOrgId    = "0193b2c4-0000-7000-8000-0000000000a1"
	)

	migrationConnString, pool := migrationTestDatabase(t, "maeve_migration_000036")

	// uniqueConstraints returns the UNIQUE constraints of the tokens table by name.
	uniqueConstraints := func() map[string]string {
		rows, err := pool.Query(ctx, `
			SELECT conname, pg_get_constraintdef(oid) FROM pg_constraint
			WHERE conrelid = 'tokens'::regclass AND contype = 'u'`)
		require.NoError(t, err)
		defer rows.Close()
		constraints := map[string]string{}
		for rows.Next() {
			var name, definition string
			require.NoError(t, rows.Scan(&name, &definition))
			constraints[name] = definition
		}
		require.NoError(t, rows.Err())
		return constraints
	}
	oldConstraints := map[string]string{"tokens_uid_unique": "UNIQUE (uid)"}
	newConstraints := map[string]string{"tokens_org_party_uid_unique": "UNIQUE (org_id, country_code, party_id, uid)"}
	// lookupIndexes returns the columns of the two lookup indexes that exist.
	lookupIndexes := func() map[string]string {
		rows, err := pool.Query(ctx, `
			SELECT indexname, substring(indexdef FROM '\((.*)\)') FROM pg_indexes
			WHERE tablename = 'tokens' AND indexname IN ('idx_tokens_org_uid', 'idx_tokens_uid')`)
		require.NoError(t, err)
		defer rows.Close()
		indexes := map[string]string{}
		for rows.Next() {
			var name, columns string
			require.NoError(t, rows.Scan(&name, &columns))
			indexes[name] = columns
		}
		require.NoError(t, rows.Err())
		return indexes
	}
	newIndexes := map[string]string{"idx_tokens_org_uid": "org_id, uid", "idx_tokens_uid": "uid"}
	// tokens returns "org/country/party/uid/contract" for every token.
	tokens := func() []string {
		rows, err := pool.Query(ctx, `
			SELECT org_id::text || '/' || country_code || '/' || party_id || '/' || uid || '/' || contract_id
			FROM tokens ORDER BY uid, 1`)
		require.NoError(t, err)
		defer rows.Close()
		found := []string{}
		for rows.Next() {
			var tok string
			require.NoError(t, rows.Scan(&tok))
			found = append(found, tok)
		}
		require.NoError(t, rows.Err())
		return found
	}
	insertToken := func(orgId, countryCode, partyId, uid string) error {
		_, err := pool.Exec(ctx, `
			INSERT INTO tokens (org_id, country_code, party_id, type, uid, contract_id, issuer, valid, cache_mode, last_updated)
			VALUES ($1, $2, $3, 'RFID', $4, $5, 'TestIssuer', true, 'ALWAYS', NOW())`,
			orgId, countryCode, partyId, uid, "CONTRACT-"+uid)
		return err
	}

	migrateTo(t, migrationConnString, versionBefore)
	require.Equal(t, oldConstraints, uniqueConstraints())
	require.Empty(t, lookupIndexes())

	// tokens that share everything but one part of the new key: a constraint on fewer
	// columns than (org, country code, party id, UID) could not be added over them
	require.NoError(t, insertToken(legacyOrgId, "GB", "TWK", "UID1"))
	require.NoError(t, insertToken(legacyOrgId, "GB", "TWK", "UID2"))
	require.NoError(t, insertToken(otherOrgId, "GB", "TWK", "UID3"))
	require.NoError(t, insertToken(legacyOrgId, "GB", "EVO", "UID4"))
	require.NoError(t, insertToken(legacyOrgId, "BE", "TWK", "UID5"))
	require.Error(t, insertToken(otherOrgId, "GB", "TWK", "UID1"), "a UID can only be stored once")
	seeded := []string{
		legacyOrgId + "/GB/TWK/UID1/CONTRACT-UID1",
		legacyOrgId + "/GB/TWK/UID2/CONTRACT-UID2",
		otherOrgId + "/GB/TWK/UID3/CONTRACT-UID3",
		legacyOrgId + "/GB/EVO/UID4/CONTRACT-UID4",
		legacyOrgId + "/BE/TWK/UID5/CONTRACT-UID5",
	}
	require.Equal(t, seeded, tokens())

	// up: the tokens are untouched and a UID can be used by another org or party
	migrateTo(t, migrationConnString, versionAfter)
	assert.Equal(t, newConstraints, uniqueConstraints())
	assert.Equal(t, newIndexes, lookupIndexes())
	assert.Equal(t, seeded, tokens())

	require.NoError(t, insertToken(otherOrgId, "GB", "TWK", "UID1"), "another org")
	require.NoError(t, insertToken(legacyOrgId, "GB", "EVO", "UID1"), "another party")
	require.NoError(t, insertToken(legacyOrgId, "BE", "TWK", "UID1"), "another country")
	require.Error(t, insertToken(legacyOrgId, "GB", "TWK", "UID1"), "the same org, country code, party id and UID")
	withDuplicates := []string{
		legacyOrgId + "/BE/TWK/UID1/CONTRACT-UID1",
		legacyOrgId + "/GB/EVO/UID1/CONTRACT-UID1",
		legacyOrgId + "/GB/TWK/UID1/CONTRACT-UID1",
		otherOrgId + "/GB/TWK/UID1/CONTRACT-UID1",
	}
	withDuplicates = append(withDuplicates, seeded[1:]...)
	require.Equal(t, withDuplicates, tokens())

	// down while a UID is stored more than once fails and changes nothing
	m, err := migrate.New("file://./migrations", migrationConnString)
	require.NoError(t, err)
	err = m.Migrate(versionBefore)
	require.Error(t, err, "UID1 is stored four times")
	assert.Equal(t, newConstraints, uniqueConstraints())
	assert.Equal(t, newIndexes, lookupIndexes())
	assert.Equal(t, withDuplicates, tokens())

	// once the duplicates are removed by hand (and the failed migration is cleared),
	// down restores the old constraint and keeps the other tokens
	_, err = pool.Exec(ctx, `DELETE FROM tokens WHERE uid = 'UID1' AND NOT (org_id = $1 AND country_code = 'GB' AND party_id = 'TWK')`, legacyOrgId)
	require.NoError(t, err)
	require.NoError(t, m.Force(versionAfter))
	sourceErr, dbErr := m.Close()
	require.NoError(t, sourceErr)
	require.NoError(t, dbErr)

	migrateTo(t, migrationConnString, versionBefore)
	assert.Equal(t, oldConstraints, uniqueConstraints())
	assert.Empty(t, lookupIndexes())
	assert.Equal(t, seeded, tokens())

	// up again with the tokens in the table
	migrateUp(t, migrationConnString)
	assert.Equal(t, newConstraints, uniqueConstraints())
	assert.Equal(t, newIndexes, lookupIndexes())
	assert.Equal(t, seeded, tokens())
}

// Migration 000045 adds the org that owns a standalone meter value. The meter values
// that are there before the migration get the org of their transaction when the
// transaction is known, else the org that owns their charge station, else the legacy
// org. A new meter value must name its org, and the migration is reversible with meter
// values in the table.
func TestMigration_AddMeterValuesOrg(t *testing.T) {
	ctx := context.Background()

	const (
		versionBefore = 44
		versionAfter  = 45
		legacyOrgId   = "00000000-0000-0000-0000-000000000000"
		oldOwner      = "0193b2c4-0000-7000-8000-0000000000a1"
		newOwner      = "0193b2c4-0000-7000-8000-0000000000b2"
		otherOwner    = "0193b2c4-0000-7000-8000-0000000000c3"
	)

	migrationConnString, pool := migrationTestDatabase(t, "maeve_migration_000045")

	// orgColumn returns whether meter_values has the org column and, when it has, whether
	// it may be NULL and its default.
	orgColumn := func() (exists bool, nullable string, columnDefault *string) {
		rows, err := pool.Query(ctx, `
			SELECT is_nullable, column_default FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'meter_values' AND column_name = 'org_id' AND data_type = 'uuid'`)
		require.NoError(t, err)
		defer rows.Close()
		for rows.Next() {
			exists = true
			require.NoError(t, rows.Scan(&nullable, &columnDefault))
		}
		require.NoError(t, rows.Err())
		return exists, nullable, columnDefault
	}
	// meterValueOrgs returns the org of every meter value, by the name in its samples.
	meterValueOrgs := func() map[string]string {
		rows, err := pool.Query(ctx, `SELECT sampled_values->>'name', org_id::text FROM meter_values`)
		require.NoError(t, err)
		defer rows.Close()
		orgs := map[string]string{}
		for rows.Next() {
			var name, orgId string
			require.NoError(t, rows.Scan(&name, &orgId))
			orgs[name] = orgId
		}
		require.NoError(t, rows.Err())
		return orgs
	}
	meterValueCount := func() int {
		var count int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM meter_values`).Scan(&count))
		return count
	}

	// 1. the schema before the migration. The charge station cs-moved belonged to the old
	// owner when its transaction tx-1 ran and belongs to the new owner now; cs-other has
	// a transaction with the same id; cs-unknown is not registered.
	migrateTo(t, migrationConnString, versionBefore)
	exists, _, _ := orgColumn()
	require.False(t, exists)
	for _, statement := range []string{
		`INSERT INTO charge_stations (charge_station_id, security_profile, org_id)
		 VALUES ('cs-moved', 0, '` + newOwner + `'), ('cs-other', 0, '` + otherOwner + `')`,
		`INSERT INTO transactions (id, charge_station_id, token_uid, token_type, start_timestamp, org_id)
		 VALUES ('tx-1', 'cs-moved', 'TOKEN', 'ISO14443', '2026-11-28 09:00:00', '` + oldOwner + `'),
		        ('tx-1', 'cs-other', 'TOKEN', 'ISO14443', '2026-11-28 09:00:00', '` + otherOwner + `'),
		        ('tx-2', 'cs-unknown', 'TOKEN', 'ISO14443', '2026-11-28 09:00:00', '` + legacyOrgId + `')`,
		`INSERT INTO meter_values (charge_station_id, evse_id, transaction_id, timestamp, sampled_values)
		 VALUES ('cs-moved', 1, 'tx-1', '2026-11-28 09:10:00', '{"name": "of a transaction of the old owner"}'),
		        ('cs-moved', 0, NULL, '2026-11-28 09:20:00', '{"name": "without a transaction"}'),
		        ('cs-moved', 1, 'tx-gone', '2026-11-28 09:30:00', '{"name": "of a transaction that is not known"}'),
		        ('cs-other', 1, 'tx-1', '2026-11-28 09:40:00', '{"name": "of the transaction with the same id of another station"}'),
		        ('cs-unknown', 1, 'tx-2', '2026-11-28 09:50:00', '{"name": "of a transaction of a station that is not registered"}'),
		        ('cs-unknown', 0, NULL, '2026-11-28 10:00:00', '{"name": "of a station that is not registered"}')`,
	} {
		_, err := pool.Exec(ctx, statement)
		require.NoError(t, err, statement)
	}
	migrated := map[string]string{
		"of a transaction of the old owner":                      oldOwner,
		"without a transaction":                                  newOwner,
		"of a transaction that is not known":                     newOwner,
		"of the transaction with the same id of another station": otherOwner,
		"of a transaction of a station that is not registered":   legacyOrgId,
		"of a station that is not registered":                    legacyOrgId,
	}

	// 2. up: every meter value has an org, and a new one must name its org
	migrateTo(t, migrationConnString, versionAfter)
	exists, nullable, columnDefault := orgColumn()
	require.True(t, exists)
	assert.Equal(t, "NO", nullable)
	assert.Nil(t, columnDefault)
	assert.Equal(t, migrated, meterValueOrgs())

	_, err := pool.Exec(ctx, `
		INSERT INTO meter_values (charge_station_id, evse_id, timestamp, sampled_values)
		VALUES ('cs-moved', 0, '2026-11-28 11:00:00', '{"name": "new"}')`)
	require.Error(t, err, "org_id has no default")
	_, err = pool.Exec(ctx, `
		INSERT INTO meter_values (charge_station_id, evse_id, timestamp, sampled_values, org_id)
		VALUES ('cs-moved', 0, '2026-11-28 11:00:00', '{"name": "new"}', $1)`, otherOwner)
	require.NoError(t, err)

	// 3. down with meter values in the table: the column goes, the meter values stay
	migrateTo(t, migrationConnString, versionBefore)
	exists, _, _ = orgColumn()
	require.False(t, exists)
	assert.Equal(t, 7, meterValueCount())

	// 4. up again: the orgs are derived again. The org that the new meter value was given
	// is not restored: it is the one of its charge station now.
	migrateUp(t, migrationConnString)
	exists, nullable, _ = orgColumn()
	require.True(t, exists)
	assert.Equal(t, "NO", nullable)
	migrated["new"] = newOwner
	assert.Equal(t, migrated, meterValueOrgs())

	// the store reads the migrated meter values by org, and gives a new one its org
	_, err = pool.Exec(ctx, `UPDATE meter_values SET sampled_values = '[]'`)
	require.NoError(t, err)
	migratedStore, err := postgres.NewStore(ctx, migrationConnString)
	require.NoError(t, err)
	defer migratedStore.Close()

	meterValueTimes := func(orgId string) []string {
		result, err := migratedStore.QueryMeterValues(ctx, store.MeterValuesFilter{ChargeStationId: "cs-moved", OrgIds: []string{orgId}, Limit: 50})
		require.NoError(t, err)
		times := []string{}
		for _, mv := range result.MeterValues {
			times = append(times, mv.MeterValue.Timestamp)
		}
		assert.Equal(t, len(times), result.Total)
		return times
	}
	require.NoError(t, migratedStore.StoreMeterValues(ctx, "cs-moved", 1, "tx-1", []store.MeterValue{{Timestamp: "2026-11-28T12:00:00Z"}}))
	require.NoError(t, migratedStore.StoreMeterValues(ctx, "cs-moved", 0, "", []store.MeterValue{{Timestamp: "2026-11-28T12:10:00Z"}}))
	assert.Equal(t, []string{"2026-11-28T12:00:00Z", "2026-11-28T09:10:00Z"}, meterValueTimes(oldOwner))
	assert.Equal(t, []string{"2026-11-28T12:10:00Z", "2026-11-28T11:00:00Z", "2026-11-28T09:30:00Z", "2026-11-28T09:20:00Z"}, meterValueTimes(newOwner))
	assert.Empty(t, meterValueTimes(otherOwner))
}
