// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type ReserveNowHandler struct {
	ReservationStore store.ReservationStore
}

func (h ReserveNowHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*ocpp16.ReserveNowJson)
	resp := response.(*ocpp16.ReserveNowResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("reserve_now.reservation_id", req.ReservationId),
		attribute.Int("reserve_now.connector_id", req.ConnectorId),
		attribute.String("reserve_now.id_tag", req.IdTag),
		attribute.String("reserve_now.expiry_date", req.ExpiryDate),
		attribute.String("reserve_now.status", string(resp.Status)),
	)

	if req.ParentIdTag != nil {
		span.SetAttributes(attribute.String("reserve_now.parent_id_tag", *req.ParentIdTag))
	}

	if resp.Status == ocpp16.ReserveNowResponseJsonStatusAccepted {
		expiryDate, err := time.Parse(time.RFC3339, req.ExpiryDate)
		if err != nil {
			return fmt.Errorf("parsing expiry date %q: %w", req.ExpiryDate, err)
		}

		reservation := &store.Reservation{
			ReservationId:   req.ReservationId,
			ChargeStationId: chargeStationId,
			ConnectorId:     req.ConnectorId,
			IdTag:           req.IdTag,
			ParentIdTag:     req.ParentIdTag,
			ExpiryDate:      expiryDate,
			Status:          store.ReservationStatusAccepted,
			CreatedAt:       time.Now(),
		}

		if err := h.ReservationStore.CreateReservation(ctx, reservation); err != nil {
			return fmt.Errorf("creating reservation: %w", err)
		}

		slog.Info("reservation accepted",
			"chargeStationId", chargeStationId,
			"reservationId", req.ReservationId,
			"connectorId", req.ConnectorId,
			"idTag", req.IdTag,
			"expiryDate", req.ExpiryDate)
	} else {
		slog.Warn("reservation not accepted",
			"chargeStationId", chargeStationId,
			"reservationId", req.ReservationId,
			"connectorId", req.ConnectorId,
			"idTag", req.IdTag,
			"status", resp.Status)
	}

	return nil
}
