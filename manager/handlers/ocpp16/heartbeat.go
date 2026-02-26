// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"k8s.io/utils/clock"
)

type HeartbeatHandler struct {
	Clock       clock.PassiveClock
	StatusStore store.StatusStore
}

func (h HeartbeatHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	now := h.Clock.Now()

	// Update last heartbeat timestamp
	if err := h.StatusStore.UpdateHeartbeat(ctx, chargeStationId, now); err != nil {
		// Log but don't fail the heartbeat
	}

	return &types.HeartbeatResponseJson{
		CurrentTime: now.Format(time.RFC3339),
	}, nil
}
