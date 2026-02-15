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

// PublishFirmwareStatusNotificationHandler handles PublishFirmwareStatusNotification
// messages sent by a Local Controller to the CSMS to report progress of a firmware
// publishing operation (making firmware available to co-located Charging Stations).
// It persists the latest publish status so the CSMS can track ongoing operations.
type PublishFirmwareStatusNotificationHandler struct {
	Store store.FirmwareStore
}

func (h PublishFirmwareStatusNotificationHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (response ocpp.Response, err error) {
	req := request.(*ocpp201.PublishFirmwareStatusNotificationRequestJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("publish_firmware_status.status", string(req.Status)))
	if req.RequestId != nil {
		span.SetAttributes(attribute.Int("publish_firmware_status.request_id", *req.RequestId))
	}
	if len(req.Location) > 0 {
		span.SetAttributes(attribute.StringSlice("publish_firmware_status.location", req.Location))
	}

	// Retrieve any existing publish firmware record so we can preserve location/checksum/requestId
	existing, err := h.Store.GetPublishFirmwareStatus(ctx, chargeStationId)
	if err != nil {
		// Non-fatal: log and continue with a fresh record
		slog.Warn("failed to retrieve existing publish firmware status",
			"chargeStationId", chargeStationId,
			"error", err,
		)
		existing = nil
	}

	pubStatus := &store.PublishFirmwareStatus{
		ChargeStationId: chargeStationId,
		Status:          store.PublishFirmwareStatusType(req.Status),
		UpdatedAt:       time.Now().UTC(),
	}

	// Carry forward persistent fields from the existing record when not supplied
	// by this notification (the CS omits them after the initial Accepted response).
	if existing != nil {
		pubStatus.Location = existing.Location
		pubStatus.Checksum = existing.Checksum
		pubStatus.RequestId = existing.RequestId
	}

	// If the request carries an explicit requestId, use it (overrides stored value).
	if req.RequestId != nil {
		pubStatus.RequestId = *req.RequestId
	}

	// When the status is Published the spec requires location URIs to be present.
	// Prefer the URIs from this notification when supplied.
	if len(req.Location) > 0 {
		// Use the first URI as the canonical Location string in the store.
		pubStatus.Location = req.Location[0]
	}

	if err = h.Store.SetPublishFirmwareStatus(ctx, chargeStationId, pubStatus); err != nil {
		slog.Error("failed to store publish firmware status notification",
			"chargeStationId", chargeStationId,
			"status", string(req.Status),
			"error", err,
		)
		return nil, err
	}

	slog.Info("publish firmware status updated",
		"chargeStationId", chargeStationId,
		"status", string(req.Status),
	)

	return &ocpp201.PublishFirmwareStatusNotificationResponseJson{}, nil
}
