// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"log/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
)

type UnlockConnectorHandler struct{}

func (u UnlockConnectorHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.UnlockConnectorJson)
	resp := response.(*types.UnlockConnectorResponseJson)

	if resp.Status == types.UnlockConnectorResponseJsonStatusUnlocked {
		slog.Info("unlock connector succeeded", "chargeStationId", chargeStationId, "connectorId", req.ConnectorId)
	} else if resp.Status == types.UnlockConnectorResponseJsonStatusUnlockFailed {
		slog.Warn("unlock connector failed", "chargeStationId", chargeStationId, "connectorId", req.ConnectorId)
	} else {
		slog.Warn("unlock connector not supported", "chargeStationId", chargeStationId, "connectorId", req.ConnectorId)
	}
	return nil
}
