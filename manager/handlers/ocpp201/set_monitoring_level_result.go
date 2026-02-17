// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SetMonitoringLevelResultHandler handles the call result of SetMonitoringLevel (CSMS -> CS).
type SetMonitoringLevelResultHandler struct{}

func (h SetMonitoringLevelResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.SetMonitoringLevelRequestJson)
	resp := response.(*types.SetMonitoringLevelResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("set_monitoring_level.severity", req.Severity),
		attribute.String("set_monitoring_level.status", string(resp.Status)),
	)

	return nil
}
