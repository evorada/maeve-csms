// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"context"
	"strings"
	"testing"
	"time"

	clone "github.com/huandu/go-clone/generic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/config"
	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/money"
	"github.com/thoughtworks/maeve-csms/manager/rating"
	"github.com/thoughtworks/maeve-csms/manager/sessions"
)

// analyzersOf returns the Analyzers that the handlers of the end of a transaction got: of
// OCPP 1.6 StopTransaction and of OCPP 2.0.1 TransactionEvent.
func analyzersOf(t *testing.T, settings *config.Config) (*rating.Analyzer, *rating.Analyzer) {
	t.Helper()
	router16, ok := settings.Ocpp16Handler.(*handlers.Router)
	require.True(t, ok)
	stop, ok := router16.CallRoutes["StopTransaction"].Handler.(ocpp16.StopTransactionHandler)
	require.True(t, ok)
	router201, ok := settings.Ocpp201Handler.(*handlers.Router)
	require.True(t, ok)
	event, ok := router201.CallRoutes["TransactionEvent"].Handler.(ocpp201.TransactionEventHandler)
	require.True(t, ok)
	return stop.Analyzer, event.Analyzer
}

// [tariff_service.idle_detection] sets the thresholds with which the manager tells when a
// transaction stopped charging. The handlers of the end of a transaction get an Analyzer
// with them, on the storage.
func TestConfigureIdleDetection(t *testing.T) {
	newConfig := func(t *testing.T, toml string) *config.BaseConfig {
		cfg := newTestConfig(t)
		cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
		require.NoError(t, cfg.Load(strings.NewReader(toml)))
		return cfg
	}

	t.Run("defaults", func(t *testing.T) {
		settings, err := config.Configure(context.TODO(), newConfig(t, ""))
		require.NoError(t, err)

		of16, of201 := analyzersOf(t, settings)
		require.NotNil(t, of16)
		assert.Same(t, of16, of201, "one Analyzer for both OCPP versions")
		assert.Equal(t, rating.IdleSettings{
			ChargingPowerShare: 0.01, MinChargingPowerW: 200, TimestampTolerance: time.Hour, FreshSession: time.Minute,
		}, of16.Defaults)
		assert.Same(t, settings.Storage, of16.Transactions)
		assert.Same(t, settings.Storage, of16.MeterValues)
		assert.Same(t, settings.Storage, of16.Assets)
		assert.Same(t, settings.Storage, of16.Registry)
		assert.Same(t, settings.Storage, of16.Tariffs)
		assert.NotNil(t, of16.Clock)
	})

	t.Run("overridden", func(t *testing.T) {
		settings, err := config.Configure(context.TODO(), newConfig(t, "[tariff_service.idle_detection]\n"+
			"charging_power_share = 0.05\nmin_charging_power_w = 150.5\ntimestamp_tolerance = \"30m\"\nfresh_session = \"90s\"\n"))
		require.NoError(t, err)

		of16, _ := analyzersOf(t, settings)
		assert.Equal(t, rating.IdleSettings{
			ChargingPowerShare: 0.05, MinChargingPowerW: 150.5, TimestampTolerance: 30 * time.Minute, FreshSession: 90 * time.Second,
		}, of16.Defaults)
	})

	t.Run("one key leaves the others at their defaults", func(t *testing.T) {
		settings, err := config.Configure(context.TODO(), newConfig(t, "[tariff_service]\ntype = \"ocpi\"\nidle_detection.min_charging_power_w = 500\n"))
		require.NoError(t, err)

		of16, _ := analyzersOf(t, settings)
		assert.Equal(t, rating.IdleSettings{
			ChargingPowerShare: 0.01, MinChargingPowerW: 500, TimestampTolerance: time.Hour, FreshSession: time.Minute,
		}, of16.Defaults)
	})

	// a number may be written with or without a decimal point
	for value, want := range map[string]float64{"0": 0, "0.0": 0, "250": 250, "250.5": 250.5, "1_000": 1000, "1e3": 1000, "+7": 7} {
		t.Run("a minimum power of "+value, func(t *testing.T) {
			settings, err := config.Configure(context.TODO(), newConfig(t, "[tariff_service.idle_detection]\nmin_charging_power_w = "+value+"\n"))
			require.NoError(t, err)

			of16, _ := analyzersOf(t, settings)
			assert.Equal(t, want, of16.Defaults.MinChargingPowerW)
			assert.Equal(t, 0.01, of16.Defaults.ChargingPowerShare)
		})
	}

	t.Run("the edges of what is accepted", func(t *testing.T) {
		settings, err := config.Configure(context.TODO(), newConfig(t, "[tariff_service.idle_detection]\n"+
			"charging_power_share = 1\nmin_charging_power_w = 0\ntimestamp_tolerance = \"0s\"\nfresh_session = \"0s\"\n"))
		require.NoError(t, err)

		of16, _ := analyzersOf(t, settings)
		assert.Equal(t, rating.IdleSettings{ChargingPowerShare: 1}, of16.Defaults)
	})

	for name, tc := range map[string]struct{ key, value string }{
		"a share of 0":                        {"charging_power_share", "0.0"},
		"a negative share":                    {"charging_power_share", "-0.01"},
		"a share above 1":                     {"charging_power_share", "1.01"},
		"a share that is no number":           {"charging_power_share", "nan"},
		"a negative minimum power":            {"min_charging_power_w", "-1"},
		"a minimum power that is no number":   {"min_charging_power_w", "nan"},
		"a minimum power without end":         {"min_charging_power_w", "inf"},
		"a tolerance that is no duration":     {"timestamp_tolerance", `"an hour"`},
		"a tolerance without a unit":          {"timestamp_tolerance", `"60"`},
		"a negative tolerance":                {"timestamp_tolerance", `"-1h"`},
		"an empty tolerance":                  {"timestamp_tolerance", `""`},
		"a share of 0 as a whole number":      {"charging_power_share", "0"},
		"a fresh session that is no duration": {"fresh_session", `"a minute"`},
		"a negative fresh session":            {"fresh_session", `"-60s"`},
		"an empty fresh session":              {"fresh_session", `""`},
	} {
		t.Run(name, func(t *testing.T) {
			settings, err := config.Configure(context.TODO(), newConfig(t, "[tariff_service.idle_detection]\n"+tc.key+" = "+tc.value+"\n"))
			require.Error(t, err)
			assert.ErrorContains(t, err, "idle_detection "+tc.key)
			assert.Nil(t, settings)
		})
	}

	// what is not a number is refused when the file is read
	for name, value := range map[string]string{"a text": `"low"`, "a boolean": "true", "a list": "[200]", "a time": "10:00:00"} {
		t.Run("a minimum power that is "+name, func(t *testing.T) {
			cfg := newTestConfig(t)
			err := cfg.Load(strings.NewReader("[tariff_service.idle_detection]\nmin_charging_power_w = " + value + "\n"))
			require.Error(t, err)
			assert.ErrorContains(t, err, "is not a number")
		})
	}
}

// ratedHandlers are the handlers that rate a transaction: OCPP 1.6 StartTransaction (which
// says whether a transaction without a tariff has a price), MeterValues and
// StopTransaction, and OCPP 2.0.1 TransactionEvent.
type ratedHandlers struct {
	start  ocpp16.StartTransactionHandler
	meter  ocpp16.MeterValuesHandler
	stop   ocpp16.StopTransactionHandler
	events ocpp201.TransactionEventHandler
}

func ratedHandlersOf(t *testing.T, settings *config.Config) ratedHandlers {
	t.Helper()
	router16, ok := settings.Ocpp16Handler.(*handlers.Router)
	require.True(t, ok)
	router201, ok := settings.Ocpp201Handler.(*handlers.Router)
	require.True(t, ok)
	var h ratedHandlers
	h.start, ok = router16.CallRoutes["StartTransaction"].Handler.(ocpp16.StartTransactionHandler)
	require.True(t, ok)
	h.meter, ok = router16.CallRoutes["MeterValues"].Handler.(ocpp16.MeterValuesHandler)
	require.True(t, ok)
	h.stop, ok = router16.CallRoutes["StopTransaction"].Handler.(ocpp16.StopTransactionHandler)
	require.True(t, ok)
	h.events, ok = router201.CallRoutes["TransactionEvent"].Handler.(ocpp201.TransactionEventHandler)
	require.True(t, ok)
	return h
}

// [tariff_service] sets how the transactions are rated. The handlers of the meter values
// and of the end of a transaction get one session service with these settings, on the
// storage.
func TestConfigureTariffService(t *testing.T) {
	newConfig := func(t *testing.T, toml string) *config.BaseConfig {
		cfg := newTestConfig(t)
		cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
		require.NoError(t, cfg.Load(strings.NewReader(toml)))
		return cfg
	}

	t.Run("the defaults of the configuration", func(t *testing.T) {
		assert.Equal(t, "ocpi", config.DefaultConfig.TariffService.Type)
		assert.Empty(t, config.DefaultConfig.TariffService.FallbackEnergyPricePerKwh)
		assert.Equal(t, "EUR", config.DefaultConfig.TariffService.FallbackCurrency)
		assert.Equal(t, 2, config.DefaultConfig.TariffService.RoundingScale)
		assert.Equal(t, "half_up", config.DefaultConfig.TariffService.RoundingMode)
		assert.Equal(t, "1m0s", config.DefaultConfig.TariffService.RunningCostInterval)
	})

	t.Run("defaults", func(t *testing.T) {
		settings, err := config.Configure(context.TODO(), newConfig(t, ""))
		require.NoError(t, err)

		service := settings.Sessions
		require.NotNil(t, service)
		assert.Equal(t, rating.Rater{Rounding: rating.Rounding{Scale: 2, Mode: money.RoundHalfUp}}, service.Rater)
		assert.Nil(t, service.Fallback, "a transaction without a tariff has no price")
		assert.Equal(t, time.Minute, service.RunningEvery)
		assert.Same(t, settings.Storage, service.Transactions)
		assert.Same(t, settings.Storage, service.MeterValues)
		assert.Same(t, settings.Storage, service.Tariffs)
		assert.Same(t, settings.Storage, service.Assets)
		assert.Same(t, settings.Storage, service.Registry)
		assert.Same(t, settings.Storage, service.Tx)
		assert.NotNil(t, service.Clock)

		h := ratedHandlersOf(t, settings)
		assert.Same(t, service, h.meter.Sessions)
		assert.Same(t, service, h.stop.Sessions)
		assert.Same(t, service, h.events.Sessions)
		require.NotNil(t, h.stop.Analyzer)
		assert.Same(t, h.stop.Analyzer, service.Analyzer, "the idle time of the rating is that of the analysis")
		require.NotNil(t, h.start.TariffSelector)
		assert.False(t, h.start.TariffSelector.Fallback)
		assert.Same(t, h.start.TariffSelector, h.events.TariffSelector)
	})

	t.Run("overridden", func(t *testing.T) {
		settings, err := config.Configure(context.TODO(), newConfig(t, "[tariff_service]\n"+
			"type = \"ocpi\"\nfallback_energy_price_per_kwh = \"0.55\"\nfallback_currency = \"PLN\"\n"+
			"rounding_scale = 3\nrounding_mode = \"half_even\"\nrunning_cost_interval = \"30s\"\n"))
		require.NoError(t, err)

		service := settings.Sessions
		assert.Equal(t, rating.Rater{Rounding: rating.Rounding{Scale: 3, Mode: money.RoundHalfEven}}, service.Rater)
		assert.Equal(t, &sessions.FallbackTariff{Currency: "PLN", PricePerKwh: 5500}, service.Fallback)
		assert.Equal(t, 30*time.Second, service.RunningEvery)
		// a transaction without a tariff is marked as one that has a price
		assert.True(t, ratedHandlersOf(t, settings).start.TariffSelector.Fallback)
	})

	t.Run("a fallback price in the default currency", func(t *testing.T) {
		settings, err := config.Configure(context.TODO(), newConfig(t, "[tariff_service]\nfallback_energy_price_per_kwh = \"0.3456\"\n"))
		require.NoError(t, err)
		assert.Equal(t, &sessions.FallbackTariff{Currency: "EUR", PricePerKwh: 3456}, settings.Sessions.Fallback)
		assert.Equal(t, rating.DefaultRounding(), settings.Sessions.Rater.Rounding)
	})

	t.Run("the edges of what is accepted", func(t *testing.T) {
		settings, err := config.Configure(context.TODO(), newConfig(t, "[tariff_service]\n"+
			"fallback_energy_price_per_kwh = \"0\"\nrounding_scale = 0\nrunning_cost_interval = \"0s\"\n"))
		require.NoError(t, err)
		assert.Equal(t, &sessions.FallbackTariff{Currency: "EUR"}, settings.Sessions.Fallback, "a price of nothing is a price")
		assert.Equal(t, rating.Rounding{Scale: 0, Mode: money.RoundHalfUp}, settings.Sessions.Rater.Rounding)
		assert.Zero(t, settings.Sessions.RunningEvery)

		settings, err = config.Configure(context.TODO(), newConfig(t, "[tariff_service]\nrounding_scale = 4\n"))
		require.NoError(t, err)
		assert.Equal(t, 4, settings.Sessions.Rater.Rounding.Scale)

		// the currency is that of the fallback price: without a price it is not looked at
		_, err = config.Configure(context.TODO(), newConfig(t, "[tariff_service]\nfallback_currency = \"\"\n"))
		require.NoError(t, err)
	})

	for name, tc := range map[string]struct{ toml, want string }{
		"a price that is no number":       {`fallback_energy_price_per_kwh = "cheap"`, "fallback_energy_price_per_kwh"},
		"a negative price":                {`fallback_energy_price_per_kwh = "-0.10"`, "fallback_energy_price_per_kwh must not be negative"},
		"a price with five decimals":      {`fallback_energy_price_per_kwh = "0.12345"`, "fallback_energy_price_per_kwh"},
		"a currency in small letters":     {"fallback_energy_price_per_kwh = \"0.55\"\nfallback_currency = \"eur\"", "fallback_currency"},
		"a currency of four letters":      {"fallback_energy_price_per_kwh = \"0.55\"\nfallback_currency = \"EURO\"", "fallback_currency"},
		"no currency for the price":       {"fallback_energy_price_per_kwh = \"0.55\"\nfallback_currency = \"\"", "fallback_currency"},
		"a negative scale":                {`rounding_scale = -1`, "rounding_scale must be from 0 to 4"},
		"a scale of five":                 {`rounding_scale = 5`, "rounding_scale must be from 0 to 4"},
		"a rounding mode that is not":     {`rounding_mode = "up"`, "rounding_mode"},
		"an empty rounding mode":          {`rounding_mode = ""`, "rounding_mode"},
		"an interval that is no duration": {`running_cost_interval = "soon"`, "running_cost_interval"},
		"an interval without a unit":      {`running_cost_interval = "60"`, "running_cost_interval"},
		"a negative interval":             {`running_cost_interval = "-1s"`, "running_cost_interval must not be negative"},
		"an empty interval":               {`running_cost_interval = ""`, "running_cost_interval"},
	} {
		t.Run(name, func(t *testing.T) {
			settings, err := config.Configure(context.TODO(), newConfig(t, "[tariff_service]\n"+tc.toml+"\n"))
			require.Error(t, err)
			assert.ErrorContains(t, err, "tariff_service")
			assert.ErrorContains(t, err, tc.want)
			assert.Nil(t, settings)
		})
	}
}

// The tariff service that charged every transaction a price per kWh is gone: a
// configuration that still names it is refused when the manager starts, with what to do.
func TestTheKwhTariffServiceIsRefused(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
	require.NoError(t, cfg.Load(strings.NewReader("[tariff_service]\ntype = \"kwh\"\n")))

	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, `tariff_service type "kwh" is no longer supported`)
	assert.ErrorContains(t, err, `fallback_energy_price_per_kwh = "0.55"`)

	settings, err := config.Configure(context.TODO(), cfg)
	require.Error(t, err)
	assert.ErrorContains(t, err, "fallback_energy_price_per_kwh")
	assert.Nil(t, settings)

	// any other type is not known
	other := clone.Clone(cfg)
	other.TariffService.Type = "flat"
	err = other.Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "TariffService.Type")
	assert.NotContains(t, err.Error(), "fallback_energy_price_per_kwh")

	// and a configuration without the section has the type that there is
	without := newTestConfig(t)
	without.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
	require.NoError(t, without.Validate())
	assert.Equal(t, "ocpi", without.TariffService.Type)
}
