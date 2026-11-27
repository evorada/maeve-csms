// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/storetest"
)

func TestCommandStore(t *testing.T) {
	storetest.TestCommandStore(t, emptyTestStore)
}

var commandTestTime = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

func queuedTestCommand(stationKey string, createdAt time.Time) *store.Command {
	return &store.Command{
		Id:          uuid.Must(uuid.NewV7()).String(),
		OrgId:       "0193b2c4-0000-7000-8000-0000000000a1",
		StationKey:  stationKey,
		OcppVersion: "ocpp1.6",
		Action:      "Reset",
		Payload:     json.RawMessage(`{"type":"Soft"}`),
		Status:      store.CommandStatusQueued,
		CreatedAt:   createdAt,
		ExpiresAt:   createdAt.Add(time.Hour),
	}
}

func commandEvent(commandId string) *store.OutboxEvent {
	return &store.OutboxEvent{
		Id:      uuid.Must(uuid.NewV7()).String(),
		OrgId:   "0193b2c4-0000-7000-8000-0000000000a1",
		Type:    "command.completed",
		Subject: "urn:cpms:0193b2c4-0000-7000-8000-0000000000a1:command/" + commandId,
		Actor:   "svc:maeve",
		Time:    commandTestTime,
		Data:    json.RawMessage(`{"command_id":"` + commandId + `"}`),
	}
}

func unpublishedOutboxEventIds(t *testing.T) []string {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `SELECT id::text FROM outbox_events WHERE published_at IS NULL ORDER BY seq`)
	require.NoError(t, err)
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	return ids
}

// The database itself keeps a second command of a station out of status sent, whatever
// writes it.
func TestCommandStore_DatabaseAllowsOneSentCommandPerStation(t *testing.T) {
	ctx := context.Background()
	emptyTestStore(t)
	first := queuedTestCommand("k7f2.cs001", commandTestTime)
	second := queuedTestCommand("k7f2.cs001", commandTestTime.Add(time.Minute))
	other := queuedTestCommand("k7f2.cs002", commandTestTime)
	for _, cmd := range []*store.Command{first, second, other} {
		require.NoError(t, testStore.CreateCommand(ctx, cmd))
	}

	markSent := `UPDATE commands SET status = 'sent', sent_at = NOW() WHERE id = $1`
	_, err := testPool.Exec(ctx, markSent, first.Id)
	require.NoError(t, err)
	_, err = testPool.Exec(ctx, markSent, second.Id)
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "want a PostgreSQL error, got %v", err)
	assert.Equal(t, "23505", pgErr.Code)
	assert.Equal(t, "commands_one_sent_per_station_idx", pgErr.ConstraintName)

	_, err = testPool.Exec(ctx, markSent, other.Id)
	assert.NoError(t, err, "another station")
}

// The methods that write join the transaction of the ctx: their writes, and the events
// they append, are committed and rolled back with it.
func TestCommandStore_JoinsTheTransaction(t *testing.T) {
	ctx := context.Background()
	emptyTestStore(t)
	cmd := queuedTestCommand("k7f2.cs001", commandTestTime)

	err := testStore.RunInTx(ctx, func(ctx context.Context) error {
		require.NoError(t, testStore.CreateCommand(ctx, cmd))
		claimed, err := testStore.ClaimNextCommand(ctx, "k7f2.cs001", commandTestTime.Add(time.Minute))
		require.NoError(t, err)
		require.NotNil(t, claimed, "the transaction sees the command it created")
		return errStopTx
	})
	require.ErrorIs(t, err, errStopTx)
	got, err := testStore.GetCommand(ctx, cmd.Id)
	require.NoError(t, err)
	assert.Nil(t, got, "the create was rolled back")
	requireNoOpenTransaction(t)

	require.NoError(t, testStore.CreateCommand(ctx, cmd))
	claimed, err := testStore.ClaimNextCommand(ctx, "k7f2.cs001", commandTestTime.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)

	err = testStore.RunInTx(ctx, func(ctx context.Context) error {
		_, err := testStore.TransitionCommand(ctx, cmd.Id, []store.CommandStatus{store.CommandStatusSent},
			store.CommandUpdate{Status: store.CommandStatusAccepted, SentAt: claimed.SentAt,
				CompletedAt: &commandTestTime, Event: commandEvent(cmd.Id)})
		require.NoError(t, err)
		_, err = testStore.SetCommandProgress(ctx, cmd.Id, json.RawMessage(`{"state":"x"}`), commandTestTime, commandEvent(cmd.Id))
		require.NoError(t, err)
		return errStopTx
	})
	require.ErrorIs(t, err, errStopTx)
	got, err = testStore.GetCommand(ctx, cmd.Id)
	require.NoError(t, err)
	assert.Equal(t, store.CommandStatusSent, got.Status, "the transition was rolled back")
	assert.Nil(t, got.Progress, "the progress was rolled back")
	assert.Empty(t, unpublishedOutboxEventIds(t), "the events were rolled back")
	requireNoOpenTransaction(t)

	ev := commandEvent(cmd.Id)
	err = testStore.RunInTx(ctx, func(ctx context.Context) error {
		_, err := testStore.TransitionCommand(ctx, cmd.Id, []store.CommandStatus{store.CommandStatusSent},
			store.CommandUpdate{Status: store.CommandStatusAccepted, SentAt: claimed.SentAt, CompletedAt: &commandTestTime, Event: ev})
		return err
	})
	require.NoError(t, err)
	got, err = testStore.GetCommand(ctx, cmd.Id)
	require.NoError(t, err)
	assert.Equal(t, store.CommandStatusAccepted, got.Status)
	assert.Equal(t, []string{ev.Id}, unpublishedOutboxEventIds(t))
	requireNoOpenTransaction(t)
}

// A transition or a claim that fails in the database undoes only what it wrote: the
// transaction of the caller goes on.
func TestCommandStore_FailureKeepsTheTransactionUsable(t *testing.T) {
	ctx := context.Background()
	emptyTestStore(t)
	first := queuedTestCommand("k7f2.cs001", commandTestTime)
	second := queuedTestCommand("k7f2.cs001", commandTestTime.Add(time.Minute))
	third := queuedTestCommand("k7f2.cs002", commandTestTime)

	err := testStore.RunInTx(ctx, func(ctx context.Context) error {
		require.NoError(t, testStore.CreateCommand(ctx, first))
		require.NoError(t, testStore.CreateCommand(ctx, second))
		claimed, err := testStore.ClaimNextCommand(ctx, "k7f2.cs001", commandTestTime.Add(2*time.Minute))
		require.NoError(t, err)
		require.Equal(t, first.Id, claimed.Id)

		// the unique index refuses a second sent command of the station
		_, err = testStore.TransitionCommand(ctx, second.Id, []store.CommandStatus{store.CommandStatusQueued},
			store.CommandUpdate{Status: store.CommandStatusSent, SentAt: &commandTestTime, Event: commandEvent(second.Id)})
		require.ErrorIs(t, err, store.ErrCommandConflict)

		// an event that cannot be stored undoes the transition it belongs to
		badEvent := commandEvent(first.Id)
		badEvent.Id = "not-a-uuid"
		_, err = testStore.TransitionCommand(ctx, first.Id, []store.CommandStatus{store.CommandStatusSent},
			store.CommandUpdate{Status: store.CommandStatusAccepted, CompletedAt: &commandTestTime, Event: badEvent})
		require.Error(t, err)

		return testStore.CreateCommand(ctx, third)
	})
	require.NoError(t, err)

	for id, want := range map[string]store.CommandStatus{
		first.Id:  store.CommandStatusSent,
		second.Id: store.CommandStatusQueued,
		third.Id:  store.CommandStatusQueued,
	} {
		got, err := testStore.GetCommand(ctx, id)
		require.NoError(t, err)
		require.NotNil(t, got, id)
		assert.Equal(t, want, got.Status, id)
	}
	assert.Empty(t, unpublishedOutboxEventIds(t))
	requireNoOpenTransaction(t)
}

// Migration 000039 creates the commands table with its indexes; down drops it again.
func TestMigration_CreateCommands(t *testing.T) {
	ctx := context.Background()

	const (
		versionBefore = 38
		versionAfter  = 39
	)

	migrationConnString, pool := migrationTestDatabase(t, "maeve_migration_000039")

	commandIndexes := func() []string {
		rows, err := pool.Query(ctx, `SELECT indexname FROM pg_indexes WHERE tablename = 'commands' ORDER BY indexname`)
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
	wantIndexes := []string{
		"commands_completed_idx",
		"commands_one_sent_per_station_idx",
		"commands_org_station_created_idx",
		"commands_pkey",
		"commands_queued_expiry_idx",
		"commands_queued_idx",
		"commands_sent_idx",
		"commands_station_action_created_idx",
	}

	migrateTo(t, migrationConnString, versionBefore)
	assert.Empty(t, commandIndexes())

	migrateTo(t, migrationConnString, versionAfter)
	assert.Equal(t, wantIndexes, commandIndexes())
	_, err := pool.Exec(ctx, `
		INSERT INTO commands (id, org_id, station_key, ocpp_version, action, payload, status, created_at, expires_at)
		VALUES (gen_random_uuid(), gen_random_uuid(), 'k7f2.cs001', 'ocpp1.5', 'Reset', '{}', 'queued', NOW(), NOW())`)
	assert.Error(t, err, "an OCPP version that is not known")
	_, err = pool.Exec(ctx, `
		INSERT INTO commands (id, org_id, station_key, ocpp_version, action, payload, status, created_at, expires_at)
		VALUES (gen_random_uuid(), gen_random_uuid(), 'k7f2.cs001', 'ocpp1.6', 'Reset', '{}', 'done', NOW(), NOW())`)
	assert.Error(t, err, "a status that is not known")

	migrateTo(t, migrationConnString, versionBefore)
	assert.Empty(t, commandIndexes())
	var tables int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM pg_tables WHERE tablename = 'commands'`).Scan(&tables))
	assert.Zero(t, tables)

	migrateTo(t, migrationConnString, versionAfter)
	assert.Equal(t, wantIndexes, commandIndexes())
}
