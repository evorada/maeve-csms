// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/connectivity"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

// ConnectivityConfig configures how the manager tracks whether the websockets of the
// charge stations are open.
type ConnectivityConfig struct {
	// LastSeenWriteInterval limits the writes of the last seen time of a station that its
	// OCPP messages cause to one per interval.
	LastSeenWriteInterval string `mapstructure:"last_seen_write_interval" toml:"last_seen_write_interval"`
	// StaleAfter is how long a connected station may show no sign of life before it is
	// marked disconnected.
	StaleAfter string `mapstructure:"stale_after" toml:"stale_after"`
	// SweepInterval is the pause between two searches for such stations.
	SweepInterval string `mapstructure:"sweep_interval" toml:"sweep_interval"`
}

// connectivitySettings are the parsed durations of a ConnectivityConfig.
type connectivitySettings struct {
	lastSeenWriteInterval time.Duration
	staleAfter            time.Duration
	sweepInterval         time.Duration
}

// parseConnectivityConfig checks the durations of [connectivity].
func parseConnectivityConfig(cfg *ConnectivityConfig) (connectivitySettings, error) {
	var settings connectivitySettings
	for _, d := range []struct {
		name  string
		value string
		into  *time.Duration
	}{
		{"last_seen_write_interval", cfg.LastSeenWriteInterval, &settings.lastSeenWriteInterval},
		{"stale_after", cfg.StaleAfter, &settings.staleAfter},
		{"sweep_interval", cfg.SweepInterval, &settings.sweepInterval},
	} {
		parsed, err := time.ParseDuration(d.value)
		if err != nil {
			return connectivitySettings{}, fmt.Errorf("failed to parse connectivity %s: %s", d.name, err)
		}
		if parsed <= 0 {
			return connectivitySettings{}, fmt.Errorf("connectivity %s must be positive: %s", d.name, d.value)
		}
		*d.into = parsed
	}

	// the messages of a station that sends no alive events update its last seen time only
	// once per write interval: with a shorter stale_after it would be swept while it talks
	if settings.staleAfter <= settings.lastSeenWriteInterval {
		return connectivitySettings{}, fmt.Errorf("connectivity stale_after (%s) must be longer than last_seen_write_interval (%s)",
			cfg.StaleAfter, cfg.LastSeenWriteInterval)
	}
	// the alive interval is a setting of the gateway, which the manager cannot read: with
	// less than twice the default, one lost alive event marks a connected station
	// disconnected, unless the gateways send them more often
	if settings.staleAfter <= 2*connectivity.DefaultGatewayAliveInterval {
		slog.Warn("connectivity stale_after is not more than twice the default alive interval of the gateway: "+
			"set the gateway's --conn-alive-interval to less than half of stale_after",
			"stale_after", cfg.StaleAfter, "gateway_default_alive_interval", connectivity.DefaultGatewayAliveInterval.String())
	}
	return settings, nil
}

// getConnectivityTracker creates what records the connectivity of the charge stations.
// The Firestore storage does not track connectivity: it returns nil there.
func getConnectivityTracker(settings connectivitySettings, storageType string, engine store.Engine) *connectivity.Tracker {
	if storageType == "firestore" {
		slog.Warn("station connectivity is not tracked: the firestore storage does not implement it")
		return nil
	}
	return &connectivity.Tracker{
		Store:          engine,
		Clock:          clock.RealClock{},
		SeenWriteEvery: settings.lastSeenWriteInterval,
		StaleAfter:     settings.staleAfter,
	}
}
