// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

const (
	commandsPrimaryKey = "commands_pkey"
	// commandsOneSentIndex is the unique index that keeps a second command of a station
	// out of status sent.
	commandsOneSentIndex = "commands_one_sent_per_station_idx"
)

// commandClaimLockClass is the first key of the advisory lock that a claim holds on its
// station until its transaction ends (the second key is a hash of the station key). The
// number has no meaning; it only has to differ from the first key of other two-key
// advisory locks in the database.
const commandClaimLockClass int32 = 1668113523

// commandId converts a command id to pgtype.UUID; false when it is not a UUID, which no
// stored command has.
func commandId(id string) (pgtype.UUID, bool) {
	pgId, err := pgUUID(id)
	return pgId, err == nil
}

func (s *Store) CreateCommand(ctx context.Context, cmd *store.Command) error {
	if cmd.Status != store.CommandStatusQueued {
		return fmt.Errorf("create command %s: status must be %s, not %q", cmd.Id, store.CommandStatusQueued, cmd.Status)
	}
	id, err := pgUUID(cmd.Id)
	if err != nil {
		return fmt.Errorf("create command: invalid id %q: %w", cmd.Id, err)
	}
	orgId, err := pgUUID(cmd.OrgId)
	if err != nil {
		return fmt.Errorf("create command %s: invalid org id %q: %w", cmd.Id, cmd.OrgId, err)
	}

	err = s.writeQueries(ctx).InsertCommand(ctx, InsertCommandParams{
		ID:               id,
		OrgID:            orgId,
		StationKey:       cmd.StationKey,
		OcppVersion:      cmd.OcppVersion,
		Action:           cmd.Action,
		Payload:          cmd.Payload,
		Status:           string(cmd.Status),
		Result:           cmd.Result,
		ErrorCode:        toPgText(cmd.ErrorCode),
		ErrorDescription: toPgText(cmd.ErrorDescription),
		Progress:         cmd.Progress,
		ProgressAt:       toNullTimestamptz(cmd.ProgressAt),
		Actor:            toPgText(cmd.Actor),
		Jti:              toPgText(cmd.Jti),
		CreatedAt:        toPgTimestamptz(cmd.CreatedAt),
		SentAt:           toNullTimestamptz(cmd.SentAt),
		CompletedAt:      toNullTimestamptz(cmd.CompletedAt),
		ExpiresAt:        toPgTimestamptz(cmd.ExpiresAt),
	})
	if isUniqueViolation(err, commandsPrimaryKey) {
		return fmt.Errorf("create command %s: %w", cmd.Id, store.ErrAlreadyExists)
	}
	if err != nil {
		return fmt.Errorf("create command %s: %w", cmd.Id, err)
	}
	return nil
}

// GetCommand reads from the primary, also when a read replica is configured: the
// dispatcher looks a command up by the id of the CallResult that answers it, which can
// arrive before a replica has the command.
func (s *Store) GetCommand(ctx context.Context, id string) (*store.Command, error) {
	pgId, ok := commandId(id)
	if !ok {
		return nil, nil
	}
	row, err := s.writeQueries(ctx).GetCommand(ctx, pgId)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get command %s: %w", id, err)
	}
	return commandFromRow(row), nil
}

// ListCommands reads from the read replica when one is configured (outside a
// transaction). The page and the count are two statements, so the count can differ
// from the page when commands are written in between.
func (s *Store) ListCommands(ctx context.Context, filter store.CommandFilter) ([]*store.Command, int, error) {
	var orgId pgtype.UUID
	if filter.OrgId != "" {
		var err error
		if orgId, err = pgUUID(filter.OrgId); err != nil {
			return nil, 0, fmt.Errorf("list commands: invalid org id %q: %w", filter.OrgId, err)
		}
	}
	if len(filter.PayloadContains) > 0 {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(filter.PayloadContains, &object); err != nil || object == nil {
			return nil, 0, fmt.Errorf("list commands: payload filter is not a JSON object: %s", filter.PayloadContains)
		}
	}
	rowLimit, err := safeIntToInt32(filter.EffectiveLimit())
	if err != nil {
		return nil, 0, fmt.Errorf("list commands: %w", err)
	}
	rowOffset, err := safeIntToInt32(filter.EffectiveOffset())
	if err != nil {
		return nil, 0, fmt.Errorf("list commands: offset: %w", err)
	}
	// a nil array is NULL in PostgreSQL, which would match nothing instead of everything
	actions := filter.Actions
	if actions == nil {
		actions = []string{}
	}
	statuses := make([]string, len(filter.Statuses))
	for i, status := range filter.Statuses {
		statuses[i] = string(status)
	}

	q := s.readQueries(ctx)
	rows, err := q.ListCommands(ctx, ListCommandsParams{
		OrgID:           orgId,
		StationKey:      toNullableText(filter.StationKey),
		Actions:         actions,
		Statuses:        statuses,
		PayloadContains: filter.PayloadContains,
		CreatedAfter:    toNullTimestamptz(filter.CreatedAfter),
		CreatedBefore:   toNullTimestamptz(filter.CreatedBefore),
		RowLimit:        rowLimit,
		RowOffset:       rowOffset,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list commands: %w", err)
	}
	total, err := q.CountCommands(ctx, CountCommandsParams{
		OrgID:           orgId,
		StationKey:      toNullableText(filter.StationKey),
		Actions:         actions,
		Statuses:        statuses,
		PayloadContains: filter.PayloadContains,
		CreatedAfter:    toNullTimestamptz(filter.CreatedAfter),
		CreatedBefore:   toNullTimestamptz(filter.CreatedBefore),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("count commands: %w", err)
	}
	return commandsFromRows(rows), int(total), nil
}

// ClaimNextCommand holds an advisory lock on the station for the rest of its
// transaction, so that the claims of one station, made by any manager replica, happen
// one after the other and take the queued commands in order. The unique index on the
// sent commands of a station is the last guard: a command that went to sent without the
// lock makes the claim fail, which is reported as nothing claimed.
//
// Inside a transaction of the caller the claim is a savepoint, and the lock is held
// until the transaction of the caller ends.
func (s *Store) ClaimNextCommand(ctx context.Context, stationKey string, now time.Time) (*store.Command, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("claim next command of %s: begin transaction: %w", stationKey, err)
	}
	committed := false
	defer func() {
		// a claim that ends without a command wrote nothing, so a rollback ends it too
		if !committed {
			rollback(ctx, tx)
		}
	}()
	q := New(tx)

	err = q.LockStationCommands(ctx, LockStationCommandsParams{LockClass: commandClaimLockClass, StationKey: stationKey})
	if err != nil {
		return nil, fmt.Errorf("claim next command of %s: lock: %w", stationKey, err)
	}
	inFlight, err := q.HasSentCommand(ctx, stationKey)
	if err != nil {
		return nil, fmt.Errorf("claim next command of %s: %w", stationKey, err)
	}
	if inFlight {
		return nil, nil
	}
	next, err := q.NextQueuedCommandForUpdate(ctx, NextQueuedCommandForUpdateParams{
		StationKey: stationKey,
		Now:        pgtype.Timestamptz{Time: now, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim next command of %s: %w", stationKey, err)
	}
	row, err := q.MarkCommandSent(ctx, MarkCommandSentParams{
		ID:     next.ID,
		SentAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if isUniqueViolation(err, commandsOneSentIndex) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim next command of %s: mark %s sent: %w", stationKey, uuidString(next.ID), err)
	}

	committed = true
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("claim next command of %s: commit: %w", stationKey, err)
	}
	return commandFromRow(row), nil
}

// TransitionCommand updates and appends upd.Event together, in a transaction of their
// own or a savepoint inside the transaction of the caller, so the event is stored
// exactly when the update is.
func (s *Store) TransitionCommand(ctx context.Context, id string, from []store.CommandStatus, upd store.CommandUpdate) (*store.Command, error) {
	pgId, ok := commandId(id)
	if !ok {
		return nil, nil
	}
	fromStatuses := make([]string, len(from))
	for i, status := range from {
		fromStatuses[i] = string(status)
	}

	tx, err := s.begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("transition command %s: begin transaction: %w", id, err)
	}
	committed := false
	defer func() {
		if !committed {
			rollback(ctx, tx)
		}
	}()
	q := New(tx)

	row, err := q.TransitionCommand(ctx, TransitionCommandParams{
		ID:               pgId,
		FromStatuses:     fromStatuses,
		Status:           string(upd.Status),
		Result:           upd.Result,
		ErrorCode:        toPgText(upd.ErrorCode),
		ErrorDescription: toPgText(upd.ErrorDescription),
		SentAt:           toNullTimestamptz(upd.SentAt),
		CompletedAt:      toNullTimestamptz(upd.CompletedAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		current, err := q.GetCommand(ctx, pgId)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("transition command %s: %w", id, err)
		}
		return nil, fmt.Errorf("transition command %s to %s: status is %s, not one of %v: %w",
			id, upd.Status, current.Status, from, store.ErrCommandConflict)
	}
	if isUniqueViolation(err, commandsOneSentIndex) {
		return nil, fmt.Errorf("transition command %s to %s: another command of the station is in flight: %w",
			id, upd.Status, store.ErrCommandConflict)
	}
	if err != nil {
		return nil, fmt.Errorf("transition command %s: %w", id, err)
	}
	if upd.Event != nil {
		if err := s.AppendOutboxEvents(withTx(ctx, tx), upd.Event); err != nil {
			return nil, fmt.Errorf("transition command %s: %w", id, err)
		}
	}

	committed = true
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("transition command %s: commit: %w", id, err)
	}
	return commandFromRow(row), nil
}

// SetCommandProgress updates and appends evt together, like TransitionCommand.
func (s *Store) SetCommandProgress(ctx context.Context, id string, progress json.RawMessage, at time.Time, evt *store.OutboxEvent) (*store.Command, error) {
	pgId, ok := commandId(id)
	if !ok {
		return nil, nil
	}

	tx, err := s.begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("set progress of command %s: begin transaction: %w", id, err)
	}
	committed := false
	defer func() {
		if !committed {
			rollback(ctx, tx)
		}
	}()

	row, err := New(tx).SetCommandProgress(ctx, SetCommandProgressParams{
		ID:         pgId,
		Progress:   progress,
		ProgressAt: pgtype.Timestamptz{Time: at, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("set progress of command %s: %w", id, err)
	}
	if evt != nil {
		if err := s.AppendOutboxEvents(withTx(ctx, tx), evt); err != nil {
			return nil, fmt.Errorf("set progress of command %s: %w", id, err)
		}
	}

	committed = true
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("set progress of command %s: commit: %w", id, err)
	}
	return commandFromRow(row), nil
}

// The lists of the dispatcher's sweep read from the primary: the sweep acts on what they
// return.

func (s *Store) ListQueuedStationKeys(ctx context.Context, now time.Time, afterStationKey string, limit int) ([]string, error) {
	rowLimit, err := commandRowLimit(limit)
	if err != nil {
		return nil, fmt.Errorf("list queued station keys: %w", err)
	}
	keys, err := s.writeQueries(ctx).ListQueuedStationKeys(ctx, ListQueuedStationKeysParams{
		Now:             pgtype.Timestamptz{Time: now, Valid: true},
		AfterStationKey: afterStationKey,
		RowLimit:        rowLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list queued station keys: %w", err)
	}
	return keys, nil
}

func (s *Store) ListExpiredQueuedCommands(ctx context.Context, now time.Time, limit int) ([]*store.Command, error) {
	rowLimit, err := commandRowLimit(limit)
	if err != nil {
		return nil, fmt.Errorf("list expired queued commands: %w", err)
	}
	rows, err := s.writeQueries(ctx).ListExpiredQueuedCommands(ctx, ListExpiredQueuedCommandsParams{
		Now:      pgtype.Timestamptz{Time: now, Valid: true},
		RowLimit: rowLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list expired queued commands: %w", err)
	}
	return commandsFromRows(rows), nil
}

func (s *Store) ListOverdueSentCommands(ctx context.Context, sentBefore time.Time, limit int) ([]*store.Command, error) {
	rowLimit, err := commandRowLimit(limit)
	if err != nil {
		return nil, fmt.Errorf("list overdue sent commands: %w", err)
	}
	rows, err := s.writeQueries(ctx).ListOverdueSentCommands(ctx, ListOverdueSentCommandsParams{
		SentBefore: pgtype.Timestamptz{Time: sentBefore, Valid: true},
		RowLimit:   rowLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list overdue sent commands: %w", err)
	}
	return commandsFromRows(rows), nil
}

func (s *Store) DeleteCompletedCommandsBefore(ctx context.Context, before time.Time, limit int) (int, error) {
	rowLimit, err := commandRowLimit(limit)
	if err != nil {
		return 0, fmt.Errorf("delete completed commands: %w", err)
	}
	deleted, err := s.writeQueries(ctx).DeleteCompletedCommandsBefore(ctx, DeleteCompletedCommandsBeforeParams{
		Before:   pgtype.Timestamptz{Time: before, Valid: true},
		RowLimit: rowLimit,
	})
	if err != nil {
		return 0, fmt.Errorf("delete completed commands: %w", err)
	}
	return int(deleted), nil
}

// commandRowLimit checks the limit of a command list or delete: at least 1.
func commandRowLimit(limit int) (int32, error) {
	if limit < 1 {
		return 0, fmt.Errorf("limit must be positive: %d", limit)
	}
	return safeIntToInt32(limit)
}

func commandsFromRows(rows []Command) []*store.Command {
	commands := make([]*store.Command, len(rows))
	for i, row := range rows {
		commands[i] = commandFromRow(row)
	}
	return commands
}

func commandFromRow(row Command) *store.Command {
	return &store.Command{
		Id:               uuidString(row.ID),
		OrgId:            uuidString(row.OrgID),
		StationKey:       row.StationKey,
		OcppVersion:      row.OcppVersion,
		Action:           row.Action,
		Payload:          json.RawMessage(row.Payload),
		Status:           store.CommandStatus(row.Status),
		Result:           json.RawMessage(row.Result),
		ErrorCode:        fromPgText(row.ErrorCode),
		ErrorDescription: fromPgText(row.ErrorDescription),
		Progress:         json.RawMessage(row.Progress),
		ProgressAt:       fromNullTimestamptz(row.ProgressAt),
		Actor:            fromPgText(row.Actor),
		Jti:              fromPgText(row.Jti),
		CreatedAt:        fromPgTimestamptz(row.CreatedAt),
		SentAt:           fromNullTimestamptz(row.SentAt),
		CompletedAt:      fromNullTimestamptz(row.CompletedAt),
		ExpiresAt:        fromPgTimestamptz(row.ExpiresAt),
	}
}
