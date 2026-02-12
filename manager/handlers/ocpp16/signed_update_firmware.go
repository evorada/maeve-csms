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

type SignedUpdateFirmwareHandler struct {
	FirmwareStore store.FirmwareStore
}

func (h SignedUpdateFirmwareHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.SignedUpdateFirmwareJson)
	resp := response.(*types.SignedUpdateFirmwareResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("signed_firmware.request_id", req.RequestId),
		attribute.String("signed_firmware.location", req.Firmware.Location),
		attribute.String("signed_firmware.retrieve_date_time", req.Firmware.RetrieveDateTime),
		attribute.String("signed_firmware.status", string(resp.Status)))

	if req.Retries != nil {
		span.SetAttributes(attribute.Int("signed_firmware.retries", *req.Retries))
	}
	if req.RetryInterval != nil {
		span.SetAttributes(attribute.Int("signed_firmware.retry_interval", *req.RetryInterval))
	}

	if resp.Status != types.SignedUpdateFirmwareResponseJsonStatusAccepted {
		slog.Warn("signed update firmware not accepted",
			"chargeStationId", chargeStationId,
			"requestId", req.RequestId,
			"status", resp.Status)
		return nil
	}

	retrieveDateTime, err := time.Parse(time.RFC3339, req.Firmware.RetrieveDateTime)
	if err != nil {
		slog.Warn("failed to parse retrieve date time, using current time",
			"chargeStationId", chargeStationId,
			"retrieveDateTime", req.Firmware.RetrieveDateTime,
			"error", err)
		retrieveDateTime = time.Now()
	}

	retryCount := 0
	if req.Retries != nil {
		retryCount = *req.Retries
	}

	firmwareStatus := &store.FirmwareUpdateStatus{
		ChargeStationId: chargeStationId,
		Status:          store.FirmwareUpdateStatusDownloading,
		Location:        req.Firmware.Location,
		RetrieveDate:    retrieveDateTime,
		RetryCount:      retryCount,
		UpdatedAt:       time.Now(),
	}

	if err := h.FirmwareStore.SetFirmwareUpdateStatus(ctx, chargeStationId, firmwareStatus); err != nil {
		slog.Error("failed to store signed firmware update status",
			"chargeStationId", chargeStationId,
			"error", err)
		return err
	}

	slog.Info("signed update firmware request accepted",
		"chargeStationId", chargeStationId,
		"requestId", req.RequestId,
		"location", req.Firmware.Location,
		"retrieveDateTime", req.Firmware.RetrieveDateTime)

	return nil
}
