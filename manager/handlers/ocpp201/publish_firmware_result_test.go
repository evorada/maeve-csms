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

func TestPublishFirmwareResultHandler_Accepted(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.PublishFirmwareResultHandler{Store: memStore}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.PublishFirmwareRequestJson{
			RequestId: 1,
			Location:  "https://firmware.example.com/v2.0.bin",
			Checksum:  "d41d8cd98f00b204e9800998ecf8427e",
		}
		resp := &types.PublishFirmwareResponseJson{
			Status: types.GenericStatusEnumTypeAccepted,
		}

		err := handler.HandleCallResult(ctx, "lc001", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"publish_firmware.request_id": 1,
		"publish_firmware.location":   "https://firmware.example.com/v2.0.bin",
		"publish_firmware.checksum":   "d41d8cd98f00b204e9800998ecf8427e",
		"publish_firmware.status":     "Accepted",
	})

	// Verify publish firmware status was persisted as Accepted
	status, err := memStore.GetPublishFirmwareStatus(ctx, "lc001")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "lc001", status.ChargeStationId)
	assert.Equal(t, "Accepted", string(status.Status))
	assert.Equal(t, "https://firmware.example.com/v2.0.bin", status.Location)
	assert.Equal(t, "d41d8cd98f00b204e9800998ecf8427e", status.Checksum)
	assert.Equal(t, 1, status.RequestId)
}

func TestPublishFirmwareResultHandler_Rejected(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.PublishFirmwareResultHandler{Store: memStore}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.PublishFirmwareRequestJson{
			RequestId: 2,
			Location:  "https://firmware.example.com/v2.0.bin",
			Checksum:  "d41d8cd98f00b204e9800998ecf8427e",
		}
		resp := &types.PublishFirmwareResponseJson{
			Status: types.GenericStatusEnumTypeRejected,
		}

		err := handler.HandleCallResult(ctx, "lc001", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"publish_firmware.request_id": 2,
		"publish_firmware.location":   "https://firmware.example.com/v2.0.bin",
		"publish_firmware.checksum":   "d41d8cd98f00b204e9800998ecf8427e",
		"publish_firmware.status":     "Rejected",
	})

	// Publish firmware status should NOT be stored when rejected
	status, err := memStore.GetPublishFirmwareStatus(ctx, "lc001")
	require.NoError(t, err)
	assert.Nil(t, status)
}

func TestPublishFirmwareResultHandler_AcceptedWithRetries(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.PublishFirmwareResultHandler{Store: memStore}

	ctx := context.Background()

	retries := 3
	retryInterval := 60
	req := &types.PublishFirmwareRequestJson{
		RequestId:     5,
		Location:      "https://firmware.example.com/v3.0.bin",
		Checksum:      "abc123def456abc123def456abc12345",
		Retries:       &retries,
		RetryInterval: &retryInterval,
	}
	resp := &types.PublishFirmwareResponseJson{
		Status: types.GenericStatusEnumTypeAccepted,
	}

	err := handler.HandleCallResult(ctx, "lc002", req, resp, nil)
	require.NoError(t, err)

	status, err := memStore.GetPublishFirmwareStatus(ctx, "lc002")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "Accepted", string(status.Status))
	assert.Equal(t, "https://firmware.example.com/v3.0.bin", status.Location)
	assert.Equal(t, "abc123def456abc123def456abc12345", status.Checksum)
	assert.Equal(t, 5, status.RequestId)
}

func TestPublishFirmwareResultHandler_MultipleStations(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.PublishFirmwareResultHandler{Store: memStore}

	ctx := context.Background()

	// First local controller accepts
	req1 := &types.PublishFirmwareRequestJson{
		RequestId: 10,
		Location:  "https://firmware.example.com/v1.bin",
		Checksum:  "aaaabbbbccccddddaaaabbbbccccdddd",
	}
	err := handler.HandleCallResult(ctx, "lc-alpha", req1, &types.PublishFirmwareResponseJson{
		Status: types.GenericStatusEnumTypeAccepted,
	}, nil)
	require.NoError(t, err)

	// Second local controller rejects
	req2 := &types.PublishFirmwareRequestJson{
		RequestId: 11,
		Location:  "https://firmware.example.com/v1.bin",
		Checksum:  "aaaabbbbccccddddaaaabbbbccccdddd",
	}
	err = handler.HandleCallResult(ctx, "lc-beta", req2, &types.PublishFirmwareResponseJson{
		Status: types.GenericStatusEnumTypeRejected,
	}, nil)
	require.NoError(t, err)

	// lc-alpha should have status, lc-beta should not
	s1, err := memStore.GetPublishFirmwareStatus(ctx, "lc-alpha")
	require.NoError(t, err)
	require.NotNil(t, s1)
	assert.Equal(t, "Accepted", string(s1.Status))
	assert.Equal(t, 10, s1.RequestId)

	s2, err := memStore.GetPublishFirmwareStatus(ctx, "lc-beta")
	require.NoError(t, err)
	assert.Nil(t, s2)
}

func TestPublishFirmwareResultHandler_SpanAttributes(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.PublishFirmwareResultHandler{Store: memStore}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	retries := 2
	retryInterval := 30

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.PublishFirmwareRequestJson{
			RequestId:     99,
			Location:      "ftp://updates.example.com/fw.bin",
			Checksum:      "11112222333344441111222233334444",
			Retries:       &retries,
			RetryInterval: &retryInterval,
		}
		resp := &types.PublishFirmwareResponseJson{
			Status: types.GenericStatusEnumTypeAccepted,
		}

		err := handler.HandleCallResult(ctx, "lc003", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"publish_firmware.request_id":    99,
		"publish_firmware.location":      "ftp://updates.example.com/fw.bin",
		"publish_firmware.checksum":      "11112222333344441111222233334444",
		"publish_firmware.status":        "Accepted",
		"publish_firmware.retries":       2,
		"publish_firmware.retry_interval": 30,
	})
}
