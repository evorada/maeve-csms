// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
	natstransport "github.com/thoughtworks/maeve-csms/manager/transport/nats"
	oteltrace "go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
)

// EventsConfig configures the domain events that the manager stores in its outbox and
// relays from there to JetStream.
type EventsConfig struct {
	// Enabled stores the events. It has no effect with Firestore storage, which has no
	// outbox: events are off there.
	Enabled bool `mapstructure:"enabled" toml:"enabled"`
	// RelayEnabled publishes the stored events to JetStream; it needs [nats].
	RelayEnabled bool `mapstructure:"relay_enabled" toml:"relay_enabled"`
	// SubjectPrefix is the first token of the subjects, "<prefix>.<type>.<org_id>".
	SubjectPrefix string `mapstructure:"subject_prefix" toml:"subject_prefix" validate:"required"`
	// Stream is the stream that captures "<prefix>.>".
	Stream string `mapstructure:"stream" toml:"stream" validate:"required"`
	// EnsureStream creates or updates the stream at startup (development); otherwise the
	// manager only checks that it exists.
	EnsureStream bool `mapstructure:"ensure_stream" toml:"ensure_stream"`
	// StreamReplicas and StreamMaxAge are only used by EnsureStream.
	StreamReplicas  int    `mapstructure:"stream_replicas" toml:"stream_replicas" validate:"min=1,max=5"`
	StreamMaxAge    string `mapstructure:"stream_max_age" toml:"stream_max_age"`
	RelayInterval   string `mapstructure:"relay_interval" toml:"relay_interval"`
	RelayBatchSize  int    `mapstructure:"relay_batch_size" toml:"relay_batch_size" validate:"min=1,max=1000"`
	OutboxRetention string `mapstructure:"outbox_retention" toml:"outbox_retention"`
	PurgeInterval   string `mapstructure:"purge_interval" toml:"purge_interval"`
}

// relaysEvents reports whether the manager publishes the events of its outbox: only
// when the events and the relay are enabled and the storage has an outbox.
func (c *BaseConfig) relaysEvents() bool {
	return c.Events.Enabled && c.Events.RelayEnabled && c.Storage.Type != "firestore"
}

// OutboxPurgeSettings say when the published events are deleted from the outbox.
type OutboxPurgeSettings struct {
	Retention time.Duration // events published longer ago are deleted
	Interval  time.Duration // pause between two runs of the purge job
}

// eventsSettings are the parsed durations of an EventsConfig.
type eventsSettings struct {
	streamMaxAge    time.Duration
	relayInterval   time.Duration
	outboxRetention time.Duration
	purgeInterval   time.Duration
}

// parseEventsConfig checks [events].
func parseEventsConfig(cfg *EventsConfig) (eventsSettings, error) {
	var settings eventsSettings
	for _, d := range []struct {
		name  string
		value string
		into  *time.Duration
	}{
		{"stream_max_age", cfg.StreamMaxAge, &settings.streamMaxAge},
		{"relay_interval", cfg.RelayInterval, &settings.relayInterval},
		{"outbox_retention", cfg.OutboxRetention, &settings.outboxRetention},
		{"purge_interval", cfg.PurgeInterval, &settings.purgeInterval},
	} {
		parsed, err := time.ParseDuration(d.value)
		if err != nil {
			return eventsSettings{}, fmt.Errorf("failed to parse events %s: %s", d.name, err)
		}
		if parsed <= 0 {
			return eventsSettings{}, fmt.Errorf("events %s must be positive: %s", d.name, d.value)
		}
		*d.into = parsed
	}

	if err := natstransport.ValidatePrefix(cfg.SubjectPrefix); err != nil {
		return eventsSettings{}, fmt.Errorf("events subject_prefix: %w", err)
	}
	if strings.ContainsAny(cfg.Stream, ".*>/\\ \t\r\n") {
		return eventsSettings{}, fmt.Errorf("events stream: not a valid stream name: %q", cfg.Stream)
	}
	return settings, nil
}

// getOutboxPurge returns when the published events are deleted, or nil when the storage
// has no outbox (Firestore). Events published before they were disabled are purged too.
func getOutboxPurge(settings eventsSettings, storageType string) *OutboxPurgeSettings {
	if storageType == "firestore" {
		return nil
	}
	return &OutboxPurgeSettings{Retention: settings.outboxRetention, Interval: settings.purgeInterval}
}

// streamSetupTimeout bounds the creation or the check of the stream at startup.
const streamSetupTimeout = 5 * time.Second

// getEventRelay creates the relay of the outbox to JetStream and creates or checks the
// stream. A stream that cannot be created or is missing does not stop the manager: the
// error is logged, the events stay in the outbox and the relay tries again on every run.
func getEventRelay(ctx context.Context, cfg *EventsConfig, settings eventsSettings, outbox store.OutboxStore,
	natsConn *nats.Conn, tracer oteltrace.Tracer) (*events.Relay, error) {
	js, err := jetstream.New(natsConn)
	if err != nil {
		return nil, fmt.Errorf("failed to create the jetstream context: %w", err)
	}

	setupStream(ctx, js, natsConn, cfg, events.StreamConfig{
		Name:       cfg.Stream,
		Subjects:   []string{cfg.SubjectPrefix + ".>"},
		MaxAge:     settings.streamMaxAge,
		Replicas:   cfg.StreamReplicas,
		Duplicates: events.DefaultDuplicateWindow,
	})

	return &events.Relay{
		Store:         outbox,
		Publisher:     &events.JetStreamPublisher{JS: js, Timeout: events.DefaultPublishTimeout},
		SubjectPrefix: cfg.SubjectPrefix,
		Product:       events.Product,
		BatchSize:     cfg.RelayBatchSize,
		MaxBatches:    events.DefaultRelayMaxBatches,
		Tracer:        tracer,
	}, nil
}

// setupStream creates the stream (ensure_stream) or checks it, and logs what failed.
// It does not wait for a connection that is not up at startup.
func setupStream(ctx context.Context, js jetstream.JetStream, natsConn *nats.Conn, cfg *EventsConfig, streamCfg events.StreamConfig) {
	attrs := []any{"stream", streamCfg.Name, "subjects", streamCfg.Subjects, "ensure_stream", cfg.EnsureStream}
	if !natsConn.IsConnected() {
		slog.Error("the event stream was not set up: NATS is not connected at startup; "+
			"the domain events stay in the outbox until they can be published", attrs...)
		return
	}

	setupCtx, cancel := context.WithTimeout(ctx, streamSetupTimeout)
	defer cancel()
	var err error
	if cfg.EnsureStream {
		err = events.EnsureStream(setupCtx, js, streamCfg)
	} else {
		err = events.CheckStream(setupCtx, js, streamCfg)
	}
	if err != nil {
		slog.Error("the event stream is not usable: the domain events stay in the outbox and the relay keeps trying",
			append(attrs, "err", err)...)
		return
	}
	slog.Info("domain events are published to JetStream", attrs...)
}
