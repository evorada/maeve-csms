// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"
	"log/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// GetCompositeScheduleResultHandler handles the response from a GetCompositeSchedule request sent to a charge station.
// The CS merges all active charging profiles for the requested EVSE and duration and returns the resulting schedule.
type GetCompositeScheduleResultHandler struct{}

func (h GetCompositeScheduleResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.GetCompositeScheduleRequestJson)
	resp := response.(*types.GetCompositeScheduleResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("get_composite_schedule.evse_id", req.EvseId),
		attribute.Int("get_composite_schedule.duration", req.Duration),
		attribute.String("get_composite_schedule.status", string(resp.Status)),
	)

	if req.ChargingRateUnit != nil {
		span.SetAttributes(attribute.String("get_composite_schedule.charging_rate_unit", string(*req.ChargingRateUnit)))
	}

	switch resp.Status {
	case types.GenericStatusEnumTypeAccepted:
		slog.Info("get composite schedule accepted",
			"chargeStationId", chargeStationId,
			"evseId", req.EvseId,
			"duration", req.Duration,
		)

		if resp.Schedule != nil {
			periodCount := len(resp.Schedule.ChargingSchedulePeriod)
			span.SetAttributes(
				attribute.String("get_composite_schedule.schedule_charging_rate_unit", string(resp.Schedule.ChargingRateUnit)),
				attribute.Int("get_composite_schedule.schedule_duration", resp.Schedule.Duration),
				attribute.Int("get_composite_schedule.period_count", periodCount),
				attribute.String("get_composite_schedule.schedule_start", resp.Schedule.ScheduleStart),
			)
			slog.Info("composite schedule details",
				"chargeStationId", chargeStationId,
				"evseId", resp.Schedule.EvseId,
				"scheduleStart", resp.Schedule.ScheduleStart,
				"scheduleDuration", resp.Schedule.Duration,
				"chargingRateUnit", resp.Schedule.ChargingRateUnit,
				"periodCount", periodCount,
			)
		} else {
			slog.Warn("get composite schedule accepted but no schedule returned",
				"chargeStationId", chargeStationId,
				"evseId", req.EvseId,
			)
		}

	case types.GenericStatusEnumTypeRejected:
		slog.Warn("get composite schedule rejected",
			"chargeStationId", chargeStationId,
			"evseId", req.EvseId,
			"duration", req.Duration,
		)
		if resp.StatusInfo != nil {
			span.SetAttributes(attribute.String("get_composite_schedule.reason_code", resp.StatusInfo.ReasonCode))
			slog.Warn("get composite schedule rejection reason",
				"chargeStationId", chargeStationId,
				"reasonCode", resp.StatusInfo.ReasonCode,
			)
		}

	default:
		slog.Warn("get composite schedule: unexpected status",
			"chargeStationId", chargeStationId,
			"evseId", req.EvseId,
			"status", resp.Status,
		)
	}

	return nil
}
