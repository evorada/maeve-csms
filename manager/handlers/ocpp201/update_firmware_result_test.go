// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"k8s.io/utils/clock"
)

func TestUpdateFirmwareResultHandler_Accepted(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.UpdateFirmwareResultHandler{Store: memStore}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.UpdateFirmwareRequestJson{
			RequestId: 1,
			Firmware: types.FirmwareType{
				Location:         "https://firmware.example.com/v2.0.bin",
				RetrieveDateTime: "2026-02-15T10:00:00Z",
			},
		}
		resp := &types.UpdateFirmwareResponseJson{
			Status: types.UpdateFirmwareStatusEnumTypeAccepted,
		}

		err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"update_firmware.request_id":        1,
		"update_firmware.location":          "https://firmware.example.com/v2.0.bin",
		"update_firmware.retrieve_date_time": "2026-02-15T10:00:00Z",
		"update_firmware.status":            "Accepted",
	})

	// Verify firmware update status was persisted as Downloading
	status, err := memStore.GetFirmwareUpdateStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "cs001", status.ChargeStationId)
	assert.Equal(t, "Downloading", string(status.Status))
	assert.Equal(t, "https://firmware.example.com/v2.0.bin", status.Location)
	assert.Equal(t, 0, status.RetryCount)
}

func TestUpdateFirmwareResultHandler_Rejected(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.UpdateFirmwareResultHandler{Store: memStore}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.UpdateFirmwareRequestJson{
			RequestId: 2,
			Firmware: types.FirmwareType{
				Location:         "https://firmware.example.com/v2.0.bin",
				RetrieveDateTime: "2026-02-15T10:00:00Z",
			},
		}
		resp := &types.UpdateFirmwareResponseJson{
			Status: types.UpdateFirmwareStatusEnumTypeRejected,
		}

		err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"update_firmware.request_id":         2,
		"update_firmware.location":           "https://firmware.example.com/v2.0.bin",
		"update_firmware.retrieve_date_time": "2026-02-15T10:00:00Z",
		"update_firmware.status":             "Rejected",
	})

	// Firmware status should NOT be stored when rejected
	status, err := memStore.GetFirmwareUpdateStatus(ctx, "cs001")
	require.NoError(t, err)
	assert.Nil(t, status)
}

func TestUpdateFirmwareResultHandler_AcceptedWithRetries(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.UpdateFirmwareResultHandler{Store: memStore}

	ctx := context.Background()

	retries := 3
	retryInterval := 60
	req := &types.UpdateFirmwareRequestJson{
		RequestId:     5,
		Retries:       &retries,
		RetryInterval: &retryInterval,
		Firmware: types.FirmwareType{
			Location:         "https://firmware.example.com/v3.0.bin",
			RetrieveDateTime: "2026-02-15T12:00:00Z",
		},
	}
	resp := &types.UpdateFirmwareResponseJson{
		Status: types.UpdateFirmwareStatusEnumTypeAccepted,
	}

	err := handler.HandleCallResult(ctx, "cs002", req, resp, nil)
	require.NoError(t, err)

	status, err := memStore.GetFirmwareUpdateStatus(ctx, "cs002")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "Downloading", string(status.Status))
	assert.Equal(t, 3, status.RetryCount)
	assert.Equal(t, "https://firmware.example.com/v3.0.bin", status.Location)
}

func TestUpdateFirmwareResultHandler_InvalidCertificate(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.UpdateFirmwareResultHandler{Store: memStore}

	ctx := context.Background()

	req := &types.UpdateFirmwareRequestJson{
		RequestId: 3,
		Firmware: types.FirmwareType{
			Location:         "https://firmware.example.com/signed.bin",
			RetrieveDateTime: "2026-02-15T10:00:00Z",
		},
	}
	resp := &types.UpdateFirmwareResponseJson{
		Status: types.UpdateFirmwareStatusEnumTypeInvalidCertificate,
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	require.NoError(t, err)

	// Status should not be stored when certificate is invalid
	status, err := memStore.GetFirmwareUpdateStatus(ctx, "cs001")
	require.NoError(t, err)
	assert.Nil(t, status)
}

func TestUpdateFirmwareResultHandler_AcceptedInvalidDate(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.UpdateFirmwareResultHandler{Store: memStore}

	ctx := context.Background()

	req := &types.UpdateFirmwareRequestJson{
		RequestId: 4,
		Firmware: types.FirmwareType{
			Location:         "https://firmware.example.com/v2.0.bin",
			RetrieveDateTime: "not-a-valid-date",
		},
	}
	resp := &types.UpdateFirmwareResponseJson{
		Status: types.UpdateFirmwareStatusEnumTypeAccepted,
	}

	// Should not fail even with invalid date - uses current time as fallback
	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	require.NoError(t, err)

	status, err := memStore.GetFirmwareUpdateStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "Downloading", string(status.Status))
}
