// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func TestGetDiagnosticsHandler_Success(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.GetDiagnosticsHandler{
		FirmwareStore: mockStore,
	}

	mockStore.On("SetDiagnosticsStatus", mock.Anything, "cs001", mock.MatchedBy(func(s *store.DiagnosticsStatus) bool {
		return s.ChargeStationId == "cs001" &&
			s.Status == store.DiagnosticsStatusUploading &&
			s.Location == "ftp://diagnostics.example.com/uploads/"
	})).Return(nil)

	request := &types.GetDiagnosticsJson{
		Location: "ftp://diagnostics.example.com/uploads/",
	}
	fileName := "diagnostics-20260212.zip"
	response := &types.GetDiagnosticsResponseJson{
		FileName: &fileName,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	require.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestGetDiagnosticsHandler_WithTimeRange(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.GetDiagnosticsHandler{
		FirmwareStore: mockStore,
	}

	mockStore.On("SetDiagnosticsStatus", mock.Anything, "cs001", mock.MatchedBy(func(s *store.DiagnosticsStatus) bool {
		return s.Location == "https://diagnostics.example.com/upload"
	})).Return(nil)

	startTime := "2026-02-01T00:00:00Z"
	stopTime := "2026-02-12T00:00:00Z"
	retries := 3
	retryInterval := 30
	request := &types.GetDiagnosticsJson{
		Location:      "https://diagnostics.example.com/upload",
		StartTime:     &startTime,
		StopTime:      &stopTime,
		Retries:       &retries,
		RetryInterval: &retryInterval,
	}
	response := &types.GetDiagnosticsResponseJson{}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	require.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestGetDiagnosticsHandler_NoFileName(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.GetDiagnosticsHandler{
		FirmwareStore: mockStore,
	}

	mockStore.On("SetDiagnosticsStatus", mock.Anything, "cs001", mock.Anything).Return(nil)

	request := &types.GetDiagnosticsJson{
		Location: "ftp://diagnostics.example.com/uploads/",
	}
	response := &types.GetDiagnosticsResponseJson{
		FileName: nil,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	require.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestGetDiagnosticsHandler_StoreError(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.GetDiagnosticsHandler{
		FirmwareStore: mockStore,
	}

	mockStore.On("SetDiagnosticsStatus", mock.Anything, "cs001", mock.Anything).
		Return(fmt.Errorf("database error"))

	request := &types.GetDiagnosticsJson{
		Location: "ftp://diagnostics.example.com/uploads/",
	}
	response := &types.GetDiagnosticsResponseJson{}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
	mockStore.AssertExpectations(t)
}
