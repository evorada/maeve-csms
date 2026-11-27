// SPDX-License-Identifier: Apache-2.0

package config

// EventsConfig configures the domain events that the manager stores in its outbox.
type EventsConfig struct {
	// Enabled stores the events. It has no effect with Firestore storage, which has no
	// outbox: events are off there.
	Enabled bool `mapstructure:"enabled" toml:"enabled"`
}
