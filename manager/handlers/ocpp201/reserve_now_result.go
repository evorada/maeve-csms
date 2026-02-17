// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type ReserveNowResultHandler struct {
	ReservationStore store.ReservationStore
}

func (h ReserveNowResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.ReserveNowRequestJson)
	resp := response.(*types.ReserveNowResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("reserve_now.id", req.Id),
		attribute.String("reserve_now.expiry_date_time", req.ExpiryDateTime),
		attribute.String("reserve_now.id_token", req.IdToken.IdToken),
		attribute.String("reserve_now.id_token_type", string(req.IdToken.Type)),
		attribute.String("reserve_now.status", string(resp.Status)),
	)
	if req.EvseId != nil {
		span.SetAttributes(attribute.Int("reserve_now.evse_id", *req.EvseId))
	}
	if req.GroupIdToken != nil {
		span.SetAttributes(attribute.String("reserve_now.group_id_token", req.GroupIdToken.IdToken))
	}
	if req.ConnectorType != nil {
		span.SetAttributes(attribute.String("reserve_now.connector_type", string(*req.ConnectorType)))
	}

	if resp.Status != types.ReserveNowStatusEnumTypeAccepted {
		slog.Warn("reserve now request not accepted",
			"chargeStationId", chargeStationId,
			"reservationId", req.Id,
			"status", resp.Status,
		)
		return nil
	}

	expiryDateTime, err := time.Parse(time.RFC3339, req.ExpiryDateTime)
	if err != nil {
		return fmt.Errorf("parsing reserve now expiryDateTime %q: %w", req.ExpiryDateTime, err)
	}

	connectorId := 0
	if req.EvseId != nil {
		connectorId = *req.EvseId
	}

	var parentIdTag *string
	if req.GroupIdToken != nil {
		groupToken := req.GroupIdToken.IdToken
		parentIdTag = &groupToken
	}

	reservation := &store.Reservation{
		ReservationId:   req.Id,
		ChargeStationId: chargeStationId,
		ConnectorId:     connectorId,
		IdTag:           req.IdToken.IdToken,
		ParentIdTag:     parentIdTag,
		ExpiryDate:      expiryDateTime,
		Status:          store.ReservationStatusAccepted,
		CreatedAt:       time.Now().UTC(),
	}

	if err := h.ReservationStore.CreateReservation(ctx, reservation); err != nil {
		return fmt.Errorf("creating reservation %d: %w", req.Id, err)
	}

	slog.Info("reserve now request accepted",
		"chargeStationId", chargeStationId,
		"reservationId", req.Id,
	)

	return nil
}
