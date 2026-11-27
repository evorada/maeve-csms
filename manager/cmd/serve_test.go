// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/config"
	"github.com/thoughtworks/maeve-csms/manager/connectivity"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"github.com/thoughtworks/maeve-csms/manager/transport/nats/natstest"
	"go.opentelemetry.io/otel/trace/noop"
	"k8s.io/utils/clock"
)

type nopEmitter struct{}

func (nopEmitter) Emit(context.Context, transport.OcppVersion, string, *transport.Message) error {
	return nil
}

func jobNames(jobList []jobs.Job) []string {
	var names []string
	for _, job := range jobList {
		names = append(names, job.Name)
	}
	return names
}

func TestBackgroundJobs(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	settings := &config.Config{
		Storage:                   engine,
		Tracer:                    noop.NewTracerProvider().Tracer(""),
		MsgEmitter:                nopEmitter{},
		SyncInterval:              time.Minute,
		SyncRetryAfter:            2 * time.Minute,
		ReservationExpiryInterval: time.Minute,
		ConnectivitySweepInterval: 30 * time.Second,
		EventRelayInterval:        time.Second,
	}

	t.Run("without connectivity and outbox (firestore)", func(t *testing.T) {
		jobList := backgroundJobs(settings)
		require.NoError(t, jobs.Validate(jobList))
		assert.Equal(t, []string{"sync-settings", "sync-certificates", "sync-triggers", "reservation-expiry"}, jobNames(jobList))
	})

	t.Run("every job", func(t *testing.T) {
		withAll := *settings
		withAll.Connectivity = &connectivity.Tracker{Store: engine}
		withAll.EventRelay = &events.Relay{Store: engine}
		withAll.OutboxPurge = &config.OutboxPurgeSettings{Retention: time.Hour, Interval: time.Hour}
		jobList := backgroundJobs(&withAll)
		require.NoError(t, jobs.Validate(jobList))
		assert.Equal(t, []string{
			"sync-settings", "sync-certificates", "sync-triggers", "reservation-expiry",
			connectivity.SweepJobName, events.RelayJobName, events.PurgeJobName,
		}, jobNames(jobList))
		for _, job := range jobList {
			want := map[string]time.Duration{
				"sync-settings": time.Minute, "sync-certificates": time.Minute, "sync-triggers": time.Minute,
				"reservation-expiry": time.Minute, connectivity.SweepJobName: 30 * time.Second,
				events.RelayJobName: time.Second, events.PurgeJobName: time.Hour,
			}[job.Name]
			assert.Equal(t, want, job.Interval, job.Name)
		}
	})
}

// The leased runner releases its leases on the NATS connection when it stops: the jobs
// must stop before the connection is drained.
func TestStopJobsAndDrainNatsReleasesTheLeasesFirst(t *testing.T) {
	_, url := natstest.RunServer(t, true)
	conn, err := nats.Connect(url)
	require.NoError(t, err)
	t.Cleanup(conn.Close)
	js, err := jetstream.New(conn)
	require.NoError(t, err)
	lease, err := jobs.NewKVLease(context.Background(), js, jobs.KVLeaseConfig{Bucket: "leases", TTL: time.Minute, Ensure: true}, "a")
	require.NoError(t, err)

	running := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	jobsDone := make(chan struct{})
	go func() {
		defer close(jobsDone)
		_ = (&jobs.LeasedRunner{Lease: lease, TTL: time.Minute}).Run(ctx, []jobs.Job{{
			Name: "sync", Interval: time.Millisecond, Run: func(ctx context.Context) error {
				select {
				case running <- struct{}{}:
				default:
				}
				<-ctx.Done()
				return nil
			}}})
	}()
	select {
	case <-running:
	case <-time.After(10 * time.Second):
		t.Fatal("the job did not run")
	}

	stopJobsAndDrainNats(cancel, jobsDone, conn, 10*time.Second, 10*time.Second)
	assert.True(t, conn.IsClosed())
	select {
	case <-jobsDone:
	default:
		t.Fatal("the runner had not returned")
	}

	// the lease was released before the connection closed: another replica can take the
	// job at once instead of waiting a minute for the lease to expire
	other, err := nats.Connect(url)
	require.NoError(t, err)
	t.Cleanup(other.Close)
	otherJs, err := jetstream.New(other)
	require.NoError(t, err)
	kv, err := otherJs.KeyValue(context.Background(), "leases")
	require.NoError(t, err)
	_, err = kv.Get(context.Background(), "job.sync")
	assert.True(t, errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted), "the lease is still held: %v", err)
}

func TestStopJobsAndDrainNatsDoesNotWaitForeverForTheJobs(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn, err := nats.Connect(url)
	require.NoError(t, err)
	t.Cleanup(conn.Close)

	stopped := false
	start := time.Now()
	stopJobsAndDrainNats(func() { stopped = true }, make(chan struct{}), conn, 100*time.Millisecond, time.Second)
	assert.True(t, stopped)
	assert.Less(t, time.Since(start), 5*time.Second)
	assert.True(t, conn.IsClosed(), "the connection was not drained after the timeout")

	// without NATS there is nothing to drain
	stopJobsAndDrainNats(func() {}, make(chan struct{}), nil, time.Millisecond, time.Second)
}
