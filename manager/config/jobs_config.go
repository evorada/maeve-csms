// SPDX-License-Identifier: Apache-2.0

package config

// JobsConfig configures the periodic background jobs.
type JobsConfig struct {
	ReservationExpiryInterval string `mapstructure:"reservation_expiry_interval" toml:"reservation_expiry_interval" validate:"required"`
}
