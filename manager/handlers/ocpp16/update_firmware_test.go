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

type MockFirmwareStore struct {
	mock.Mock
}

func (m *MockFirmwareStore) SetFirmwareUpdateStatus(ctx context.Context, chargeStationId string, status *store.FirmwareUpdateStatus) error {
	args := m.Called(ctx, chargeStationId, status)
	return args.Error(0)
}

func (m *MockFirmwareStore) GetFirmwareUpdateStatus(ctx context.Context, chargeStationId string) (*store.FirmwareUpdateStatus, error) {
	args := m.Called(ctx, chargeStationId)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*store.FirmwareUpdateStatus), args.Error(1)
}

func (m *MockFirmwareStore) SetDiagnosticsStatus(ctx context.Context, chargeStationId string, status *store.DiagnosticsStatus) error {
	args := m.Called(ctx, chargeStationId, status)
	return args.Error(0)
}

func (m *MockFirmwareStore) GetDiagnosticsStatus(ctx context.Context, chargeStationId string) (*store.DiagnosticsStatus, error) {
	args := m.Called(ctx, chargeStationId)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*store.DiagnosticsStatus), args.Error(1)
}

func (m *MockFirmwareStore) SetPublishFirmwareStatus(ctx context.Context, chargeStationId string, status *store.PublishFirmwareStatus) error {
	args := m.Called(ctx, chargeStationId, status)
	return args.Error(0)
}

func (m *MockFirmwareStore) GetPublishFirmwareStatus(ctx context.Context, chargeStationId string) (*store.PublishFirmwareStatus, error) {
	args := m.Called(ctx, chargeStationId)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*store.PublishFirmwareStatus), args.Error(1)
}

func TestUpdateFirmwareHandler_Success(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.UpdateFirmwareHandler{
		FirmwareStore: mockStore,
	}

	mockStore.On("SetFirmwareUpdateStatus", mock.Anything, "cs001", mock.MatchedBy(func(s *store.FirmwareUpdateStatus) bool {
		return s.ChargeStationId == "cs001" &&
			s.Status == store.FirmwareUpdateStatusDownloading &&
			s.Location == "https://firmware.example.com/v2.0.bin" &&
			s.RetryCount == 3
	})).Return(nil)

	retries := 3
	request := &types.UpdateFirmwareJson{
		Location:     "https://firmware.example.com/v2.0.bin",
		RetrieveDate: "2026-02-12T10:00:00Z",
		Retries:      &retries,
	}
	response := &types.UpdateFirmwareResponseJson{}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	require.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestUpdateFirmwareHandler_NoRetries(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.UpdateFirmwareHandler{
		FirmwareStore: mockStore,
	}

	mockStore.On("SetFirmwareUpdateStatus", mock.Anything, "cs001", mock.MatchedBy(func(s *store.FirmwareUpdateStatus) bool {
		return s.RetryCount == 0 &&
			s.Location == "ftp://firmware.example.com/fw.bin"
	})).Return(nil)

	request := &types.UpdateFirmwareJson{
		Location:     "ftp://firmware.example.com/fw.bin",
		RetrieveDate: "2026-02-12T10:00:00Z",
	}
	response := &types.UpdateFirmwareResponseJson{}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	require.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestUpdateFirmwareHandler_WithRetryInterval(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.UpdateFirmwareHandler{
		FirmwareStore: mockStore,
	}

	mockStore.On("SetFirmwareUpdateStatus", mock.Anything, "cs001", mock.Anything).Return(nil)

	retries := 5
	retryInterval := 60
	request := &types.UpdateFirmwareJson{
		Location:      "https://firmware.example.com/v3.0.bin",
		RetrieveDate:  "2026-03-01T08:00:00Z",
		Retries:       &retries,
		RetryInterval: &retryInterval,
	}
	response := &types.UpdateFirmwareResponseJson{}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	require.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestUpdateFirmwareHandler_StoreError(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.UpdateFirmwareHandler{
		FirmwareStore: mockStore,
	}

	mockStore.On("SetFirmwareUpdateStatus", mock.Anything, "cs001", mock.Anything).
		Return(fmt.Errorf("database error"))

	request := &types.UpdateFirmwareJson{
		Location:     "https://firmware.example.com/v2.0.bin",
		RetrieveDate: "2026-02-12T10:00:00Z",
	}
	response := &types.UpdateFirmwareResponseJson{}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
	mockStore.AssertExpectations(t)
}

func TestUpdateFirmwareHandler_InvalidRetrieveDate(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.UpdateFirmwareHandler{
		FirmwareStore: mockStore,
	}

	// Should still succeed but use current time as fallback
	mockStore.On("SetFirmwareUpdateStatus", mock.Anything, "cs001", mock.Anything).Return(nil)

	request := &types.UpdateFirmwareJson{
		Location:     "https://firmware.example.com/v2.0.bin",
		RetrieveDate: "not-a-date",
	}
	response := &types.UpdateFirmwareResponseJson{}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	require.NoError(t, err)
	mockStore.AssertExpectations(t)
}
