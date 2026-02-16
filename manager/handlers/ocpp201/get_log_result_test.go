// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"k8s.io/utils/clock"
)

func TestGetLogResultHandler_Accepted(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.GetLogResultHandler{Store: memStore}

	tracer, exporter := testutil.GetTracer()
	ctx := context.Background()

	oldest := "2026-02-10T00:00:00Z"
	latest := "2026-02-12T00:00:00Z"
	retries := 3
	retryInterval := 60
	fileName := "diagnostics-20260212.log"

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.GetLogRequestJson{
			RequestId: 77,
			LogType:   types.LogEnumTypeDiagnosticsLog,
			Log: types.LogParametersType{
				RemoteLocation:  "https://uploads.example.com/logs",
				OldestTimestamp: &oldest,
				LatestTimestamp: &latest,
			},
			Retries:       &retries,
			RetryInterval: &retryInterval,
		}
		resp := &types.GetLogResponseJson{
			Status:   types.LogStatusEnumTypeAccepted,
			Filename: &fileName,
		}

		err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"get_log.log_type":         "DiagnosticsLog",
		"get_log.request_id":       77,
		"get_log.remote_location":  "https://uploads.example.com/logs",
		"get_log.status":           "Accepted",
		"get_log.oldest_timestamp": "2026-02-10T00:00:00Z",
		"get_log.latest_timestamp": "2026-02-12T00:00:00Z",
		"get_log.retries":          3,
		"get_log.retry_interval":   60,
		"get_log.filename":         "diagnostics-20260212.log",
	})

	status, err := memStore.GetDiagnosticsStatus(context.Background(), "cs001")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "cs001", status.ChargeStationId)
	assert.Equal(t, "Uploading", string(status.Status))
	assert.Equal(t, "https://uploads.example.com/logs", status.Location)
}

func TestGetLogResultHandler_AcceptedCanceled(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.GetLogResultHandler{Store: memStore}

	req := &types.GetLogRequestJson{
		RequestId: 88,
		LogType:   types.LogEnumTypeDiagnosticsLog,
		Log: types.LogParametersType{
			RemoteLocation: "https://uploads.example.com/logs",
		},
	}
	resp := &types.GetLogResponseJson{Status: types.LogStatusEnumTypeAcceptedCanceled}

	err := handler.HandleCallResult(context.Background(), "cs002", req, resp, nil)
	require.NoError(t, err)

	status, err := memStore.GetDiagnosticsStatus(context.Background(), "cs002")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "Uploading", string(status.Status))
}

func TestGetLogResultHandler_Rejected(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.GetLogResultHandler{Store: memStore}

	req := &types.GetLogRequestJson{
		RequestId: 99,
		LogType:   types.LogEnumTypeSecurityLog,
		Log: types.LogParametersType{
			RemoteLocation: "https://uploads.example.com/security",
		},
	}
	resp := &types.GetLogResponseJson{Status: types.LogStatusEnumTypeRejected}

	err := handler.HandleCallResult(context.Background(), "cs003", req, resp, nil)
	require.NoError(t, err)

	status, err := memStore.GetDiagnosticsStatus(context.Background(), "cs003")
	require.NoError(t, err)
	assert.Nil(t, status)
}

func TestGetLogResultHandler_MultipleStations(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.GetLogResultHandler{Store: memStore}

	req1 := &types.GetLogRequestJson{
		RequestId: 1,
		LogType:   types.LogEnumTypeDiagnosticsLog,
		Log:       types.LogParametersType{RemoteLocation: "https://a.example.com/logs"},
	}
	resp1 := &types.GetLogResponseJson{Status: types.LogStatusEnumTypeAccepted}

	req2 := &types.GetLogRequestJson{
		RequestId: 2,
		LogType:   types.LogEnumTypeSecurityLog,
		Log:       types.LogParametersType{RemoteLocation: "https://b.example.com/security"},
	}
	resp2 := &types.GetLogResponseJson{Status: types.LogStatusEnumTypeAccepted}

	err := handler.HandleCallResult(context.Background(), "csA", req1, resp1, nil)
	require.NoError(t, err)
	err = handler.HandleCallResult(context.Background(), "csB", req2, resp2, nil)
	require.NoError(t, err)

	statusA, err := memStore.GetDiagnosticsStatus(context.Background(), "csA")
	require.NoError(t, err)
	statusB, err := memStore.GetDiagnosticsStatus(context.Background(), "csB")
	require.NoError(t, err)

	require.NotNil(t, statusA)
	require.NotNil(t, statusB)
	assert.Equal(t, "https://a.example.com/logs", statusA.Location)
	assert.Equal(t, "https://b.example.com/security", statusB.Location)
}
