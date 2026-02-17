// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

type StatusNotificationHandler struct {
	Store store.ChargeStationSettingsStore
}

func (h StatusNotificationHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	span := trace.SpanFromContext(ctx)

	req := request.(*types.StatusNotificationRequestJson)

	span.SetAttributes(
		attribute.Int("status.evse_id", req.EvseId),
		attribute.Int("status.connector_id", req.ConnectorId),
		attribute.String("status.connector_status", string(req.ConnectorStatus)))

	statusKey := fmt.Sprintf("ocpp201.connector_status.%d.%d", req.EvseId, req.ConnectorId)
	if err := h.Store.UpdateChargeStationSettings(ctx, chargeStationId, &store.ChargeStationSettings{
		ChargeStationId: chargeStationId,
		Settings: map[string]*store.ChargeStationSetting{
			statusKey: {
				Value:  string(req.ConnectorStatus),
				Status: store.ChargeStationSettingStatusAccepted,
			},
		},
	}); err != nil {
		return nil, fmt.Errorf("persist connector status for %s evse %d connector %d: %w", chargeStationId, req.EvseId, req.ConnectorId, err)
	}

	return &types.StatusNotificationResponseJson{}, nil
}
