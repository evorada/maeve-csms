// SPDX-License-Identifier: Apache-2.0

package jobs

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

// LocalRunner runs every job in this process (no cross-replica coordination).
type LocalRunner struct {
	Clock  clock.Clock  // clock.RealClock{} when nil
	Tracer trace.Tracer // optional; noop when nil
}

// Run starts one loop per job: wait for the job's interval, run the job once, repeat.
// A run that fails or panics is logged and does not stop the loop. Run blocks until
// ctx is cancelled and every loop has returned, then returns nil. It returns an error
// without running anything when the jobs are not valid.
func (r *LocalRunner) Run(ctx context.Context, jobs []Job) error {
	if err := Validate(jobs); err != nil {
		return err
	}

	var clk clock.Clock = clock.RealClock{}
	if r.Clock != nil {
		clk = r.Clock
	}
	tracer := r.Tracer
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("")
	}

	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Add(1)
		go func(job Job) {
			defer wg.Done()
			runPeriodically(ctx, clk, tracer, job)
		}(job)
	}
	wg.Wait()

	return nil
}

func runPeriodically(ctx context.Context, clk clock.Clock, tracer trace.Tracer, job Job) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-clk.After(job.Interval):
		}
		// the interval may elapse at the same moment as the cancellation: never start a run after it
		if ctx.Err() != nil {
			return
		}
		runOnce(ctx, tracer, job)
	}
}

func runOnce(ctx context.Context, tracer trace.Tracer, job Job) {
	ctx, span := tracer.Start(ctx, "job "+job.Name, trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attribute.String("job.name", job.Name)))
	defer span.End()

	defer func() {
		if p := recover(); p != nil {
			// a deferred function runs on the stack of the panic, so both stacks show where it happened
			span.RecordError(fmt.Errorf("panic: %v", p), trace.WithStackTrace(true))
			span.SetStatus(codes.Error, "job panicked")
			slog.Error("job panicked", "job", job.Name, "panic", p, "stack", string(debug.Stack()))
		}
	}()

	if err := job.Run(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "job failed")
		slog.Error("job failed", "job", job.Name, "err", err)
	}
}
