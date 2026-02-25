// SPDX-License-Identifier: Apache-2.0

package inmemory_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"k8s.io/utils/clock"
	clockTest "k8s.io/utils/clock/testing"
)

func TestDisplayMessageStore_SetAndGet(t *testing.T) {
	now := time.Now()
	clk := clockTest.NewFakeClock(now)
	s := inmemory.NewStore(clk)

	ctx := context.Background()
	csId := "CS001"
	messageId := 1

	msg := &store.DisplayMessage{
		ChargeStationId: csId,
		Id:              messageId,
		Priority:        store.MessagePriorityInFront,
		Message: store.MessageContent{
			Content: "Welcome!",
			Format:  store.MessageFormatASCII,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Set message
	err := s.SetDisplayMessage(ctx, msg)
	require.NoError(t, err)

	// Get message
	retrieved, err := s.GetDisplayMessage(ctx, csId, messageId)
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	assert.Equal(t, csId, retrieved.ChargeStationId)
	assert.Equal(t, messageId, retrieved.Id)
	assert.Equal(t, store.MessagePriorityInFront, retrieved.Priority)
	assert.Equal(t, "Welcome!", retrieved.Message.Content)
	assert.Equal(t, store.MessageFormatASCII, retrieved.Message.Format)
}

func TestDisplayMessageStore_List(t *testing.T) {
	now := time.Now()
	clk := clockTest.NewFakeClock(now)
	s := inmemory.NewStore(clk)

	ctx := context.Background()
	csId := "CS001"

	state := store.MessageStateIdle
	msg1 := &store.DisplayMessage{
		ChargeStationId: csId,
		Id:              1,
		Priority:        store.MessagePriorityInFront,
		State:           &state,
		Message: store.MessageContent{
			Content: "Message 1",
			Format:  store.MessageFormatASCII,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	msg2 := &store.DisplayMessage{
		ChargeStationId: csId,
		Id:              2,
		Priority:        store.MessagePriorityNormalCycle,
		State:           &state,
		Message: store.MessageContent{
			Content: "Message 2",
			Format:  store.MessageFormatHTML,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	require.NoError(t, s.SetDisplayMessage(ctx, msg1))
	require.NoError(t, s.SetDisplayMessage(ctx, msg2))

	// List all messages
	messages, err := s.ListDisplayMessages(ctx, csId, nil, nil)
	require.NoError(t, err)
	assert.Len(t, messages, 2)

	// Filter by state
	messages, err = s.ListDisplayMessages(ctx, csId, &state, nil)
	require.NoError(t, err)
	assert.Len(t, messages, 2)

	// Filter by priority
	priority := store.MessagePriorityInFront
	messages, err = s.ListDisplayMessages(ctx, csId, nil, &priority)
	require.NoError(t, err)
	assert.Len(t, messages, 1)
	assert.Equal(t, "Message 1", messages[0].Message.Content)

	// Filter by both
	messages, err = s.ListDisplayMessages(ctx, csId, &state, &priority)
	require.NoError(t, err)
	assert.Len(t, messages, 1)
}

func TestDisplayMessageStore_Delete(t *testing.T) {
	now := time.Now()
	clk := clockTest.NewFakeClock(now)
	s := inmemory.NewStore(clk)

	ctx := context.Background()
	csId := "CS001"

	msg := &store.DisplayMessage{
		ChargeStationId: csId,
		Id:              1,
		Priority:        store.MessagePriorityNormalCycle,
		Message: store.MessageContent{
			Content: "Test",
			Format:  store.MessageFormatASCII,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	require.NoError(t, s.SetDisplayMessage(ctx, msg))

	// Delete message
	err := s.DeleteDisplayMessage(ctx, csId, 1)
	require.NoError(t, err)

	// Verify deleted
	retrieved, err := s.GetDisplayMessage(ctx, csId, 1)
	require.NoError(t, err)
	assert.Nil(t, retrieved)
}

func TestDisplayMessageStore_DeleteAll(t *testing.T) {
	now := time.Now()
	clk := clockTest.NewFakeClock(now)
	s := inmemory.NewStore(clk)

	ctx := context.Background()
	csId := "CS001"

	for i := 1; i <= 3; i++ {
		msg := &store.DisplayMessage{
			ChargeStationId: csId,
			Id:              i,
			Priority:        store.MessagePriorityNormalCycle,
			Message: store.MessageContent{
				Content: "Test",
				Format:  store.MessageFormatASCII,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
		require.NoError(t, s.SetDisplayMessage(ctx, msg))
	}

	// Delete all
	err := s.DeleteAllDisplayMessages(ctx, csId)
	require.NoError(t, err)

	// Verify all deleted
	messages, err := s.ListDisplayMessages(ctx, csId, nil, nil)
	require.NoError(t, err)
	assert.Len(t, messages, 0)
}

var _ store.Engine = (*inmemory.Store)(nil)
var _ clock.PassiveClock = (*clockTest.FakeClock)(nil)
