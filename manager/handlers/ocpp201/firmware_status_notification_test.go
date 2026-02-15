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

func TestFirmwareStatusNotification(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.FirmwareStatusNotificationHandler{Store: memStore}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.FirmwareStatusNotificationRequestJson{
			Status: types.FirmwareStatusEnumTypeDownloading,
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		assert.Equal(t, &types.FirmwareStatusNotificationResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"firmware_status.status": "Downloading",
	})

	// Verify status was persisted
	status, err := memStore.GetFirmwareUpdateStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "cs001", status.ChargeStationId)
	assert.Equal(t, "Downloading", string(status.Status))
}

func TestFirmwareStatusNotificationWithRequestId(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.FirmwareStatusNotificationHandler{Store: memStore}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		requestId := 42
		req := &types.FirmwareStatusNotificationRequestJson{
			Status:    types.FirmwareStatusEnumTypeDownloading,
			RequestId: &requestId,
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		assert.Equal(t, &types.FirmwareStatusNotificationResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"firmware_status.status":     "Downloading",
		"firmware_status.request_id": 42,
	})

	// Verify status was persisted
	status, err := memStore.GetFirmwareUpdateStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "cs001", status.ChargeStationId)
	assert.Equal(t, "Downloading", string(status.Status))
}

func TestFirmwareStatusNotificationInstalledStatus(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.FirmwareStatusNotificationHandler{Store: memStore}

	ctx := context.Background()

	req := &types.FirmwareStatusNotificationRequestJson{
		Status: types.FirmwareStatusEnumTypeInstalled,
	}

	resp, err := handler.HandleCall(ctx, "cs001", req)
	require.NoError(t, err)
	assert.Equal(t, &types.FirmwareStatusNotificationResponseJson{}, resp)

	// Verify final Installed status is persisted
	status, err := memStore.GetFirmwareUpdateStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "Installed", string(status.Status))
}

func TestFirmwareStatusNotificationStatusUpdates(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.FirmwareStatusNotificationHandler{Store: memStore}

	ctx := context.Background()

	// Simulate progression: Downloading -> Downloaded -> Installing -> Installed
	statuses := []types.FirmwareStatusEnumType{
		types.FirmwareStatusEnumTypeDownloading,
		types.FirmwareStatusEnumTypeDownloaded,
		types.FirmwareStatusEnumTypeInstalling,
		types.FirmwareStatusEnumTypeInstalled,
	}

	for _, s := range statuses {
		req := &types.FirmwareStatusNotificationRequestJson{Status: s}
		_, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		stored, err := memStore.GetFirmwareUpdateStatus(ctx, "cs001")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, string(s), string(stored.Status))
	}
}

func TestFirmwareStatusNotificationMultipleChargeStations(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.FirmwareStatusNotificationHandler{Store: memStore}

	ctx := context.Background()

	// cs001 is downloading, cs002 is installed
	req1 := &types.FirmwareStatusNotificationRequestJson{Status: types.FirmwareStatusEnumTypeDownloading}
	req2 := &types.FirmwareStatusNotificationRequestJson{Status: types.FirmwareStatusEnumTypeInstalled}

	_, err := handler.HandleCall(ctx, "cs001", req1)
	require.NoError(t, err)

	_, err = handler.HandleCall(ctx, "cs002", req2)
	require.NoError(t, err)

	status1, err := memStore.GetFirmwareUpdateStatus(ctx, "cs001")
	require.NoError(t, err)
	assert.Equal(t, "Downloading", string(status1.Status))

	status2, err := memStore.GetFirmwareUpdateStatus(ctx, "cs002")
	require.NoError(t, err)
	assert.Equal(t, "Installed", string(status2.Status))
}
