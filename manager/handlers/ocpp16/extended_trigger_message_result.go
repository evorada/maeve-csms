// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"log/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type ExtendedTriggerMessageResultHandler struct{}

func (h ExtendedTriggerMessageResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*ocpp16.ExtendedTriggerMessageJson)
	resp := response.(*ocpp16.ExtendedTriggerMessageResponseJson)

	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.String("extended_trigger.requested_message", string(req.RequestedMessage)),
		attribute.String("extended_trigger.status", string(resp.Status)))

	if req.ConnectorId != nil {
		span.SetAttributes(attribute.Int("extended_trigger.connector_id", *req.ConnectorId))
	}

	if resp.Status == ocpp16.ExtendedTriggerMessageResponseJsonStatusAccepted {
		slog.Info("extended trigger message accepted",
			"chargeStationId", chargeStationId,
			"requestedMessage", req.RequestedMessage)
	} else {
		slog.Warn("extended trigger message not accepted",
			"chargeStationId", chargeStationId,
			"requestedMessage", req.RequestedMessage,
			"status", resp.Status)
	}

	return nil
}
