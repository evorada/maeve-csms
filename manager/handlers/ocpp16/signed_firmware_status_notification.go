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

type SignedFirmwareStatusNotificationHandler struct {
	FirmwareStore store.FirmwareStore
}

func (h SignedFirmwareStatusNotificationHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	req := request.(*types.SignedFirmwareStatusNotificationJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("signed_firmware.status", string(req.Status)))

	if req.RequestId != nil {
		span.SetAttributes(attribute.Int("signed_firmware.request_id", *req.RequestId))
	}

	// Map signed firmware status to store status
	storeStatus, ok := mapSignedFirmwareStatus(req.Status)
	if !ok {
		slog.Warn("unknown signed firmware status received",
			"chargeStationId", chargeStationId,
			"status", req.Status)
		return &types.SignedFirmwareStatusNotificationResponseJson{}, nil
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
		slog.Error("failed to store signed firmware update status",
			"chargeStationId", chargeStationId,
			"error", err)
		return &types.SignedFirmwareStatusNotificationResponseJson{}, err
	}

	slog.Info("signed firmware status notification received",
		"chargeStationId", chargeStationId,
		"status", req.Status,
		"requestId", req.RequestId)

	return &types.SignedFirmwareStatusNotificationResponseJson{}, nil
}

func mapSignedFirmwareStatus(status types.SignedFirmwareStatusNotificationJsonStatus) (store.FirmwareUpdateStatusType, bool) {
	switch status {
	case types.SignedFirmwareStatusNotificationJsonStatusDownloaded:
		return store.FirmwareUpdateStatusDownloaded, true
	case types.SignedFirmwareStatusNotificationJsonStatusDownloadFailed:
		return store.FirmwareUpdateStatusDownloadFailed, true
	case types.SignedFirmwareStatusNotificationJsonStatusDownloading:
		return store.FirmwareUpdateStatusDownloading, true
	case types.SignedFirmwareStatusNotificationJsonStatusDownloadScheduled:
		return store.FirmwareUpdateStatusDownloadScheduled, true
	case types.SignedFirmwareStatusNotificationJsonStatusDownloadPaused:
		return store.FirmwareUpdateStatusDownloadPaused, true
	case types.SignedFirmwareStatusNotificationJsonStatusIdle:
		return store.FirmwareUpdateStatusIdle, true
	case types.SignedFirmwareStatusNotificationJsonStatusInstallationFailed:
		return store.FirmwareUpdateStatusInstallationFailed, true
	case types.SignedFirmwareStatusNotificationJsonStatusInstalling:
		return store.FirmwareUpdateStatusInstalling, true
	case types.SignedFirmwareStatusNotificationJsonStatusInstalled:
		return store.FirmwareUpdateStatusInstalled, true
	case types.SignedFirmwareStatusNotificationJsonStatusInstallRebooting:
		return store.FirmwareUpdateStatusInstallRebooting, true
	case types.SignedFirmwareStatusNotificationJsonStatusInstallScheduled:
		return store.FirmwareUpdateStatusInstallScheduled, true
	case types.SignedFirmwareStatusNotificationJsonStatusInstallVerificationFailed:
		return store.FirmwareUpdateStatusInstallVerificationFailed, true
	case types.SignedFirmwareStatusNotificationJsonStatusInvalidSignature:
		return store.FirmwareUpdateStatusInvalidSignature, true
	case types.SignedFirmwareStatusNotificationJsonStatusSignatureVerified:
		return store.FirmwareUpdateStatusSignatureVerified, true
	default:
		return "", false
	}
}
