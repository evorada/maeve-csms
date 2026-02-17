// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SetVariableMonitoringResultHandler handles the call result of SetVariableMonitoring (CSMS -> CS).
type SetVariableMonitoringResultHandler struct{}

func (h SetVariableMonitoringResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.SetVariableMonitoringRequestJson)
	resp := response.(*types.SetVariableMonitoringResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("set_variable_monitoring.request_items", len(req.SetMonitoringData)),
		attribute.Int("set_variable_monitoring.result_items", len(resp.SetMonitoringResult)),
	)

	if len(resp.SetMonitoringResult) > 0 {
		span.SetAttributes(
			attribute.String("set_variable_monitoring.first_result.status", string(resp.SetMonitoringResult[0].Status)),
			attribute.String("set_variable_monitoring.first_result.type", string(resp.SetMonitoringResult[0].Type)),
			attribute.String("set_variable_monitoring.first_result.component", resp.SetMonitoringResult[0].Component.Name),
			attribute.String("set_variable_monitoring.first_result.variable", resp.SetMonitoringResult[0].Variable.Name),
			attribute.Int("set_variable_monitoring.first_result.severity", resp.SetMonitoringResult[0].Severity),
		)
	}

	return nil
}
