// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestLogStatusNotificationHandler(t *testing.T) {
	handler := LogStatusNotificationHandler{}

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
	handler := LogStatusNotificationHandler{}

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
	handler := LogStatusNotificationHandler{}

	statuses := []ocpp16.UploadLogStatusEnumType{
		ocpp16.UploadLogStatusEnumTypeBadMessage,
		ocpp16.UploadLogStatusEnumTypeIdle,
		ocpp16.UploadLogStatusEnumTypeNotSupportedOperation,
		ocpp16.UploadLogStatusEnumTypePermissionDenied,
		ocpp16.UploadLogStatusEnumTypeUploaded,
		ocpp16.UploadLogStatusEnumTypeUploadFailure,
		ocpp16.UploadLogStatusEnumTypeUploading,
	}

	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
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
		})
	}
}
