// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
)

// CostUpdatedResultHandler handles the CallResult for the CostUpdated CSMS-to-CS message.
// CostUpdated is sent by the CSMS to update the running cost on the charge station's display.
// The charge station responds with an empty body to acknowledge receipt.
type CostUpdatedResultHandler struct {
	Store store.Engine
}

func (h CostUpdatedResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.CostUpdatedRequestJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("cost_updated.transaction_id", req.TransactionId),
		attribute.Float64("cost_updated.total_cost", req.TotalCost))

	slog.Info("cost update acknowledged by charge station",
		slog.String("chargeStationId", chargeStationId),
		slog.String("transactionId", req.TransactionId),
		slog.Float64("totalCost", req.TotalCost))

	if err := h.Store.UpdateTransactionCost(ctx, chargeStationId, req.TransactionId, req.TotalCost); err != nil {
		return err
	}

	return nil
}
