// SPDX-License-Identifier: Apache-2.0

package jobs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"go.opentelemetry.io/otel/codes"
	clocktesting "k8s.io/utils/clock/testing"
)

// waitTimeout only bounds how long a broken runner can block a test: nothing waits this long when the tests pass.
const waitTimeout = 10 * time.Second

// runningRunner is a runner started in the background by startRunner.
type runningRunner struct {
	cancel context.CancelFunc
	done   chan struct{} // closed when Run has returned
	err    error         // result of Run, valid once done is closed
}

// startRunner runs the jobs in the background. The runner is stopped at the end of the test.
func startRunner(t *testing.T, runner jobs.Runner, jobList []jobs.Job) *runningRunner {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := &runningRunner{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		r.err = runner.Run(ctx, jobList)
	}()
	t.Cleanup(func() {
		cancel()
		_ = r.wait(t)
	})
	return r
}

// wait blocks until Run has returned and gives its result.
func (r *runningRunner) wait(t *testing.T) error {
	t.Helper()
	select {
	case <-r.done:
	case <-time.After(waitTimeout):
		t.Fatal("the runner did not return")
	}
	return r.err
}

// waitUntilIdle blocks until the job is waiting for its interval: any earlier run has completed.
func waitUntilIdle(t *testing.T, clk *clocktesting.FakeClock) {
	t.Helper()
	require.Eventually(t, clk.HasWaiters, waitTimeout, time.Millisecond, "the job never waited for its interval")
}

// step moves the clock forward once the job is waiting for its interval.
func step(t *testing.T, clk *clocktesting.FakeClock, d time.Duration) {
	t.Helper()
	waitUntilIdle(t, clk)
	clk.Step(d)
}

// waitForRun blocks until the job reports a run.
func waitForRun[T any](t *testing.T, runs <-chan T) T {
	t.Helper()
	select {
	case run := <-runs:
		return run
	case <-time.After(waitTimeout):
		t.Fatal("the job did not run")
	}
	panic("unreachable")
}

func TestLocalRunnerRunsJobOncePerInterval(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	started := clk.Now()
	runs := make(chan time.Time, 10)
	r := startRunner(t, &jobs.LocalRunner{Clock: clk}, []jobs.Job{{
		Name:     "tick",
		Interval: time.Minute,
		Run: func(context.Context) error {
			runs <- clk.Now()
			return nil
		},
	}})

	// the first run happens after one whole interval: not at the start and not before
	step(t, clk, 59*time.Second)
	step(t, clk, time.Second)
	assert.Equal(t, started.Add(time.Minute), waitForRun(t, runs))

	step(t, clk, time.Minute)
	assert.Equal(t, started.Add(2*time.Minute), waitForRun(t, runs))

	step(t, clk, time.Minute)
	assert.Equal(t, started.Add(3*time.Minute), waitForRun(t, runs))

	r.cancel()
	require.NoError(t, r.wait(t))
	assert.Empty(t, runs, "the job ran more than once per interval")
}

func TestLocalRunnerWaitsAWholeIntervalAfterTheEndOfARun(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	started := clk.Now()
	runs := make(chan time.Time, 10)
	release := make(chan struct{})
	r := startRunner(t, &jobs.LocalRunner{Clock: clk}, []jobs.Job{{
		Name:     "slow",
		Interval: time.Minute,
		Run: func(context.Context) error {
			runs <- clk.Now()
			<-release
			return nil
		},
	}})

	step(t, clk, time.Minute)
	assert.Equal(t, started.Add(time.Minute), waitForRun(t, runs))

	// the run takes five minutes: the intervals that pass meanwhile are not caught up afterwards
	clk.Step(5 * time.Minute)
	release <- struct{}{}

	step(t, clk, 59*time.Second)
	step(t, clk, time.Second)
	assert.Equal(t, started.Add(7*time.Minute), waitForRun(t, runs))
	release <- struct{}{}

	waitUntilIdle(t, clk)
	r.cancel()
	require.NoError(t, r.wait(t))
	assert.Empty(t, runs, "the job ran again without waiting for its interval")
}

func TestLocalRunnerKeepsRunningAfterAnError(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	tracer, exporter := testutil.GetTracer()
	runs := make(chan int, 10)
	count := 0 // only used by the job's goroutine
	startRunner(t, &jobs.LocalRunner{Clock: clk, Tracer: tracer}, []jobs.Job{{
		Name:     "failing",
		Interval: time.Minute,
		Run: func(context.Context) error {
			count++
			runs <- count
			if count == 1 {
				return errors.New("store unavailable")
			}
			return nil
		},
	}})

	step(t, clk, time.Minute)
	assert.Equal(t, 1, waitForRun(t, runs))
	step(t, clk, time.Minute)
	assert.Equal(t, 2, waitForRun(t, runs))
	step(t, clk, time.Minute)
	assert.Equal(t, 3, waitForRun(t, runs))

	// every run has its own span, the failed one carries the error
	waitUntilIdle(t, clk)
	spans := exporter.GetSpans()
	require.Len(t, spans, 3)
	testutil.AssertSpan(t, &spans[0], "job failing", map[string]any{"job.name": "failing"})
	assert.Equal(t, codes.Error, spans[0].Status.Code)
	require.Len(t, spans[0].Events, 1)
	assert.Equal(t, "exception", spans[0].Events[0].Name)
	testutil.AssertSpan(t, &spans[1], "job failing", map[string]any{"job.name": "failing"})
	assert.Equal(t, codes.Unset, spans[1].Status.Code)
	assert.Empty(t, spans[1].Events)
}

func TestLocalRunnerKeepsRunningAfterAPanic(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	tracer, exporter := testutil.GetTracer()
	runs := make(chan int, 10)
	count := 0 // only used by the job's goroutine
	startRunner(t, &jobs.LocalRunner{Clock: clk, Tracer: tracer}, []jobs.Job{{
		Name:     "panicking",
		Interval: time.Minute,
		Run: func(context.Context) error {
			count++
			runs <- count
			if count == 1 {
				panic("the first run panics")
			}
			return nil
		},
	}})

	step(t, clk, time.Minute)
	assert.Equal(t, 1, waitForRun(t, runs))
	step(t, clk, time.Minute)
	assert.Equal(t, 2, waitForRun(t, runs))

	// the span of the run that panicked is ended and carries the panic
	waitUntilIdle(t, clk)
	spans := exporter.GetSpans()
	require.Len(t, spans, 2)
	testutil.AssertSpan(t, &spans[0], "job panicking", map[string]any{"job.name": "panicking"})
	assert.Equal(t, codes.Error, spans[0].Status.Code)
	assert.Equal(t, "job panicked", spans[0].Status.Description)
	require.Len(t, spans[0].Events, 1)
	assert.Equal(t, "exception", spans[0].Events[0].Name)
	assert.Equal(t, codes.Unset, spans[1].Status.Code)
}

func TestLocalRunnerStopsWhenContextIsCancelled(t *testing.T) {
	t.Run("while waiting for the interval", func(t *testing.T) {
		clk := clocktesting.NewFakeClock(time.Now())
		runs := make(chan struct{}, 10)
		r := startRunner(t, &jobs.LocalRunner{Clock: clk}, []jobs.Job{{
			Name:     "tick",
			Interval: time.Minute,
			Run: func(context.Context) error {
				runs <- struct{}{}
				return nil
			},
		}})

		step(t, clk, time.Minute)
		waitForRun(t, runs)
		waitUntilIdle(t, clk)

		r.cancel()
		require.NoError(t, r.wait(t))

		// Run has returned, so nothing is left that could run the job
		clk.Step(10 * time.Minute)
		assert.Empty(t, runs, "the job ran after the runner was stopped")
	})

	t.Run("while a job is running", func(t *testing.T) {
		clk := clocktesting.NewFakeClock(time.Now())
		running := make(chan struct{})
		cancelled := make(chan struct{})
		release := make(chan struct{})
		r := startRunner(t, &jobs.LocalRunner{Clock: clk}, []jobs.Job{{
			Name:     "long",
			Interval: time.Minute,
			Run: func(ctx context.Context) error {
				close(running)
				<-ctx.Done()
				close(cancelled)
				<-release
				return ctx.Err()
			},
		}})

		step(t, clk, time.Minute)
		waitForRun(t, running)
		r.cancel()

		// the job is told to stop through its context...
		waitForRun(t, cancelled)
		// ...and Run does not return before the job has
		select {
		case <-r.done:
			t.Fatal("the runner returned while a job was still running")
		case <-time.After(50 * time.Millisecond):
		}
		close(release)
		require.NoError(t, r.wait(t))
	})

	t.Run("without jobs", func(t *testing.T) {
		r := startRunner(t, &jobs.LocalRunner{}, nil)
		r.cancel()
		require.NoError(t, r.wait(t))
	})
}

func TestLocalRunnerRejectsInvalidJobs(t *testing.T) {
	ran := make(chan struct{}, 10)
	run := func(context.Context) error {
		ran <- struct{}{}
		return nil
	}
	valid := jobs.Job{Name: "valid-job-1", Interval: time.Minute, Run: run}

	tests := map[string][]jobs.Job{
		"duplicate names":   {valid, {Name: "other", Interval: time.Minute, Run: run}, {Name: "valid-job-1", Interval: time.Hour, Run: run}},
		"zero interval":     {valid, {Name: "other", Run: run}},
		"negative interval": {{Name: "other", Interval: -time.Minute, Run: run}},
		"empty name":        {{Interval: time.Minute, Run: run}},
		"upper case name":   {{Name: "Reservation-Expiry", Interval: time.Minute, Run: run}},
		"name with a space": {{Name: "reservation expiry", Interval: time.Minute, Run: run}},
		"name with a dot":   {{Name: "reservation.expiry", Interval: time.Minute, Run: run}},
		"no run function":   {{Name: "other", Interval: time.Minute}},
	}
	for name, jobList := range tests {
		t.Run(name, func(t *testing.T) {
			require.Error(t, jobs.Validate(jobList))

			// the runner refuses the whole list at once: it does not wait for the context and runs nothing
			clk := clocktesting.NewFakeClock(time.Now())
			r := startRunner(t, &jobs.LocalRunner{Clock: clk}, jobList)
			require.Error(t, r.wait(t))
			assert.False(t, clk.HasWaiters(), "a job was started")
			assert.Empty(t, ran)
		})
	}

	t.Run("valid jobs", func(t *testing.T) {
		assert.NoError(t, jobs.Validate(nil))
		assert.NoError(t, jobs.Validate([]jobs.Job{valid, {Name: "0", Interval: time.Nanosecond, Run: run}}))
	})
}
