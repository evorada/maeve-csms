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

func TestGetDisplayMessagesResultHandler(t *testing.T) {
	handler := ocpp201.GetDisplayMessagesResultHandler{}
	tracer, exporter := testutil.GetTracer()
	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		priority := types.MessagePriorityEnumTypeInFront
		state := types.MessageStateEnumTypeCharging
		req := &types.GetDisplayMessagesRequestJson{
			RequestId: 77,
			Id:        []int{1, 2},
			Priority:  &priority,
			State:     &state,
		}
		resp := &types.GetDisplayMessagesResponseJson{
			Status: types.GetDisplayMessagesStatusEnumTypeAccepted,
		}

		err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"get_display_messages.request_id": 77,
		"get_display_messages.status":     "Accepted",
		"get_display_messages.priority":   "InFront",
		"get_display_messages.state":      "Charging",
		"get_display_messages.id_count":   2,
	})
}
