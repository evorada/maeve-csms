// SPDX-License-Identifier: Apache-2.0

package jobs_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	clocktesting "k8s.io/utils/clock/testing"
)

// The fake-clock tests use a lease TTL far longer than the renewal interval and move the
// clock in steps far shorter than it, so that a renewal never misses its deadline just
// because a goroutine was not scheduled between two steps.
const (
	testTTL        = 30 * time.Minute
	testRenewEvery = time.Minute
	testInterval   = 30 * time.Second
	fakeStep       = 10 * time.Second
)

// leaseTable is the shared store of the fake leases: a lease expires testTTL after its
// last write, on the fake clock.
type leaseTable struct {
	clk *clocktesting.FakeClock

	mu      sync.Mutex
	entries map[string]tableEntry
	log     []string // what happened, in order (shared with the jobs of a test)
}

type tableEntry struct {
	holder  string
	expires time.Time
}

func newLeaseTable(clk *clocktesting.FakeClock) *leaseTable {
	return &leaseTable{clk: clk, entries: make(map[string]tableEntry)}
}

// holder is who holds the lease now ("" when nobody does).
func (tb *leaseTable) holder(name string) string {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	e, ok := tb.entries[name]
	if !ok || !e.expires.After(tb.clk.Now()) {
		return ""
	}
	return e.holder
}

// expires is when the lease expires unless it is renewed.
func (tb *leaseTable) expires(name string) time.Time {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return tb.entries[name].expires
}

// takeOver writes the lease for another holder, as another instance would after an expiry.
func (tb *leaseTable) takeOver(name, holder string) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.entries[name] = tableEntry{holder: holder, expires: tb.clk.Now().Add(testTTL)}
}

func (tb *leaseTable) remove(name string) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	delete(tb.entries, name)
}

func (tb *leaseTable) record(event string) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.log = append(tb.log, event)
}

func (tb *leaseTable) events() []string {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return append([]string(nil), tb.log...)
}

// fakeLease is the lease of one runner on a leaseTable.
type fakeLease struct {
	table  *leaseTable
	holder string

	mu        sync.Mutex
	err       error // when set, every call fails with it
	hangRenew bool  // when set, Renew blocks until its context ends
	calls     map[string]int
	renewedAt []time.Time
}

func newFakeLease(table *leaseTable, holder string) *fakeLease {
	return &fakeLease{table: table, holder: holder, calls: make(map[string]int)}
}

func (l *fakeLease) setErr(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.err = err
}

func (l *fakeLease) setHangRenew(hang bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hangRenew = hang
}

func (l *fakeLease) count(op string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls[op]
}

func (l *fakeLease) renewals() []time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]time.Time(nil), l.renewedAt...)
}

// call counts the call and returns the configured failure.
func (l *fakeLease) call(op string) (error, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls[op]++
	return l.err, l.hangRenew
}

func (l *fakeLease) Acquire(_ context.Context, name string) (bool, error) {
	if err, _ := l.call("acquire"); err != nil {
		return false, err
	}
	tb := l.table
	tb.mu.Lock()
	defer tb.mu.Unlock()
	now := tb.clk.Now()
	if e, ok := tb.entries[name]; ok && e.expires.After(now) {
		return false, nil
	}
	tb.entries[name] = tableEntry{holder: l.holder, expires: now.Add(testTTL)}
	tb.log = append(tb.log, "acquire "+name+" "+l.holder)
	return true, nil
}

func (l *fakeLease) Renew(ctx context.Context, name string) error {
	err, hang := l.call("renew")
	if hang {
		<-ctx.Done()
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	tb := l.table
	tb.mu.Lock()
	defer tb.mu.Unlock()
	now := tb.clk.Now()
	e, ok := tb.entries[name]
	if !ok || e.holder != l.holder || !e.expires.After(now) {
		return jobs.ErrLeaseLost
	}
	tb.entries[name] = tableEntry{holder: l.holder, expires: now.Add(testTTL)}
	l.mu.Lock()
	l.renewedAt = append(l.renewedAt, now)
	l.mu.Unlock()
	return nil
}

func (l *fakeLease) Release(_ context.Context, name string) error {
	if err, _ := l.call("release"); err != nil {
		return err
	}
	tb := l.table
	tb.mu.Lock()
	defer tb.mu.Unlock()
	if e, ok := tb.entries[name]; ok && e.holder == l.holder {
		delete(tb.entries, name)
		tb.log = append(tb.log, "release "+name+" "+l.holder)
	}
	return nil
}

func newLeasedRunner(clk *clocktesting.FakeClock, lease jobs.Lease) *jobs.LeasedRunner {
	return &jobs.LeasedRunner{Lease: lease, Clock: clk, TTL: testTTL, RenewEvery: testRenewEvery}
}

// jobRuns records the runs of the jobs of a test.
type jobRuns struct {
	mu   sync.Mutex
	runs map[string]int // "<job> <runner>" → completed and running runs
}

func (r *jobRuns) add(job, runner string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runs == nil {
		r.runs = make(map[string]int)
	}
	r.runs[job+" "+runner]++
}

func (r *jobRuns) count(job, runner string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs[job+" "+runner]
}

// countingJob is a job that only counts its runs.
func countingJob(name, runner string, runs *jobRuns) jobs.Job {
	return jobs.Job{Name: name, Interval: testInterval, Run: func(context.Context) error {
		runs.add(name, runner)
		return nil
	}}
}

// blockingJob is a job whose runs last until their context ends; it reports the start
// and the end of each run on the channels.
func blockingJob(name string, started chan<- time.Time, stopped chan<- time.Time, clk *clocktesting.FakeClock) jobs.Job {
	return jobs.Job{Name: name, Interval: testInterval, Run: func(ctx context.Context) error {
		started <- clk.Now()
		<-ctx.Done()
		stopped <- clk.Now()
		return nil
	}}
}

// within reports whether cond holds within d.
func within(d time.Duration, cond func() bool) bool {
	end := time.Now().Add(d)
	for {
		if cond() {
			return true
		}
		if time.Now().After(end) {
			return false
		}
		time.Sleep(200 * time.Microsecond)
	}
}

// advanceUntil moves the fake clock forward in steps of fakeStep until cond holds; after
// each step the runner's goroutines get a moment to react.
func advanceUntil(t *testing.T, clk *clocktesting.FakeClock, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for !within(5*time.Millisecond, cond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		clk.Step(fakeStep)
	}
}

// advance moves the fake clock forward by d in steps of fakeStep, giving the goroutines a
// moment after each step.
func advance(clk *clocktesting.FakeClock, d time.Duration) {
	for moved := time.Duration(0); moved < d; moved += fakeStep {
		clk.Step(fakeStep)
		time.Sleep(2 * time.Millisecond)
	}
}

// receive waits for a value on ch, moving the fake clock forward until it comes.
func receive[T any](t *testing.T, clk *clocktesting.FakeClock, ch <-chan T, what string) T {
	t.Helper()
	var v T
	advanceUntil(t, clk, what, func() bool {
		select {
		case v = <-ch:
			return true
		default:
			return false
		}
	})
	return v
}

func TestLeasedRunnerRunsAJobOnOneRunnerAtATime(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	table := newLeaseTable(clk)
	var runs jobRuns
	leaseA, leaseB := newFakeLease(table, "a"), newFakeLease(table, "b")

	a := startRunner(t, newLeasedRunner(clk, leaseA), []jobs.Job{countingJob("sync", "a", &runs)})
	require.Eventually(t, func() bool { return table.holder("sync") == "a" }, waitTimeout, time.Millisecond)
	startRunner(t, newLeasedRunner(clk, leaseB), []jobs.Job{countingJob("sync", "b", &runs)})
	require.Eventually(t, func() bool { return leaseB.count("acquire") == 1 }, waitTimeout, time.Millisecond)

	// a runs the job every interval and renews its lease; b keeps asking and never runs it
	advanceUntil(t, clk, "a has run the job five times", func() bool { return runs.count("sync", "a") >= 5 })
	advance(clk, 5*time.Minute)
	assert.Zero(t, runs.count("sync", "b"))
	assert.Greater(t, leaseB.count("acquire"), 3, "b did not keep asking for the lease")
	assert.GreaterOrEqual(t, leaseA.count("renew"), 3)
	assert.Equal(t, "a", table.holder("sync"))

	// stopping a releases its lease: b takes it over on its next attempt
	a.cancel()
	require.NoError(t, a.wait(t))
	assert.Equal(t, 1, leaseA.count("release"))
	runsOfA := runs.count("sync", "a")
	advanceUntil(t, clk, "b runs the job", func() bool { return runs.count("sync", "b") >= 2 })
	assert.Equal(t, "b", table.holder("sync"))
	assert.Equal(t, runsOfA, runs.count("sync", "a"), "a ran the job after it stopped")
}

func TestLeasedRunnerRenewsEveryThirdOfTheTTLByDefault(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	table := newLeaseTable(clk)
	lease := newFakeLease(table, "a")
	var runs jobRuns
	started := clk.Now()
	startRunner(t, &jobs.LeasedRunner{Lease: lease, Clock: clk, TTL: testTTL}, []jobs.Job{countingJob("sync", "a", &runs)})
	require.Eventually(t, func() bool { return table.holder("sync") == "a" }, waitTimeout, time.Millisecond)

	advanceUntil(t, clk, "three renewals", func() bool { return len(lease.renewals()) >= 3 })
	previous := started
	for i, at := range lease.renewals()[:3] {
		// never earlier than a third of the TTL after the previous write; the steps of the
		// fake clock are the only reason to be later
		gap := at.Sub(previous)
		assert.GreaterOrEqual(t, gap, testTTL/3, "renewal %d", i+1)
		assert.Less(t, gap, testTTL/3+time.Minute, "renewal %d", i+1)
		previous = at
	}
}

func TestLeasedRunnerDoesNotInterruptARunToRenew(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	table := newLeaseTable(clk)
	lease := newFakeLease(table, "a")
	started := make(chan struct{}, 10)
	cancelled := make(chan struct{}, 10)
	finish := make(chan struct{})
	startRunner(t, newLeasedRunner(clk, lease), []jobs.Job{{Name: "slow", Interval: testInterval, Run: func(ctx context.Context) error {
		started <- struct{}{}
		select {
		case <-ctx.Done():
			cancelled <- struct{}{}
		case <-finish:
		}
		return nil
	}}})

	receive(t, clk, started, "the first run")
	// the run lasts many renewal intervals
	advanceUntil(t, clk, "five renewals", func() bool { return len(lease.renewals()) >= 5 })
	assert.Empty(t, cancelled, "a renewal interrupted the run")
	assert.Empty(t, started, "a second run started while the first was running")

	close(finish)
	receive(t, clk, started, "the next run")
	assert.Empty(t, cancelled)
}

func TestLeasedRunnerStopsTheJobWhenARenewalFails(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	table := newLeaseTable(clk)
	leaseA, leaseB := newFakeLease(table, "a"), newFakeLease(table, "b")
	startedA, stoppedA := make(chan time.Time, 10), make(chan time.Time, 10)
	startedB, stoppedB := make(chan time.Time, 10), make(chan time.Time, 10)

	startRunner(t, newLeasedRunner(clk, leaseA), []jobs.Job{blockingJob("sync", startedA, stoppedA, clk)})
	require.Eventually(t, func() bool { return table.holder("sync") == "a" }, waitTimeout, time.Millisecond)
	startRunner(t, newLeasedRunner(clk, leaseB), []jobs.Job{blockingJob("sync", startedB, stoppedB, clk)})
	receive(t, clk, startedA, "the run of a")

	// NATS is down for a: its next renewal fails and cancels the run
	advanceUntil(t, clk, "a renewal of a", func() bool { return len(leaseA.renewals()) >= 1 })
	leaseA.setErr(errors.New("nats: connection closed"))
	stoppedAt := receive(t, clk, stoppedA, "the run of a is cancelled")
	expires := table.expires("sync") // from the last write of a
	assert.True(t, stoppedAt.Before(expires), "the run was cancelled only when the lease had expired")

	// a starts no further run, while it keeps trying to get the lease back
	acquires := leaseA.count("acquire")
	advance(clk, 5*time.Minute)
	assert.Empty(t, startedA, "a started a run without its lease")
	assert.Greater(t, leaseA.count("acquire"), acquires, "a stopped trying to get its lease back")

	// b takes the job over once the lease of a has expired: within about one TTL
	startedAtB := receive(t, clk, startedB, "b runs the job")
	assert.False(t, startedAtB.Before(expires), "b started before the lease of a expired")
	assert.True(t, startedAtB.Before(expires.Add(2*testRenewEvery+testInterval+3*fakeStep)), "b took too long to take over")
	assert.Empty(t, startedA)
}

func TestLeasedRunnerStopsTheJobWhenTheLeaseIsTakenOver(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	table := newLeaseTable(clk)
	lease := newFakeLease(table, "a")
	started, stopped := make(chan time.Time, 10), make(chan time.Time, 10)
	startRunner(t, newLeasedRunner(clk, lease), []jobs.Job{blockingJob("sync", started, stopped, clk)})
	receive(t, clk, started, "the run")

	// another instance writes the lease (as after an expiry that a did not notice)
	table.takeOver("sync", "x")
	receive(t, clk, stopped, "the run is cancelled")
	assert.Equal(t, "x", table.holder("sync"), "a deleted the lease of another holder")

	// a waits until the lease of x expires, then runs the job again
	expires := table.expires("sync")
	advance(clk, 10*time.Minute)
	assert.Empty(t, started)
	startedAt := receive(t, clk, started, "the next run of a")
	assert.False(t, startedAt.Before(expires))
}

func TestLeasedRunnerStopsTheJobBeforeTheLeaseCanExpireWhenRenewalsHang(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	table := newLeaseTable(clk)
	lease := newFakeLease(table, "a")
	started, stopped := make(chan time.Time, 10), make(chan time.Time, 10)
	startRunner(t, newLeasedRunner(clk, lease), []jobs.Job{blockingJob("sync", started, stopped, clk)})
	receive(t, clk, started, "the run")
	advanceUntil(t, clk, "a renewal", func() bool { return len(lease.renewals()) >= 1 })

	// the renewals get no answer: the watchdog cancels the run one renewal interval before
	// the lease can expire, without waiting for the hanging call
	lease.setHangRenew(true)
	stoppedAt := receive(t, clk, stopped, "the run is cancelled")
	renewals := lease.renewals()
	lastWrite := renewals[len(renewals)-1]
	// the steps of the fake clock are the only imprecision
	stopBy := lastWrite.Add(testTTL - testRenewEvery)
	assert.False(t, stoppedAt.Before(stopBy.Add(-fakeStep)), "cancelled earlier than needed: %s after the last write", stoppedAt.Sub(lastWrite))
	assert.True(t, stoppedAt.Before(stopBy.Add(3*fakeStep)), "cancelled too late: %s after the last write", stoppedAt.Sub(lastWrite))
	assert.True(t, stoppedAt.Before(table.expires("sync")))
}

func TestLeasedRunnerReleasesItsLeasesWhenCancelled(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	table := newLeaseTable(clk)
	lease := newFakeLease(table, "a")
	slow := func(name string) jobs.Job {
		return jobs.Job{Name: name, Interval: testInterval, Run: func(ctx context.Context) error {
			table.record("start " + name)
			<-ctx.Done()
			table.record("stop " + name)
			return nil
		}}
	}
	// "other" is held by another instance: a does not hold it and must not release it
	table.takeOver("other", "x")
	r := startRunner(t, newLeasedRunner(clk, lease), []jobs.Job{slow("one"), slow("two"), countingJob("other", "a", &jobRuns{})})
	advanceUntil(t, clk, "both jobs run", func() bool { return countPrefix(table.events(), "start ") == 2 })

	r.cancel()
	require.NoError(t, r.wait(t))
	assert.Empty(t, table.holder("one"))
	assert.Empty(t, table.holder("two"))
	assert.Equal(t, "x", table.holder("other"))

	// each lease is released only after its job returned
	events := table.events()
	for _, name := range []string{"one", "two"} {
		stop, release := indexOf(events, "stop "+name), indexOf(events, "release "+name+" a")
		require.NotEqual(t, -1, stop, name)
		require.NotEqual(t, -1, release, name)
		assert.Less(t, stop, release, "%s was released before its job returned: %v", name, events)
	}
}

func TestLeasedRunnerSpreadsJobsOverRunners(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	table := newLeaseTable(clk)
	var runs jobRuns
	leaseA, leaseB := newFakeLease(table, "a"), newFakeLease(table, "b")

	// a takes "one" and finds "two" held by another instance
	table.takeOver("two", "x")
	startRunner(t, newLeasedRunner(clk, leaseA), []jobs.Job{countingJob("one", "a", &runs), countingJob("two", "a", &runs)})
	require.Eventually(t, func() bool { return leaseA.count("acquire") == 2 }, waitTimeout, time.Millisecond)
	assert.Equal(t, "a", table.holder("one"))

	// "two" is free when b starts: b takes it before a asks again
	table.remove("two")
	startRunner(t, newLeasedRunner(clk, leaseB), []jobs.Job{countingJob("one", "b", &runs), countingJob("two", "b", &runs)})
	require.Eventually(t, func() bool { return table.holder("two") == "b" }, waitTimeout, time.Millisecond)

	advanceUntil(t, clk, "both jobs run", func() bool { return runs.count("one", "a") >= 3 && runs.count("two", "b") >= 3 })
	assert.Zero(t, runs.count("one", "b"))
	assert.Zero(t, runs.count("two", "a"))
}

func TestLeasedRunnerRejectsInvalidSettings(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	lease := newFakeLease(newLeaseTable(clk), "a")
	job := countingJob("sync", "a", &jobRuns{})

	for name, tc := range map[string]struct {
		runner *jobs.LeasedRunner
		jobs   []jobs.Job
	}{
		"duplicate job names":         {newLeasedRunner(clk, lease), []jobs.Job{job, job}},
		"no lease":                    {&jobs.LeasedRunner{Clock: clk}, []jobs.Job{job}},
		"renewal at half the ttl":     {&jobs.LeasedRunner{Lease: lease, Clock: clk, TTL: time.Minute, RenewEvery: 30 * time.Second}, []jobs.Job{job}},
		"renewal longer than the ttl": {&jobs.LeasedRunner{Lease: lease, Clock: clk, RenewEvery: time.Minute}, []jobs.Job{job}},
		"negative renewal interval":   {&jobs.LeasedRunner{Lease: lease, Clock: clk, RenewEvery: -time.Second}, []jobs.Job{job}},
		"negative ttl":                {&jobs.LeasedRunner{Lease: lease, Clock: clk, TTL: -time.Minute}, []jobs.Job{job}},
	} {
		t.Run(name, func(t *testing.T) {
			r := startRunner(t, tc.runner, tc.jobs)
			require.Error(t, r.wait(t))
			assert.Zero(t, lease.count("acquire"), "a lease was asked for")
		})
	}

	t.Run("without jobs", func(t *testing.T) {
		r := startRunner(t, newLeasedRunner(clk, lease), nil)
		r.cancel()
		require.NoError(t, r.wait(t))
	})
}

func countPrefix(events []string, prefix string) int {
	n := 0
	for _, e := range events {
		if len(e) >= len(prefix) && e[:len(prefix)] == prefix {
			n++
		}
	}
	return n
}

func indexOf(events []string, event string) int {
	for i, e := range events {
		if e == event {
			return i
		}
	}
	return -1
}
