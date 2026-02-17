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

func TestSetVariableMonitoringResultHandler(t *testing.T) {
	handler := ocpp201.SetVariableMonitoringResultHandler{}

	tracer, exporter := testutil.GetTracer()
	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.SetVariableMonitoringRequestJson{
			SetMonitoringData: []types.SetMonitoringDataType{
				{
					Type:     types.MonitorEnumTypeUpperThreshold,
					Value:    32.5,
					Severity: 3,
					Component: types.ComponentType{
						Name: "EVSE",
					},
					Variable: types.VariableType{
						Name: "Current",
					},
				},
			},
		}

		id := 101
		resp := &types.SetVariableMonitoringResponseJson{
			SetMonitoringResult: []types.SetMonitoringResultType{
				{
					Id:       &id,
					Status:   types.SetMonitoringStatusEnumTypeAccepted,
					Type:     types.MonitorEnumTypeUpperThreshold,
					Severity: 3,
					Component: types.ComponentType{
						Name: "EVSE",
					},
					Variable: types.VariableType{
						Name: "Current",
					},
				},
			},
		}

		err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"set_variable_monitoring.request_items":          1,
		"set_variable_monitoring.result_items":           1,
		"set_variable_monitoring.first_result.status":    "Accepted",
		"set_variable_monitoring.first_result.type":      "UpperThreshold",
		"set_variable_monitoring.first_result.component": "EVSE",
		"set_variable_monitoring.first_result.variable":  "Current",
		"set_variable_monitoring.first_result.severity":  3,
	})
}
