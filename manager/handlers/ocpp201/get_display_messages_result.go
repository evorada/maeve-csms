// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type GetDisplayMessagesResultHandler struct{}

func (h GetDisplayMessagesResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.GetDisplayMessagesRequestJson)
	resp := response.(*types.GetDisplayMessagesResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("get_display_messages.request_id", req.RequestId),
		attribute.String("get_display_messages.status", string(resp.Status)),
	)

	if req.Priority != nil {
		span.SetAttributes(attribute.String("get_display_messages.priority", string(*req.Priority)))
	}
	if req.State != nil {
		span.SetAttributes(attribute.String("get_display_messages.state", string(*req.State)))
	}
	if len(req.Id) > 0 {
		span.SetAttributes(attribute.Int("get_display_messages.id_count", len(req.Id)))
	}

	return nil
}
