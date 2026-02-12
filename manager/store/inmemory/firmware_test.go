// SPDX-License-Identifier: Apache-2.0

package inmemory_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	clocktesting "k8s.io/utils/clock/testing"
)

func TestSetAndGetFirmwareUpdateStatus(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	clk := clocktesting.NewFakeClock(now)
	s := inmemory.NewStore(clk)

	status := &store.FirmwareUpdateStatus{
		Status:       store.FirmwareUpdateStatusDownloading,
		Location:     "https://example.com/firmware.bin",
		RetrieveDate: now,
		RetryCount:   3,
		UpdatedAt:    now,
	}

	err := s.SetFirmwareUpdateStatus(ctx, "cs001", status)
	require.NoError(t, err)

	got, err := s.GetFirmwareUpdateStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "cs001", got.ChargeStationId)
	assert.Equal(t, store.FirmwareUpdateStatusDownloading, got.Status)
	assert.Equal(t, "https://example.com/firmware.bin", got.Location)
	assert.Equal(t, 3, got.RetryCount)
	assert.Equal(t, now, got.RetrieveDate)
	assert.Equal(t, now, got.UpdatedAt)
}

func TestGetFirmwareUpdateStatus_NotFound(t *testing.T) {
	ctx := context.Background()
	clk := clocktesting.NewFakeClock(time.Now())
	s := inmemory.NewStore(clk)

	got, err := s.GetFirmwareUpdateStatus(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestFirmwareUpdateStatus_Update(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	clk := clocktesting.NewFakeClock(now)
	s := inmemory.NewStore(clk)

	err := s.SetFirmwareUpdateStatus(ctx, "cs001", &store.FirmwareUpdateStatus{
		Status:       store.FirmwareUpdateStatusDownloading,
		Location:     "https://example.com/firmware.bin",
		RetrieveDate: now,
		RetryCount:   0,
		UpdatedAt:    now,
	})
	require.NoError(t, err)

	later := now.Add(5 * time.Minute)
	err = s.SetFirmwareUpdateStatus(ctx, "cs001", &store.FirmwareUpdateStatus{
		Status:       store.FirmwareUpdateStatusInstalled,
		Location:     "https://example.com/firmware.bin",
		RetrieveDate: now,
		RetryCount:   0,
		UpdatedAt:    later,
	})
	require.NoError(t, err)

	got, err := s.GetFirmwareUpdateStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, store.FirmwareUpdateStatusInstalled, got.Status)
	assert.Equal(t, later, got.UpdatedAt)
}

func TestSetAndGetDiagnosticsStatus(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	clk := clocktesting.NewFakeClock(now)
	s := inmemory.NewStore(clk)

	status := &store.DiagnosticsStatus{
		Status:    store.DiagnosticsStatusUploading,
		Location:  "ftp://example.com/diagnostics/",
		UpdatedAt: now,
	}

	err := s.SetDiagnosticsStatus(ctx, "cs001", status)
	require.NoError(t, err)

	got, err := s.GetDiagnosticsStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "cs001", got.ChargeStationId)
	assert.Equal(t, store.DiagnosticsStatusUploading, got.Status)
	assert.Equal(t, "ftp://example.com/diagnostics/", got.Location)
	assert.Equal(t, now, got.UpdatedAt)
}

func TestGetDiagnosticsStatus_NotFound(t *testing.T) {
	ctx := context.Background()
	clk := clocktesting.NewFakeClock(time.Now())
	s := inmemory.NewStore(clk)

	got, err := s.GetDiagnosticsStatus(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDiagnosticsStatus_Update(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	clk := clocktesting.NewFakeClock(now)
	s := inmemory.NewStore(clk)

	err := s.SetDiagnosticsStatus(ctx, "cs001", &store.DiagnosticsStatus{
		Status:    store.DiagnosticsStatusUploading,
		Location:  "ftp://example.com/diagnostics/",
		UpdatedAt: now,
	})
	require.NoError(t, err)

	later := now.Add(5 * time.Minute)
	err = s.SetDiagnosticsStatus(ctx, "cs001", &store.DiagnosticsStatus{
		Status:    store.DiagnosticsStatusUploaded,
		Location:  "ftp://example.com/diagnostics/",
		UpdatedAt: later,
	})
	require.NoError(t, err)

	got, err := s.GetDiagnosticsStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, store.DiagnosticsStatusUploaded, got.Status)
	assert.Equal(t, later, got.UpdatedAt)
}

func TestFirmwareUpdateStatus_AllStatuses(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	clk := clocktesting.NewFakeClock(now)
	s := inmemory.NewStore(clk)

	statuses := []store.FirmwareUpdateStatusType{
		store.FirmwareUpdateStatusDownloading,
		store.FirmwareUpdateStatusDownloaded,
		store.FirmwareUpdateStatusInstallationFailed,
		store.FirmwareUpdateStatusInstalling,
		store.FirmwareUpdateStatusInstalled,
		store.FirmwareUpdateStatusIdle,
	}

	for _, st := range statuses {
		t.Run(string(st), func(t *testing.T) {
			err := s.SetFirmwareUpdateStatus(ctx, "cs-"+string(st), &store.FirmwareUpdateStatus{
				Status:       st,
				Location:     "https://example.com/fw.bin",
				RetrieveDate: now,
				UpdatedAt:    now,
			})
			require.NoError(t, err)

			got, err := s.GetFirmwareUpdateStatus(ctx, "cs-"+string(st))
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, st, got.Status)
		})
	}
}

func TestDiagnosticsStatus_AllStatuses(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	clk := clocktesting.NewFakeClock(now)
	s := inmemory.NewStore(clk)

	statuses := []store.DiagnosticsStatusType{
		store.DiagnosticsStatusIdle,
		store.DiagnosticsStatusUploaded,
		store.DiagnosticsStatusUploadFailed,
		store.DiagnosticsStatusUploading,
	}

	for _, st := range statuses {
		t.Run(string(st), func(t *testing.T) {
			err := s.SetDiagnosticsStatus(ctx, "cs-"+string(st), &store.DiagnosticsStatus{
				Status:    st,
				Location:  "ftp://example.com/diag/",
				UpdatedAt: now,
			})
			require.NoError(t, err)

			got, err := s.GetDiagnosticsStatus(ctx, "cs-"+string(st))
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, st, got.Status)
		})
	}
}
