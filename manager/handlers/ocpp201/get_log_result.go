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

// GetLogResultHandler handles the call result to GetLog (CSMS -> CS).
// On acceptance, diagnostics upload state is persisted as Uploading and later
// updated by LogStatusNotification.
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

	if req.Log.OldestTimestamp != nil {
		span.SetAttributes(attribute.String("get_log.oldest_timestamp", *req.Log.OldestTimestamp))
	}
	if req.Log.LatestTimestamp != nil {
		span.SetAttributes(attribute.String("get_log.latest_timestamp", *req.Log.LatestTimestamp))
	}
	if req.Retries != nil {
		span.SetAttributes(attribute.Int("get_log.retries", *req.Retries))
	}
	if req.RetryInterval != nil {
		span.SetAttributes(attribute.Int("get_log.retry_interval", *req.RetryInterval))
	}
	if resp.Filename != nil {
		span.SetAttributes(attribute.String("get_log.filename", *resp.Filename))
	}

	if resp.Status == types.LogStatusEnumTypeRejected {
		slog.Warn("get log request rejected by charge station",
			"chargeStationId", chargeStationId,
			"requestId", req.RequestId,
		)
		return nil
	}

	diagnosticsStatus := &store.DiagnosticsStatus{
		ChargeStationId: chargeStationId,
		Status:          store.DiagnosticsStatusUploading,
		Location:        req.Log.RemoteLocation,
		UpdatedAt:       time.Now().UTC(),
	}

	if err := h.Store.SetDiagnosticsStatus(ctx, chargeStationId, diagnosticsStatus); err != nil {
		slog.Error("failed to store diagnostics status after get log acceptance",
			"chargeStationId", chargeStationId,
			"error", err,
		)
		return err
	}

	slog.Info("get log request accepted by charge station",
		"chargeStationId", chargeStationId,
		"requestId", req.RequestId,
		"status", resp.Status,
	)

	return nil
}
