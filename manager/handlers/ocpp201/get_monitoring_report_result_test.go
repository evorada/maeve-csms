// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
)

func TestGetMonitoringReportResultHandler(t *testing.T) {
	handler := ocpp201.GetMonitoringReportResultHandler{}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, `test`)
		defer span.End()

		req := &types.GetMonitoringReportRequestJson{
			RequestId: 73,
		}
		resp := &types.GetMonitoringReportResponseJson{
			Status: types.GenericDeviceModelStatusEnumTypeAccepted,
		}

		err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"get_monitoring_report.request_id": 73,
		"get_monitoring_report.status":     "Accepted",
	})
}
