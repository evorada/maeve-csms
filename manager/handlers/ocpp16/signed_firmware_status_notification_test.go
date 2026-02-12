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

func TestSignedFirmwareStatusNotificationHandler_AllStatuses(t *testing.T) {
	tests := []struct {
		name          string
		status        types.SignedFirmwareStatusNotificationJsonStatus
		expectedStore store.FirmwareUpdateStatusType
	}{
		{"Downloaded", types.SignedFirmwareStatusNotificationJsonStatusDownloaded, store.FirmwareUpdateStatusDownloaded},
		{"DownloadFailed", types.SignedFirmwareStatusNotificationJsonStatusDownloadFailed, store.FirmwareUpdateStatusDownloadFailed},
		{"Downloading", types.SignedFirmwareStatusNotificationJsonStatusDownloading, store.FirmwareUpdateStatusDownloading},
		{"DownloadScheduled", types.SignedFirmwareStatusNotificationJsonStatusDownloadScheduled, store.FirmwareUpdateStatusDownloadScheduled},
		{"DownloadPaused", types.SignedFirmwareStatusNotificationJsonStatusDownloadPaused, store.FirmwareUpdateStatusDownloadPaused},
		{"Idle", types.SignedFirmwareStatusNotificationJsonStatusIdle, store.FirmwareUpdateStatusIdle},
		{"InstallationFailed", types.SignedFirmwareStatusNotificationJsonStatusInstallationFailed, store.FirmwareUpdateStatusInstallationFailed},
		{"Installing", types.SignedFirmwareStatusNotificationJsonStatusInstalling, store.FirmwareUpdateStatusInstalling},
		{"Installed", types.SignedFirmwareStatusNotificationJsonStatusInstalled, store.FirmwareUpdateStatusInstalled},
		{"InstallRebooting", types.SignedFirmwareStatusNotificationJsonStatusInstallRebooting, store.FirmwareUpdateStatusInstallRebooting},
		{"InstallScheduled", types.SignedFirmwareStatusNotificationJsonStatusInstallScheduled, store.FirmwareUpdateStatusInstallScheduled},
		{"InstallVerificationFailed", types.SignedFirmwareStatusNotificationJsonStatusInstallVerificationFailed, store.FirmwareUpdateStatusInstallVerificationFailed},
		{"InvalidSignature", types.SignedFirmwareStatusNotificationJsonStatusInvalidSignature, store.FirmwareUpdateStatusInvalidSignature},
		{"SignatureVerified", types.SignedFirmwareStatusNotificationJsonStatusSignatureVerified, store.FirmwareUpdateStatusSignatureVerified},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := new(MockFirmwareStore)
			handler := handlers.SignedFirmwareStatusNotificationHandler{
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
					s.Location == "https://firmware.example.com/v2.0.bin"
			})).Return(nil)

			requestId := 42
			request := &types.SignedFirmwareStatusNotificationJson{
				Status:    tt.status,
				RequestId: &requestId,
			}

			resp, err := handler.HandleCall(context.Background(), "cs001", request)
			require.NoError(t, err)
			assert.IsType(t, &types.SignedFirmwareStatusNotificationResponseJson{}, resp)
			mockStore.AssertExpectations(t)
		})
	}
}

func TestSignedFirmwareStatusNotificationHandler_NoExistingStatus(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.SignedFirmwareStatusNotificationHandler{
		FirmwareStore: mockStore,
	}

	mockStore.On("GetFirmwareUpdateStatus", mock.Anything, "cs001").Return(nil, fmt.Errorf("not found"))
	mockStore.On("SetFirmwareUpdateStatus", mock.Anything, "cs001", mock.MatchedBy(func(s *store.FirmwareUpdateStatus) bool {
		return s.Status == store.FirmwareUpdateStatusInstalled &&
			s.ChargeStationId == "cs001"
	})).Return(nil)

	request := &types.SignedFirmwareStatusNotificationJson{
		Status: types.SignedFirmwareStatusNotificationJsonStatusInstalled,
	}

	resp, err := handler.HandleCall(context.Background(), "cs001", request)
	require.NoError(t, err)
	assert.IsType(t, &types.SignedFirmwareStatusNotificationResponseJson{}, resp)
	mockStore.AssertExpectations(t)
}

func TestSignedFirmwareStatusNotificationHandler_StoreError(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.SignedFirmwareStatusNotificationHandler{
		FirmwareStore: mockStore,
	}

	existingStatus := &store.FirmwareUpdateStatus{
		ChargeStationId: "cs001",
		Status:          store.FirmwareUpdateStatusDownloading,
	}

	mockStore.On("GetFirmwareUpdateStatus", mock.Anything, "cs001").Return(existingStatus, nil)
	mockStore.On("SetFirmwareUpdateStatus", mock.Anything, "cs001", mock.Anything).
		Return(fmt.Errorf("database error"))

	request := &types.SignedFirmwareStatusNotificationJson{
		Status: types.SignedFirmwareStatusNotificationJsonStatusSignatureVerified,
	}

	resp, err := handler.HandleCall(context.Background(), "cs001", request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
	assert.IsType(t, &types.SignedFirmwareStatusNotificationResponseJson{}, resp)
	mockStore.AssertExpectations(t)
}

func TestSignedFirmwareStatusNotificationHandler_UnknownStatus(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.SignedFirmwareStatusNotificationHandler{
		FirmwareStore: mockStore,
	}

	request := &types.SignedFirmwareStatusNotificationJson{
		Status: types.SignedFirmwareStatusNotificationJsonStatus("UnknownStatus"),
	}

	resp, err := handler.HandleCall(context.Background(), "cs001", request)
	require.NoError(t, err)
	assert.IsType(t, &types.SignedFirmwareStatusNotificationResponseJson{}, resp)
	mockStore.AssertNotCalled(t, "GetFirmwareUpdateStatus")
	mockStore.AssertNotCalled(t, "SetFirmwareUpdateStatus")
}

func TestSignedFirmwareStatusNotificationHandler_NoRequestId(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.SignedFirmwareStatusNotificationHandler{
		FirmwareStore: mockStore,
	}

	existingStatus := &store.FirmwareUpdateStatus{
		ChargeStationId: "cs001",
		Status:          store.FirmwareUpdateStatusIdle,
	}

	mockStore.On("GetFirmwareUpdateStatus", mock.Anything, "cs001").Return(existingStatus, nil)
	mockStore.On("SetFirmwareUpdateStatus", mock.Anything, "cs001", mock.MatchedBy(func(s *store.FirmwareUpdateStatus) bool {
		return s.Status == store.FirmwareUpdateStatusDownloading
	})).Return(nil)

	request := &types.SignedFirmwareStatusNotificationJson{
		Status: types.SignedFirmwareStatusNotificationJsonStatusDownloading,
	}

	resp, err := handler.HandleCall(context.Background(), "cs001", request)
	require.NoError(t, err)
	assert.IsType(t, &types.SignedFirmwareStatusNotificationResponseJson{}, resp)
	mockStore.AssertExpectations(t)
}
