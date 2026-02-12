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

type UpdateFirmwareHandler struct {
	FirmwareStore store.FirmwareStore
}

func (h UpdateFirmwareHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.UpdateFirmwareJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("firmware.location", req.Location),
		attribute.String("firmware.retrieve_date", req.RetrieveDate))

	if req.Retries != nil {
		span.SetAttributes(attribute.Int("firmware.retries", *req.Retries))
	}
	if req.RetryInterval != nil {
		span.SetAttributes(attribute.Int("firmware.retry_interval", *req.RetryInterval))
	}

	retrieveDate, err := time.Parse(time.RFC3339, req.RetrieveDate)
	if err != nil {
		slog.Warn("failed to parse retrieve date, using current time",
			"chargeStationId", chargeStationId,
			"retrieveDate", req.RetrieveDate,
			"error", err)
		retrieveDate = time.Now()
	}

	retryCount := 0
	if req.Retries != nil {
		retryCount = *req.Retries
	}

	// Record firmware update status as downloading
	firmwareStatus := &store.FirmwareUpdateStatus{
		ChargeStationId: chargeStationId,
		Status:          store.FirmwareUpdateStatusDownloading,
		Location:        req.Location,
		RetrieveDate:    retrieveDate,
		RetryCount:      retryCount,
		UpdatedAt:       time.Now(),
	}

	if err := h.FirmwareStore.SetFirmwareUpdateStatus(ctx, chargeStationId, firmwareStatus); err != nil {
		slog.Error("failed to store firmware update status",
			"chargeStationId", chargeStationId,
			"error", err)
		return err
	}

	slog.Info("update firmware request sent",
		"chargeStationId", chargeStationId,
		"location", req.Location,
		"retrieveDate", req.RetrieveDate)

	return nil
}
