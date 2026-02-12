// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type CancelReservationHandler struct {
	ReservationStore store.ReservationStore
}

func (h CancelReservationHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*ocpp16.CancelReservationJson)
	resp := response.(*ocpp16.CancelReservationResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("cancel_reservation.reservation_id", req.ReservationId),
		attribute.String("cancel_reservation.status", string(resp.Status)),
	)

	if resp.Status == ocpp16.CancelReservationResponseJsonStatusAccepted {
		if err := h.ReservationStore.CancelReservation(ctx, req.ReservationId); err != nil {
			return fmt.Errorf("cancelling reservation %d: %w", req.ReservationId, err)
		}

		slog.Info("reservation cancelled",
			"chargeStationId", chargeStationId,
			"reservationId", req.ReservationId)
	} else {
		slog.Warn("reservation cancellation rejected",
			"chargeStationId", chargeStationId,
			"reservationId", req.ReservationId,
			"status", resp.Status)
	}

	return nil
}
