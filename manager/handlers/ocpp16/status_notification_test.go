// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"testing"
)

func TestStatusNotificationHandler(t *testing.T) {
	// TODO: Update test with proper mock StatusStore
	t.Skip("Skipping until StatusStore mock is implemented")

	/*
		timestamp := "2023-05-01T01:00:00+01:00"
		req := &types.StatusNotificationJson{
			Timestamp:   &timestamp,
			ConnectorId: 2,
			ErrorCode:   types.StatusNotificationJsonErrorCodeNoError,
			Status:      types.StatusNotificationJsonStatusPreparing,
		}

		handler := handlers.StatusNotificationHandler{
			StatusStore: mockStore,
		}

		got, err := handler.HandleCall(context.Background(), "cs001", req)
		assert.NoError(t, err)

		want := &types.StatusNotificationResponseJson{}

		assert.Equal(t, want, got)
	*/
}
