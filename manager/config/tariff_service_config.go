// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/rating"
)

// Float is a number of the configuration that may have a fraction. The TOML file may
// give it with or without a decimal point (0.01, 200, 0).
//
// It reads the number from the text of the file itself: the TOML decoder does not read
// every whole number into a float64 (it fails on 0).
type Float float64

func (f *Float) UnmarshalText(text []byte) error {
	value, err := strconv.ParseFloat(string(text), 64)
	if err != nil {
		return fmt.Errorf("%q is not a number", text)
	}
	*f = Float(value)
	return nil
}

type TariffServiceConfig struct {
	Type string `mapstructure:"type" toml:"type" validate:"required,oneof=kwh"`
	// IdleDetection configures how the manager tells when a transaction stopped charging.
	IdleDetection IdleDetectionConfig `mapstructure:"idle_detection" toml:"idle_detection"`
}

// IdleDetectionConfig configures how the manager tells, from the readings of the energy
// register, when a transaction stopped charging and started to be idle (plugged in
// without charging). A tariff can set the two powers for its own transactions.
type IdleDetectionConfig struct {
	// ChargingPowerShare is the share of the highest power of the connector from which a
	// transaction charges, above 0 and at most 1.
	ChargingPowerShare Float `mapstructure:"charging_power_share" toml:"charging_power_share"`
	// MinChargingPowerW is the lowest power in W that counts as charging, whatever the
	// highest power of the connector.
	MinChargingPowerW Float `mapstructure:"min_charging_power_w" toml:"min_charging_power_w"`
	// TimestampTolerance is how long before the start or after the end of a transaction
	// a meter reading may be timed and still be used.
	TimestampTolerance string `mapstructure:"timestamp_tolerance" toml:"timestamp_tolerance"`
	// FreshSession is how long after its start a transaction is never said to be parking.
	FreshSession string `mapstructure:"fresh_session" toml:"fresh_session"`
}

// parseIdleDetectionConfig checks [tariff_service.idle_detection] and returns the idle
// settings of the manager.
func parseIdleDetectionConfig(cfg *IdleDetectionConfig) (rating.IdleSettings, error) {
	settings := rating.IdleSettings{
		ChargingPowerShare: float64(cfg.ChargingPowerShare),
		MinChargingPowerW:  float64(cfg.MinChargingPowerW),
	}
	// the same limits as for the idle settings of a tariff
	if share := settings.ChargingPowerShare; math.IsNaN(share) || share <= 0 || share > 1 {
		return rating.IdleSettings{}, fmt.Errorf("tariff_service idle_detection charging_power_share must be above 0 and at most 1: %v", share)
	}
	if power := settings.MinChargingPowerW; math.IsNaN(power) || math.IsInf(power, 0) || power < 0 {
		return rating.IdleSettings{}, fmt.Errorf("tariff_service idle_detection min_charging_power_w must not be negative: %v", power)
	}
	for _, d := range []struct {
		name  string
		value string
		into  *time.Duration
	}{
		{"timestamp_tolerance", cfg.TimestampTolerance, &settings.TimestampTolerance},
		{"fresh_session", cfg.FreshSession, &settings.FreshSession},
	} {
		parsed, err := time.ParseDuration(d.value)
		if err != nil {
			return rating.IdleSettings{}, fmt.Errorf("failed to parse tariff_service idle_detection %s: %s", d.name, err)
		}
		if parsed < 0 {
			return rating.IdleSettings{}, fmt.Errorf("tariff_service idle_detection %s must not be negative: %s", d.name, d.value)
		}
		*d.into = parsed
	}
	return settings, nil
}
