// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func TestDiagnosticsStatusNotificationHandler_AllStatuses(t *testing.T) {
	tests := []struct {
		name          string
		status        types.DiagnosticsStatusNotificationJsonStatus
		expectedStore store.DiagnosticsStatusType
	}{
		{"Idle", types.DiagnosticsStatusNotificationJsonStatusIdle, store.DiagnosticsStatusIdle},
		{"Uploaded", types.DiagnosticsStatusNotificationJsonStatusUploaded, store.DiagnosticsStatusUploaded},
		{"UploadFailed", types.DiagnosticsStatusNotificationJsonStatusUploadFailed, store.DiagnosticsStatusUploadFailed},
		{"Uploading", types.DiagnosticsStatusNotificationJsonStatusUploading, store.DiagnosticsStatusUploading},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := new(MockFirmwareStore)
			handler := handlers.DiagnosticsStatusNotificationHandler{
				FirmwareStore: mockStore,
			}

			existingStatus := &store.DiagnosticsStatus{
				ChargeStationId: "cs001",
				Status:          store.DiagnosticsStatusUploading,
				Location:        "https://diagnostics.example.com/upload",
				UpdatedAt:       time.Now().Add(-5 * time.Minute),
			}

			mockStore.On("GetDiagnosticsStatus", mock.Anything, "cs001").Return(existingStatus, nil)
			mockStore.On("SetDiagnosticsStatus", mock.Anything, "cs001", mock.MatchedBy(func(s *store.DiagnosticsStatus) bool {
				return s.Status == tt.expectedStore &&
					s.Location == "https://diagnostics.example.com/upload" // preserved from existing
			})).Return(nil)

			request := &types.DiagnosticsStatusNotificationJson{
				Status: tt.status,
			}

			resp, err := handler.HandleCall(context.Background(), "cs001", request)
			require.NoError(t, err)
			assert.IsType(t, &types.DiagnosticsStatusNotificationResponseJson{}, resp)
			mockStore.AssertExpectations(t)
		})
	}
}

func TestDiagnosticsStatusNotificationHandler_NoExistingStatus(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.DiagnosticsStatusNotificationHandler{
		FirmwareStore: mockStore,
	}

	mockStore.On("GetDiagnosticsStatus", mock.Anything, "cs001").Return(nil, fmt.Errorf("not found"))
	mockStore.On("SetDiagnosticsStatus", mock.Anything, "cs001", mock.MatchedBy(func(s *store.DiagnosticsStatus) bool {
		return s.Status == store.DiagnosticsStatusUploaded &&
			s.ChargeStationId == "cs001"
	})).Return(nil)

	request := &types.DiagnosticsStatusNotificationJson{
		Status: types.DiagnosticsStatusNotificationJsonStatusUploaded,
	}

	resp, err := handler.HandleCall(context.Background(), "cs001", request)
	require.NoError(t, err)
	assert.IsType(t, &types.DiagnosticsStatusNotificationResponseJson{}, resp)
	mockStore.AssertExpectations(t)
}

func TestDiagnosticsStatusNotificationHandler_StoreError(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.DiagnosticsStatusNotificationHandler{
		FirmwareStore: mockStore,
	}

	existingStatus := &store.DiagnosticsStatus{
		ChargeStationId: "cs001",
		Status:          store.DiagnosticsStatusUploading,
	}

	mockStore.On("GetDiagnosticsStatus", mock.Anything, "cs001").Return(existingStatus, nil)
	mockStore.On("SetDiagnosticsStatus", mock.Anything, "cs001", mock.Anything).
		Return(fmt.Errorf("database error"))

	request := &types.DiagnosticsStatusNotificationJson{
		Status: types.DiagnosticsStatusNotificationJsonStatusUploaded,
	}

	resp, err := handler.HandleCall(context.Background(), "cs001", request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
	assert.IsType(t, &types.DiagnosticsStatusNotificationResponseJson{}, resp)
	mockStore.AssertExpectations(t)
}

func TestDiagnosticsStatusNotificationHandler_UnknownStatus(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.DiagnosticsStatusNotificationHandler{
		FirmwareStore: mockStore,
	}

	request := &types.DiagnosticsStatusNotificationJson{
		Status: types.DiagnosticsStatusNotificationJsonStatus("UnknownStatus"),
	}

	resp, err := handler.HandleCall(context.Background(), "cs001", request)
	require.NoError(t, err)
	assert.IsType(t, &types.DiagnosticsStatusNotificationResponseJson{}, resp)
	mockStore.AssertNotCalled(t, "GetDiagnosticsStatus")
	mockStore.AssertNotCalled(t, "SetDiagnosticsStatus")
}
