// SPDX-License-Identifier: Apache-2.0

//go:build integration

package firestore_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/firestore"
	"k8s.io/utils/clock"
)

func TestSetAndGetFirmwareUpdateStatus(t *testing.T) {
	defer cleanupAllCollections(t, "test-project")

	ctx := context.Background()
	s, err := firestore.NewStore(ctx, "test-project", clock.RealClock{})
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	status := &store.FirmwareUpdateStatus{
		Status:       store.FirmwareUpdateStatusDownloading,
		Location:     "https://example.com/firmware.bin",
		RetrieveDate: now,
		RetryCount:   3,
		UpdatedAt:    now,
	}

	err = s.SetFirmwareUpdateStatus(ctx, "cs001", status)
	require.NoError(t, err)

	got, err := s.GetFirmwareUpdateStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "cs001", got.ChargeStationId)
	assert.Equal(t, store.FirmwareUpdateStatusDownloading, got.Status)
	assert.Equal(t, "https://example.com/firmware.bin", got.Location)
	assert.Equal(t, 3, got.RetryCount)
}

func TestGetFirmwareUpdateStatus_NotFound(t *testing.T) {
	defer cleanupAllCollections(t, "test-project")

	ctx := context.Background()
	s, err := firestore.NewStore(ctx, "test-project", clock.RealClock{})
	require.NoError(t, err)

	got, err := s.GetFirmwareUpdateStatus(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestSetAndGetDiagnosticsStatus(t *testing.T) {
	defer cleanupAllCollections(t, "test-project")

	ctx := context.Background()
	s, err := firestore.NewStore(ctx, "test-project", clock.RealClock{})
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	status := &store.DiagnosticsStatus{
		Status:    store.DiagnosticsStatusUploading,
		Location:  "ftp://example.com/diagnostics/",
		UpdatedAt: now,
	}

	err = s.SetDiagnosticsStatus(ctx, "cs001", status)
	require.NoError(t, err)

	got, err := s.GetDiagnosticsStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "cs001", got.ChargeStationId)
	assert.Equal(t, store.DiagnosticsStatusUploading, got.Status)
	assert.Equal(t, "ftp://example.com/diagnostics/", got.Location)
}

func TestGetDiagnosticsStatus_NotFound(t *testing.T) {
	defer cleanupAllCollections(t, "test-project")

	ctx := context.Background()
	s, err := firestore.NewStore(ctx, "test-project", clock.RealClock{})
	require.NoError(t, err)

	got, err := s.GetDiagnosticsStatus(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestFirmwareUpdateStatus_Update(t *testing.T) {
	defer cleanupAllCollections(t, "test-project")

	ctx := context.Background()
	s, err := firestore.NewStore(ctx, "test-project", clock.RealClock{})
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)

	// Set initial status
	err = s.SetFirmwareUpdateStatus(ctx, "cs001", &store.FirmwareUpdateStatus{
		Status:       store.FirmwareUpdateStatusDownloading,
		Location:     "https://example.com/firmware.bin",
		RetrieveDate: now,
		RetryCount:   0,
		UpdatedAt:    now,
	})
	require.NoError(t, err)

	// Update status
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
}
