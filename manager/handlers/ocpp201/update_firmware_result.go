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

// UpdateFirmwareResultHandler handles the response from a Charging Station to an
// UpdateFirmwareRequest. When the CS accepts the request, the firmware update
// status is persisted as "Downloading" so that subsequent FirmwareStatusNotification
// messages can be correlated to the originating request.
type UpdateFirmwareResultHandler struct {
	Store store.FirmwareStore
}

func (h UpdateFirmwareResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.UpdateFirmwareRequestJson)
	resp := response.(*types.UpdateFirmwareResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("update_firmware.request_id", req.RequestId),
		attribute.String("update_firmware.location", req.Firmware.Location),
		attribute.String("update_firmware.retrieve_date_time", req.Firmware.RetrieveDateTime),
		attribute.String("update_firmware.status", string(resp.Status)),
	)
	if req.Retries != nil {
		span.SetAttributes(attribute.Int("update_firmware.retries", *req.Retries))
	}
	if req.RetryInterval != nil {
		span.SetAttributes(attribute.Int("update_firmware.retry_interval", *req.RetryInterval))
	}

	if resp.Status != types.UpdateFirmwareStatusEnumTypeAccepted {
		slog.Warn("update firmware request rejected by charge station",
			"chargeStationId", chargeStationId,
			"status", string(resp.Status),
			"requestId", req.RequestId,
		)
		return nil
	}

	retrieveDate, err := time.Parse(time.RFC3339, req.Firmware.RetrieveDateTime)
	if err != nil {
		slog.Warn("failed to parse firmware retrieve date time, using current time",
			"chargeStationId", chargeStationId,
			"retrieveDateTime", req.Firmware.RetrieveDateTime,
			"error", err,
		)
		retrieveDate = time.Now().UTC()
	}

	retryCount := 0
	if req.Retries != nil {
		retryCount = *req.Retries
	}

	firmwareStatus := &store.FirmwareUpdateStatus{
		ChargeStationId: chargeStationId,
		Status:          store.FirmwareUpdateStatusDownloading,
		Location:        req.Firmware.Location,
		RetrieveDate:    retrieveDate,
		RetryCount:      retryCount,
		UpdatedAt:       time.Now().UTC(),
	}

	if err := h.Store.SetFirmwareUpdateStatus(ctx, chargeStationId, firmwareStatus); err != nil {
		slog.Error("failed to store firmware update status after acceptance",
			"chargeStationId", chargeStationId,
			"error", err,
		)
		return err
	}

	slog.Info("update firmware accepted by charge station",
		"chargeStationId", chargeStationId,
		"location", req.Firmware.Location,
		"retrieveDateTime", req.Firmware.RetrieveDateTime,
		"requestId", req.RequestId,
	)

	return nil
}
