// SPDX-License-Identifier: Apache-2.0

package inmemory_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/clock"

	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/store/storetest"
)

func TestOutboxStore(t *testing.T) {
	storetest.TestOutboxStore(t, func(t *testing.T) store.Engine {
		return inmemory.NewStore(clock.RealClock{})
	})
}

func TestRunInTx_PanicDropsStagedOutboxEvents(t *testing.T) {
	ctx := context.Background()
	s := inmemory.NewStore(clock.RealClock{})

	assert.Panics(t, func() {
		_ = s.RunInTx(ctx, func(ctx context.Context) error {
			require.NoError(t, s.AppendOutboxEvents(ctx, &store.OutboxEvent{
				Id:    "0193b2c4-0000-7000-8000-000000000001",
				OrgId: "0193b2c4-0000-7000-8000-0000000000a1",
				Type:  "session.started",
				Time:  time.Now(),
				Data:  json.RawMessage(`{}`),
			}))
			panic("fn failed")
		})
	})

	relayed, err := s.RelayOutboxEvents(ctx, 10, func(_ context.Context, events []*store.OutboxEvent) (int, error) {
		return len(events), nil
	})
	require.NoError(t, err)
	assert.Equal(t, 0, relayed)
}

func TestAppendOutboxEvents_SeqIsSetWhenTheTransactionEnds(t *testing.T) {
	ctx := context.Background()
	s := inmemory.NewStore(clock.RealClock{})
	inTx := &store.OutboxEvent{Id: "in-tx", Data: json.RawMessage(`{}`)}
	outside := &store.OutboxEvent{Id: "outside", Data: json.RawMessage(`{}`)}

	err := s.RunInTx(ctx, func(ctx context.Context) error {
		require.NoError(t, s.AppendOutboxEvents(ctx, inTx))
		assert.Zero(t, inTx.Seq, "the event is only set aside")
		assert.False(t, inTx.CreatedAt.IsZero())

		// an event appended outside the transaction while it runs is stored at once
		require.NoError(t, s.AppendOutboxEvents(context.Background(), outside))
		assert.Equal(t, int64(1), outside.Seq)
		return nil
	})
	require.NoError(t, err)

	assert.Equal(t, int64(2), inTx.Seq)
}
