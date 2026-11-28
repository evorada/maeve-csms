// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	oteltrace "go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

// JobsConfig configures the periodic background jobs and the runner that runs them.
type JobsConfig struct {
	// Runner is "nats_kv" (each job runs on one replica at a time, coordinated through a
	// lease in a NATS KV bucket; needs [nats]) or "local" (every job runs in this
	// process, for a single instance without NATS).
	Runner string `mapstructure:"runner" toml:"runner" validate:"omitempty,oneof=local nats_kv"`
	// LeaseBucket is the KV bucket of the leases.
	LeaseBucket string `mapstructure:"lease_bucket" toml:"lease_bucket"`
	// LeaseTTL is how long a lease lasts after its last renewal; it is renewed every third
	// of it.
	LeaseTTL string `mapstructure:"lease_ttl" toml:"lease_ttl"`
	// EnsureBucket creates or updates the bucket at startup (development); otherwise it
	// must exist.
	EnsureBucket bool `mapstructure:"ensure_bucket" toml:"ensure_bucket"`
	// BucketReplicas is only used by EnsureBucket.
	BucketReplicas            int    `mapstructure:"bucket_replicas" toml:"bucket_replicas" validate:"min=1,max=5"`
	SyncInterval              string `mapstructure:"sync_interval" toml:"sync_interval"`
	SyncRetryAfter            string `mapstructure:"sync_retry_after" toml:"sync_retry_after"`
	ReservationExpiryInterval string `mapstructure:"reservation_expiry_interval" toml:"reservation_expiry_interval" validate:"required"`
}

// usesLeasedRunner reports whether the jobs run on the NATS KV leased runner (an empty
// runner is the default, nats_kv).
func (c *JobsConfig) usesLeasedRunner() bool {
	return c.Runner != "local"
}

// jobsSettings are the parsed durations of a JobsConfig.
type jobsSettings struct {
	leaseTTL                  time.Duration
	syncInterval              time.Duration
	syncRetryAfter            time.Duration
	reservationExpiryInterval time.Duration
}

// parseJobsConfig checks [jobs]: every duration must parse and be positive.
func parseJobsConfig(cfg *JobsConfig) (jobsSettings, error) {
	var settings jobsSettings
	for _, d := range []struct {
		name  string
		value string
		into  *time.Duration
	}{
		{"lease ttl", cfg.LeaseTTL, &settings.leaseTTL},
		{"sync interval", cfg.SyncInterval, &settings.syncInterval},
		{"sync retry after", cfg.SyncRetryAfter, &settings.syncRetryAfter},
		{"reservation expiry interval", cfg.ReservationExpiryInterval, &settings.reservationExpiryInterval},
	} {
		parsed, err := time.ParseDuration(d.value)
		if err != nil {
			return jobsSettings{}, fmt.Errorf("failed to parse %s: %s", d.name, err)
		}
		if parsed <= 0 {
			return jobsSettings{}, fmt.Errorf("%s must be positive: %s", d.name, d.value)
		}
		*d.into = parsed
	}
	return settings, nil
}

// leaseSetupTimeout bounds the creation or the lookup of the lease bucket at startup.
const leaseSetupTimeout = 5 * time.Second

// getJobRunner creates the runner of the background jobs. With the leased runner, a
// bucket that cannot be bound at startup (NATS not connected, bucket missing) does not
// stop the manager: the error is logged and the runner keeps trying; no job runs
// until it can take its lease.
func getJobRunner(ctx context.Context, cfg *JobsConfig, settings jobsSettings, natsConn *nats.Conn, tracer oteltrace.Tracer) (jobs.Runner, error) {
	if !cfg.usesLeasedRunner() {
		slog.Info("background jobs run in this process without coordination (jobs runner \"local\")")
		return &jobs.LocalRunner{Clock: clock.RealClock{}, Tracer: tracer}, nil
	}

	js, err := jetstream.New(natsConn)
	if err != nil {
		return nil, fmt.Errorf("failed to create the jetstream context of the job leases: %w", err)
	}
	lease, err := jobs.NewUnboundKVLease(js, jobs.KVLeaseConfig{
		Bucket:   cfg.LeaseBucket,
		TTL:      settings.leaseTTL,
		Replicas: cfg.BucketReplicas,
		Ensure:   cfg.EnsureBucket,
	}, jobs.NewInstanceId())
	if err != nil {
		return nil, err
	}

	attrs := []any{"bucket", cfg.LeaseBucket, "ensure_bucket", cfg.EnsureBucket, "instance", lease.Instance()}
	if !natsConn.IsConnected() {
		slog.Error("the job lease bucket was not set up: NATS is not connected at startup; "+
			"the background jobs start once their leases can be taken", attrs...)
	} else {
		setupCtx, cancel := context.WithTimeout(ctx, leaseSetupTimeout)
		err := lease.Bind(setupCtx)
		cancel()
		if err != nil {
			slog.Error("the job lease bucket is not usable: the background jobs start once their leases can be taken",
				append(attrs, "err", err)...)
		} else {
			slog.Info("background jobs run on one replica at a time, coordinated through NATS KV leases", attrs...)
		}
	}

	return &jobs.LeasedRunner{
		Lease:  lease,
		Clock:  clock.RealClock{},
		TTL:    settings.leaseTTL,
		Tracer: tracer,
	}, nil
}
