// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ClearDisplayMessageResultHandler handles responses to ClearDisplayMessage requests.
type ClearDisplayMessageResultHandler struct{}

func (h ClearDisplayMessageResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.ClearDisplayMessageRequestJson)
	resp := response.(*types.ClearDisplayMessageResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("clear_display_message.id", req.Id),
		attribute.String("clear_display_message.status", string(resp.Status)),
	)

	return nil
}
