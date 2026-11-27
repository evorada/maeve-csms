// SPDX-License-Identifier: Apache-2.0

package nats

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// The header name is checked here, before a server sees it: NATS servers from 2.11 on
// rewrite any spelling of "traceparent" to lower case, older ones pass it as it is.
func TestHeaderCarrierWritesTheW3CHeaderName(t *testing.T) {
	traceID, _ := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	spanID, _ := trace.SpanIDFromHex("b7ad6b7169203331")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	}))

	header := nats.Header{}
	propagation.TraceContext{}.Inject(ctx, headerCarrier(header))

	assert.Equal(t, nats.Header{"traceparent": {"00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"}}, header)
}

func TestHeaderCarrierReadsBothHeaderNames(t *testing.T) {
	const traceparent = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	for _, name := range []string{"traceparent", "Traceparent"} {
		t.Run(name, func(t *testing.T) {
			ctx := propagation.TraceContext{}.Extract(context.Background(), headerCarrier(nats.Header{name: {traceparent}}))
			sc := trace.SpanContextFromContext(ctx)
			assert.True(t, sc.IsValid())
			assert.Equal(t, "0af7651916cd43dd8448eb211c80319c", sc.TraceID().String())
		})
	}
}
