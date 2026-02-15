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

// UnpublishFirmwareResultHandler handles the response from a Local Controller to an
// UnpublishFirmwareRequest. When the Local Controller successfully unpublishes the
// firmware (status=Unpublished), the publish firmware status is updated in the store
// to reflect that publishing has stopped.
type UnpublishFirmwareResultHandler struct {
	Store store.FirmwareStore
}

func (h UnpublishFirmwareResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.UnpublishFirmwareRequestJson)
	resp := response.(*types.UnpublishFirmwareResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("unpublish_firmware.checksum", req.Checksum),
		attribute.String("unpublish_firmware.status", string(resp.Status)),
	)

	switch resp.Status {
	case types.UnpublishFirmwareStatusUnpublished:
		// Firmware was successfully unpublished — mark it as idle in the store
		pubStatus := &store.PublishFirmwareStatus{
			ChargeStationId: chargeStationId,
			Status:          store.PublishFirmwareStatusIdle,
			Checksum:        req.Checksum,
			UpdatedAt:       time.Now().UTC(),
		}
		if err := h.Store.SetPublishFirmwareStatus(ctx, chargeStationId, pubStatus); err != nil {
			slog.Error("failed to update publish firmware status after unpublish",
				"chargeStationId", chargeStationId,
				"checksum", req.Checksum,
				"error", err,
			)
			return err
		}
		slog.Info("firmware successfully unpublished by local controller",
			"chargeStationId", chargeStationId,
			"checksum", req.Checksum,
		)

	case types.UnpublishFirmwareStatusDownloadOngoing:
		slog.Warn("unpublish firmware rejected: download ongoing on local controller",
			"chargeStationId", chargeStationId,
			"checksum", req.Checksum,
		)

	case types.UnpublishFirmwareStatusNoFirmware:
		slog.Info("unpublish firmware: no matching firmware on local controller",
			"chargeStationId", chargeStationId,
			"checksum", req.Checksum,
		)
	}

	return nil
}
