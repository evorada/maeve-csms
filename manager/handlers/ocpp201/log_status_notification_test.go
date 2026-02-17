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

func TestLogStatusNotification(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.LogStatusNotificationHandler{Store: memStore}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.LogStatusNotificationRequestJson{
			Status: types.UploadLogStatusEnumTypeUploaded,
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		assert.Equal(t, &types.LogStatusNotificationResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"log_status.status": "Uploaded",
	})

	// Verify status was persisted
	diagStatus, err := memStore.GetDiagnosticsStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, diagStatus)
	assert.Equal(t, "cs001", diagStatus.ChargeStationId)
	assert.Equal(t, store.DiagnosticsStatusUploaded, diagStatus.Status)
}

func TestLogStatusNotificationWithRequestId(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.LogStatusNotificationHandler{Store: memStore}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		requestId := 999
		req := &types.LogStatusNotificationRequestJson{
			Status:    types.UploadLogStatusEnumTypeIdle,
			RequestId: &requestId,
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		assert.Equal(t, &types.LogStatusNotificationResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"log_status.status":     "Idle",
		"log_status.request_id": 999,
	})

	// Verify Idle status was persisted
	diagStatus, err := memStore.GetDiagnosticsStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, diagStatus)
	assert.Equal(t, store.DiagnosticsStatusIdle, diagStatus.Status)
}

func TestLogStatusNotificationUploadFailure(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.LogStatusNotificationHandler{Store: memStore}

	tracer, _ := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.LogStatusNotificationRequestJson{
			Status: types.UploadLogStatusEnumTypeUploadFailure,
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		assert.Equal(t, &types.LogStatusNotificationResponseJson{}, resp)
	}()

	// UploadFailure should map to UploadFailed in the store
	diagStatus, err := memStore.GetDiagnosticsStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, diagStatus)
	assert.Equal(t, store.DiagnosticsStatusUploadFailed, diagStatus.Status)
}

func TestLogStatusNotificationStatusProgression(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.LogStatusNotificationHandler{Store: memStore}

	ctx := context.Background()
	tracer, _ := testutil.GetTracer()

	statuses := []struct {
		ocpp201Status types.UploadLogStatusEnumType
		storeStatus   store.DiagnosticsStatusType
	}{
		{types.UploadLogStatusEnumTypeUploading, store.DiagnosticsStatusUploading},
		{types.UploadLogStatusEnumTypeUploaded, store.DiagnosticsStatusUploaded},
	}

	for _, s := range statuses {
		func() {
			ctx, span := tracer.Start(ctx, "test")
			defer span.End()

			req := &types.LogStatusNotificationRequestJson{
				Status: s.ocpp201Status,
			}

			resp, err := handler.HandleCall(ctx, "cs001", req)
			require.NoError(t, err)
			assert.Equal(t, &types.LogStatusNotificationResponseJson{}, resp)
		}()

		diagStatus, err := memStore.GetDiagnosticsStatus(ctx, "cs001")
		require.NoError(t, err)
		assert.Equal(t, s.storeStatus, diagStatus.Status,
			"status %s should map to %s", s.ocpp201Status, s.storeStatus)
	}
}

func TestLogStatusNotificationMultipleStations(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.LogStatusNotificationHandler{Store: memStore}

	ctx := context.Background()
	tracer, _ := testutil.GetTracer()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.LogStatusNotificationRequestJson{
			Status: types.UploadLogStatusEnumTypeUploading,
		}

		_, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)
	}()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.LogStatusNotificationRequestJson{
			Status: types.UploadLogStatusEnumTypeUploaded,
		}

		_, err := handler.HandleCall(ctx, "cs002", req)
		require.NoError(t, err)
	}()

	status1, err := memStore.GetDiagnosticsStatus(ctx, "cs001")
	require.NoError(t, err)
	assert.Equal(t, store.DiagnosticsStatusUploading, status1.Status)

	status2, err := memStore.GetDiagnosticsStatus(ctx, "cs002")
	require.NoError(t, err)
	assert.Equal(t, store.DiagnosticsStatusUploaded, status2.Status)
}
