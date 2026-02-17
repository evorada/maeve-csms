// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ClearVariableMonitoringResultHandler handles the call result of ClearVariableMonitoring (CSMS -> CS).
type ClearVariableMonitoringResultHandler struct{}

func (h ClearVariableMonitoringResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.ClearVariableMonitoringRequestJson)
	resp := response.(*types.ClearVariableMonitoringResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("clear_variable_monitoring.request_ids", len(req.Id)),
		attribute.Int("clear_variable_monitoring.result_items", len(resp.ClearMonitoringResult)),
	)

	if len(resp.ClearMonitoringResult) > 0 {
		span.SetAttributes(
			attribute.String("clear_variable_monitoring.first_result.status", string(resp.ClearMonitoringResult[0].Status)),
			attribute.Int("clear_variable_monitoring.first_result.id", resp.ClearMonitoringResult[0].Id),
		)
	}

	return nil
}
