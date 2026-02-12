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

type GetLocalListVersionHandler struct{}

func (h GetLocalListVersionHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	resp := response.(*ocpp16.GetLocalListVersionResponseJson)

	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.Int("local_auth_list.version", resp.ListVersion))

	slog.Info("received local list version",
		"chargeStationId", chargeStationId,
		"listVersion", resp.ListVersion)

	return nil
}
