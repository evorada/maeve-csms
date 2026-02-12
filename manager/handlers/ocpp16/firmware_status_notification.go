// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"log/slog"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type FirmwareStatusNotificationHandler struct {
	FirmwareStore store.FirmwareStore
}

func (h FirmwareStatusNotificationHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	req := request.(*types.FirmwareStatusNotificationJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("firmware.status", string(req.Status)))

	// Map OCPP 1.6 firmware status to store status
	storeStatus, ok := mapFirmwareStatus(req.Status)
	if !ok {
		slog.Warn("unknown firmware status received",
			"chargeStationId", chargeStationId,
			"status", req.Status)
		return &types.FirmwareStatusNotificationResponseJson{}, nil
	}

	// Get existing status to preserve location/retrieve date info
	existing, err := h.FirmwareStore.GetFirmwareUpdateStatus(ctx, chargeStationId)
	if err != nil {
		slog.Warn("failed to get existing firmware status, creating new entry",
			"chargeStationId", chargeStationId,
			"error", err)
		existing = &store.FirmwareUpdateStatus{
			ChargeStationId: chargeStationId,
		}
	}

	existing.Status = storeStatus
	existing.UpdatedAt = time.Now()

	if err := h.FirmwareStore.SetFirmwareUpdateStatus(ctx, chargeStationId, existing); err != nil {
		slog.Error("failed to store firmware update status",
			"chargeStationId", chargeStationId,
			"error", err)
		return &types.FirmwareStatusNotificationResponseJson{}, err
	}

	slog.Info("firmware status notification received",
		"chargeStationId", chargeStationId,
		"status", req.Status)

	return &types.FirmwareStatusNotificationResponseJson{}, nil
}

func mapFirmwareStatus(status types.FirmwareStatusNotificationJsonStatus) (store.FirmwareUpdateStatusType, bool) {
	switch status {
	case types.FirmwareStatusNotificationJsonStatusDownloaded:
		return store.FirmwareUpdateStatusDownloaded, true
	case types.FirmwareStatusNotificationJsonStatusDownloadFailed:
		return store.FirmwareUpdateStatusDownloadFailed, true
	case types.FirmwareStatusNotificationJsonStatusDownloading:
		return store.FirmwareUpdateStatusDownloading, true
	case types.FirmwareStatusNotificationJsonStatusIdle:
		return store.FirmwareUpdateStatusIdle, true
	case types.FirmwareStatusNotificationJsonStatusInstallationFailed:
		return store.FirmwareUpdateStatusInstallationFailed, true
	case types.FirmwareStatusNotificationJsonStatusInstalling:
		return store.FirmwareUpdateStatusInstalling, true
	case types.FirmwareStatusNotificationJsonStatusInstalled:
		return store.FirmwareUpdateStatusInstalled, true
	default:
		return "", false
	}
}
