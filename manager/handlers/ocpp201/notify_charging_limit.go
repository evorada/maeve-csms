// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// NotifyChargingLimitHandler handles the NotifyChargingLimit message sent from a
// Charge Station to the CSMS. The CS sends this to inform the CSMS of an external
// charging limit (e.g. from an Energy Management System) that is being applied,
// and optionally includes the resulting charging schedules.
type NotifyChargingLimitHandler struct{}

func (h NotifyChargingLimitHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	req := request.(*ocpp201.NotifyChargingLimitRequestJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("notify_charging_limit.charging_limit_source", string(req.ChargingLimit.ChargingLimitSource)),
	)

	if req.ChargingLimit.IsGridCritical != nil {
		span.SetAttributes(attribute.Bool("notify_charging_limit.is_grid_critical", *req.ChargingLimit.IsGridCritical))
	}

	if req.EvseId != nil {
		span.SetAttributes(attribute.Int("notify_charging_limit.evse_id", *req.EvseId))
	}

	if len(req.ChargingSchedule) > 0 {
		span.SetAttributes(attribute.Int("notify_charging_limit.charging_schedule_count", len(req.ChargingSchedule)))
	}

	return &ocpp201.NotifyChargingLimitResponseJson{}, nil
}
