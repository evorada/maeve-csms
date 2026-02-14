// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
)

type ResetResultHandler struct{}

func (h ResetResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.ResetRequestJson)
	resp := response.(*types.ResetResponseJson)

	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.String("reset.type", string(req.Type)),
		attribute.String("reset.status", string(resp.Status)))

	// Log reset result with appropriate level
	logAttrs := []any{
		"charge_station_id", chargeStationId,
		"reset_type", string(req.Type),
		"status", string(resp.Status),
	}

	// Add status info if present
	if resp.StatusInfo != nil {
		logAttrs = append(logAttrs, "reason_code", resp.StatusInfo.ReasonCode)
		if resp.StatusInfo.AdditionalInfo != nil {
			logAttrs = append(logAttrs, "additional_info", *resp.StatusInfo.AdditionalInfo)
		}
	}

	switch resp.Status {
	case types.ResetStatusEnumTypeAccepted:
		slog.InfoCtx(ctx, "reset accepted by charge station", logAttrs...)
	case types.ResetStatusEnumTypeScheduled:
		slog.InfoCtx(ctx, "reset scheduled by charge station", logAttrs...)
	case types.ResetStatusEnumTypeRejected:
		slog.WarnCtx(ctx, "reset rejected by charge station", logAttrs...)
	}

	return nil
}
