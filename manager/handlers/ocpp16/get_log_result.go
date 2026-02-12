// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"log/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type GetLogResultHandler struct{}

func (h GetLogResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.GetLogJson)
	resp := response.(*types.GetLogResponseJson)

	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.String("get_log.log_type", string(req.LogType)),
		attribute.Int("get_log.request_id", req.RequestId),
		attribute.String("get_log.remote_location", req.Log.RemoteLocation),
		attribute.String("get_log.status", string(resp.Status)))

	if resp.Filename != nil {
		span.SetAttributes(attribute.String("get_log.filename", *resp.Filename))
	}

	if resp.Status == types.GetLogResponseJsonStatusAccepted {
		slog.Info("get log accepted",
			"chargeStationId", chargeStationId,
			"logType", req.LogType,
			"requestId", req.RequestId,
			"remoteLocation", req.Log.RemoteLocation)
	} else {
		slog.Warn("get log not accepted",
			"chargeStationId", chargeStationId,
			"logType", req.LogType,
			"requestId", req.RequestId,
			"status", resp.Status)
	}

	return nil
}
