// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// FirmwareStatusNotificationHandler handles FirmwareStatusNotification messages
// sent by the Charging Station to report progress of a firmware update.
// It persists the status in the store for tracking and audit.
type FirmwareStatusNotificationHandler struct {
	Store store.FirmwareStore
}

func (h FirmwareStatusNotificationHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (response ocpp.Response, err error) {
	req := request.(*ocpp201.FirmwareStatusNotificationRequestJson)

	span := trace.SpanFromContext(ctx)

	span.SetAttributes(attribute.String("firmware_status.status", string(req.Status)))
	if req.RequestId != nil {
		span.SetAttributes(attribute.Int("firmware_status.request_id", *req.RequestId))
	}

	status := &store.FirmwareUpdateStatus{
		ChargeStationId: chargeStationId,
		Status:          store.FirmwareUpdateStatusType(req.Status),
		UpdatedAt:       time.Now().UTC(),
	}

	if h.Store != nil {
		if err = h.Store.SetFirmwareUpdateStatus(ctx, chargeStationId, status); err != nil {
			return nil, err
		}
	}

	return &ocpp201.FirmwareStatusNotificationResponseJson{}, nil
}
