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
		rows, err := pool.Query(ctx, `
			SELECT transaction_id, charge_station_id FROM transaction_meter_values ORDER BY transaction_id, timestamp`)
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
