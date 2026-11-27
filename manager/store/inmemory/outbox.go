// SPDX-License-Identifier: Apache-2.0

package inmemory

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// stagedOutboxEvent is an outbox event that is about to be stored: the copy to store,
// and the event of the caller, which gets the Seq of the copy when it is stored.
type stagedOutboxEvent struct {
	stored *store.OutboxEvent
	caller *store.OutboxEvent
}

// AppendOutboxEvents stores copies of the events. Inside a transaction (RunInTx) they are
// set aside and stored, with their Seq, when the transaction ends without an error;
// CreatedAt is set at once.
func (s *Store) AppendOutboxEvents(ctx context.Context, events ...*store.OutboxEvent) error {
	s.Lock()
	defer s.Unlock()
	s.appendOutboxEventsLocked(ctx, events...)
	return nil
}

// appendOutboxEventsLocked is AppendOutboxEvents for a caller that holds the store
// mutex, so that the events are appended together with the change the caller makes.
func (s *Store) appendOutboxEventsLocked(ctx context.Context, events ...*store.OutboxEvent) {
	now := s.clock.Now().UTC()
	staged := make([]stagedOutboxEvent, len(events))
	for i, ev := range events {
		ev.CreatedAt = now
		stored := copyOutboxEvent(ev)
		// like a new row in PostgreSQL: not published yet, whatever the caller set
		stored.PublishedSeq = 0
		stored.PublishedAt = nil
		staged[i] = stagedOutboxEvent{stored: stored, caller: ev}
	}

	if tx := memTxFrom(ctx); tx != nil {
		tx.outbox = append(tx.outbox, staged...)
		return
	}
	s.storeOutboxEvents(staged)
}

// storeOutboxEvents appends events to the outbox and gives them their Seq. The caller
// holds the store mutex.
func (s *Store) storeOutboxEvents(staged []stagedOutboxEvent) {
	for _, ev := range staged {
		s.outboxSeq++
		ev.stored.Seq = s.outboxSeq
		ev.caller.Seq = s.outboxSeq
		s.outbox = append(s.outbox, ev.stored)
	}
}

// RelayOutboxEvents calls publish without the store mutex, so publish may use the store.
func (s *Store) RelayOutboxEvents(ctx context.Context, limit int, publish store.OutboxPublishFunc) (int, error) {
	if limit < 1 {
		return 0, fmt.Errorf("relay outbox events: limit must be positive: %d", limit)
	}
	if !s.relayMu.TryLock() {
		return 0, nil
	}
	defer s.relayMu.Unlock()

	s.Lock()
	var claimed []*store.OutboxEvent
	for _, ev := range s.outbox {
		if ev.PublishedAt == nil {
			claimed = append(claimed, ev)
			if len(claimed) == limit {
				break
			}
		}
	}
	events := make([]*store.OutboxEvent, len(claimed))
	for i, ev := range claimed {
		events[i] = copyOutboxEvent(ev)
	}
	s.Unlock()

	if len(events) == 0 {
		return 0, nil
	}
	published, publishErr := publish(ctx, events)
	published = max(0, min(published, len(events)))

	s.Lock()
	defer s.Unlock()
	now := s.clock.Now().UTC()
	// only this batch marks events, and a purge only removes published ones, so the
	// claimed events are still unpublished and in the outbox
	for _, ev := range claimed[:published] {
		s.outboxPublishedSeq++
		ev.PublishedSeq = s.outboxPublishedSeq
		publishedAt := now
		ev.PublishedAt = &publishedAt
	}
	return published, publishErr
}

func (s *Store) ListOutboxEvents(_ context.Context, filter store.OutboxEventFilter) ([]*store.OutboxEvent, error) {
	s.Lock()
	defer s.Unlock()

	matching := []*store.OutboxEvent{}
	for _, ev := range s.outbox {
		if ev.PublishedAt == nil || ev.PublishedSeq <= filter.AfterPublishedSeq {
			continue
		}
		if len(filter.OrgIds) > 0 && !slices.Contains(filter.OrgIds, ev.OrgId) {
			continue
		}
		if len(filter.Types) > 0 && !slices.Contains(filter.Types, ev.Type) {
			continue
		}
		matching = append(matching, ev)
	}
	slices.SortFunc(matching, func(a, b *store.OutboxEvent) int {
		return cmp.Compare(a.PublishedSeq, b.PublishedSeq)
	})
	if limit := filter.EffectiveLimit(); len(matching) > limit {
		matching = matching[:limit]
	}

	events := make([]*store.OutboxEvent, len(matching))
	for i, ev := range matching {
		events[i] = copyOutboxEvent(ev)
	}
	return events, nil
}

func (s *Store) PurgePublishedOutboxEvents(_ context.Context, publishedBefore time.Time) (int64, error) {
	s.Lock()
	defer s.Unlock()

	before := len(s.outbox)
	s.outbox = slices.DeleteFunc(s.outbox, func(ev *store.OutboxEvent) bool {
		return ev.PublishedAt != nil && ev.PublishedAt.Before(publishedBefore)
	})
	return int64(before - len(s.outbox)), nil
}

func copyOutboxEvent(ev *store.OutboxEvent) *store.OutboxEvent {
	c := *ev
	c.Data = slices.Clone(ev.Data)
	if ev.PublishedAt != nil {
		publishedAt := *ev.PublishedAt
		c.PublishedAt = &publishedAt
	}
	return &c
}
