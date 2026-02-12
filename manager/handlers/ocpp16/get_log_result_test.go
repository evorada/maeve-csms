// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
)

func TestGetLogResultHandler_HandleCallResult(t *testing.T) {
	handler := ocpp16.GetLogResultHandler{}
	ctx := context.Background()
	chargeStationId := "cs001"

	tests := []struct {
		name     string
		request  ocpp.Request
		response ocpp.Response
	}{
		{
			name: "accepted diagnostics log",
			request: &types.GetLogJson{
				LogType:   types.LogEnumTypeDiagnosticsLog,
				RequestId: 1,
				Log: types.LogParametersType{
					RemoteLocation: "ftp://example.com/logs/",
				},
			},
			response: &types.GetLogResponseJson{
				Status: types.GetLogResponseJsonStatusAccepted,
			},
		},
		{
			name: "accepted security log",
			request: &types.GetLogJson{
				LogType:   types.LogEnumTypeSecurityLog,
				RequestId: 2,
				Log: types.LogParametersType{
					RemoteLocation: "https://example.com/logs/",
				},
			},
			response: &types.GetLogResponseJson{
				Status: types.GetLogResponseJsonStatusAccepted,
			},
		},
		{
			name: "accepted with filename",
			request: &types.GetLogJson{
				LogType:   types.LogEnumTypeDiagnosticsLog,
				RequestId: 3,
				Log: types.LogParametersType{
					RemoteLocation: "ftp://example.com/logs/",
				},
			},
			response: &types.GetLogResponseJson{
				Status:   types.GetLogResponseJsonStatusAccepted,
				Filename: strPtr("diagnostics-20260212.log"),
			},
		},
		{
			name: "rejected",
			request: &types.GetLogJson{
				LogType:   types.LogEnumTypeDiagnosticsLog,
				RequestId: 4,
				Log: types.LogParametersType{
					RemoteLocation: "ftp://example.com/logs/",
				},
			},
			response: &types.GetLogResponseJson{
				Status: types.GetLogResponseJsonStatusRejected,
			},
		},
		{
			name: "accepted canceled",
			request: &types.GetLogJson{
				LogType:   types.LogEnumTypeSecurityLog,
				RequestId: 5,
				Log: types.LogParametersType{
					RemoteLocation: "https://example.com/logs/",
				},
			},
			response: &types.GetLogResponseJson{
				Status: types.GetLogResponseJsonStatusAcceptedCanceled,
			},
		},
		{
			name: "with retries and retry interval",
			request: &types.GetLogJson{
				LogType:   types.LogEnumTypeDiagnosticsLog,
				RequestId: 6,
				Log: types.LogParametersType{
					RemoteLocation: "ftp://example.com/logs/",
				},
				Retries:       intP(3),
				RetryInterval: intP(60),
			},
			response: &types.GetLogResponseJson{
				Status: types.GetLogResponseJsonStatusAccepted,
			},
		},
		{
			name: "with oldest and latest timestamps",
			request: &types.GetLogJson{
				LogType:   types.LogEnumTypeSecurityLog,
				RequestId: 7,
				Log: types.LogParametersType{
					RemoteLocation:  "https://example.com/logs/",
					OldestTimestamp: strPtr("2026-02-01T00:00:00Z"),
					LatestTimestamp: strPtr("2026-02-12T00:00:00Z"),
				},
			},
			response: &types.GetLogResponseJson{
				Status: types.GetLogResponseJsonStatusAccepted,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handler.HandleCallResult(ctx, chargeStationId, tt.request, tt.response, nil)
			require.NoError(t, err)
		})
	}
}

func TestGetLogResultHandler_AllLogTypes(t *testing.T) {
	handler := ocpp16.GetLogResultHandler{}
	ctx := context.Background()

	logTypes := []types.LogEnumType{
		types.LogEnumTypeDiagnosticsLog,
		types.LogEnumTypeSecurityLog,
	}

	for _, logType := range logTypes {
		t.Run(string(logType), func(t *testing.T) {
			request := &types.GetLogJson{
				LogType:   logType,
				RequestId: 1,
				Log: types.LogParametersType{
					RemoteLocation: "ftp://example.com/logs/",
				},
			}
			response := &types.GetLogResponseJson{
				Status: types.GetLogResponseJsonStatusAccepted,
			}

			err := handler.HandleCallResult(ctx, "cs001", request, response, nil)
			require.NoError(t, err)
		})
	}
}

func TestGetLogResultHandler_AllStatuses(t *testing.T) {
	handler := ocpp16.GetLogResultHandler{}
	ctx := context.Background()

	statuses := []types.GetLogResponseJsonStatus{
		types.GetLogResponseJsonStatusAccepted,
		types.GetLogResponseJsonStatusRejected,
		types.GetLogResponseJsonStatusAcceptedCanceled,
	}

	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			request := &types.GetLogJson{
				LogType:   types.LogEnumTypeDiagnosticsLog,
				RequestId: 1,
				Log: types.LogParametersType{
					RemoteLocation: "ftp://example.com/logs/",
				},
			}
			response := &types.GetLogResponseJson{
				Status: status,
			}

			err := handler.HandleCallResult(ctx, "cs001", request, response, nil)
			require.NoError(t, err)
		})
	}
}

func strPtr(s string) *string {
	return &s
}

func intP(i int) *int {
	return &i
}
