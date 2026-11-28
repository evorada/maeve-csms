// SPDX-License-Identifier: Apache-2.0

package jobs

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

// errRenewalTooLate says that no renewal of the lease succeeded before the job had to
// stop: the lease may expire before the next renewal could get through.
var errRenewalTooLate = errors.New("no renewal of the lease succeeded in time")

// LeasedRunner runs each job only while this instance holds the job's lease.
//
// Per job it takes the lease, runs the job every interval while it holds the lease and
// renews the lease every RenewEvery. The job's context is cancelled, and no further run
// starts, as soon as a renewal fails or (when renewals hang) at the latest
// TTL - RenewEvery after the start of the last successful write of the lease: the
// lease is valid until at least TTL after that start, which leaves RenewEvery for the
// job to stop before another instance can take the lease. Once the job has returned the
// lease is released, and the runner tries to take it again.
type LeasedRunner struct {
	Lease      Lease
	Clock      clock.WithDelayedExecution // clock.RealClock{} when nil
	TTL        time.Duration              // TTL of the leases (DefaultLeaseTTL when 0)
	RenewEvery time.Duration              // TTL/3 when 0; must be shorter than TTL/2
	Tracer     trace.Tracer               // optional; noop when nil
}

// Run runs the jobs until ctx is cancelled, each only while its lease is held. Different
// jobs may be held by different instances, which spreads the load. Run returns nil once
// ctx is cancelled, every job has returned and its lease is released. It returns an
// error without running anything when the jobs or the timings are not valid.
func (r *LeasedRunner) Run(ctx context.Context, jobs []Job) error {
	if err := Validate(jobs); err != nil {
		return err
	}
	if r.Lease == nil {
		return errors.New("leased runner: no lease")
	}
	ttl := r.TTL
	if ttl == 0 {
		ttl = DefaultLeaseTTL
	}
	renewEvery := r.RenewEvery
	if renewEvery == 0 {
		renewEvery = ttl / 3
	}
	if ttl < 0 || renewEvery <= 0 || 2*renewEvery >= ttl {
		return fmt.Errorf("leased runner: the renewal interval %s must be positive and shorter than half the lease ttl %s", renewEvery, ttl)
	}

	var clk clock.WithDelayedExecution = clock.RealClock{}
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
			lj := &leasedJob{job: job, lease: r.Lease, clk: clk, tracer: tracer, ttl: ttl, renewEvery: renewEvery}
			lj.run(ctx)
		}(job)
	}
	wg.Wait()

	return nil
}

// leasedJob is the loop of one job of a LeasedRunner.
type leasedJob struct {
	job        Job
	lease      Lease
	clk        clock.WithDelayedExecution
	tracer     trace.Tracer
	ttl        time.Duration
	renewEvery time.Duration
}

func (j *leasedJob) run(ctx context.Context) {
	for {
		written, ok := j.acquire(ctx)
		if !ok {
			return
		}
		j.hold(ctx, written)
		if ctx.Err() != nil {
			return
		}
	}
}

// acquire tries to take the lease until it succeeds or ctx is cancelled. It returns when
// the successful attempt started: the lease lasts at least the TTL from then.
func (j *leasedJob) acquire(ctx context.Context) (time.Time, bool) {
	failing := false
	for ctx.Err() == nil {
		start := j.clk.Now()
		opCtx, cancel := j.withTimeout(ctx, j.renewEvery)
		ok, err := j.lease.Acquire(opCtx, j.job.Name)
		cancel()
		switch {
		case err != nil && ctx.Err() == nil:
			// logged once per outage: every job retries every few seconds
			if !failing {
				slog.Warn("job lease not available", "job", j.job.Name, "err", err)
				failing = true
			} else {
				slog.Debug("job lease not available", "job", j.job.Name, "err", err)
			}
		case err == nil && ok:
			slog.Info("job lease acquired", "job", j.job.Name)
			return start, true
		case err == nil:
			failing = false
		}

		select {
		case <-ctx.Done():
		case <-j.clk.After(j.retryDelay()):
		}
	}
	return time.Time{}, false
}

// hold runs the job while the lease is held. It returns once the lease is lost or ctx is
// cancelled, the job has returned and the lease is released.
func (j *leasedJob) hold(ctx context.Context, written time.Time) {
	// the job runs with leaseCtx: it ends when the lease is lost, when the deadline of the
	// lease passes without a renewal, and when ctx ends
	leaseCtx, lose := context.WithCancel(ctx)
	defer lose()

	jobDone := make(chan struct{})
	go func() {
		defer close(jobDone)
		runPeriodically(leaseCtx, j.clk, j.tracer, j.job)
	}()

	err := j.keepRenewing(ctx, leaseCtx, lose, written)
	lose()
	<-jobDone
	if err != nil {
		slog.Warn("job lease lost", "job", j.job.Name, "err", err)
	}

	// only after the job has returned: another instance may start it once the key is gone
	releaseCtx, cancel := j.withTimeout(context.WithoutCancel(ctx), j.renewEvery)
	defer cancel()
	if err := j.lease.Release(releaseCtx, j.job.Name); err != nil {
		slog.Warn("releasing job lease", "job", j.job.Name, "err", err)
	}
}

// keepRenewing renews the lease RenewEvery after the start of each successful write and
// calls lose when the job must stop. It returns nil when ctx is cancelled, otherwise why
// the lease is lost.
func (j *leasedJob) keepRenewing(ctx, leaseCtx context.Context, lose context.CancelFunc, written time.Time) error {
	for {
		// cancels the job even when a renewal hangs
		watchdog := j.clk.AfterFunc(j.stopBy(written).Sub(j.clk.Now()), lose)

		select {
		case <-leaseCtx.Done():
			watchdog.Stop()
			if ctx.Err() != nil {
				return nil
			}
			return errRenewalTooLate
		case <-j.clk.After(written.Add(j.renewEvery).Sub(j.clk.Now())):
		}

		start := j.clk.Now()
		err := j.lease.Renew(leaseCtx, j.job.Name)
		fired := !watchdog.Stop()
		switch {
		case ctx.Err() != nil:
			return nil
		case err != nil && fired:
			return fmt.Errorf("%w: %w", errRenewalTooLate, err)
		case err != nil:
			return err
		case fired:
			// the renewal got through, but the job has already been told to stop
			return errRenewalTooLate
		}
		written = start
	}
}

// stopBy is when the job must be cancelled at the latest after a write of the lease
// that started at written: RenewEvery before the earliest moment the lease can expire.
func (j *leasedJob) stopBy(written time.Time) time.Time {
	return written.Add(j.ttl - j.renewEvery)
}

// retryDelay is RenewEvery plus up to 20 % jitter, so that instances that start
// together do not keep asking at the same moment.
func (j *leasedJob) retryDelay() time.Duration {
	//#nosec G404 - the jitter of a retry does not need a secure random number generator
	return j.renewEvery + time.Duration(rand.Int64N(int64(j.renewEvery)/5+1))
}

// withTimeout is context.WithTimeout on the runner's clock.
func (j *leasedJob) withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	timer := j.clk.AfterFunc(d, cancel)
	return ctx, func() {
		timer.Stop()
		cancel()
	}
}
