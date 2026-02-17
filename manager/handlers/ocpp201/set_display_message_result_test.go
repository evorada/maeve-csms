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

func TestSetDisplayMessageResultHandler(t *testing.T) {
	handler := ocpp201.SetDisplayMessageResultHandler{}

	tracer, exporter := testutil.GetTracer()
	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.SetDisplayMessageRequestJson{
			Message: types.MessageInfoType{
				Id:       42,
				Priority: types.MessagePriorityEnumTypeAlwaysFront,
				Display: &types.ComponentType{
					Name: "Display",
				},
				Message: types.MessageContentType{
					Format:  types.MessageFormatEnumTypeUTF8,
					Content: "Charge complete",
				},
			},
		}
		resp := &types.SetDisplayMessageResponseJson{Status: types.DisplayMessageStatusEnumTypeAccepted}

		err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"set_display_message.message_id": int64(42),
		"set_display_message.priority":   "AlwaysFront",
		"set_display_message.status":     "Accepted",
		"set_display_message.component":  "Display",
	})
}
