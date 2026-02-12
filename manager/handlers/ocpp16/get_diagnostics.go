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

type GetDiagnosticsHandler struct {
	FirmwareStore store.FirmwareStore
}

func (h GetDiagnosticsHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.GetDiagnosticsJson)
	resp := response.(*types.GetDiagnosticsResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("diagnostics.location", req.Location))

	if req.StartTime != nil {
		span.SetAttributes(attribute.String("diagnostics.start_time", *req.StartTime))
	}
	if req.StopTime != nil {
		span.SetAttributes(attribute.String("diagnostics.stop_time", *req.StopTime))
	}
	if req.Retries != nil {
		span.SetAttributes(attribute.Int("diagnostics.retries", *req.Retries))
	}
	if req.RetryInterval != nil {
		span.SetAttributes(attribute.Int("diagnostics.retry_interval", *req.RetryInterval))
	}
	if resp.FileName != nil {
		span.SetAttributes(attribute.String("diagnostics.file_name", *resp.FileName))
	}

	// Record diagnostics status as uploading
	diagnosticsStatus := &store.DiagnosticsStatus{
		ChargeStationId: chargeStationId,
		Status:          store.DiagnosticsStatusUploading,
		Location:        req.Location,
		UpdatedAt:       time.Now(),
	}

	if err := h.FirmwareStore.SetDiagnosticsStatus(ctx, chargeStationId, diagnosticsStatus); err != nil {
		slog.Error("failed to store diagnostics status",
			"chargeStationId", chargeStationId,
			"error", err)
		return err
	}

	slog.Info("get diagnostics request sent",
		"chargeStationId", chargeStationId,
		"location", req.Location)

	return nil
}
