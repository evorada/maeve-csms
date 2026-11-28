// SPDX-License-Identifier: Apache-2.0

package jobs_test

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	"github.com/thoughtworks/maeve-csms/manager/transport/nats/natstest"
)

const kvTestTTL = time.Second

func kvTestConfig() jobs.KVLeaseConfig {
	return jobs.KVLeaseConfig{Bucket: "test_job_leases", TTL: kvTestTTL, Replicas: 1, Ensure: true}
}

// jetStreamOf connects to the server; the connection is closed at the end of the test.
func jetStreamOf(t *testing.T, url string, opts ...nats.Option) jetstream.JetStream {
	t.Helper()
	conn, err := nats.Connect(url, opts...)
	require.NoError(t, err)
	t.Cleanup(conn.Close)
	js, err := jetstream.New(conn)
	require.NoError(t, err)
	return js
}

func newTestKVLease(t *testing.T, js jetstream.JetStream, instance string) *jobs.KVLease {
	t.Helper()
	lease, err := jobs.NewKVLease(context.Background(), js, kvTestConfig(), instance)
	require.NoError(t, err)
	return lease
}

func TestKVLeaseIsExclusive(t *testing.T) {
	_, url := natstest.RunServer(t, true)
	ctx := context.Background()
	a := newTestKVLease(t, jetStreamOf(t, url), "a")
	b := newTestKVLease(t, jetStreamOf(t, url), "b")

	ok, err := a.Acquire(ctx, "sync")
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = b.Acquire(ctx, "sync")
	require.NoError(t, err)
	assert.False(t, ok, "the lease of a was taken")
	ok, err = a.Acquire(ctx, "sync")
	require.NoError(t, err)
	assert.False(t, ok, "a held lease is not taken twice, also not by its holder")

	// another job is another lease
	ok, err = b.Acquire(ctx, "relay")
	require.NoError(t, err)
	assert.True(t, ok)

	require.NoError(t, a.Release(ctx, "sync"))
	ok, err = b.Acquire(ctx, "sync")
	require.NoError(t, err)
	assert.True(t, ok, "the released lease could not be taken")
	ok, err = a.Acquire(ctx, "sync")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestKVLeaseKeepsTheLeaseWhileItIsRenewed(t *testing.T) {
	_, url := natstest.RunServer(t, true)
	ctx := context.Background()
	a := newTestKVLease(t, jetStreamOf(t, url), "a")
	b := newTestKVLease(t, jetStreamOf(t, url), "b")

	ok, err := a.Acquire(ctx, "sync")
	require.NoError(t, err)
	require.True(t, ok)

	// renewed every third of the TTL for three TTLs: nobody else gets it
	for end := time.Now().Add(3 * kvTestTTL); time.Now().Before(end); {
		time.Sleep(kvTestTTL / 3)
		require.NoError(t, a.Renew(ctx, "sync"))
		ok, err := b.Acquire(ctx, "sync")
		require.NoError(t, err)
		require.False(t, ok, "the lease was taken while it was renewed")
	}

	// without renewals it expires after the TTL and b takes it
	stopped := time.Now()
	require.Eventually(t, func() bool {
		ok, err := b.Acquire(ctx, "sync")
		require.NoError(t, err)
		return ok
	}, 10*time.Second, 20*time.Millisecond)
	assert.GreaterOrEqual(t, time.Since(stopped), kvTestTTL*2/3, "taken before the TTL could have passed")
}

func TestKVLeaseRenewFailsAfterATakeOver(t *testing.T) {
	_, url := natstest.RunServer(t, true)
	ctx := context.Background()
	jsA := jetStreamOf(t, url)
	a := newTestKVLease(t, jsA, "a")
	b := newTestKVLease(t, jetStreamOf(t, url), "b")

	t.Run("after an expiry", func(t *testing.T) {
		ok, err := a.Acquire(ctx, "sync")
		require.NoError(t, err)
		require.True(t, ok)
		require.Eventually(t, func() bool {
			ok, err := b.Acquire(ctx, "sync")
			require.NoError(t, err)
			return ok
		}, 10*time.Second, 20*time.Millisecond)

		err = a.Renew(ctx, "sync")
		require.ErrorIs(t, err, jobs.ErrLeaseLost)
		// a lease that is lost is forgotten: a's release does not delete the key of b
		require.NoError(t, a.Release(ctx, "sync"))
		require.NoError(t, b.Renew(ctx, "sync"), "a release of a removed the lease of b")
		require.ErrorIs(t, a.Renew(ctx, "sync"), jobs.ErrLeaseLost)
	})

	t.Run("after a write of another holder", func(t *testing.T) {
		ok, err := a.Acquire(ctx, "relay")
		require.NoError(t, err)
		require.True(t, ok)
		kv, err := jsA.KeyValue(ctx, kvTestConfig().Bucket)
		require.NoError(t, err)
		_, err = kv.Put(ctx, "job.relay", []byte(`{"holder":"x"}`))
		require.NoError(t, err)

		require.ErrorIs(t, a.Renew(ctx, "relay"), jobs.ErrLeaseLost)
		require.NoError(t, a.Release(ctx, "relay"))
		entry, err := kv.Get(ctx, "job.relay")
		require.NoError(t, err, "a deleted the lease of x")
		assert.JSONEq(t, `{"holder":"x"}`, string(entry.Value()))
	})

	t.Run("a release after a renewal of another holder", func(t *testing.T) {
		ok, err := a.Acquire(ctx, "purge")
		require.NoError(t, err)
		require.True(t, ok)
		kv, err := jsA.KeyValue(ctx, kvTestConfig().Bucket)
		require.NoError(t, err)
		_, err = kv.Put(ctx, "job.purge", []byte(`{"holder":"x"}`))
		require.NoError(t, err)

		// the release only deletes the revision that a wrote
		require.NoError(t, a.Release(ctx, "purge"))
		_, err = kv.Get(ctx, "job.purge")
		require.NoError(t, err, "a deleted the lease of x")
	})

	t.Run("a lease that is not held", func(t *testing.T) {
		require.ErrorIs(t, a.Renew(ctx, "never-acquired"), jobs.ErrLeaseLost)
		require.NoError(t, a.Release(ctx, "never-acquired"))
	})
}

func TestKVLeaseWritesTheHolder(t *testing.T) {
	_, url := natstest.RunServer(t, true)
	ctx := context.Background()
	js := jetStreamOf(t, url)
	before := time.Now().UTC().Add(-time.Second)
	a := newTestKVLease(t, js, "host-1-abc123")

	ok, err := a.Acquire(ctx, "sync")
	require.NoError(t, err)
	require.True(t, ok)
	kv, err := js.KeyValue(ctx, kvTestConfig().Bucket)
	require.NoError(t, err)

	var value struct {
		Holder     string    `json:"holder"`
		AcquiredAt time.Time `json:"acquired_at"`
		RenewedAt  time.Time `json:"renewed_at"`
	}
	entry, err := kv.Get(ctx, "job.sync")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(entry.Value(), &value))
	assert.Equal(t, "host-1-abc123", value.Holder)
	assert.True(t, value.AcquiredAt.After(before))
	assert.Equal(t, value.AcquiredAt, value.RenewedAt)

	time.Sleep(10 * time.Millisecond)
	require.NoError(t, a.Renew(ctx, "sync"))
	acquiredAt := value.AcquiredAt
	entry, err = kv.Get(ctx, "job.sync")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(entry.Value(), &value))
	assert.Equal(t, acquiredAt, value.AcquiredAt, "a renewal keeps the time of the acquisition")
	assert.True(t, value.RenewedAt.After(value.AcquiredAt))

	require.NoError(t, a.Release(ctx, "sync"))
	_, err = kv.Get(ctx, "job.sync")
	require.ErrorIs(t, err, jetstream.ErrKeyNotFound)
}

func TestKVLeaseBucket(t *testing.T) {
	t.Run("ensure creates the bucket", func(t *testing.T) {
		_, url := natstest.RunServer(t, true)
		ctx := context.Background()
		js := jetStreamOf(t, url)
		newTestKVLease(t, js, "a")

		kv, err := js.KeyValue(ctx, "test_job_leases")
		require.NoError(t, err)
		status, err := kv.Status(ctx)
		require.NoError(t, err)
		assert.Equal(t, kvTestTTL, status.TTL())
		assert.Equal(t, int64(1), status.History())
		info := status.(*jetstream.KeyValueBucketStatus).StreamInfo()
		assert.Equal(t, jetstream.FileStorage, info.Config.Storage)
		assert.Equal(t, 1, info.Config.Replicas)

		// a second instance with the same settings uses the same bucket
		newTestKVLease(t, js, "b")
	})

	t.Run("without ensure the bucket must exist", func(t *testing.T) {
		_, url := natstest.RunServer(t, true)
		cfg := kvTestConfig()
		cfg.Ensure = false
		lease, err := jobs.NewKVLease(context.Background(), jetStreamOf(t, url), cfg, "a")
		require.Error(t, err)
		assert.ErrorIs(t, err, jetstream.ErrBucketNotFound)
		assert.Nil(t, lease)
	})

	t.Run("without ensure an existing bucket is used", func(t *testing.T) {
		_, url := natstest.RunServer(t, true)
		js := jetStreamOf(t, url)
		newTestKVLease(t, js, "a") // creates it
		cfg := kvTestConfig()
		cfg.Ensure = false
		lease, err := jobs.NewKVLease(context.Background(), js, cfg, "b")
		require.NoError(t, err)
		ok, err := lease.Acquire(context.Background(), "sync")
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("without ensure a bucket with a shorter or no ttl is refused", func(t *testing.T) {
		for name, ttl := range map[string]time.Duration{"shorter": kvTestTTL / 2, "none": 0} {
			t.Run(name, func(t *testing.T) {
				_, url := natstest.RunServer(t, true)
				js := jetStreamOf(t, url)
				_, err := js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{Bucket: "test_job_leases", TTL: ttl})
				require.NoError(t, err)
				cfg := kvTestConfig()
				cfg.Ensure = false
				lease, err := jobs.NewKVLease(context.Background(), js, cfg, "a")
				require.Error(t, err)
				assert.ErrorContains(t, err, "must be at least the lease TTL")
				assert.Nil(t, lease)
			})
		}
	})

	t.Run("a bucket that is deleted is created again", func(t *testing.T) {
		_, url := natstest.RunServer(t, true)
		ctx := context.Background()
		js := jetStreamOf(t, url)
		lease := newTestKVLease(t, js, "a")
		require.NoError(t, js.DeleteKeyValue(ctx, "test_job_leases"))

		_, err := lease.Acquire(ctx, "sync")
		require.Error(t, err)
		ok, err := lease.Acquire(ctx, "sync")
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("an unbound lease binds on first use", func(t *testing.T) {
		_, url := natstest.RunServer(t, true)
		ctx := context.Background()
		js := jetStreamOf(t, url)
		lease, err := jobs.NewUnboundKVLease(js, kvTestConfig(), "a")
		require.NoError(t, err)
		_, err = js.KeyValue(ctx, "test_job_leases")
		require.ErrorIs(t, err, jetstream.ErrBucketNotFound, "the constructor contacted NATS")

		ok, err := lease.Acquire(ctx, "sync")
		require.NoError(t, err)
		assert.True(t, ok)
		_, err = js.KeyValue(ctx, "test_job_leases")
		require.NoError(t, err)
	})

	t.Run("jetstream disabled", func(t *testing.T) {
		_, url := natstest.RunServer(t, false)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := jobs.NewKVLease(ctx, jetStreamOf(t, url), kvTestConfig(), "a")
		require.Error(t, err)
	})
}

func TestKVLeaseFailsAtOnceWithoutAConnection(t *testing.T) {
	srv, url := natstest.RunServer(t, true)
	ctx := context.Background()
	js := jetStreamOf(t, url, nats.MaxReconnects(-1), nats.ReconnectWait(10*time.Millisecond))
	lease := newTestKVLease(t, js, "a")
	ok, err := lease.Acquire(ctx, "sync")
	require.NoError(t, err)
	require.True(t, ok)

	srv.Shutdown()
	srv.WaitForShutdown()
	require.Eventually(t, func() bool { return !js.Conn().IsConnected() }, 5*time.Second, time.Millisecond)

	// nothing is buffered to be sent after a reconnect, and nothing waits for one
	start := time.Now()
	require.Error(t, lease.Renew(ctx, "sync"))
	_, err = lease.Acquire(ctx, "relay")
	require.Error(t, err)
	assert.Less(t, time.Since(start), time.Second)
	buffered, err := js.Conn().Buffered()
	require.NoError(t, err)
	assert.Zero(t, buffered)
}

func TestNewKVLeaseRejectsInvalidSettings(t *testing.T) {
	_, url := natstest.RunServer(t, true)
	js := jetStreamOf(t, url)
	for name, tc := range map[string]struct {
		mutate   func(*jobs.KVLeaseConfig)
		js       jetstream.JetStream
		instance string
	}{
		"empty bucket":      {mutate: func(c *jobs.KVLeaseConfig) { c.Bucket = "" }},
		"bucket with a dot": {mutate: func(c *jobs.KVLeaseConfig) { c.Bucket = "job.leases" }},
		"zero ttl":          {mutate: func(c *jobs.KVLeaseConfig) { c.TTL = 0 }},
		"ttl below 100ms":   {mutate: func(c *jobs.KVLeaseConfig) { c.TTL = 99 * time.Millisecond }},
		"negative replicas": {mutate: func(c *jobs.KVLeaseConfig) { c.Replicas = -1 }},
		"no instance":       {instance: "-"},
		"no jetstream":      {js: nil, instance: "nil"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := kvTestConfig()
			if tc.mutate != nil {
				tc.mutate(&cfg)
			}
			instance, stream := "a", js
			switch tc.instance {
			case "-":
				instance = ""
			case "nil":
				stream = nil
			}
			lease, err := jobs.NewUnboundKVLease(stream, cfg, instance)
			require.Error(t, err)
			assert.Nil(t, lease)
		})
	}
}

func TestNewInstanceId(t *testing.T) {
	host, err := os.Hostname()
	require.NoError(t, err)
	a, b := jobs.NewInstanceId(), jobs.NewInstanceId()
	assert.Regexp(t, "^"+regexp.QuoteMeta(host)+"-[0-9a-f]{6}$", a)
	assert.NotEqual(t, a, b)
}
