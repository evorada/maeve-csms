// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type RemoteStopTransactionHandler struct {
	TransactionStore store.TransactionStore
}

func (r RemoteStopTransactionHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*ocpp16.RemoteStopTransactionJson)
	resp := response.(*ocpp16.RemoteStopTransactionResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("request.transactionId", req.TransactionId),
		attribute.String("response.status", string(resp.Status)))

	// Validate transaction if TransactionStore is available
	if r.TransactionStore != nil {
		transactionId := fmt.Sprintf("%d", req.TransactionId)
		tx, err := r.TransactionStore.FindTransaction(ctx, chargeStationId, transactionId)
		if err != nil {
			return fmt.Errorf("find transaction: %w", err)
		}
		if tx == nil {
			slog.Warn("remote stop transaction requested for unknown transaction",
				"chargeStationId", chargeStationId,
				"transactionId", req.TransactionId,
				"status", resp.Status)
		}
	}

	if resp.Status == ocpp16.RemoteStopTransactionResponseJsonStatusAccepted {
		slog.Info("remote stop transaction accepted",
			"chargeStationId", chargeStationId,
			"transactionId", req.TransactionId)
	} else {
		slog.Warn("remote stop transaction rejected",
			"chargeStationId", chargeStationId,
			"transactionId", req.TransactionId,
			"status", resp.Status)
	}

	return nil
}
