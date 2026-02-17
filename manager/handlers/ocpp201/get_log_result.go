// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"
	"log/slog"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type GetLogResultHandler struct {
	Store store.FirmwareStore
}

func (h GetLogResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.GetLogRequestJson)
	resp := response.(*types.GetLogResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("get_log.log_type", string(req.LogType)),
		attribute.Int("get_log.request_id", req.RequestId),
		attribute.String("get_log.remote_location", req.Log.RemoteLocation),
		attribute.String("get_log.status", string(resp.Status)),
	)

	if resp.Filename != nil {
		span.SetAttributes(attribute.String("get_log.filename", *resp.Filename))
	}

	diagnosticsStatus := &store.DiagnosticsStatus{
		ChargeStationId: chargeStationId,
		Location:        req.Log.RemoteLocation,
		UpdatedAt:       time.Now(),
	}

	switch resp.Status {
	case types.LogStatusEnumTypeAccepted, types.LogStatusEnumTypeAcceptedCanceled:
		diagnosticsStatus.Status = store.DiagnosticsStatusUploading
		slog.Info("get log accepted",
			"chargeStationId", chargeStationId,
			"logType", req.LogType,
			"requestId", req.RequestId,
			"remoteLocation", req.Log.RemoteLocation,
		)
	case types.LogStatusEnumTypeRejected:
		diagnosticsStatus.Status = store.DiagnosticsStatusUploadFailed
		slog.Warn("get log not accepted",
			"chargeStationId", chargeStationId,
			"logType", req.LogType,
			"requestId", req.RequestId,
			"status", resp.Status,
		)
	}

	if h.Store == nil {
		slog.Warn("firmware store is nil, skipping diagnostics status update", "chargeStationId", chargeStationId)
		return nil
	}

	if diagnosticsStatus.Status == "" {
		return nil
	}

	if err := h.Store.SetDiagnosticsStatus(ctx, chargeStationId, diagnosticsStatus); err != nil {
		slog.Error("failed to store diagnostics status after get log result",
			"chargeStationId", chargeStationId,
			"error", err,
		)
		return err
	}

	return nil
}
