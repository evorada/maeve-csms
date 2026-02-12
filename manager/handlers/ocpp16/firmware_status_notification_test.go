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

func TestFirmwareStatusNotificationHandler_AllStatuses(t *testing.T) {
	tests := []struct {
		name          string
		status        types.FirmwareStatusNotificationJsonStatus
		expectedStore store.FirmwareUpdateStatusType
	}{
		{"Downloaded", types.FirmwareStatusNotificationJsonStatusDownloaded, store.FirmwareUpdateStatusDownloaded},
		{"DownloadFailed", types.FirmwareStatusNotificationJsonStatusDownloadFailed, store.FirmwareUpdateStatusDownloadFailed},
		{"Downloading", types.FirmwareStatusNotificationJsonStatusDownloading, store.FirmwareUpdateStatusDownloading},
		{"Idle", types.FirmwareStatusNotificationJsonStatusIdle, store.FirmwareUpdateStatusIdle},
		{"InstallationFailed", types.FirmwareStatusNotificationJsonStatusInstallationFailed, store.FirmwareUpdateStatusInstallationFailed},
		{"Installing", types.FirmwareStatusNotificationJsonStatusInstalling, store.FirmwareUpdateStatusInstalling},
		{"Installed", types.FirmwareStatusNotificationJsonStatusInstalled, store.FirmwareUpdateStatusInstalled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := new(MockFirmwareStore)
			handler := handlers.FirmwareStatusNotificationHandler{
				FirmwareStore: mockStore,
			}

			existingStatus := &store.FirmwareUpdateStatus{
				ChargeStationId: "cs001",
				Status:          store.FirmwareUpdateStatusDownloading,
				Location:        "https://firmware.example.com/v2.0.bin",
				RetrieveDate:    time.Now(),
				RetryCount:      3,
				UpdatedAt:       time.Now().Add(-5 * time.Minute),
			}

			mockStore.On("GetFirmwareUpdateStatus", mock.Anything, "cs001").Return(existingStatus, nil)
			mockStore.On("SetFirmwareUpdateStatus", mock.Anything, "cs001", mock.MatchedBy(func(s *store.FirmwareUpdateStatus) bool {
				return s.Status == tt.expectedStore &&
					s.Location == "https://firmware.example.com/v2.0.bin" // preserved from existing
			})).Return(nil)

			request := &types.FirmwareStatusNotificationJson{
				Status: tt.status,
			}

			resp, err := handler.HandleCall(context.Background(), "cs001", request)
			require.NoError(t, err)
			assert.IsType(t, &types.FirmwareStatusNotificationResponseJson{}, resp)
			mockStore.AssertExpectations(t)
		})
	}
}

func TestFirmwareStatusNotificationHandler_NoExistingStatus(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.FirmwareStatusNotificationHandler{
		FirmwareStore: mockStore,
	}

	mockStore.On("GetFirmwareUpdateStatus", mock.Anything, "cs001").Return(nil, fmt.Errorf("not found"))
	mockStore.On("SetFirmwareUpdateStatus", mock.Anything, "cs001", mock.MatchedBy(func(s *store.FirmwareUpdateStatus) bool {
		return s.Status == store.FirmwareUpdateStatusInstalled &&
			s.ChargeStationId == "cs001"
	})).Return(nil)

	request := &types.FirmwareStatusNotificationJson{
		Status: types.FirmwareStatusNotificationJsonStatusInstalled,
	}

	resp, err := handler.HandleCall(context.Background(), "cs001", request)
	require.NoError(t, err)
	assert.IsType(t, &types.FirmwareStatusNotificationResponseJson{}, resp)
	mockStore.AssertExpectations(t)
}

func TestFirmwareStatusNotificationHandler_StoreError(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.FirmwareStatusNotificationHandler{
		FirmwareStore: mockStore,
	}

	existingStatus := &store.FirmwareUpdateStatus{
		ChargeStationId: "cs001",
		Status:          store.FirmwareUpdateStatusDownloading,
	}

	mockStore.On("GetFirmwareUpdateStatus", mock.Anything, "cs001").Return(existingStatus, nil)
	mockStore.On("SetFirmwareUpdateStatus", mock.Anything, "cs001", mock.Anything).
		Return(fmt.Errorf("database error"))

	request := &types.FirmwareStatusNotificationJson{
		Status: types.FirmwareStatusNotificationJsonStatusDownloaded,
	}

	resp, err := handler.HandleCall(context.Background(), "cs001", request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
	// Should still return response even on store error
	assert.IsType(t, &types.FirmwareStatusNotificationResponseJson{}, resp)
	mockStore.AssertExpectations(t)
}

func TestFirmwareStatusNotificationHandler_UnknownStatus(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.FirmwareStatusNotificationHandler{
		FirmwareStore: mockStore,
	}

	request := &types.FirmwareStatusNotificationJson{
		Status: types.FirmwareStatusNotificationJsonStatus("UnknownStatus"),
	}

	resp, err := handler.HandleCall(context.Background(), "cs001", request)
	require.NoError(t, err)
	assert.IsType(t, &types.FirmwareStatusNotificationResponseJson{}, resp)
	// Store should not be called for unknown status
	mockStore.AssertNotCalled(t, "GetFirmwareUpdateStatus")
	mockStore.AssertNotCalled(t, "SetFirmwareUpdateStatus")
}
