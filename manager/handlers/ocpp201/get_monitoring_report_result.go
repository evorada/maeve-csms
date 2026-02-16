// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// GetMonitoringReportResultHandler handles the call result of GetMonitoringReport (CSMS -> CS).
type GetMonitoringReportResultHandler struct{}

func (h GetMonitoringReportResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.GetMonitoringReportRequestJson)
	resp := response.(*types.GetMonitoringReportResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("get_monitoring_report.request_id", req.RequestId),
		attribute.String("get_monitoring_report.status", string(resp.Status)),
	)

	return nil
}
