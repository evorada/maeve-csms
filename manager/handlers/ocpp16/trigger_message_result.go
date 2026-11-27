// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type TriggerMessageResultHandler struct {
	Store store.ChargeStationTriggerMessageStore
}

func (c TriggerMessageResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*ocpp16.TriggerMessageJson)
	resp := response.(*ocpp16.TriggerMessageResponseJson)

	if resp.Status == ocpp16.TriggerMessageResponseJsonStatusAccepted {
		setTriggerSpanAttributes(ctx, req, string(resp.Status))
		return c.Store.DeleteChargeStationTriggerMessage(ctx, chargeStationId)
	}

	return c.setTriggerStatus(ctx, chargeStationId, req, store.TriggerStatus(resp.Status))
}

// HandleCallError records that the charge station refused the TriggerMessage call with a
// CallError: only pending trigger messages are (re)sent, so the call is not repeated.
func (c TriggerMessageResultHandler) HandleCallError(ctx context.Context, chargeStationId string, request ocpp.Request, callError *handlers.CallError, state any) error {
	req, ok := request.(*ocpp16.TriggerMessageJson)
	if !ok || req == nil {
		return fmt.Errorf("no request for call error: %w", callError)
	}

	status := store.TriggerStatusRejected
	if callError.ErrorCode == transport.ErrorNotImplemented || callError.ErrorCode == transport.ErrorNotSupported {
		status = store.TriggerStatusNotImplemented
	}

	return c.setTriggerStatus(ctx, chargeStationId, req, status)
}

func (c TriggerMessageResultHandler) setTriggerStatus(ctx context.Context, chargeStationId string, req *ocpp16.TriggerMessageJson, status store.TriggerStatus) error {
	setTriggerSpanAttributes(ctx, req, string(status))

	return c.Store.SetChargeStationTriggerMessage(ctx, chargeStationId, &store.ChargeStationTriggerMessage{
		ChargeStationId: chargeStationId,
		TriggerMessage:  store.TriggerMessage(req.RequestedMessage),
		ConnectorId:     req.ConnectorId,
		TriggerStatus:   status,
	})
}

func setTriggerSpanAttributes(ctx context.Context, req *ocpp16.TriggerMessageJson, status string) {
	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.String("trigger.requested_message", string(req.RequestedMessage)),
		attribute.String("trigger.status", status))

	if req.ConnectorId != nil {
		span.SetAttributes(attribute.Int("trigger.connector_id", *req.ConnectorId))
	}
}
