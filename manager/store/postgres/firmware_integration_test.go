// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func TestFirmwareUpdateStatus_SetAndGet(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	status := &store.FirmwareUpdateStatus{
		Status:    store.FirmwareUpdateStatusDownloading,
		Location:  "https://example.com/fw.bin",
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}

	err = testStore.SetFirmwareUpdateStatus(ctx, "cs001", status)
	require.NoError(t, err)

	got, err := testStore.GetFirmwareUpdateStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, store.FirmwareUpdateStatusDownloading, got.Status)
}

func TestFirmwareUpdateRequest_SetGetDelete(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	request := &store.FirmwareUpdateRequest{
		ChargeStationId: "cs001",
		Location:        "https://example.com/firmware.bin",
		SendAfter:       time.Now().UTC().Truncate(time.Second),
	}

	err = testStore.SetFirmwareUpdateRequest(ctx, "cs001", request)
	require.NoError(t, err)

	got, err := testStore.GetFirmwareUpdateRequest(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "https://example.com/firmware.bin", got.Location)

	err = testStore.DeleteFirmwareUpdateRequest(ctx, "cs001")
	require.NoError(t, err)

	got, err = testStore.GetFirmwareUpdateRequest(ctx, "cs001")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDiagnosticsStatus_SetAndGet(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	status := &store.DiagnosticsStatus{
		Status:    store.DiagnosticsStatusUploading,
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}

	err = testStore.SetDiagnosticsStatus(ctx, "cs001", status)
	require.NoError(t, err)

	got, err := testStore.GetDiagnosticsStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, store.DiagnosticsStatusUploading, got.Status)
}

func TestPublishFirmwareStatus_SetAndGet(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	status := &store.PublishFirmwareStatus{
		Status:    store.PublishFirmwareStatusDownloaded,
		RequestId: 1,
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}

	err = testStore.SetPublishFirmwareStatus(ctx, "cs001", status)
	require.NoError(t, err)

	got, err := testStore.GetPublishFirmwareStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, store.PublishFirmwareStatusDownloaded, got.Status)
}

func TestLogStatus_SetAndGet(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	status := &store.LogStatus{
		Status:    store.LogStatusUploading,
		RequestId: 1,
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}

	err = testStore.SetLogStatus(ctx, "cs001", status)
	require.NoError(t, err)

	got, err := testStore.GetLogStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, store.LogStatusUploading, got.Status)
}
