// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"
	"log/slog"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type LogStatusNotificationHandler struct {
	Store store.FirmwareStore
}

func (h LogStatusNotificationHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (response ocpp.Response, err error) {
	req := request.(*ocpp201.LogStatusNotificationRequestJson)

	span := trace.SpanFromContext(ctx)

	span.SetAttributes(attribute.String("log_status.status", string(req.Status)))
	if req.RequestId != nil {
		span.SetAttributes(attribute.Int("log_status.request_id", *req.RequestId))
	}

	// Map OCPP 2.0.1 UploadLogStatusEnumType to store DiagnosticsStatusType
	storeStatus, ok := mapLogUploadStatus(req.Status)
	if !ok {
		slog.Warn("unknown log upload status received",
			"chargeStationId", chargeStationId,
			"status", req.Status)
		return &ocpp201.LogStatusNotificationResponseJson{}, nil
	}

	if h.Store != nil {
		// Get existing status to preserve location info
		existing, err := h.Store.GetDiagnosticsStatus(ctx, chargeStationId)
		if err != nil {
			slog.Warn("failed to get existing log upload status, creating new entry",
				"chargeStationId", chargeStationId,
				"error", err)
			existing = nil
		}
		if existing == nil {
			existing = &store.DiagnosticsStatus{
				ChargeStationId: chargeStationId,
			}
		}

		existing.Status = storeStatus
		existing.UpdatedAt = time.Now()

		if err := h.Store.SetDiagnosticsStatus(ctx, chargeStationId, existing); err != nil {
			slog.Error("failed to store log upload status",
				"chargeStationId", chargeStationId,
				"error", err)
			return &ocpp201.LogStatusNotificationResponseJson{}, err
		}
	}

	slog.Info("log status notification received",
		"chargeStationId", chargeStationId,
		"status", req.Status)

	return &ocpp201.LogStatusNotificationResponseJson{}, nil
}

// mapLogUploadStatus maps OCPP 2.0.1 UploadLogStatusEnumType to store.DiagnosticsStatusType.
// OCPP 2.0.1 LogStatusNotification replaces OCPP 1.6 DiagnosticsStatusNotification.
func mapLogUploadStatus(status ocpp201.UploadLogStatusEnumType) (store.DiagnosticsStatusType, bool) {
	switch status {
	case ocpp201.UploadLogStatusEnumTypeIdle:
		return store.DiagnosticsStatusIdle, true
	case ocpp201.UploadLogStatusEnumTypeUploaded:
		return store.DiagnosticsStatusUploaded, true
	case ocpp201.UploadLogStatusEnumTypeUploadFailure,
		ocpp201.UploadLogStatusEnumTypeBadMessage,
		ocpp201.UploadLogStatusEnumTypeNotSupportedOperation,
		ocpp201.UploadLogStatusEnumTypePermissionDenied:
		return store.DiagnosticsStatusUploadFailed, true
	case ocpp201.UploadLogStatusEnumTypeUploading:
		return store.DiagnosticsStatusUploading, true
	case ocpp201.UploadLogStatusEnumTypeAcceptedCanceled:
		return store.DiagnosticsStatusIdle, true
	default:
		return "", false
	}
}
