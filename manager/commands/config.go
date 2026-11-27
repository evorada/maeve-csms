// SPDX-License-Identifier: Apache-2.0

package commands

import "time"

// The defaults of Config. A zero duration of a Config means its default, except for
// ConnectDispatchDelay.
const (
	DefaultResponseTimeout      = 30 * time.Second
	DefaultConnectDispatchDelay = 5 * time.Second
	DefaultSweepInterval        = 5 * time.Second
	DefaultTTL                  = time.Hour
	DefaultMaxTTL               = 7 * 24 * time.Hour
	DefaultRetention            = 90 * 24 * time.Hour
)

// Config holds the timings of the command queue.
type Config struct {
	// ResponseTimeout is how long a sent command waits for its CallResult or CallError
	// before the sweep marks it timed out and sends the next command of the station.
	ResponseTimeout time.Duration
	// ConnectDispatchDelay is the pause between the connection of a station and the
	// first command sent to it, so that a rebooting station can send its
	// BootNotification first. 0 is no pause.
	ConnectDispatchDelay time.Duration
	// SweepInterval is the pause between two runs of the sweep.
	SweepInterval time.Duration
	// DefaultTTL is how long a command may wait in the queue when neither the caller,
	// TTL nor the catalogue says otherwise.
	DefaultTTL time.Duration
	// MaxTTL is the longest time a command may wait in the queue.
	MaxTTL time.Duration
	// Retention is how long a completed command is kept.
	Retention time.Duration
	// TTL is how long a command of an action may wait in the queue, by action.
	TTL map[string]time.Duration
}

func orDefault(d, def time.Duration) time.Duration {
	if d == 0 {
		return def
	}
	return d
}

func (c Config) responseTimeout() time.Duration {
	return orDefault(c.ResponseTimeout, DefaultResponseTimeout)
}

func (c Config) sweepInterval() time.Duration {
	return orDefault(c.SweepInterval, DefaultSweepInterval)
}

func (c Config) maxTTL() time.Duration {
	return orDefault(c.MaxTTL, DefaultMaxTTL)
}

func (c Config) retention() time.Duration {
	return orDefault(c.Retention, DefaultRetention)
}

// TTLFor returns how long a command of the action may wait in the queue when its caller
// does not say: the configured TTL of the action, else the default of the catalogue,
// else DefaultTTL.
func (c Config) TTLFor(spec ActionSpec) time.Duration {
	if ttl, ok := c.TTL[spec.Action]; ok && ttl > 0 {
		return ttl
	}
	if spec.DefaultTTL > 0 {
		return spec.DefaultTTL
	}
	return orDefault(c.DefaultTTL, DefaultTTL)
}
