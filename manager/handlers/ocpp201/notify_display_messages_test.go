// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
)

func TestNotifyDisplayMessagesHandler(t *testing.T) {
	handler := handlers.NotifyDisplayMessagesHandler{}
	tracer, exporter := testutil.GetTracer()
	ctx := context.Background()
	tbc := true

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.NotifyDisplayMessagesRequestJson{
			RequestId: 42,
			Tbc:       &tbc,
			MessageInfo: []types.MessageInfoType{
				{Id: 1, Priority: types.MessagePriorityEnumTypeNormalCycle, Message: types.MessageContentType{Format: types.MessageFormatEnumTypeUTF8, Content: "hello"}},
			},
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)
		assert.Equal(t, &types.NotifyDisplayMessagesResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"notify_display_messages.request_id":    42,
		"notify_display_messages.message_count": 1,
		"notify_display_messages.tbc":           true,
	})
}
