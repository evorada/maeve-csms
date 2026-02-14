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

func TestResetResultHandler(t *testing.T) {
	handler := ocpp201.ResetResultHandler{}

	tests := []struct {
		name           string
		resetType      types.ResetEnumType
		status         types.ResetStatusEnumType
		statusInfo     *types.StatusInfoType
		expectedAttrs  map[string]any
	}{
		{
			name:      "accepted reset",
			resetType: types.ResetEnumTypeOnIdle,
			status:    types.ResetStatusEnumTypeAccepted,
			expectedAttrs: map[string]any{
				"reset.type":   "OnIdle",
				"reset.status": "Accepted",
			},
		},
		{
			name:      "scheduled reset",
			resetType: types.ResetEnumTypeImmediate,
			status:    types.ResetStatusEnumTypeScheduled,
			expectedAttrs: map[string]any{
				"reset.type":   "Immediate",
				"reset.status": "Scheduled",
			},
		},
		{
			name:      "rejected reset",
			resetType: types.ResetEnumTypeImmediate,
			status:    types.ResetStatusEnumTypeRejected,
			expectedAttrs: map[string]any{
				"reset.type":   "Immediate",
				"reset.status": "Rejected",
			},
		},
		{
			name:      "rejected with status info",
			resetType: types.ResetEnumTypeOnIdle,
			status:    types.ResetStatusEnumTypeRejected,
			statusInfo: &types.StatusInfoType{
				ReasonCode: "Busy",
				AdditionalInfo: func() *string {
					s := "Active transaction in progress"
					return &s
				}(),
			},
			expectedAttrs: map[string]any{
				"reset.type":   "OnIdle",
				"reset.status": "Rejected",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracer, exporter := testutil.GetTracer()

			ctx := context.Background()

			func() {
				ctx, span := tracer.Start(ctx, `test`)
				defer span.End()

				req := &types.ResetRequestJson{
					Type: tt.resetType,
				}
				resp := &types.ResetResponseJson{
					Status:     tt.status,
					StatusInfo: tt.statusInfo,
				}

				err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
				require.NoError(t, err)
			}()

			testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", tt.expectedAttrs)
		})
	}
}
