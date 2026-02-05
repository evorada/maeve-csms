// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"log/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
)

type ResetHandler struct{}

func (r ResetHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.ResetJson)
	resp := response.(*types.ResetResponseJson)

	if resp.Status == types.ResetResponseJsonStatusAccepted {
		slog.Info("reset accepted", "chargeStationId", chargeStationId, "type", req.Type)
	} else {
		slog.Warn("reset rejected", "chargeStationId", chargeStationId, "type", req.Type)
	}
	return nil
}
