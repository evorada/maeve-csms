// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	clockTest "k8s.io/utils/clock/testing"
)

func TestCostUpdatedResultHandler_StoresCostAndTraces(t *testing.T) {
	now := time.Now()
	engine := inmemory.NewStore(clockTest.NewFakePassiveClock(now))

	handler := ocpp201.CostUpdatedResultHandler{Store: engine}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.CostUpdatedRequestJson{
			TransactionId: "tx-001",
			TotalCost:     12.50,
		}
		resp := &types.CostUpdatedResponseJson{}

		err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
		require.NoError(t, err)
	}()

	// Verify trace attributes
	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"cost_updated.transaction_id": "tx-001",
		"cost_updated.total_cost":     12.50,
	})

	// Verify the cost was persisted
	transaction, err := engine.FindTransaction(ctx, "cs001", "tx-001")
	require.NoError(t, err)
	require.NotNil(t, transaction)
	require.NotNil(t, transaction.LastCost)
	assert.InDelta(t, 12.50, *transaction.LastCost, 0.001)
}

func TestCostUpdatedResultHandler_UpdatesExistingTransaction(t *testing.T) {
	now := time.Now()
	engine := inmemory.NewStore(clockTest.NewFakePassiveClock(now))

	// Create a transaction first
	ctx := context.Background()
	err := engine.CreateTransaction(ctx, "cs001", "tx-002", "RFID001", "ISO14443", nil, 1, false)
	require.NoError(t, err)

	handler := ocpp201.CostUpdatedResultHandler{Store: engine}

	// Send initial cost
	req := &types.CostUpdatedRequestJson{
		TransactionId: "tx-002",
		TotalCost:     5.00,
	}
	err = handler.HandleCallResult(ctx, "cs001", req, &types.CostUpdatedResponseJson{}, nil)
	require.NoError(t, err)

	// Verify initial cost stored
	transaction, err := engine.FindTransaction(ctx, "cs001", "tx-002")
	require.NoError(t, err)
	require.NotNil(t, transaction.LastCost)
	assert.InDelta(t, 5.00, *transaction.LastCost, 0.001)

	// Send updated cost
	req.TotalCost = 18.75
	err = handler.HandleCallResult(ctx, "cs001", req, &types.CostUpdatedResponseJson{}, nil)
	require.NoError(t, err)

	// Verify updated cost stored
	transaction, err = engine.FindTransaction(ctx, "cs001", "tx-002")
	require.NoError(t, err)
	require.NotNil(t, transaction.LastCost)
	assert.InDelta(t, 18.75, *transaction.LastCost, 0.001)
}

func TestCostUpdatedResultHandler_ZeroCost(t *testing.T) {
	now := time.Now()
	engine := inmemory.NewStore(clockTest.NewFakePassiveClock(now))

	handler := ocpp201.CostUpdatedResultHandler{Store: engine}

	ctx := context.Background()
	req := &types.CostUpdatedRequestJson{
		TransactionId: "tx-003",
		TotalCost:     0.0,
	}
	err := handler.HandleCallResult(ctx, "cs001", req, &types.CostUpdatedResponseJson{}, nil)
	require.NoError(t, err)

	transaction, err := engine.FindTransaction(ctx, "cs001", "tx-003")
	require.NoError(t, err)
	require.NotNil(t, transaction)
	require.NotNil(t, transaction.LastCost)
	assert.InDelta(t, 0.0, *transaction.LastCost, 0.001)
}
