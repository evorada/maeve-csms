// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	clockTest "k8s.io/utils/clock/testing"
)

// failingLogStatusStore fails every write of a log status.
type failingLogStatusStore struct {
	store.FirmwareStore
	err error
}

func (s failingLogStatusStore) SetLogStatus(context.Context, string, *store.LogStatus) error {
	return s.err
}

func newLogStatusNotificationHandler(now time.Time) (LogStatusNotificationHandler, *inmemory.Store) {
	clk := clockTest.NewFakePassiveClock(now)
	engine := inmemory.NewStore(clk)
	return LogStatusNotificationHandler{Clock: clk, FirmwareStore: engine, EventStore: engine}, engine
}

func TestLogStatusNotificationHandler(t *testing.T) {
	handler, _ := newLogStatusNotificationHandler(time.Now())

	traceExporter := tracetest.NewInMemoryExporter()
	tracerProvider := trace.NewTracerProvider(
		trace.WithSampler(trace.AlwaysSample()),
		trace.WithSyncer(traceExporter),
	)
	otel.SetTracerProvider(tracerProvider)

	ctx := context.Background()

	func() {
		ctx, span := tracerProvider.Tracer("test").Start(ctx, "test")
		defer span.End()

		req := &ocpp16.LogStatusNotificationJson{
			Status: ocpp16.UploadLogStatusEnumTypeUploaded,
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		assert.Equal(t, &ocpp16.LogStatusNotificationResponseJson{}, resp)
	}()

	require.Len(t, traceExporter.GetSpans(), 1)
	require.Len(t, traceExporter.GetSpans()[0].Attributes, 1)
	for _, attr := range traceExporter.GetSpans()[0].Attributes {
		switch attr.Key {
		case "log_status.status":
			assert.Equal(t, "Uploaded", attr.Value.AsString())
		default:
			t.Errorf("unexpected attribute %s", attr.Key)
		}
	}
}

func TestLogStatusNotificationHandlerWithRequestId(t *testing.T) {
	handler, _ := newLogStatusNotificationHandler(time.Now())

	traceExporter := tracetest.NewInMemoryExporter()
	tracerProvider := trace.NewTracerProvider(
		trace.WithSampler(trace.AlwaysSample()),
		trace.WithSyncer(traceExporter),
	)
	otel.SetTracerProvider(tracerProvider)

	ctx := context.Background()

	func() {
		ctx, span := tracerProvider.Tracer("test").Start(ctx, "test")
		defer span.End()

		requestId := 42
		req := &ocpp16.LogStatusNotificationJson{
			Status:    ocpp16.UploadLogStatusEnumTypeIdle,
			RequestId: &requestId,
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		assert.Equal(t, &ocpp16.LogStatusNotificationResponseJson{}, resp)
	}()

	require.Len(t, traceExporter.GetSpans(), 1)
	require.Len(t, traceExporter.GetSpans()[0].Attributes, 2)
	foundStatus := false
	foundRequestId := false
	for _, attr := range traceExporter.GetSpans()[0].Attributes {
		switch attr.Key {
		case "log_status.status":
			assert.Equal(t, "Idle", attr.Value.AsString())
			foundStatus = true
		case "log_status.request_id":
			assert.Equal(t, int64(42), attr.Value.AsInt64())
			foundRequestId = true
		default:
			t.Errorf("unexpected attribute %s", attr.Key)
		}
	}
	assert.True(t, foundStatus, "expected log_status.status attribute")
	assert.True(t, foundRequestId, "expected log_status.request_id attribute")
}

func TestLogStatusNotificationHandlerAllStatuses(t *testing.T) {
	statuses := map[ocpp16.UploadLogStatusEnumType]store.LogStatusType{
		ocpp16.UploadLogStatusEnumTypeBadMessage:            store.LogStatusBadMessage,
		ocpp16.UploadLogStatusEnumTypeIdle:                  store.LogStatusIdle,
		ocpp16.UploadLogStatusEnumTypeNotSupportedOperation: store.LogStatusNotSupportedOperation,
		ocpp16.UploadLogStatusEnumTypePermissionDenied:      store.LogStatusPermissionDenied,
		ocpp16.UploadLogStatusEnumTypeUploaded:              store.LogStatusUploaded,
		ocpp16.UploadLogStatusEnumTypeUploadFailure:         store.LogStatusUploadFailure,
		ocpp16.UploadLogStatusEnumTypeUploading:             store.LogStatusUploading,
	}

	for status, wantStatus := range statuses {
		t.Run(string(status), func(t *testing.T) {
			handler, engine := newLogStatusNotificationHandler(time.Now())

			traceExporter := tracetest.NewInMemoryExporter()
			tracerProvider := trace.NewTracerProvider(
				trace.WithSampler(trace.AlwaysSample()),
				trace.WithSyncer(traceExporter),
			)

			ctx := context.Background()

			func() {
				ctx, span := tracerProvider.Tracer("test").Start(ctx, "test")
				defer span.End()

				req := &ocpp16.LogStatusNotificationJson{
					Status: status,
				}

				resp, err := handler.HandleCall(ctx, "cs001", req)
				require.NoError(t, err)
				assert.Equal(t, &ocpp16.LogStatusNotificationResponseJson{}, resp)
			}()

			require.Len(t, traceExporter.GetSpans(), 1)

			logStatus, err := engine.GetLogStatus(ctx, "cs001")
			require.NoError(t, err)
			require.NotNil(t, logStatus)
			assert.Equal(t, wantStatus, logStatus.Status)
		})
	}
}

func TestLogStatusNotificationHandlerStoresStatusAndEvent(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	handler, engine := newLogStatusNotificationHandler(now)
	ctx := context.Background()

	requestId := 7
	resp, err := handler.HandleCall(ctx, "cs001", &ocpp16.LogStatusNotificationJson{
		Status:    ocpp16.UploadLogStatusEnumTypeUploading,
		RequestId: &requestId,
	})
	require.NoError(t, err)
	assert.Equal(t, &ocpp16.LogStatusNotificationResponseJson{}, resp)

	logStatus, err := engine.GetLogStatus(ctx, "cs001")
	require.NoError(t, err)
	assert.Equal(t, &store.LogStatus{
		ChargeStationId: "cs001",
		Status:          store.LogStatusUploading,
		RequestId:       7,
		UpdatedAt:       now,
	}, logStatus)

	events, total, err := engine.ListChargeStationEvents(ctx, "cs001", 0, 10)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, events, 1)
	got := events[0]
	assert.Equal(t, "LogStatusNotification", got.EventType)
	assert.Equal(t, store.ChargeStationEventCategoryLogStatus, got.Category)
	assert.True(t, now.Equal(got.Timestamp), "timestamp: %s", got.Timestamp)
	require.NotNil(t, got.EventData)
	assert.JSONEq(t, `{"status":"Uploading","requestId":7}`, *got.EventData)

	// the next notification replaces the current status and adds to the history
	_, err = handler.HandleCall(ctx, "cs001", &ocpp16.LogStatusNotificationJson{
		Status:    ocpp16.UploadLogStatusEnumTypeUploaded,
		RequestId: &requestId,
	})
	require.NoError(t, err)

	logStatus, err = engine.GetLogStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, logStatus)
	assert.Equal(t, store.LogStatusUploaded, logStatus.Status)

	events, total, err = engine.ListChargeStationEvents(ctx, "cs001", 0, 10)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	var payloads []string
	for _, event := range events {
		require.NotNil(t, event.EventData)
		payloads = append(payloads, *event.EventData)
	}
	assert.ElementsMatch(t, []string{`{"status":"Uploading","requestId":7}`, `{"status":"Uploaded","requestId":7}`}, payloads)
}

func TestLogStatusNotificationHandlerWithoutRequestIdStoresZero(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	handler, engine := newLogStatusNotificationHandler(now)
	ctx := context.Background()

	_, err := handler.HandleCall(ctx, "cs001", &ocpp16.LogStatusNotificationJson{
		Status: ocpp16.UploadLogStatusEnumTypeIdle,
	})
	require.NoError(t, err)

	logStatus, err := engine.GetLogStatus(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, logStatus)
	assert.Equal(t, store.LogStatusIdle, logStatus.Status)
	assert.Equal(t, 0, logStatus.RequestId)

	events, _, err := engine.ListChargeStationEvents(ctx, "cs001", 0, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.NotNil(t, events[0].EventData)
	assert.JSONEq(t, `{"status":"Idle","requestId":0}`, *events[0].EventData)
	assert.Equal(t, store.ChargeStationEventCategoryLogStatus, events[0].Category)
}

func TestLogStatusNotificationHandlerReturnsStoreErrors(t *testing.T) {
	storeErr := errors.New("store is down")
	requestId := 7
	req := &ocpp16.LogStatusNotificationJson{
		Status:    ocpp16.UploadLogStatusEnumTypeUploading,
		RequestId: &requestId,
	}

	t.Run("log status", func(t *testing.T) {
		handler, engine := newLogStatusNotificationHandler(time.Now())
		handler.FirmwareStore = failingLogStatusStore{FirmwareStore: engine, err: storeErr}

		resp, err := handler.HandleCall(context.Background(), "cs001", req)
		assert.ErrorIs(t, err, storeErr)
		assert.Nil(t, resp)

		// no history without the status it belongs to
		_, total, err := engine.ListChargeStationEvents(context.Background(), "cs001", 0, 10)
		require.NoError(t, err)
		assert.Equal(t, 0, total)
	})

	t.Run("event", func(t *testing.T) {
		handler, _ := newLogStatusNotificationHandler(time.Now())
		handler.EventStore = failingEventStore{err: storeErr}

		resp, err := handler.HandleCall(context.Background(), "cs001", req)
		assert.ErrorIs(t, err, storeErr)
		assert.Nil(t, resp)
	})
}
