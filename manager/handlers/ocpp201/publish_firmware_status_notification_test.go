// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"k8s.io/utils/clock"
)

func TestPublishFirmwareStatusNotificationBasic(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.PublishFirmwareStatusNotificationHandler{Store: memStore}

	tracer, exporter := testutil.GetTracer()
	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.PublishFirmwareStatusNotificationRequestJson{
			Status: types.PublishFirmwareStatusEnumTypeDownloading,
		}

		resp, err := handler.HandleCall(ctx, "lc001", req)
		require.NoError(t, err)
		assert.Equal(t, &types.PublishFirmwareStatusNotificationResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"publish_firmware_status.status": "Downloading",
	})

	// Verify status was persisted
	status, err := memStore.GetPublishFirmwareStatus(ctx, "lc001")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "lc001", status.ChargeStationId)
	assert.Equal(t, store.PublishFirmwareStatusType("Downloading"), status.Status)
}

func TestPublishFirmwareStatusNotificationWithRequestId(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.PublishFirmwareStatusNotificationHandler{Store: memStore}

	tracer, exporter := testutil.GetTracer()
	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		requestId := 7
		req := &types.PublishFirmwareStatusNotificationRequestJson{
			Status:    types.PublishFirmwareStatusEnumTypeDownloaded,
			RequestId: &requestId,
		}

		resp, err := handler.HandleCall(ctx, "lc001", req)
		require.NoError(t, err)
		assert.Equal(t, &types.PublishFirmwareStatusNotificationResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"publish_firmware_status.status":     "Downloaded",
		"publish_firmware_status.request_id": 7,
	})

	status, err := memStore.GetPublishFirmwareStatus(ctx, "lc001")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, store.PublishFirmwareStatusType("Downloaded"), status.Status)
	assert.Equal(t, 7, status.RequestId)
}

func TestPublishFirmwareStatusNotificationPublishedWithLocations(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.PublishFirmwareStatusNotificationHandler{Store: memStore}

	ctx := context.Background()

	// When status is Published, the spec requires location URIs to be present.
	requestId := 3
	req := &types.PublishFirmwareStatusNotificationRequestJson{
		Status:    types.PublishFirmwareStatusEnumTypePublished,
		RequestId: &requestId,
		Location:  []string{"http://192.168.1.1/firmware.bin", "ftp://192.168.1.1/firmware.bin"},
	}

	resp, err := handler.HandleCall(ctx, "lc002", req)
	require.NoError(t, err)
	assert.Equal(t, &types.PublishFirmwareStatusNotificationResponseJson{}, resp)

	status, err := memStore.GetPublishFirmwareStatus(ctx, "lc002")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, store.PublishFirmwareStatusType("Published"), status.Status)
	// First URI is stored as the canonical Location
	assert.Equal(t, "http://192.168.1.1/firmware.bin", status.Location)
	assert.Equal(t, 3, status.RequestId)
}

func TestPublishFirmwareStatusNotificationPreservesExistingMetadata(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.PublishFirmwareStatusNotificationHandler{Store: memStore}

	ctx := context.Background()

	// First: CSMS sends PublishFirmware and stores initial accepted status
	initialStatus := &store.PublishFirmwareStatus{
		ChargeStationId: "lc003",
		Status:          store.PublishFirmwareStatusAccepted,
		Location:        "http://firmware.example.com/v2.bin",
		Checksum:        "abc123def456",
		RequestId:       5,
	}
	err := memStore.SetPublishFirmwareStatus(ctx, "lc003", initialStatus)
	require.NoError(t, err)

	// Then: LC sends a status update without location/checksum/requestId
	req := &types.PublishFirmwareStatusNotificationRequestJson{
		Status: types.PublishFirmwareStatusEnumTypeDownloading,
	}

	_, err = handler.HandleCall(ctx, "lc003", req)
	require.NoError(t, err)

	// Verify metadata was preserved from the initial record
	status, err := memStore.GetPublishFirmwareStatus(ctx, "lc003")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, store.PublishFirmwareStatusType("Downloading"), status.Status)
	assert.Equal(t, "http://firmware.example.com/v2.bin", status.Location)
	assert.Equal(t, "abc123def456", status.Checksum)
	assert.Equal(t, 5, status.RequestId)
}

func TestPublishFirmwareStatusNotificationMultipleLocalControllers(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.PublishFirmwareStatusNotificationHandler{Store: memStore}

	ctx := context.Background()

	// lc001: downloading, lc002: published
	requestId2 := 2
	req1 := &types.PublishFirmwareStatusNotificationRequestJson{
		Status: types.PublishFirmwareStatusEnumTypeDownloading,
	}
	req2 := &types.PublishFirmwareStatusNotificationRequestJson{
		Status:    types.PublishFirmwareStatusEnumTypePublished,
		RequestId: &requestId2,
		Location:  []string{"http://192.168.2.1/fw.bin"},
	}

	_, err := handler.HandleCall(ctx, "lc001", req1)
	require.NoError(t, err)

	_, err = handler.HandleCall(ctx, "lc002", req2)
	require.NoError(t, err)

	s1, err := memStore.GetPublishFirmwareStatus(ctx, "lc001")
	require.NoError(t, err)
	assert.Equal(t, store.PublishFirmwareStatusType("Downloading"), s1.Status)

	s2, err := memStore.GetPublishFirmwareStatus(ctx, "lc002")
	require.NoError(t, err)
	assert.Equal(t, store.PublishFirmwareStatusType("Published"), s2.Status)
	assert.Equal(t, "http://192.168.2.1/fw.bin", s2.Location)
}
