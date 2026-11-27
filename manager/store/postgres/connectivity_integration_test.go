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

func TestConnectivityStore(t *testing.T) {
	storetest.TestConnectivityStore(t, emptyTestStore)
}

// The connectivity methods join the transaction of the ctx: their writes roll back with
// it.
func TestConnectivityStore_JoinsTheTransaction(t *testing.T) {
	ctx := context.Background()
	emptyTestStore(t)
	connectedAt := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

	err := testStore.RunInTx(ctx, func(ctx context.Context) error {
		applied, err := testStore.RecordStationConnected(ctx, "cs001", "conn-a", connectedAt)
		require.NoError(t, err)
		require.True(t, applied)
		return errStopTx
	})
	require.ErrorIs(t, err, errStopTx)
	_, err = testStore.GetChargeStationStatus(ctx, "cs001")
	assert.Error(t, err, "the connection was rolled back")
	requireNoOpenTransaction(t)

	applied, err := testStore.RecordStationConnected(ctx, "cs001", "conn-a", connectedAt)
	require.NoError(t, err)
	require.True(t, applied)
	err = testStore.RunInTx(ctx, func(ctx context.Context) error {
		applied, err := testStore.RecordStationDisconnected(ctx, "cs001", "conn-a", connectedAt.Add(time.Minute))
		require.NoError(t, err)
		require.True(t, applied)
		ids, err := testStore.DisconnectStaleStations(ctx, connectedAt.Add(time.Hour), connectedAt.Add(time.Hour))
		require.NoError(t, err)
		assert.Empty(t, ids, "the transaction sees its own disconnection")
		return errStopTx
	})
	require.ErrorIs(t, err, errStopTx)
	status, err := testStore.GetChargeStationStatus(ctx, "cs001")
	require.NoError(t, err)
	assert.True(t, status.Connected, "the disconnection was rolled back")
	requireNoOpenTransaction(t)
}

// Migration 000038 adds the connectivity columns to charge_station_status. The rows that
// are there keep their values and get no connectivity yet; down removes the columns
// again.
func TestMigration_StationConnectivity(t *testing.T) {
	ctx := context.Background()

	const (
		versionBefore = 37
		versionAfter  = 38
	)

	migrationConnString, pool := migrationTestDatabase(t, "maeve_migration_000038")

	connectivityColumns := func() map[string]string {
		rows, err := pool.Query(ctx, `
			SELECT column_name, data_type FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'charge_station_status'
			  AND column_name IN ('last_connected_at', 'last_disconnected_at', 'last_seen_at', 'connection_id')`)
		require.NoError(t, err)
		defer rows.Close()
		columns := map[string]string{}
		for rows.Next() {
			var name, dataType string
			require.NoError(t, rows.Scan(&name, &dataType))
			columns[name] = dataType
		}
		require.NoError(t, rows.Err())
		return columns
	}
	hasSeenIndex := func() bool {
		var count int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM pg_indexes WHERE tablename = 'charge_station_status' AND indexname = 'idx_charge_station_status_seen'`).Scan(&count))
		return count == 1
	}
	type row struct {
		connected bool
		model     string
	}
	statusRow := func() row {
		var r row
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT connected, model FROM charge_station_status WHERE charge_station_id = 'k7f2.cs001'`).Scan(&r.connected, &r.model))
		return r
	}

	migrateTo(t, migrationConnString, versionBefore)
	require.Empty(t, connectivityColumns())
	_, err := pool.Exec(ctx, `
		INSERT INTO charge_station_status (charge_station_id, connected, last_heartbeat, model, updated_at)
		VALUES ('k7f2.cs001', true, NOW(), 'Model X', NOW())`)
	require.NoError(t, err)

	migrateTo(t, migrationConnString, versionAfter)
	assert.Equal(t, map[string]string{
		"last_connected_at":    "timestamp with time zone",
		"last_disconnected_at": "timestamp with time zone",
		"last_seen_at":         "timestamp with time zone",
		"connection_id":        "text",
	}, connectivityColumns())
	assert.True(t, hasSeenIndex())
	assert.Equal(t, row{connected: true, model: "Model X"}, statusRow())
	var nulls int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM charge_station_status
		WHERE last_connected_at IS NULL AND last_disconnected_at IS NULL AND last_seen_at IS NULL AND connection_id IS NULL`).Scan(&nulls))
	assert.Equal(t, 1, nulls)

	migrateTo(t, migrationConnString, versionBefore)
	assert.Empty(t, connectivityColumns())
	assert.False(t, hasSeenIndex())
	assert.Equal(t, row{connected: true, model: "Model X"}, statusRow())
}
