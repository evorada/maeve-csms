// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"k8s.io/utils/clock"
)

func TestGetLogResultHandler(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.GetLogResultHandler{Store: memStore}

	tracer, exporter := testutil.GetTracer()
	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		filename := "diag.log"
		req := &types.GetLogRequestJson{
			LogType:   types.LogEnumTypeDiagnosticsLog,
			RequestId: 42,
			Log: types.LogParametersType{
				RemoteLocation: "https://example.com/logs",
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
		"get_log.remote_location": "https://example.com/logs",
		"get_log.status":          "Accepted",
		"get_log.filename":        "diag.log",
	})

	diagStatus, err := memStore.GetDiagnosticsStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, diagStatus)
	assert.Equal(t, store.DiagnosticsStatusUploading, diagStatus.Status)
	assert.Equal(t, "https://example.com/logs", diagStatus.Location)
}

func TestGetLogResultHandler_RejectedStoresFailedStatus(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.GetLogResultHandler{Store: memStore}
	ctx := context.Background()

	req := &types.GetLogRequestJson{
		LogType:   types.LogEnumTypeDiagnosticsLog,
		RequestId: 84,
		Log: types.LogParametersType{
			RemoteLocation: "https://example.com/logs/rejected",
		},
	}
	resp := &types.GetLogResponseJson{Status: types.LogStatusEnumTypeRejected}

	err := handler.HandleCallResult(ctx, "cs002", req, resp, nil)
	require.NoError(t, err)

	diagStatus, err := memStore.GetDiagnosticsStatus(ctx, "cs002")
	require.NoError(t, err)
	require.NotNil(t, diagStatus)
	assert.Equal(t, store.DiagnosticsStatusUploadFailed, diagStatus.Status)
	assert.Equal(t, "https://example.com/logs/rejected", diagStatus.Location)
}

func TestGetLogResultHandler_NilStoreDoesNotFail(t *testing.T) {
	handler := ocpp201.GetLogResultHandler{}
	ctx := context.Background()

	req := &types.GetLogRequestJson{
		LogType:   types.LogEnumTypeDiagnosticsLog,
		RequestId: 21,
		Log: types.LogParametersType{RemoteLocation: "https://example.com/logs/nil-store"},
	}
	resp := &types.GetLogResponseJson{Status: types.LogStatusEnumTypeAccepted}

	err := handler.HandleCallResult(ctx, "cs003", req, resp, nil)
	require.NoError(t, err)
}
