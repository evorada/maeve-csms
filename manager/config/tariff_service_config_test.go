// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/config"
	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/rating"
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
		settings, err := config.Configure(context.TODO(), newConfig(t, "[tariff_service]\ntype = \"kwh\"\nidle_detection.min_charging_power_w = 500\n"))
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
