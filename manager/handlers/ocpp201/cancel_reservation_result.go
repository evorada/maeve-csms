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

type CancelReservationResultHandler struct {
	ReservationStore store.ReservationStore
}

func (h CancelReservationResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.CancelReservationRequestJson)
	resp := response.(*types.CancelReservationResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("cancel_reservation.reservation_id", req.ReservationId),
		attribute.String("cancel_reservation.status", string(resp.Status)),
	)

	if resp.Status == types.CancelReservationStatusEnumTypeAccepted {
		if err := h.ReservationStore.CancelReservation(ctx, req.ReservationId); err != nil {
			return fmt.Errorf("cancelling reservation %d: %w", req.ReservationId, err)
		}

		slog.Info("reservation cancelled",
			"chargeStationId", chargeStationId,
			"reservationId", req.ReservationId,
		)
	} else {
		slog.Warn("reservation cancellation rejected",
			"chargeStationId", chargeStationId,
			"reservationId", req.ReservationId,
			"status", resp.Status,
		)
	}

	return nil
}
