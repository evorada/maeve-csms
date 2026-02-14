// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ClearedChargingLimitHandler handles the ClearedChargingLimit message sent from a
// Charge Station to the CSMS. The CS sends this when an external charging limit
// (e.g. from an Energy Management System) that was previously applied has been cleared.
type ClearedChargingLimitHandler struct{}

func (h ClearedChargingLimitHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	req := request.(*ocpp201.ClearedChargingLimitRequestJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("cleared_charging_limit.charging_limit_source", string(req.ChargingLimitSource)),
	)
	if req.EvseId != nil {
		span.SetAttributes(attribute.Int("cleared_charging_limit.evse_id", *req.EvseId))
	}

	return &ocpp201.ClearedChargingLimitResponseJson{}, nil
}
