// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func TestDisplayMessage_SetAndGet(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	msg := &store.DisplayMessage{
		ChargeStationId: "cs001",
		Id:              1,
		Priority:        store.MessagePriorityNormalCycle,
		Message: store.MessageContent{
			Content: "Hello World",
			Format:  store.MessageFormatASCII,
		},
	}

	err = testStore.SetDisplayMessage(ctx, msg)
	require.NoError(t, err)

	got, err := testStore.GetDisplayMessage(ctx, "cs001", 1)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Hello World", got.Message.Content)
}

func TestDisplayMessage_List(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	for i := 1; i <= 3; i++ {
		err := testStore.SetDisplayMessage(ctx, &store.DisplayMessage{
			ChargeStationId: "cs001",
			Id:              i,
			Priority:        store.MessagePriorityNormalCycle,
			Message: store.MessageContent{
				Content: "Message",
				Format:  store.MessageFormatASCII,
			},
		})
		require.NoError(t, err)
	}

	results, err := testStore.ListDisplayMessages(ctx, "cs001", nil, nil)
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestDisplayMessage_Delete(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	err = testStore.SetDisplayMessage(ctx, &store.DisplayMessage{
		ChargeStationId: "cs001",
		Id:              1,
		Priority:        store.MessagePriorityNormalCycle,
		Message: store.MessageContent{
			Content: "Temp",
			Format:  store.MessageFormatASCII,
		},
	})
	require.NoError(t, err)

	err = testStore.DeleteDisplayMessage(ctx, "cs001", 1)
	require.NoError(t, err)

	got, err := testStore.GetDisplayMessage(ctx, "cs001", 1)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDisplayMessage_DeleteAll(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	for i := 1; i <= 3; i++ {
		err := testStore.SetDisplayMessage(ctx, &store.DisplayMessage{
			ChargeStationId: "cs001",
			Id:              i,
			Priority:        store.MessagePriorityNormalCycle,
			Message: store.MessageContent{
				Content: "Msg",
				Format:  store.MessageFormatASCII,
			},
		})
		require.NoError(t, err)
	}

	err = testStore.DeleteAllDisplayMessages(ctx, "cs001")
	require.NoError(t, err)

	results, err := testStore.ListDisplayMessages(ctx, "cs001", nil, nil)
	require.NoError(t, err)
	assert.Empty(t, results)
}
