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

func TestClearVariableMonitoringResultHandler(t *testing.T) {
	handler := ocpp201.ClearVariableMonitoringResultHandler{}

	tracer, exporter := testutil.GetTracer()
	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.ClearVariableMonitoringRequestJson{
			Id: []int{101, 102},
		}

		resp := &types.ClearVariableMonitoringResponseJson{
			ClearMonitoringResult: []types.ClearMonitoringResultType{
				{
					Id:     101,
					Status: types.ClearMonitoringStatusEnumTypeAccepted,
				},
				{
					Id:     102,
					Status: types.ClearMonitoringStatusEnumTypeNotFound,
				},
			},
		}

		err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"clear_variable_monitoring.request_ids":         2,
		"clear_variable_monitoring.result_items":        2,
		"clear_variable_monitoring.first_result.status": "Accepted",
		"clear_variable_monitoring.first_result.id":     101,
	})
}
