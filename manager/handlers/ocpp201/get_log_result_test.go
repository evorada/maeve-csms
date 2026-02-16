// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	clocktest "k8s.io/utils/clock/testing"
)

func TestGetLogResultHandler(t *testing.T) {
	clock := clocktest.NewFakePassiveClock(time.Now())
	storeEngine := inmemory.NewStore(clock)
	handler := ocpp201.GetLogResultHandler{Store: storeEngine}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, `test`)
		defer span.End()

		filename := "diag-42.log"
		req := &types.GetLogRequestJson{
			LogType:   types.LogEnumTypeDiagnosticsLog,
			RequestId: 42,
			Log: types.LogParametersType{
				RemoteLocation: "sftp://logs.example.com/incoming",
			},
		}
		resp := &types.GetLogResponseJson{
			Status:   types.LogStatusEnumTypeAccepted,
			Filename: &filename,
		}

		err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"get_log.log_type":        "DiagnosticsLog",
		"get_log.request_id":      42,
		"get_log.remote_location": "sftp://logs.example.com/incoming",
		"get_log.status":          "Accepted",
		"get_log.filename":        "diag-42.log",
	})

	diagnosticsStatus, err := storeEngine.GetDiagnosticsStatus(context.Background(), "cs001")
	require.NoError(t, err)
	require.Equal(t, store.DiagnosticsStatusUploading, diagnosticsStatus.Status)
	require.Equal(t, "sftp://logs.example.com/incoming", diagnosticsStatus.Location)
}
