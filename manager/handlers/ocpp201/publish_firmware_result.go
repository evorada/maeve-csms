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

// PublishFirmwareResultHandler handles the response from a Local Controller to a
// PublishFirmwareRequest. When the Local Controller accepts the request, the publish
// firmware status is persisted so that subsequent PublishFirmwareStatusNotification
// messages can be correlated to the originating request.
type PublishFirmwareResultHandler struct {
	Store store.FirmwareStore
}

func (h PublishFirmwareResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.PublishFirmwareRequestJson)
	resp := response.(*types.PublishFirmwareResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("publish_firmware.request_id", req.RequestId),
		attribute.String("publish_firmware.location", req.Location),
		attribute.String("publish_firmware.checksum", req.Checksum),
		attribute.String("publish_firmware.status", string(resp.Status)),
	)
	if req.Retries != nil {
		span.SetAttributes(attribute.Int("publish_firmware.retries", *req.Retries))
	}
	if req.RetryInterval != nil {
		span.SetAttributes(attribute.Int("publish_firmware.retry_interval", *req.RetryInterval))
	}

	if resp.Status != types.GenericStatusEnumTypeAccepted {
		slog.Warn("publish firmware request rejected by local controller",
			"chargeStationId", chargeStationId,
			"status", string(resp.Status),
			"requestId", req.RequestId,
			"location", req.Location,
		)
		return nil
	}

	pubStatus := &store.PublishFirmwareStatus{
		ChargeStationId: chargeStationId,
		Status:          store.PublishFirmwareStatusAccepted,
		Location:        req.Location,
		Checksum:        req.Checksum,
		RequestId:       req.RequestId,
		UpdatedAt:       time.Now().UTC(),
	}

	if err := h.Store.SetPublishFirmwareStatus(ctx, chargeStationId, pubStatus); err != nil {
		slog.Error("failed to store publish firmware status after acceptance",
			"chargeStationId", chargeStationId,
			"error", err,
		)
		return err
	}

	slog.Info("publish firmware accepted by local controller",
		"chargeStationId", chargeStationId,
		"location", req.Location,
		"checksum", req.Checksum,
		"requestId", req.RequestId,
	)

	return nil
}
