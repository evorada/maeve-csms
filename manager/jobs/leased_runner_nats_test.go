// SPDX-License-Identifier: Apache-2.0

package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	"github.com/thoughtworks/maeve-csms/manager/transport/nats/natstest"
)

// These tests run the leased runner on the real clock against an embedded NATS server,
// with the lease TTL of kvTestTTL (renewed every third of it).

var errCut = errors.New("cut off from NATS")

// partitionableLease cuts a lease off from NATS on demand: renewals then hang until their
// context ends, as requests to a server that does not answer do, and the other calls fail.
type partitionableLease struct {
	jobs.Lease
	cut atomic.Bool
}

func (p *partitionableLease) Acquire(ctx context.Context, name string) (bool, error) {
	if p.cut.Load() {
		return false, errCut
	}
	return p.Lease.Acquire(ctx, name)
}

func (p *partitionableLease) Renew(ctx context.Context, name string) error {
	if p.cut.Load() {
		<-ctx.Done()
		return ctx.Err()
	}
	return p.Lease.Renew(ctx, name)
}

func (p *partitionableLease) Release(ctx context.Context, name string) error {
	if p.cut.Load() {
		return errCut
	}
	return p.Lease.Release(ctx, name)
}

// overlapMonitor records the runs of a job by several runners and every moment at which
// two runners were in a run at the same time.
type overlapMonitor struct {
	mu       sync.Mutex
	inside   map[string]bool
	runs     map[string]int
	overlaps []string
}

func newOverlapMonitor() *overlapMonitor {
	return &overlapMonitor{inside: make(map[string]bool), runs: make(map[string]int)}
}

func (m *overlapMonitor) enter(runner string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for other, in := range m.inside {
		if in && other != runner {
			m.overlaps = append(m.overlaps, fmt.Sprintf("%s started while %s was running", runner, other))
		}
	}
	m.inside[runner] = true
	m.runs[runner]++
}

func (m *overlapMonitor) exit(runner string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inside[runner] = false
}

func (m *overlapMonitor) count(runner string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runs[runner]
}

func (m *overlapMonitor) found() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.overlaps...)
}

// monitoredJob runs often and for a while, so that runs of two runners would overlap.
func monitoredJob(m *overlapMonitor, runner string) jobs.Job {
	return jobs.Job{Name: "sync", Interval: 10 * time.Millisecond, Run: func(ctx context.Context) error {
		m.enter(runner)
		defer m.exit(runner)
		select {
		case <-ctx.Done():
		case <-time.After(30 * time.Millisecond):
		}
		return nil
	}}
}

type natsRunner struct {
	name  string
	lease *partitionableLease
	run   *runningRunner
}

func startNatsRunner(t *testing.T, url, name string, job jobs.Job) *natsRunner {
	t.Helper()
	lease := &partitionableLease{Lease: newTestKVLease(t, jetStreamOf(t, url), name)}
	return &natsRunner{
		name:  name,
		lease: lease,
		run:   startRunner(t, &jobs.LeasedRunner{Lease: lease, TTL: kvTestTTL}, []jobs.Job{job}),
	}
}

// holderOf is the holder written in the lease of the job ("" when there is none).
func holderOf(t *testing.T, kv jetstream.KeyValue, job string) string {
	t.Helper()
	entry, err := kv.Get(context.Background(), "job."+job)
	if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted) {
		return ""
	}
	require.NoError(t, err)
	var value struct {
		Holder string `json:"holder"`
	}
	require.NoError(t, json.Unmarshal(entry.Value(), &value))
	return value.Holder
}

func TestLeasedRunnersOnOneBucketNeverRunAJobTwiceAtOnce(t *testing.T) {
	_, url := natstest.RunServer(t, true)
	mon := newOverlapMonitor()
	runners := make(map[string]*natsRunner)
	for _, name := range []string{"r0", "r1"} {
		runners[name] = startNatsRunner(t, url, name, monitoredJob(mon, name))
	}
	kv, err := jetStreamOf(t, url).KeyValue(context.Background(), kvTestConfig().Bucket)
	require.NoError(t, err)

	// waitForAHolder waits until one of the runners (other than skip) holds the lease and
	// has run the job twice since.
	waitForAHolder := func(skip string) *natsRunner {
		t.Helper()
		var holder *natsRunner
		require.Eventually(t, func() bool {
			holder = runners[holderOf(t, kv, "sync")]
			return holder != nil && holder.name != skip
		}, 10*time.Second, 5*time.Millisecond, "no runner took the lease")
		runs := mon.count(holder.name)
		require.Eventually(t, func() bool { return mon.count(holder.name) >= runs+2 }, 10*time.Second, 5*time.Millisecond,
			"%s holds the lease and does not run the job", holder.name)
		return holder
	}

	next := len(runners)
	for round, disrupt := range []string{"stop", "partition", "take over", "partition", "stop"} {
		holder := waitForAHolder("")
		switch disrupt {
		case "stop":
			// a replica that shuts down releases its lease
			holder.run.cancel()
			require.NoError(t, holder.run.wait(t))
			delete(runners, holder.name)
			name := fmt.Sprintf("r%d", next)
			next++
			runners[name] = startNatsRunner(t, url, name, monitoredJob(mon, name))
			waitForAHolder(holder.name)
		case "partition":
			// a replica that cannot reach NATS stops its run before its lease can expire
			holder.lease.cut.Store(true)
			waitForAHolder(holder.name)
			holder.lease.cut.Store(false)
		case "take over":
			// another instance writes the lease
			_, err := kv.Put(context.Background(), "job.sync", []byte(`{"holder":"outsider"}`))
			require.NoError(t, err)
			waitForAHolder("")
		}
		require.Empty(t, mon.found(), "round %d (%s)", round, disrupt)
	}

	// r0 or r1 first, then after the first stop each of the two live runners in turn
	ran := 0
	for i := 0; i < next; i++ {
		if mon.count(fmt.Sprintf("r%d", i)) > 0 {
			ran++
		}
	}
	assert.GreaterOrEqual(t, ran, 3, "the job did not move between the runners")
}

func TestLeasedRunnerOnNatsStopsAJobWhoseLeaseIsTakenOver(t *testing.T) {
	_, url := natstest.RunServer(t, true)
	started, stopped := make(chan time.Time, 10), make(chan time.Time, 10)
	startNatsRunner(t, url, "a", jobs.Job{Name: "sync", Interval: 10 * time.Millisecond, Run: func(ctx context.Context) error {
		started <- time.Now()
		<-ctx.Done()
		stopped <- time.Now()
		return nil
	}})
	waitForRun(t, started)
	kv, err := jetStreamOf(t, url).KeyValue(context.Background(), kvTestConfig().Bucket)
	require.NoError(t, err)
	require.Equal(t, "a", holderOf(t, kv, "sync"))

	takenOver := time.Now()
	_, err = kv.Put(context.Background(), "job.sync", []byte(`{"holder":"x"}`))
	require.NoError(t, err)

	// the next renewal finds another revision and cancels the run, long before the lease
	// of x could expire
	stoppedAt := waitForRun(t, stopped)
	assert.Less(t, stoppedAt.Sub(takenOver), kvTestTTL*2/3)
	assert.Equal(t, "x", holderOf(t, kv, "sync"), "a removed the lease of x")

	// a runs the job again only once the lease of x has expired
	restartedAt := waitForRun(t, started)
	assert.GreaterOrEqual(t, restartedAt.Sub(takenOver), kvTestTTL)
	assert.Equal(t, "a", holderOf(t, kv, "sync"))
}

// restartableServer is an embedded NATS server with JetStream that can be stopped and
// started again on the same port and store directory.
type restartableServer struct {
	opts server.Options
	srv  *server.Server
}

func runRestartableServer(t *testing.T) *restartableServer {
	opts := natsserver.DefaultTestOptions
	opts.Port = -1
	opts.JetStream = true
	opts.StoreDir = t.TempDir()
	s := &restartableServer{opts: opts}
	s.start()
	s.opts.Port = s.srv.Addr().(*net.TCPAddr).Port
	t.Cleanup(s.stop)
	return s
}

func (s *restartableServer) start() {
	opts := s.opts
	s.srv = natsserver.RunServer(&opts)
}

func (s *restartableServer) stop() {
	s.srv.Shutdown()
	s.srv.WaitForShutdown()
}

func TestLeasedRunnerOnNatsStopsDuringAnOutageAndRecovers(t *testing.T) {
	srv := runRestartableServer(t)
	url := srv.srv.ClientURL()
	started, stopped := make(chan time.Time, 10), make(chan time.Time, 10)
	js := jetStreamOf(t, url, nats.MaxReconnects(-1), nats.ReconnectWait(20*time.Millisecond))
	lease := newTestKVLease(t, js, "a")
	startRunner(t, &jobs.LeasedRunner{Lease: lease, TTL: kvTestTTL}, []jobs.Job{{Name: "sync", Interval: 10 * time.Millisecond, Run: func(ctx context.Context) error {
		started <- time.Now()
		<-ctx.Done()
		stopped <- time.Now()
		return nil
	}}})
	waitForRun(t, started)

	// NATS goes away: the run is cancelled at the next renewal, before the lease could
	// expire, and no run starts while NATS is away
	down := time.Now()
	srv.stop()
	stoppedAt := waitForRun(t, stopped)
	assert.Less(t, stoppedAt.Sub(down), kvTestTTL*2/3)
	select {
	case <-started:
		t.Fatal("a run started while NATS was down")
	case <-time.After(2 * kvTestTTL):
	}

	// NATS is back: the runner takes the lease again and runs the job
	srv.start()
	waitForRun(t, started)
	require.True(t, js.Conn().IsConnected())
}
