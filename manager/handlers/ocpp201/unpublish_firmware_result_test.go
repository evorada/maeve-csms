// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"k8s.io/utils/clock"
)

func TestUnpublishFirmwareResultHandler_Unpublished(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.UnpublishFirmwareResultHandler{Store: memStore}

	ctx := context.Background()

	// Pre-seed a published firmware status
	err := memStore.SetPublishFirmwareStatus(ctx, "lc001", &store.PublishFirmwareStatus{
		ChargeStationId: "lc001",
		Status:          store.PublishFirmwareStatusPublished,
		Location:        "https://firmware.example.com/v2.0.bin",
		Checksum:        "d41d8cd98f00b204e9800998ecf8427e",
		RequestId:       1,
		UpdatedAt:       time.Now().UTC(),
	})
	require.NoError(t, err)

	tracer, exporter := testutil.GetTracer()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.UnpublishFirmwareRequestJson{
			Checksum: "d41d8cd98f00b204e9800998ecf8427e",
		}
		resp := &types.UnpublishFirmwareResponseJson{
			Status: types.UnpublishFirmwareStatusUnpublished,
		}

		err := handler.HandleCallResult(ctx, "lc001", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"unpublish_firmware.checksum": "d41d8cd98f00b204e9800998ecf8427e",
		"unpublish_firmware.status":   "Unpublished",
	})

	// Status should now be Idle after successful unpublish
	status, err := memStore.GetPublishFirmwareStatus(ctx, "lc001")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "lc001", status.ChargeStationId)
	assert.Equal(t, string(store.PublishFirmwareStatusIdle), string(status.Status))
	assert.Equal(t, "d41d8cd98f00b204e9800998ecf8427e", status.Checksum)
}

func TestUnpublishFirmwareResultHandler_DownloadOngoing(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.UnpublishFirmwareResultHandler{Store: memStore}

	ctx := context.Background()

	// Pre-seed a downloading firmware status
	originalStatus := &store.PublishFirmwareStatus{
		ChargeStationId: "lc002",
		Status:          store.PublishFirmwareStatusDownloading,
		Location:        "https://firmware.example.com/v3.0.bin",
		Checksum:        "abc123def456abc123def456abc12345",
		RequestId:       5,
		UpdatedAt:       time.Now().UTC(),
	}
	err := memStore.SetPublishFirmwareStatus(ctx, "lc002", originalStatus)
	require.NoError(t, err)

	req := &types.UnpublishFirmwareRequestJson{
		Checksum: "abc123def456abc123def456abc12345",
	}
	resp := &types.UnpublishFirmwareResponseJson{
		Status: types.UnpublishFirmwareStatusDownloadOngoing,
	}

	err = handler.HandleCallResult(ctx, "lc002", req, resp, nil)
	require.NoError(t, err)

	// Status should remain unchanged (download is ongoing, unpublish rejected)
	status, err := memStore.GetPublishFirmwareStatus(ctx, "lc002")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, string(store.PublishFirmwareStatusDownloading), string(status.Status))
}

func TestUnpublishFirmwareResultHandler_NoFirmware(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.UnpublishFirmwareResultHandler{Store: memStore}

	ctx := context.Background()

	tracer, exporter := testutil.GetTracer()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.UnpublishFirmwareRequestJson{
			Checksum: "ffffffffffffffffffffffffffffffff",
		}
		resp := &types.UnpublishFirmwareResponseJson{
			Status: types.UnpublishFirmwareStatusNoFirmware,
		}

		// No error expected — station simply doesn't have this firmware
		err := handler.HandleCallResult(ctx, "lc003", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"unpublish_firmware.checksum": "ffffffffffffffffffffffffffffffff",
		"unpublish_firmware.status":   "NoFirmware",
	})

	// No status should exist for this station
	status, err := memStore.GetPublishFirmwareStatus(ctx, "lc003")
	require.NoError(t, err)
	assert.Nil(t, status)
}

func TestUnpublishFirmwareResultHandler_MultipleStations(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.UnpublishFirmwareResultHandler{Store: memStore}

	ctx := context.Background()

	// Both stations have firmware published
	checksum := "aaaabbbbccccddddaaaabbbbccccdddd"
	for _, lcId := range []string{"lc-alpha", "lc-beta"} {
		err := memStore.SetPublishFirmwareStatus(ctx, lcId, &store.PublishFirmwareStatus{
			ChargeStationId: lcId,
			Status:          store.PublishFirmwareStatusPublished,
			Location:        "ftp://fw.example.com/image.bin",
			Checksum:        checksum,
			RequestId:       10,
			UpdatedAt:       time.Now().UTC(),
		})
		require.NoError(t, err)
	}

	// Unpublish succeeds on lc-alpha
	err := handler.HandleCallResult(ctx, "lc-alpha", &types.UnpublishFirmwareRequestJson{
		Checksum: checksum,
	}, &types.UnpublishFirmwareResponseJson{
		Status: types.UnpublishFirmwareStatusUnpublished,
	}, nil)
	require.NoError(t, err)

	// Unpublish fails on lc-beta (download ongoing)
	err = handler.HandleCallResult(ctx, "lc-beta", &types.UnpublishFirmwareRequestJson{
		Checksum: checksum,
	}, &types.UnpublishFirmwareResponseJson{
		Status: types.UnpublishFirmwareStatusDownloadOngoing,
	}, nil)
	require.NoError(t, err)

	// lc-alpha → Idle; lc-beta → still Published
	sA, err := memStore.GetPublishFirmwareStatus(ctx, "lc-alpha")
	require.NoError(t, err)
	require.NotNil(t, sA)
	assert.Equal(t, string(store.PublishFirmwareStatusIdle), string(sA.Status))

	sB, err := memStore.GetPublishFirmwareStatus(ctx, "lc-beta")
	require.NoError(t, err)
	require.NotNil(t, sB)
	assert.Equal(t, string(store.PublishFirmwareStatusPublished), string(sB.Status))
}

func TestUnpublishFirmwareResultHandler_SpanAttributes(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.UnpublishFirmwareResultHandler{Store: memStore}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.UnpublishFirmwareRequestJson{
			Checksum: "12345678901234567890123456789012",
		}
		resp := &types.UnpublishFirmwareResponseJson{
			Status: types.UnpublishFirmwareStatusUnpublished,
		}

		err := handler.HandleCallResult(ctx, "lc004", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"unpublish_firmware.checksum": "12345678901234567890123456789012",
		"unpublish_firmware.status":   "Unpublished",
	})
}
