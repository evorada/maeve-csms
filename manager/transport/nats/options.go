// SPDX-License-Identifier: Apache-2.0

package nats

import (
	"go.opentelemetry.io/otel/trace"
)

// Opt configures an Emitter or a Listener.
type Opt[T Emitter | Listener] func(h *T)

// WithSubjectPrefix sets the first token of the subjects (default "ocpp").
func WithSubjectPrefix[T Emitter | Listener](prefix string) Opt[T] {
	return func(h *T) {
		switch x := any(h).(type) {
		case *Emitter:
			x.prefix = prefix
		case *Listener:
			x.prefix = prefix
		}
	}
}

// WithOtelTracer sets the tracer of the publish and receive spans.
func WithOtelTracer[T Emitter | Listener](tracer trace.Tracer) Opt[T] {
	return func(h *T) {
		switch x := any(h).(type) {
		case *Emitter:
			x.tracer = tracer
		case *Listener:
			x.tracer = tracer
		}
	}
}

// WithQueueGroup sets the queue group of the subscription to all charge stations
// (default "maeve-manager"): each message goes to one member of the group.
func WithQueueGroup(group string) Opt[Listener] {
	return func(l *Listener) {
		l.queueGroup = group
	}
}

// WithWorkers sets how many messages a listener connection handles at the same time
// (default 16). Messages of one charge station are always handled in order.
func WithWorkers(workers int) Opt[Listener] {
	return func(l *Listener) {
		l.workers = workers
	}
}
