// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type ClearChargingProfileHandler struct {
	ChargingProfileStore store.ChargingProfileStore
}

func (h ClearChargingProfileHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.ClearChargingProfileJson)
	resp := response.(*types.ClearChargingProfileResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("response.status", string(resp.Status)),
	)

	if req.Id != nil {
		span.SetAttributes(attribute.Int("request.id", *req.Id))
	}
	if req.ConnectorId != nil {
		span.SetAttributes(attribute.Int("request.connectorId", *req.ConnectorId))
	}
	if req.ChargingProfilePurpose != nil {
		span.SetAttributes(attribute.String("request.chargingProfilePurpose", string(*req.ChargingProfilePurpose)))
	}
	if req.StackLevel != nil {
		span.SetAttributes(attribute.Int("request.stackLevel", *req.StackLevel))
	}

	if resp.Status == types.ClearChargingProfileResponseJsonStatusAccepted {
		slog.Info("clear charging profile accepted",
			"chargeStationId", chargeStationId,
		)

		if h.ChargingProfileStore != nil {
			var purpose *store.ChargingProfilePurpose
			if req.ChargingProfilePurpose != nil {
				p := store.ChargingProfilePurpose(*req.ChargingProfilePurpose)
				purpose = &p
			}

			cleared, err := h.ChargingProfileStore.ClearChargingProfile(ctx, chargeStationId, req.Id, req.ConnectorId, purpose, req.StackLevel)
			if err != nil {
				return fmt.Errorf("clearing charging profiles: %w", err)
			}

			slog.Info("charging profiles cleared from store",
				"chargeStationId", chargeStationId,
				"clearedCount", cleared,
			)
		}
	} else {
		slog.Warn("clear charging profile unknown",
			"chargeStationId", chargeStationId,
			"status", resp.Status,
		)
	}

	return nil
}
