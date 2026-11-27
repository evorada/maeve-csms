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

// outboxRelayLockKey is the key of the advisory lock that a relay batch holds until its
// transaction ends. Every manager that shares the database takes the same key, so one
// batch runs at a time and published_seq follows the order in which events are
// published. The number has no meaning; it only has to differ from the keys of other
// advisory locks in the database.
const outboxRelayLockKey int64 = 4361923870117

// outboxMarkTimeout is how long a relay batch may take to mark what was published and
// to commit, once the context of the caller is cancelled.
const outboxMarkTimeout = 5 * time.Second

// AppendOutboxEvents stores the events in the transaction that ctx carries. Without one
// it stores them in a transaction of their own, so that they are stored all or none.
func (s *Store) AppendOutboxEvents(ctx context.Context, events ...*store.OutboxEvent) error {
	if len(events) == 0 {
		return nil
	}
	return s.RunInTx(ctx, func(ctx context.Context) error {
		q := s.writeQueries(ctx)
		for _, ev := range events {
			id, err := pgUUID(ev.Id)
			if err != nil {
				return fmt.Errorf("append outbox event: invalid id %q: %w", ev.Id, err)
			}
			row, err := q.InsertOutboxEvent(ctx, InsertOutboxEventParams{
				ID:          id,
				OrgID:       ev.OrgId,
				Type:        ev.Type,
				Subject:     ev.Subject,
				Actor:       ev.Actor,
				Traceparent: toNullableText(ev.TraceParent),
				EventTime:   pgtype.Timestamptz{Time: ev.Time, Valid: true},
				Data:        ev.Data,
			})
			if err != nil {
				return fmt.Errorf("append outbox event %s: %w", ev.Id, err)
			}
			ev.Seq = row.Seq
			ev.CreatedAt = fromPgTimestamptz(row.CreatedAt)
		}
		return nil
	})
}

// RelayOutboxEvents runs one relay batch in a transaction of its own on the primary,
// also when ctx carries a transaction: the lock and the claimed rows must be released
// when the batch ends, not when the transaction of a caller ends.
//
// What publish returns is marked and committed also when ctx is cancelled by then (a
// manager that shuts down), so that events that went out are not published again.
func (s *Store) RelayOutboxEvents(ctx context.Context, limit int, publish store.OutboxPublishFunc) (int, error) {
	if limit < 1 {
		return 0, fmt.Errorf("relay outbox events: limit must be positive: %d", limit)
	}
	rowLimit, err := safeIntToInt32(limit)
	if err != nil {
		return 0, fmt.Errorf("relay outbox events: %w", err)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("relay outbox events: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			rollback(ctx, tx)
		}
	}()
	q := New(tx)

	locked, err := q.TryLockOutboxRelay(ctx, outboxRelayLockKey)
	if err != nil {
		return 0, fmt.Errorf("relay outbox events: lock: %w", err)
	}
	if !locked {
		// another batch is running; the rollback ends a transaction that wrote nothing
		return 0, nil
	}

	rows, err := q.ClaimUnpublishedOutboxEvents(ctx, rowLimit)
	if err != nil {
		return 0, fmt.Errorf("relay outbox events: claim: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	events := make([]*store.OutboxEvent, len(rows))
	for i, row := range rows {
		events[i] = outboxEventFromRow(row)
	}

	published, publishErr := publish(ctx, events)
	published = max(0, min(published, len(events)))
	if published == 0 {
		return 0, publishErr
	}

	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), outboxMarkTimeout)
	defer cancel()
	publishedAt := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	for _, ev := range events[:published] {
		err := q.MarkOutboxEventPublished(markCtx, MarkOutboxEventPublishedParams{Seq: ev.Seq, PublishedAt: publishedAt})
		if err != nil {
			return 0, errors.Join(fmt.Errorf("relay outbox events: mark %s published: %w", ev.Id, err), publishErr)
		}
	}
	// Commit ends the transaction whether it succeeds or not.
	committed = true
	if err := tx.Commit(markCtx); err != nil {
		return 0, errors.Join(fmt.Errorf("relay outbox events: commit: %w", err), publishErr)
	}
	return published, publishErr
}

// ListOutboxEvents reads from the read replica when one is configured (outside a
// transaction), so a replay can lag behind the primary; it never skips an event.
func (s *Store) ListOutboxEvents(ctx context.Context, filter store.OutboxEventFilter) ([]*store.OutboxEvent, error) {
	rowLimit, err := safeIntToInt32(filter.EffectiveLimit())
	if err != nil {
		return nil, fmt.Errorf("list outbox events: %w", err)
	}
	// a nil array is NULL in PostgreSQL, which would match nothing instead of everything
	orgIds, types := filter.OrgIds, filter.Types
	if orgIds == nil {
		orgIds = []string{}
	}
	if types == nil {
		types = []string{}
	}
	rows, err := s.readQueries(ctx).ListPublishedOutboxEvents(ctx, ListPublishedOutboxEventsParams{
		AfterPublishedSeq: filter.AfterPublishedSeq,
		OrgIds:            orgIds,
		Types:             types,
		RowLimit:          rowLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list outbox events: %w", err)
	}
	events := make([]*store.OutboxEvent, len(rows))
	for i, row := range rows {
		events[i] = outboxEventFromRow(row)
	}
	return events, nil
}

// PurgePublishedOutboxEvents deletes in batches, so that no single statement locks the
// whole table.
func (s *Store) PurgePublishedOutboxEvents(ctx context.Context, publishedBefore time.Time) (int64, error) {
	cutoff := pgtype.Timestamptz{Time: publishedBefore, Valid: true}
	var total int64
	for {
		deleted, err := s.writeQueries(ctx).PurgePublishedOutboxEventsBatch(ctx, cutoff)
		if err != nil {
			return total, fmt.Errorf("purge published outbox events: %w", err)
		}
		total += deleted
		if deleted == 0 {
			return total, nil
		}
	}
}

func outboxEventFromRow(row OutboxEvent) *store.OutboxEvent {
	ev := &store.OutboxEvent{
		Id:          uuidString(row.ID),
		Seq:         row.Seq,
		OrgId:       row.OrgID,
		Type:        row.Type,
		Subject:     row.Subject,
		Actor:       row.Actor,
		TraceParent: fromNullableText(row.Traceparent),
		Time:        fromPgTimestamptz(row.EventTime),
		Data:        json.RawMessage(row.Data),
		CreatedAt:   fromPgTimestamptz(row.CreatedAt),
		PublishedAt: fromNullTimestamptz(row.PublishedAt),
	}
	if row.PublishedSeq.Valid {
		ev.PublishedSeq = row.PublishedSeq.Int64
	}
	return ev
}
