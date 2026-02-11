// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"log/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type GetCompositeScheduleHandler struct{}

func (h GetCompositeScheduleHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.GetCompositeScheduleJson)
	resp := response.(*types.GetCompositeScheduleResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("request.connectorId", req.ConnectorId),
		attribute.Int("request.duration", req.Duration),
		attribute.String("response.status", string(resp.Status)),
	)

	if req.ChargingRateUnit != nil {
		span.SetAttributes(attribute.String("request.chargingRateUnit", string(*req.ChargingRateUnit)))
	}

	if resp.Status == types.GetCompositeScheduleResponseJsonStatusAccepted {
		slog.Info("get composite schedule accepted",
			"chargeStationId", chargeStationId,
			"connectorId", req.ConnectorId,
			"duration", req.Duration,
		)

		if resp.ChargingSchedule != nil {
			periodCount := len(resp.ChargingSchedule.ChargingSchedulePeriod)
			span.SetAttributes(
				attribute.String("response.chargingRateUnit", string(resp.ChargingSchedule.ChargingRateUnit)),
				attribute.Int("response.periodCount", periodCount),
			)
			slog.Info("composite schedule details",
				"chargeStationId", chargeStationId,
				"connectorId", req.ConnectorId,
				"chargingRateUnit", resp.ChargingSchedule.ChargingRateUnit,
				"periods", periodCount,
			)
		}
	} else {
		slog.Warn("get composite schedule rejected",
			"chargeStationId", chargeStationId,
			"connectorId", req.ConnectorId,
			"status", resp.Status,
		)
	}

	return nil
}
