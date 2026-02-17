// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SetDisplayMessageResultHandler handles responses to SetDisplayMessage requests.
type SetDisplayMessageResultHandler struct{}

func (h SetDisplayMessageResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.SetDisplayMessageRequestJson)
	resp := response.(*types.SetDisplayMessageResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("set_display_message.message_id", req.Message.Id),
		attribute.String("set_display_message.priority", string(req.Message.Priority)),
		attribute.String("set_display_message.status", string(resp.Status)),
	)

	if req.Message.Display != nil {
		span.SetAttributes(attribute.String("set_display_message.component", req.Message.Display.Name))
	}

	return nil
}
