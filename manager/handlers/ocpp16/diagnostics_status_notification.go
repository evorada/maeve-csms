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

type DiagnosticsStatusNotificationHandler struct {
	FirmwareStore store.FirmwareStore
}

func (h DiagnosticsStatusNotificationHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	req := request.(*types.DiagnosticsStatusNotificationJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("diagnostics.status", string(req.Status)))

	// Map OCPP 1.6 diagnostics status to store status
	storeStatus, ok := mapDiagnosticsStatus(req.Status)
	if !ok {
		slog.Warn("unknown diagnostics status received",
			"chargeStationId", chargeStationId,
			"status", req.Status)
		return &types.DiagnosticsStatusNotificationResponseJson{}, nil
	}

	// Get existing status to preserve location info
	existing, err := h.FirmwareStore.GetDiagnosticsStatus(ctx, chargeStationId)
	if err != nil {
		slog.Warn("failed to get existing diagnostics status, creating new entry",
			"chargeStationId", chargeStationId,
			"error", err)
		existing = &store.DiagnosticsStatus{
			ChargeStationId: chargeStationId,
		}
	}

	existing.Status = storeStatus
	existing.UpdatedAt = time.Now()

	if err := h.FirmwareStore.SetDiagnosticsStatus(ctx, chargeStationId, existing); err != nil {
		slog.Error("failed to store diagnostics status",
			"chargeStationId", chargeStationId,
			"error", err)
		return &types.DiagnosticsStatusNotificationResponseJson{}, err
	}

	slog.Info("diagnostics status notification received",
		"chargeStationId", chargeStationId,
		"status", req.Status)

	return &types.DiagnosticsStatusNotificationResponseJson{}, nil
}

func mapDiagnosticsStatus(status types.DiagnosticsStatusNotificationJsonStatus) (store.DiagnosticsStatusType, bool) {
	switch status {
	case types.DiagnosticsStatusNotificationJsonStatusIdle:
		return store.DiagnosticsStatusIdle, true
	case types.DiagnosticsStatusNotificationJsonStatusUploaded:
		return store.DiagnosticsStatusUploaded, true
	case types.DiagnosticsStatusNotificationJsonStatusUploadFailed:
		return store.DiagnosticsStatusUploadFailed, true
	case types.DiagnosticsStatusNotificationJsonStatusUploading:
		return store.DiagnosticsStatusUploading, true
	default:
		return "", false
	}
}
