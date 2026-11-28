// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/money"
	"github.com/thoughtworks/maeve-csms/manager/rating"
	"github.com/thoughtworks/maeve-csms/manager/sessions"
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

// TariffServiceConfig configures how the manager works out what a transaction costs.
type TariffServiceConfig struct {
	// Type is "ocpi": a transaction is rated with the version of the OCPI tariff that was
	// chosen for it when it started.
	Type string `mapstructure:"type" toml:"type" validate:"required,oneof=ocpi"`
	// FallbackEnergyPricePerKwh is the price per kWh, without VAT, of a transaction that
	// has no tariff, as a decimal number in a string ("0.55"). Empty: such a transaction
	// has no price.
	FallbackEnergyPricePerKwh string `mapstructure:"fallback_energy_price_per_kwh" toml:"fallback_energy_price_per_kwh"`
	// FallbackCurrency is the currency of FallbackEnergyPricePerKwh (ISO 4217).
	FallbackCurrency string `mapstructure:"fallback_currency" toml:"fallback_currency"`
	// RoundingScale is the number of decimals of the amounts of a price, 0 to 4.
	RoundingScale int `mapstructure:"rounding_scale" toml:"rounding_scale"`
	// RoundingMode is what happens to a half: "half_up" or "half_even".
	RoundingMode string `mapstructure:"rounding_mode" toml:"rounding_mode"`
	// RunningCostInterval is the least time between two ratings of a transaction that
	// runs.
	RunningCostInterval string `mapstructure:"running_cost_interval" toml:"running_cost_interval"`
	// IdleDetection configures how the manager tells when a transaction stopped charging.
	IdleDetection IdleDetectionConfig `mapstructure:"idle_detection" toml:"idle_detection"`
}

// DefaultRunningCostInterval is the default of tariff_service running_cost_interval.
const DefaultRunningCostInterval = time.Minute

// legacyTariffServiceType is the tariff service that charged every transaction 0.55 per
// kWh of the reading of its energy register. It is gone.
const legacyTariffServiceType = "kwh"

// errLegacyTariffService says what replaces the legacy tariff service.
var errLegacyTariffService = errors.New(`tariff_service type "kwh" is no longer supported: use type = "ocpi" (the default), ` +
	`and set fallback_energy_price_per_kwh = "0.55" (with fallback_currency) to go on charging the transactions without a tariff 0.55 per kWh`)

var tariffServiceCurrency = regexp.MustCompile(`^[A-Z]{3}$`)

// tariffServiceSettings are the settings of [tariff_service] for the rating.
type tariffServiceSettings struct {
	// fallback is the price of the transactions without a tariff; nil when there is none
	fallback            *sessions.FallbackTariff
	rounding            rating.Rounding
	runningCostInterval time.Duration
}

// parseTariffServiceConfig checks [tariff_service] and returns the settings of the rating.
func parseTariffServiceConfig(cfg *TariffServiceConfig) (tariffServiceSettings, error) {
	var settings tariffServiceSettings

	if cfg.FallbackEnergyPricePerKwh != "" {
		price, err := money.ParseAmount(cfg.FallbackEnergyPricePerKwh)
		if err != nil {
			return settings, fmt.Errorf("failed to parse tariff_service fallback_energy_price_per_kwh: %s", err)
		}
		if price < 0 {
			return settings, fmt.Errorf("tariff_service fallback_energy_price_per_kwh must not be negative: %s", cfg.FallbackEnergyPricePerKwh)
		}
		if !tariffServiceCurrency.MatchString(cfg.FallbackCurrency) {
			return settings, fmt.Errorf("tariff_service fallback_currency must be an ISO 4217 currency code of 3 capital letters: %q", cfg.FallbackCurrency)
		}
		settings.fallback = &sessions.FallbackTariff{Currency: cfg.FallbackCurrency, PricePerKwh: price}
	}

	if cfg.RoundingScale < 0 || cfg.RoundingScale > 4 {
		return settings, fmt.Errorf("tariff_service rounding_scale must be from 0 to 4: %d", cfg.RoundingScale)
	}
	switch mode := money.RoundingMode(cfg.RoundingMode); mode {
	case money.RoundHalfUp, money.RoundHalfEven:
		settings.rounding = rating.Rounding{Scale: cfg.RoundingScale, Mode: mode}
	default:
		return settings, fmt.Errorf("tariff_service rounding_mode must be %q or %q: %q", money.RoundHalfUp, money.RoundHalfEven, cfg.RoundingMode)
	}

	interval, err := time.ParseDuration(cfg.RunningCostInterval)
	if err != nil {
		return settings, fmt.Errorf("failed to parse tariff_service running_cost_interval: %s", err)
	}
	if interval < 0 {
		return settings, fmt.Errorf("tariff_service running_cost_interval must not be negative: %s", cfg.RunningCostInterval)
	}
	settings.runningCostInterval = interval
	return settings, nil
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
