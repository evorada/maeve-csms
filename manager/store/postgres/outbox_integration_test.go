// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/storetest"
)

func TestOutboxStore(t *testing.T) {
	storetest.TestOutboxStore(t, emptyTestStore)
}

func outboxTestEvent(eventType string) *store.OutboxEvent {
	return &store.OutboxEvent{
		Id:      uuid.Must(uuid.NewV7()).String(),
		OrgId:   testOrgA,
		Type:    eventType,
		Subject: "urn:cpms:" + testOrgA + ":session/tx-1",
		Actor:   "svc:test",
		Time:    time.Now().UTC(),
		Data:    json.RawMessage(`{}`),
	}
}

// relayOutbox publishes every unpublished event and returns their ids.
func relayOutbox(t *testing.T) []string {
	t.Helper()
	var ids []string
	_, err := testStore.RelayOutboxEvents(context.Background(), 100, func(_ context.Context, events []*store.OutboxEvent) (int, error) {
		for _, ev := range events {
			ids = append(ids, ev.Id)
		}
		return len(events), nil
	})
	require.NoError(t, err)
	return ids
}

func TestRunInTx_TokenAndOutboxEventCommitTogether(t *testing.T) {
	t.Run("rolled back together", func(t *testing.T) {
		defer truncateAll(t)
		ctx := context.Background()

		err := testStore.RunInTx(ctx, func(ctx context.Context) error {
			require.NoError(t, testStore.SetToken(ctx, orgTestToken("TXOUTBOX", testOrgA)))
			require.NoError(t, testStore.AppendOutboxEvents(ctx, outboxTestEvent("token.created")))
			return errStopTx
		})
		require.ErrorIs(t, err, errStopTx)

		requireNoOpenTransaction(t)
		assert.Nil(t, committedToken(t, "TXOUTBOX"))
		assert.Empty(t, relayOutbox(t))
	})

	t.Run("committed together", func(t *testing.T) {
		defer truncateAll(t)
		ctx := context.Background()
		ev := outboxTestEvent("token.created")

		err := testStore.RunInTx(ctx, func(ctx context.Context) error {
			require.NoError(t, testStore.SetToken(ctx, orgTestToken("TXOUTBOX", testOrgA)))
			return testStore.AppendOutboxEvents(ctx, ev)
		})
		require.NoError(t, err)

		requireNoOpenTransaction(t)
		assert.NotNil(t, committedToken(t, "TXOUTBOX"))
		assert.Equal(t, []string{ev.Id}, relayOutbox(t))
	})
}

// A Seq can commit after a higher one. The replay cursor is the published seq, which
// follows the order of publishing, so a reader that pages by it skips nothing.
func TestOutbox_PublishedSeqFollowsTheOrderOfPublishing(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()
	late, early := outboxTestEvent("session.started"), outboxTestEvent("session.updated")

	appended, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- testStore.RunInTx(ctx, func(ctx context.Context) error {
			if err := testStore.AppendOutboxEvents(ctx, late); err != nil {
				return err
			}
			close(appended)
			<-release
			return nil
		})
	}()
	<-appended

	require.NoError(t, testStore.AppendOutboxEvents(ctx, early))
	require.Less(t, late.Seq, early.Seq, "the open transaction took the lower seq")
	assert.Equal(t, []string{early.Id}, relayOutbox(t), "only the committed event can be published")

	close(release)
	require.NoError(t, <-done)
	assert.Equal(t, []string{late.Id}, relayOutbox(t))

	listed, err := testStore.ListOutboxEvents(ctx, store.OutboxEventFilter{})
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, []string{early.Id, late.Id}, []string{listed[0].Id, listed[1].Id})

	// a reader that has seen the early event still gets the late one
	after, err := testStore.ListOutboxEvents(ctx, store.OutboxEventFilter{AfterPublishedSeq: listed[0].PublishedSeq})
	require.NoError(t, err)
	require.Len(t, after, 1)
	assert.Equal(t, late.Id, after[0].Id)
}

func TestOutbox_RelayMarksWhatWasPublishedWhenTheContextIsCancelled(t *testing.T) {
	defer truncateAll(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first, second := outboxTestEvent("session.started"), outboxTestEvent("session.updated")
	require.NoError(t, testStore.AppendOutboxEvents(ctx, first, second))

	// the manager shuts down after the first event went out
	relayed, err := testStore.RelayOutboxEvents(ctx, 10, func(ctx context.Context, events []*store.OutboxEvent) (int, error) {
		cancel()
		return 1, ctx.Err()
	})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, relayed)
	requireNoOpenTransaction(t)

	assert.Equal(t, []string{second.Id}, relayOutbox(t), "the first event should not be published again")
}

func TestOutbox_RelayLeavesNoTransactionOpen(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()
	require.NoError(t, testStore.AppendOutboxEvents(ctx, outboxTestEvent("session.started")))

	_, err := testStore.RelayOutboxEvents(ctx, 10, func(context.Context, []*store.OutboxEvent) (int, error) {
		return 0, errStopTx
	})
	require.ErrorIs(t, err, errStopTx)
	requireNoOpenTransaction(t)

	assert.Len(t, relayOutbox(t), 1)
	requireNoOpenTransaction(t)
}

func TestAppendOutboxEvents_InvalidIdStoresNothing(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()
	valid, invalid := outboxTestEvent("session.started"), outboxTestEvent("session.updated")
	invalid.Id = "not-a-uuid"

	require.Error(t, testStore.AppendOutboxEvents(ctx, valid, invalid))
	requireNoOpenTransaction(t)
	assert.Empty(t, relayOutbox(t), "the events of one call are stored all or none")
}
