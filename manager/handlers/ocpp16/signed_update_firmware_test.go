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

func TestSignedUpdateFirmwareHandler_Accepted(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.SignedUpdateFirmwareHandler{
		FirmwareStore: mockStore,
	}

	mockStore.On("SetFirmwareUpdateStatus", mock.Anything, "cs001", mock.MatchedBy(func(s *store.FirmwareUpdateStatus) bool {
		return s.ChargeStationId == "cs001" &&
			s.Status == store.FirmwareUpdateStatusDownloading &&
			s.Location == "https://firmware.example.com/v2.0-signed.bin" &&
			s.RetryCount == 3
	})).Return(nil)

	retries := 3
	request := &types.SignedUpdateFirmwareJson{
		RequestId: 42,
		Firmware: types.SignedUpdateFirmwareFirmwareType{
			Location:           "https://firmware.example.com/v2.0-signed.bin",
			RetrieveDateTime:   "2026-02-12T10:00:00Z",
			SigningCertificate: "MIIC...",
			Signature:          "abc123...",
		},
		Retries: &retries,
	}
	response := &types.SignedUpdateFirmwareResponseJson{
		Status: types.SignedUpdateFirmwareResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	require.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestSignedUpdateFirmwareHandler_Rejected(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.SignedUpdateFirmwareHandler{
		FirmwareStore: mockStore,
	}

	request := &types.SignedUpdateFirmwareJson{
		RequestId: 42,
		Firmware: types.SignedUpdateFirmwareFirmwareType{
			Location:           "https://firmware.example.com/v2.0-signed.bin",
			RetrieveDateTime:   "2026-02-12T10:00:00Z",
			SigningCertificate: "MIIC...",
			Signature:          "abc123...",
		},
	}
	response := &types.SignedUpdateFirmwareResponseJson{
		Status: types.SignedUpdateFirmwareResponseJsonStatusRejected,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	require.NoError(t, err)
	// Store should not be called when rejected
	mockStore.AssertNotCalled(t, "SetFirmwareUpdateStatus")
}

func TestSignedUpdateFirmwareHandler_InvalidCertificate(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.SignedUpdateFirmwareHandler{
		FirmwareStore: mockStore,
	}

	request := &types.SignedUpdateFirmwareJson{
		RequestId: 42,
		Firmware: types.SignedUpdateFirmwareFirmwareType{
			Location:           "https://firmware.example.com/v2.0-signed.bin",
			RetrieveDateTime:   "2026-02-12T10:00:00Z",
			SigningCertificate: "invalid-cert",
			Signature:          "abc123...",
		},
	}
	response := &types.SignedUpdateFirmwareResponseJson{
		Status: types.SignedUpdateFirmwareResponseJsonStatusInvalidCertificate,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	require.NoError(t, err)
	mockStore.AssertNotCalled(t, "SetFirmwareUpdateStatus")
}

func TestSignedUpdateFirmwareHandler_StoreError(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.SignedUpdateFirmwareHandler{
		FirmwareStore: mockStore,
	}

	mockStore.On("SetFirmwareUpdateStatus", mock.Anything, "cs001", mock.Anything).
		Return(fmt.Errorf("database error"))

	request := &types.SignedUpdateFirmwareJson{
		RequestId: 42,
		Firmware: types.SignedUpdateFirmwareFirmwareType{
			Location:           "https://firmware.example.com/v2.0-signed.bin",
			RetrieveDateTime:   "2026-02-12T10:00:00Z",
			SigningCertificate: "MIIC...",
			Signature:          "abc123...",
		},
	}
	response := &types.SignedUpdateFirmwareResponseJson{
		Status: types.SignedUpdateFirmwareResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
	mockStore.AssertExpectations(t)
}

func TestSignedUpdateFirmwareHandler_NoRetries(t *testing.T) {
	mockStore := new(MockFirmwareStore)
	handler := handlers.SignedUpdateFirmwareHandler{
		FirmwareStore: mockStore,
	}

	mockStore.On("SetFirmwareUpdateStatus", mock.Anything, "cs001", mock.MatchedBy(func(s *store.FirmwareUpdateStatus) bool {
		return s.RetryCount == 0
	})).Return(nil)

	request := &types.SignedUpdateFirmwareJson{
		RequestId: 42,
		Firmware: types.SignedUpdateFirmwareFirmwareType{
			Location:           "https://firmware.example.com/v2.0-signed.bin",
			RetrieveDateTime:   "2026-02-12T10:00:00Z",
			SigningCertificate: "MIIC...",
			Signature:          "abc123...",
		},
	}
	response := &types.SignedUpdateFirmwareResponseJson{
		Status: types.SignedUpdateFirmwareResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	require.NoError(t, err)
	mockStore.AssertExpectations(t)
}
