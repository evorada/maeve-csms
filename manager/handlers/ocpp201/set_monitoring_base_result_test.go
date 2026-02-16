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

func TestSetMonitoringBaseResultHandler(t *testing.T) {
	handler := ocpp201.SetMonitoringBaseResultHandler{}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, `test`)
		defer span.End()

		req := &types.SetMonitoringBaseRequestJson{
			MonitoringBase: types.MonitoringBaseEnumTypeFactoryDefault,
		}
		resp := &types.SetMonitoringBaseResponseJson{
			Status: types.GenericDeviceModelStatusEnumTypeAccepted,
		}

		err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"set_monitoring_base.monitoring_base": "FactoryDefault",
		"set_monitoring_base.status":          "Accepted",
	})
}
