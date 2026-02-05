// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"log/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
)

type ChangeAvailabilityHandler struct{}

func (c ChangeAvailabilityHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.ChangeAvailabilityJson)
	resp := response.(*types.ChangeAvailabilityResponseJson)

	switch resp.Status {
	case types.ChangeAvailabilityResponseJsonStatusAccepted:
		slog.Info("change availability accepted",
			"chargeStationId", chargeStationId,
			"connectorId", req.ConnectorId,
			"type", req.Type)
	case types.ChangeAvailabilityResponseJsonStatusScheduled:
		slog.Info("change availability scheduled",
			"chargeStationId", chargeStationId,
			"connectorId", req.ConnectorId,
			"type", req.Type)
	case types.ChangeAvailabilityResponseJsonStatusRejected:
		slog.Warn("change availability rejected",
			"chargeStationId", chargeStationId,
			"connectorId", req.ConnectorId,
			"type", req.Type)
	}

	return nil
}
