// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type ReservationStatusUpdateHandler struct {
	ReservationStore store.ReservationStore
}

func (h ReservationStatusUpdateHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	req := request.(*types.ReservationStatusUpdateRequestJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("reservation_status_update.id", req.ReservationId),
		attribute.String("reservation_status_update.status", string(req.ReservationUpdateStatus)),
	)

	if h.ReservationStore == nil {
		return &types.ReservationStatusUpdateResponseJson{}, nil
	}

	status, err := mapReservationUpdateStatus(req.ReservationUpdateStatus)
	if err != nil {
		slog.Warn("unknown reservation update status", "chargeStationId", chargeStationId, "status", req.ReservationUpdateStatus)
		return &types.ReservationStatusUpdateResponseJson{}, nil
	}

	if err := h.ReservationStore.UpdateReservationStatus(ctx, req.ReservationId, status); err != nil {
		slog.Warn("failed to update reservation status",
			"chargeStationId", chargeStationId,
			"reservationId", req.ReservationId,
			"status", status,
			"error", err,
		)
	}

	return &types.ReservationStatusUpdateResponseJson{}, nil
}

func mapReservationUpdateStatus(status types.ReservationUpdateStatusEnumType) (store.ReservationStatus, error) {
	switch status {
	case types.ReservationUpdateStatusEnumTypeExpired:
		return store.ReservationStatusExpired, nil
	case types.ReservationUpdateStatusEnumTypeRemoved:
		return store.ReservationStatusCancelled, nil
	default:
		return "", fmt.Errorf("unsupported reservation update status: %s", status)
	}
}
