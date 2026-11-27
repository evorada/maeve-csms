// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// errOutboxTest is what the outbox tests return to make something fail.
var errOutboxTest = errors.New("stop")

// testOutboxEvent is an event that is ready to be appended. Its id is a new UUIDv7.
func testOutboxEvent(orgId, eventType, resourceId string) *store.OutboxEvent {
	return &store.OutboxEvent{
		Id:          uuid.Must(uuid.NewV7()).String(),
		OrgId:       orgId,
		Type:        eventType,
		Subject:     "urn:cpms:" + orgId + ":session/" + resourceId,
		Actor:       "station:chrg01.cs001",
		TraceParent: "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
		Time:        time.Date(2026, 11, 27, 10, 30, 0, 0, time.UTC),
		Data:        json.RawMessage(`{"resource":"` + resourceId + `"}`),
	}
}

// outboxRecorder is a publish func that publishes everything and records it.
type outboxRecorder struct {
	calls     int
	published []*store.OutboxEvent
}

func (r *outboxRecorder) publish(_ context.Context, events []*store.OutboxEvent) (int, error) {
	r.calls++
	r.published = append(r.published, events...)
	return len(events), nil
}

func outboxIds(events []*store.OutboxEvent) []string {
	ids := make([]string, len(events))
	for i, ev := range events {
		ids[i] = ev.Id
	}
	return ids
}

// appendOutboxEvents appends events, one call each, outside a transaction.
func appendOutboxEvents(t *testing.T, s store.Engine, events ...*store.OutboxEvent) {
	t.Helper()
	for _, ev := range events {
		require.NoError(t, s.AppendOutboxEvents(context.Background(), ev))
	}
}

// relayAll publishes every unpublished event and returns them.
func relayAll(t *testing.T, s store.Engine) []*store.OutboxEvent {
	t.Helper()
	recorder := &outboxRecorder{}
	_, err := s.RelayOutboxEvents(context.Background(), store.MaxOutboxEventListLimit, recorder.publish)
	require.NoError(t, err)
	return recorder.published
}

// listOutbox lists the published events with a filter.
func listOutbox(t *testing.T, s store.Engine, filter store.OutboxEventFilter) []*store.OutboxEvent {
	t.Helper()
	events, err := s.ListOutboxEvents(context.Background(), filter)
	require.NoError(t, err)
	return events
}

// TestOutboxStore checks the transactional outbox of a storage backend: appending,
// relaying in order with one relay batch at a time, the replay list and the purge.
func TestOutboxStore(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()

	t.Run("appended events are relayed in Seq order and marked published", func(t *testing.T) {
		s := newStore(t)
		first := testOutboxEvent(orgA, "session.started", "tx-1")
		second := testOutboxEvent(orgA, "session.updated", "tx-1")
		third := testOutboxEvent(orgB, "session.completed", "tx-2")
		third.TraceParent = ""
		require.NoError(t, s.AppendOutboxEvents(ctx, first, second))
		require.NoError(t, s.AppendOutboxEvents(ctx, third))

		assert.Positive(t, first.Seq)
		assert.Less(t, first.Seq, second.Seq)
		assert.Less(t, second.Seq, third.Seq)
		for _, ev := range []*store.OutboxEvent{first, second, third} {
			assert.False(t, ev.CreatedAt.IsZero(), "CreatedAt should be set")
		}

		recorder := &outboxRecorder{}
		relayed, err := s.RelayOutboxEvents(ctx, 10, recorder.publish)
		require.NoError(t, err)
		assert.Equal(t, 3, relayed)
		assert.Equal(t, 1, recorder.calls)
		require.Equal(t, outboxIds([]*store.OutboxEvent{first, second, third}), outboxIds(recorder.published))

		// what the relay reads is what was appended
		got := recorder.published[0]
		assert.Equal(t, first.Seq, got.Seq)
		assert.Equal(t, first.OrgId, got.OrgId)
		assert.Equal(t, first.Type, got.Type)
		assert.Equal(t, first.Subject, got.Subject)
		assert.Equal(t, first.Actor, got.Actor)
		assert.Equal(t, first.TraceParent, got.TraceParent)
		assert.True(t, first.Time.Equal(got.Time), "time: want %s, got %s", first.Time, got.Time)
		assert.JSONEq(t, string(first.Data), string(got.Data))
		assert.Zero(t, got.PublishedSeq, "the relay is given the events before they are marked")
		assert.Nil(t, got.PublishedAt)
		assert.Equal(t, "", recorder.published[2].TraceParent)

		listed := listOutbox(t, s, store.OutboxEventFilter{})
		require.Equal(t, outboxIds(recorder.published), outboxIds(listed))
		for i, ev := range listed {
			assert.Positive(t, ev.PublishedSeq)
			assert.Equal(t, listed[0].PublishedSeq+int64(i), ev.PublishedSeq, "published seqs should have no gap")
			assert.NotNil(t, ev.PublishedAt)
		}

		// nothing is left to publish
		again := &outboxRecorder{}
		relayed, err = s.RelayOutboxEvents(ctx, 10, again.publish)
		require.NoError(t, err)
		assert.Equal(t, 0, relayed)
		assert.Equal(t, 0, again.calls, "publish should not be called without events")
	})

	t.Run("a stored event does not change with the event of the caller", func(t *testing.T) {
		s := newStore(t)
		ev := testOutboxEvent(orgA, "session.started", "tx-1")
		data := `{"resource":"tx-1"}`
		appendOutboxEvents(t, s, ev)

		ev.Type = "session.changed"
		ev.Data[2] = 'X'

		relayed := relayAll(t, s)
		require.Len(t, relayed, 1)
		assert.Equal(t, "session.started", relayed[0].Type)
		assert.JSONEq(t, data, string(relayed[0].Data))
	})

	t.Run("a publish that fails marks only the published prefix", func(t *testing.T) {
		s := newStore(t)
		events := []*store.OutboxEvent{
			testOutboxEvent(orgA, "session.started", "tx-1"),
			testOutboxEvent(orgA, "session.updated", "tx-1"),
			testOutboxEvent(orgA, "session.completed", "tx-1"),
		}
		appendOutboxEvents(t, s, events...)

		relayed, err := s.RelayOutboxEvents(ctx, 10, func(_ context.Context, claimed []*store.OutboxEvent) (int, error) {
			require.Len(t, claimed, 3)
			return 1, errOutboxTest
		})
		assert.ErrorIs(t, err, errOutboxTest)
		assert.Equal(t, 1, relayed)
		assert.Equal(t, outboxIds(events[:1]), outboxIds(listOutbox(t, s, store.OutboxEventFilter{})))

		// the next relay starts at the second event
		assert.Equal(t, outboxIds(events[1:]), outboxIds(relayAll(t, s)))
		assert.Equal(t, outboxIds(events), outboxIds(listOutbox(t, s, store.OutboxEventFilter{})))
	})

	t.Run("a publish that publishes nothing marks nothing", func(t *testing.T) {
		s := newStore(t)
		ev := testOutboxEvent(orgA, "session.started", "tx-1")
		appendOutboxEvents(t, s, ev)

		relayed, err := s.RelayOutboxEvents(ctx, 10, func(context.Context, []*store.OutboxEvent) (int, error) {
			return 0, errOutboxTest
		})
		assert.ErrorIs(t, err, errOutboxTest)
		assert.Equal(t, 0, relayed)
		assert.Empty(t, listOutbox(t, s, store.OutboxEventFilter{}))
		assert.Equal(t, []string{ev.Id}, outboxIds(relayAll(t, s)))
	})

	t.Run("a publish that reports more than it was given marks what it was given", func(t *testing.T) {
		s := newStore(t)
		ev := testOutboxEvent(orgA, "session.started", "tx-1")
		appendOutboxEvents(t, s, ev)

		relayed, err := s.RelayOutboxEvents(ctx, 10, func(_ context.Context, claimed []*store.OutboxEvent) (int, error) {
			return len(claimed) + 5, nil
		})
		require.NoError(t, err)
		assert.Equal(t, 1, relayed)
		assert.Equal(t, []string{ev.Id}, outboxIds(listOutbox(t, s, store.OutboxEventFilter{})))
	})

	t.Run("a relay batch claims at most limit events", func(t *testing.T) {
		s := newStore(t)
		events := []*store.OutboxEvent{
			testOutboxEvent(orgA, "session.started", "tx-1"),
			testOutboxEvent(orgA, "session.updated", "tx-1"),
			testOutboxEvent(orgA, "session.completed", "tx-1"),
		}
		appendOutboxEvents(t, s, events...)

		recorder := &outboxRecorder{}
		relayed, err := s.RelayOutboxEvents(ctx, 2, recorder.publish)
		require.NoError(t, err)
		assert.Equal(t, 2, relayed)
		assert.Equal(t, outboxIds(events[:2]), outboxIds(recorder.published))
		assert.Equal(t, outboxIds(events[2:]), outboxIds(relayAll(t, s)))

		for _, limit := range []int{0, -1} {
			_, err := s.RelayOutboxEvents(ctx, limit, recorder.publish)
			assert.Error(t, err, "limit %d", limit)
		}
	})

	t.Run("a second relay batch returns at once while one runs", func(t *testing.T) {
		s := newStore(t)
		ev := testOutboxEvent(orgA, "session.started", "tx-1")
		appendOutboxEvents(t, s, ev)

		started, release := make(chan struct{}), make(chan struct{})
		var wg sync.WaitGroup
		var firstRelayed int
		var firstErr error
		wg.Add(1)
		go func() {
			defer wg.Done()
			firstRelayed, firstErr = s.RelayOutboxEvents(ctx, 10, func(_ context.Context, claimed []*store.OutboxEvent) (int, error) {
				close(started)
				<-release
				return len(claimed), nil
			})
		}()
		<-started

		second := &outboxRecorder{}
		relayed, err := s.RelayOutboxEvents(ctx, 10, second.publish)
		close(release)
		wg.Wait()

		require.NoError(t, err)
		assert.Equal(t, 0, relayed)
		assert.Equal(t, 0, second.calls, "the second batch should not publish")
		require.NoError(t, firstErr)
		assert.Equal(t, 1, firstRelayed)

		// the lock is free again
		appendOutboxEvents(t, s, testOutboxEvent(orgA, "session.updated", "tx-1"))
		assert.Len(t, relayAll(t, s), 1)
	})

	t.Run("an event appended in a transaction that fails is not stored", func(t *testing.T) {
		s := newStore(t)

		err := s.RunInTx(ctx, func(ctx context.Context) error {
			require.NoError(t, s.AppendOutboxEvents(ctx, testOutboxEvent(orgA, "session.started", "tx-1")))
			return errOutboxTest
		})
		require.ErrorIs(t, err, errOutboxTest)

		assert.Empty(t, relayAll(t, s))
	})

	t.Run("events appended in a transaction are stored when it succeeds", func(t *testing.T) {
		s := newStore(t)
		outer := testOutboxEvent(orgA, "session.started", "tx-1")
		inner := testOutboxEvent(orgA, "session.updated", "tx-1")

		err := s.RunInTx(ctx, func(ctx context.Context) error {
			require.NoError(t, s.AppendOutboxEvents(ctx, outer))
			// a nested transaction joins the outer one
			return s.RunInTx(ctx, func(ctx context.Context) error {
				return s.AppendOutboxEvents(ctx, inner)
			})
		})
		require.NoError(t, err)

		assert.Positive(t, outer.Seq)
		assert.Less(t, outer.Seq, inner.Seq)
		assert.Equal(t, []string{outer.Id, inner.Id}, outboxIds(relayAll(t, s)))
	})

	t.Run("events appended in a nested transaction roll back with the outer one", func(t *testing.T) {
		s := newStore(t)

		err := s.RunInTx(ctx, func(ctx context.Context) error {
			require.NoError(t, s.RunInTx(ctx, func(ctx context.Context) error {
				return s.AppendOutboxEvents(ctx, testOutboxEvent(orgA, "session.started", "tx-1"))
			}))
			return errOutboxTest
		})
		require.ErrorIs(t, err, errOutboxTest)

		assert.Empty(t, relayAll(t, s))
	})

	t.Run("a relay inside a transaction neither joins it nor sees its events", func(t *testing.T) {
		s := newStore(t)
		ev := testOutboxEvent(orgA, "session.started", "tx-1")

		err := s.RunInTx(ctx, func(ctx context.Context) error {
			require.NoError(t, s.AppendOutboxEvents(ctx, ev))
			recorder := &outboxRecorder{}
			relayed, err := s.RelayOutboxEvents(ctx, 10, recorder.publish)
			require.NoError(t, err)
			assert.Equal(t, 0, relayed, "the event is not committed yet")
			return nil
		})
		require.NoError(t, err)

		assert.Equal(t, []string{ev.Id}, outboxIds(relayAll(t, s)))
	})

	t.Run("the list replays published events by cursor, org and type", func(t *testing.T) {
		s := newStore(t)
		aStarted := testOutboxEvent(orgA, "session.started", "tx-1")
		bStarted := testOutboxEvent(orgB, "session.started", "tx-2")
		aCompleted := testOutboxEvent(orgA, "session.completed", "tx-1")
		cStation := testOutboxEvent(orgC, "station.registered", "cs-1")
		appendOutboxEvents(t, s, aStarted, bStarted, aCompleted, cStation)
		require.Len(t, relayAll(t, s), 4)
		unpublished := testOutboxEvent(orgA, "session.updated", "tx-1")
		appendOutboxEvents(t, s, unpublished)

		all := listOutbox(t, s, store.OutboxEventFilter{})
		require.Equal(t, outboxIds([]*store.OutboxEvent{aStarted, bStarted, aCompleted, cStation}), outboxIds(all))
		assert.Equal(t, all, listOutbox(t, s, store.OutboxEventFilter{Limit: -1}), "a limit below 1 is the default")
		assert.Equal(t, all, listOutbox(t, s, store.OutboxEventFilter{AfterPublishedSeq: -1}),
			"unpublished events are not listed with any cursor")

		// paging with the published seq as cursor
		page1 := listOutbox(t, s, store.OutboxEventFilter{Limit: 2})
		assert.Equal(t, outboxIds(all[:2]), outboxIds(page1))
		page2 := listOutbox(t, s, store.OutboxEventFilter{AfterPublishedSeq: page1[1].PublishedSeq, Limit: 2})
		assert.Equal(t, outboxIds(all[2:]), outboxIds(page2))
		assert.Empty(t, listOutbox(t, s, store.OutboxEventFilter{AfterPublishedSeq: page2[1].PublishedSeq, Limit: 2}))

		assert.Equal(t, outboxIds([]*store.OutboxEvent{aStarted, aCompleted}),
			outboxIds(listOutbox(t, s, store.OutboxEventFilter{OrgIds: []string{orgA}})))
		assert.Equal(t, outboxIds([]*store.OutboxEvent{aStarted, aCompleted, cStation}),
			outboxIds(listOutbox(t, s, store.OutboxEventFilter{OrgIds: []string{orgA, orgC}})))
		assert.Equal(t, outboxIds([]*store.OutboxEvent{aStarted, bStarted}),
			outboxIds(listOutbox(t, s, store.OutboxEventFilter{Types: []string{"session.started"}})))
		assert.Equal(t, outboxIds([]*store.OutboxEvent{bStarted}),
			outboxIds(listOutbox(t, s, store.OutboxEventFilter{OrgIds: []string{orgB}, Types: []string{"session.started"}})))
		assert.Empty(t, listOutbox(t, s, store.OutboxEventFilter{OrgIds: []string{orgB}, Types: []string{"session.completed"}}))
		assert.Equal(t, outboxIds([]*store.OutboxEvent{aCompleted}),
			outboxIds(listOutbox(t, s, store.OutboxEventFilter{AfterPublishedSeq: all[0].PublishedSeq, OrgIds: []string{orgA}, Limit: 1})))

		listed := all[1]
		assert.Equal(t, bStarted.Seq, listed.Seq)
		assert.Equal(t, bStarted.Subject, listed.Subject)
		assert.JSONEq(t, string(bStarted.Data), string(listed.Data))
	})

	t.Run("the list returns at most the maximum", func(t *testing.T) {
		s := newStore(t)
		events := make([]*store.OutboxEvent, store.MaxOutboxEventListLimit+1)
		for i := range events {
			events[i] = testOutboxEvent(orgA, "session.updated", fmt.Sprintf("tx-%d", i))
		}
		require.NoError(t, s.AppendOutboxEvents(ctx, events...))
		require.Len(t, relayAll(t, s), store.MaxOutboxEventListLimit)
		require.Len(t, relayAll(t, s), 1)

		assert.Len(t, listOutbox(t, s, store.OutboxEventFilter{Limit: store.MaxOutboxEventListLimit + 1}), store.MaxOutboxEventListLimit)
		assert.Len(t, listOutbox(t, s, store.OutboxEventFilter{}), store.DefaultOutboxEventListLimit)
	})

	t.Run("the purge deletes only events published before the cutoff", func(t *testing.T) {
		s := newStore(t)
		early := []*store.OutboxEvent{
			testOutboxEvent(orgA, "session.started", "tx-1"),
			testOutboxEvent(orgA, "session.updated", "tx-1"),
		}
		appendOutboxEvents(t, s, early...)
		require.Len(t, relayAll(t, s), 2)

		// the next batch is published at a later time
		time.Sleep(5 * time.Millisecond)
		late := testOutboxEvent(orgA, "session.completed", "tx-1")
		appendOutboxEvents(t, s, late)
		require.Len(t, relayAll(t, s), 1)
		unpublished := testOutboxEvent(orgB, "session.started", "tx-2")
		appendOutboxEvents(t, s, unpublished)

		listed := listOutbox(t, s, store.OutboxEventFilter{})
		require.Len(t, listed, 3)
		cutoff := *listed[2].PublishedAt
		require.True(t, listed[1].PublishedAt.Before(cutoff), "the batches should be published at different times")

		purged, err := s.PurgePublishedOutboxEvents(ctx, cutoff)
		require.NoError(t, err)
		assert.Equal(t, int64(2), purged)
		assert.Equal(t, []string{late.Id}, outboxIds(listOutbox(t, s, store.OutboxEventFilter{})))

		purged, err = s.PurgePublishedOutboxEvents(ctx, time.Now().Add(time.Hour))
		require.NoError(t, err)
		assert.Equal(t, int64(1), purged)
		assert.Empty(t, listOutbox(t, s, store.OutboxEventFilter{}))

		// an unpublished event is never purged
		assert.Equal(t, []string{unpublished.Id}, outboxIds(relayAll(t, s)))
	})
}
