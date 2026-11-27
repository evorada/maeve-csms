// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type RemoteStartTransactionHandler struct {
	TokenStore store.TokenStore
	// OrgResolver gives the org whose tokens are looked up. It is needed when
	// TokenStore is set.
	OrgResolver tenancy.StationOrgResolver
}

func (r RemoteStartTransactionHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*ocpp16.RemoteStartTransactionJson)
	resp := response.(*ocpp16.RemoteStartTransactionResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("request.idTag", req.IdTag),
		attribute.String("response.status", string(resp.Status)))

	if req.ConnectorId != nil {
		span.SetAttributes(attribute.Int("request.connectorId", *req.ConnectorId))
	}

	// Validate token if TokenStore is available
	if r.TokenStore != nil {
		orgId, err := r.OrgResolver.OrgIdForStation(ctx, chargeStationId)
		if err != nil {
			return fmt.Errorf("lookup token: %w", err)
		}
		tok, err := r.TokenStore.LookupTokenForOrg(ctx, orgId, req.IdTag)
		if err != nil {
			return fmt.Errorf("lookup token: %w", err)
		}
		if tok == nil {
			slog.Warn("remote start transaction requested with unknown token",
				"chargeStationId", chargeStationId,
				"idTag", req.IdTag,
				"status", resp.Status)
		} else if !tok.Valid {
			slog.Warn("remote start transaction requested with invalid token",
				"chargeStationId", chargeStationId,
				"idTag", req.IdTag,
				"status", resp.Status)
		}
	}

	if resp.Status == ocpp16.RemoteStartTransactionResponseJsonStatusAccepted {
		logAttrs := []any{
			"chargeStationId", chargeStationId,
			"idTag", req.IdTag,
		}
		if req.ConnectorId != nil {
			logAttrs = append(logAttrs, "connectorId", *req.ConnectorId)
		}
		if req.ChargingProfile != nil {
			logAttrs = append(logAttrs, "chargingProfileId", req.ChargingProfile.ChargingProfileId)
		}
		slog.Info("remote start transaction accepted", logAttrs...)
	} else {
		logAttrs := []any{
			"chargeStationId", chargeStationId,
			"idTag", req.IdTag,
			"status", resp.Status,
		}
		if req.ConnectorId != nil {
			logAttrs = append(logAttrs, "connectorId", *req.ConnectorId)
		}
		slog.Warn("remote start transaction rejected", logAttrs...)
	}

	return nil
}
