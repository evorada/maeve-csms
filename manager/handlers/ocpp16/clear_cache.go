// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"log/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
)

type ClearCacheHandler struct{}

func (c ClearCacheHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	resp := response.(*types.ClearCacheResponseJson)

	if resp.Status == types.ClearCacheResponseJsonStatusAccepted {
		slog.Info("clear cache accepted", "chargeStationId", chargeStationId)
	} else {
		slog.Warn("clear cache rejected", "chargeStationId", chargeStationId)
	}
	return nil
}
