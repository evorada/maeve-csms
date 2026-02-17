// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type NotifyDisplayMessagesHandler struct{}

func (h NotifyDisplayMessagesHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	req := request.(*types.NotifyDisplayMessagesRequestJson)
	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.Int("notify_display_messages.request_id", req.RequestId),
		attribute.Int("notify_display_messages.message_count", len(req.MessageInfo)),
	)

	if req.Tbc != nil {
		span.SetAttributes(attribute.Bool("notify_display_messages.tbc", *req.Tbc))
	}

	return &types.NotifyDisplayMessagesResponseJson{}, nil
}
