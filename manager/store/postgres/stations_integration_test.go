// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store/storetest"
)

func TestChargeStationRegistryStore(t *testing.T) {
	storetest.TestChargeStationRegistryStore(t, emptyTestStore)
}

// The stations that are there before the registry fields were registered by an operator:
// they become active, accept only the tokens of the token store as before, and keep
// their runtime details.
func TestMigration_AddStationRegistryFields(t *testing.T) {
	ctx := context.Background()

	const (
		versionBefore = 41
		versionAfter  = 42
	)

	migrationConnString, pool := migrationTestDatabase(t, "maeve_migration_000042")

	columns := func(table string) []string {
		rows, err := pool.Query(ctx, `
			SELECT column_name FROM information_schema.columns
			WHERE table_name = $1 ORDER BY column_name`, table)
		require.NoError(t, err)
		defer rows.Close()
		names := []string{}
		for rows.Next() {
			var name string
			require.NoError(t, rows.Scan(&name))
			names = append(names, name)
		}
		require.NoError(t, rows.Err())
		return names
	}
	registryIndexes := func() []string {
		rows, err := pool.Query(ctx, `
			SELECT indexname FROM pg_indexes
			WHERE tablename = 'charge_stations' AND indexname IN ('idx_charge_stations_org_state', 'idx_charge_stations_location')
			ORDER BY indexname`)
		require.NoError(t, err)
		defer rows.Close()
		names := []string{}
		for rows.Next() {
			var name string
			require.NoError(t, rows.Scan(&name))
			names = append(names, name)
		}
		require.NoError(t, rows.Err())
		return names
	}
	registryColumns := []string{"allow_any_token", "display_name", "location_id", "max_capacity_kw", "state", "state_changed_at", "state_reason"}
	runtimeColumns := []string{"iccid", "imsi", "last_boot_at", "meter_serial_number", "meter_type"}

	migrateTo(t, migrationConnString, versionBefore)
	for _, column := range registryColumns {
		assert.NotContains(t, columns("charge_stations"), column)
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO charge_stations (charge_station_id, security_profile, base64_sha256_password, org_id)
		VALUES ('k7f2.cs001', 1, 'DEADBEEF', '0193b2c4-0000-7000-8000-0000000000a1'),
		       ('cs002', 0, NULL, '00000000-0000-0000-0000-000000000000');
		INSERT INTO charge_station_runtime (charge_station_id, ocpp_version, vendor, model)
		VALUES ('k7f2.cs001', '1.6', 'Acme', 'Volt 22')`)
	require.NoError(t, err)
	migratedAt := time.Now()

	migrateTo(t, migrationConnString, versionAfter)
	assert.Subset(t, columns("charge_stations"), registryColumns)
	assert.Subset(t, columns("charge_station_runtime"), runtimeColumns)
	assert.Equal(t, []string{"idx_charge_stations_location", "idx_charge_stations_org_state"}, registryIndexes())

	rows, err := pool.Query(ctx, `
		SELECT charge_station_id, state, allow_any_token, state_changed_at,
		       location_id IS NULL AND display_name IS NULL AND max_capacity_kw IS NULL AND state_reason IS NULL
		FROM charge_stations ORDER BY charge_station_id`)
	require.NoError(t, err)
	stations := 0
	for rows.Next() {
		var (
			key, state     string
			allowAnyToken  bool
			stateChangedAt time.Time
			unset          bool
		)
		require.NoError(t, rows.Scan(&key, &state, &allowAnyToken, &stateChangedAt, &unset))
		assert.Equal(t, "active", state, key)
		assert.False(t, allowAnyToken, key)
		assert.WithinDuration(t, migratedAt, stateChangedAt, time.Minute, key)
		assert.True(t, unset, key)
		stations++
	}
	require.NoError(t, rows.Err())
	rows.Close()
	assert.Equal(t, 2, stations)

	var vendor string
	var iccid *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT vendor, iccid FROM charge_station_runtime WHERE charge_station_id = 'k7f2.cs001'`).Scan(&vendor, &iccid))
	assert.Equal(t, "Acme", vendor)
	assert.Nil(t, iccid)

	// a new station is active too, and a station loses a location that is deleted
	_, err = pool.Exec(ctx, `
		INSERT INTO charge_stations (charge_station_id, security_profile, org_id) VALUES ('k7f2.cs003', 1, '0193b2c4-0000-7000-8000-0000000000a1');
		INSERT INTO asset_locations (id, org_id, party_ref, ocpi_id, address, city, country, latitude, longitude, time_zone)
		VALUES ('0193b2c5-0000-7000-8000-000000000001', '0193b2c4-0000-7000-8000-0000000000a1', 'party-a', 'LOC-1', 'Calle Mayor 1', 'Madrid', 'ESP', '40.416775', '-3.703790', 'Europe/Madrid');
		UPDATE charge_stations SET location_id = '0193b2c5-0000-7000-8000-000000000001' WHERE charge_station_id = 'k7f2.cs003';
		DELETE FROM asset_locations WHERE id = '0193b2c5-0000-7000-8000-000000000001'`)
	require.NoError(t, err)
	var state string
	var locationId *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT state, location_id::text FROM charge_stations WHERE charge_station_id = 'k7f2.cs003'`).Scan(&state, &locationId))
	assert.Equal(t, "active", state)
	assert.Nil(t, locationId)

	for name, statement := range map[string]string{
		"state that is not known":      `UPDATE charge_stations SET state = 'retired' WHERE charge_station_id = 'k7f2.cs001'`,
		"location that does not exist": `UPDATE charge_stations SET location_id = '0193b2c5-0000-7000-8000-000000000099' WHERE charge_station_id = 'k7f2.cs001'`,
		"ICCID longer than 20":         `UPDATE charge_station_runtime SET iccid = '123456789012345678901' WHERE charge_station_id = 'k7f2.cs001'`,
	} {
		_, err := pool.Exec(ctx, statement)
		assert.Error(t, err, name)
	}

	migrateTo(t, migrationConnString, versionBefore)
	for _, column := range registryColumns {
		assert.NotContains(t, columns("charge_stations"), column)
	}
	for _, column := range runtimeColumns {
		assert.NotContains(t, columns("charge_station_runtime"), column)
	}
	assert.Empty(t, registryIndexes())
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM charge_stations`).Scan(&count))
	assert.Equal(t, 3, count, "rolling back keeps the stations")

	migrateTo(t, migrationConnString, versionAfter)
	assert.Equal(t, []string{"idx_charge_stations_location", "idx_charge_stations_org_state"}, registryIndexes())
}
