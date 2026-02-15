// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// NotifyEVChargingScheduleHandler handles the NotifyEVChargingSchedule message sent
// from a Charge Station to the CSMS. The CS sends this to report the charging schedule
// negotiated with the EV via ISO 15118. The CSMS acknowledges receipt but does not
// necessarily approve the schedule.
type NotifyEVChargingScheduleHandler struct{}

func (h NotifyEVChargingScheduleHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	req := request.(*ocpp201.NotifyEVChargingScheduleRequestJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("notify_ev_charging_schedule.evse_id", req.EvseId),
		attribute.String("notify_ev_charging_schedule.time_base", req.TimeBase),
		attribute.Int("notify_ev_charging_schedule.schedule_id", req.ChargingSchedule.Id),
		attribute.String("notify_ev_charging_schedule.charging_rate_unit", string(req.ChargingSchedule.ChargingRateUnit)),
		attribute.Int("notify_ev_charging_schedule.period_count", len(req.ChargingSchedule.ChargingSchedulePeriod)),
	)

	if req.ChargingSchedule.Duration != nil {
		span.SetAttributes(attribute.Int("notify_ev_charging_schedule.duration", *req.ChargingSchedule.Duration))
	}

	if req.ChargingSchedule.StartSchedule != nil {
		span.SetAttributes(attribute.String("notify_ev_charging_schedule.start_schedule", *req.ChargingSchedule.StartSchedule))
	}

	if req.ChargingSchedule.MinChargingRate != nil {
		span.SetAttributes(attribute.Float64("notify_ev_charging_schedule.min_charging_rate", *req.ChargingSchedule.MinChargingRate))
	}

	return &ocpp201.NotifyEVChargingScheduleResponseJson{
		Status: ocpp201.GenericStatusEnumTypeAccepted,
	}, nil
}
