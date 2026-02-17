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

// GetChargingProfilesResultHandler handles the response from a GetChargingProfiles request sent to a charge station.
// When Accepted, the CS will follow up with one or more ReportChargingProfiles messages referencing the requestId.
type GetChargingProfilesResultHandler struct{}

func (h GetChargingProfilesResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.GetChargingProfilesRequestJson)
	resp := response.(*types.GetChargingProfilesResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("get_charging_profiles.request_id", req.RequestId),
		attribute.String("get_charging_profiles.status", string(resp.Status)),
	)

	if req.EvseId != nil {
		span.SetAttributes(attribute.Int("get_charging_profiles.evse_id", *req.EvseId))
	}

	switch resp.Status {
	case types.GetChargingProfileStatusEnumTypeAccepted:
		slog.Info("get charging profiles accepted: charge station will send ReportChargingProfiles",
			"chargeStationId", chargeStationId,
			"requestId", req.RequestId,
		)
	case types.GetChargingProfileStatusEnumTypeNoProfiles:
		slog.Info("get charging profiles: no matching profiles on charge station",
			"chargeStationId", chargeStationId,
			"requestId", req.RequestId,
		)
	default:
		slog.Warn("get charging profiles: unexpected status",
			"chargeStationId", chargeStationId,
			"requestId", req.RequestId,
			"status", resp.Status,
		)
	}

	return nil
}
