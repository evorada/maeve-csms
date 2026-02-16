// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SetMonitoringBaseResultHandler handles the call result of SetMonitoringBase (CSMS -> CS).
type SetMonitoringBaseResultHandler struct{}

func (h SetMonitoringBaseResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.SetMonitoringBaseRequestJson)
	resp := response.(*types.SetMonitoringBaseResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("set_monitoring_base.monitoring_base", string(req.MonitoringBase)),
		attribute.String("set_monitoring_base.status", string(resp.Status)),
	)

	return nil
}
