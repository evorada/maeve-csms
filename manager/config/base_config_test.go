// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	clone "github.com/huandu/go-clone/generic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/config"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

func TestParseConfig(t *testing.T) {
	cfg := clone.Clone(&config.DefaultConfig)
	err := cfg.LoadFromFile("testdata/config.toml")
	require.NoError(t, err)

	want := &config.BaseConfig{
		Api: config.ApiSettingsConfig{
			Addr:         ":9410",
			InternalAddr: ":9412",
			Host:         "example.com",
			WsPort:       80,
			WssPort:      443,
			OrgName:      "Example",
			Auth: config.AuthConfig{
				Mode:               "jwt",
				JWKSURL:            "https://platform.example.com/.well-known/jwks.json",
				Issuer:             "platform.example.com",
				Audience:           "maeve-test",
				MinRefreshInterval: "10m",
				ClockSkew:          "2s",
				MaxTokenLifetime:   "90s",
				Dev: config.AuthDevConfig{
					OrgId:    "0193b2c4-0000-7000-8000-00000000000b",
					OrgScope: "self",
					Perms:    []string{"cpms.station.read", "cpms.token.read"},
				},
			},
		},
		Transport: config.TransportConfig{
			Type: "mqtt",
			Mqtt: &config.MqttSettingsConfig{
				Urls:              []string{"mqtt://127.0.0.1:1883"},
				Prefix:            "cs",
				Group:             "manager",
				ConnectTimeout:    "10s",
				ConnectRetryDelay: "1s",
				KeepAliveInterval: "10s",
			},
		},
		Ocpp: config.OcppSettingsConfig{
			HeartbeatInterval: "10m",
			Ocpp16Enabled:     false,
			Ocpp201Enabled:    true,
		},
		Observability: config.ObservabilitySettingsConfig{
			LogFormat:         "text",
			OtelCollectorAddr: "localhost:4317",
			TlsKeylogFile:     "/keylog/manager.log",
		},
		Storage: config.StorageConfig{
			Type: "firestore",
			FirestoreStorage: &config.FirestoreStorageConfig{
				ProjectId: "*detect-project-id*",
			},
		},
		ContractCertValidator: config.ContractCertValidatorConfig{
			Type: "ocsp",
			Ocsp: &config.OcspContractCertValidatorConfig{
				RootCertProvider: config.RootCertProviderConfig{
					Type: "opcp",
					Opcp: &config.OpcpRootCertProviderConfig{
						Url: "https://open.plugncharge-test.hubject.com",
						Ttl: "24h",
						HttpAuth: config.HttpAuthConfig{
							Type: "env_token",
							EnvToken: &config.EnvHttpTokenConfig{
								EnvVar: "RCP_TOKEN",
							},
						},
					},
				},
				MaxAttempts: 3,
			},
		},
		ContractCertProvider: config.ContractCertProviderConfig{
			Type: "opcp",
			Opcp: &config.OpcpContractCertProviderConfig{
				Url: "https://open.plugncharge-test.hubject.com",
				HttpAuth: config.HttpAuthConfig{
					Type: "env_token",
					EnvToken: &config.EnvHttpTokenConfig{
						EnvVar: "EST_TOKEN",
					},
				},
			},
		},
		ChargeStationCertProvider: config.ChargeStationCertProviderConfig{
			Type: "opcp",
			Opcp: &config.OpcpChargeStationCertProviderConfig{
				Url: "https://open.plugncharge-test.hubject.com",
				HttpAuth: config.HttpAuthConfig{
					Type: "hubject_test_token",
					HubjectTestToken: &config.HubjectTestHttpTokenConfig{
						Url: "https://hubject.stoplight.io/docs/open-plugncharge/6bb8b3bc79c2e-authorization-token",
					},
				},
			},
		},
		TariffService: config.TariffServiceConfig{
			Type: "kwh",
		},
		Jobs: config.JobsConfig{
			ReservationExpiryInterval: "30s",
		},
		Tenancy: config.TenancyConfig{
			DefaultOrgId: "0193b2c4-0000-7000-8000-00000000000a",
			Mode:         "single",
			CacheTTL:     "30s",
		},
		Events: config.EventsConfig{
			Enabled:         false,
			RelayEnabled:    true,
			SubjectPrefix:   "cpms",
			Stream:          "CPMS_EVENTS",
			StreamReplicas:  1,
			StreamMaxAge:    "720h",
			RelayInterval:   "1s",
			RelayBatchSize:  100,
			OutboxRetention: "720h",
			PurgeInterval:   "1h",
		},
		Connectivity: config.ConnectivityConfig{
			LastSeenWriteInterval: "1m",
			StaleAfter:            "10m",
			SweepInterval:         "15s",
		},
		// the default stays and is not used with the MQTT transport
		Nats: &config.NatsConfig{
			Urls: []string{"nats://localhost:4222"},
		},
	}

	assert.Equal(t, want, cfg)

	err = cfg.Validate()
	assert.NoError(t, err)
}

func TestValidateConfig(t *testing.T) {
	cfg := clone.Clone(&config.DefaultConfig)

	// by default the callers of the API are authenticated with tokens of the platform,
	// and there is no default for where its keys are
	assert.Equal(t, "jwt", cfg.Api.Auth.Mode)
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "JWKSURL")

	cfg.Api.Auth.JWKSURL = "https://platform.example.com/.well-known/jwks.json"
	assert.NoError(t, cfg.Validate())
}

// The configurations for local runs must stay configurations that the manager accepts.
// They use the NATS server of docker-compose, as the gateway does.
func TestExampleConfigsAreValid(t *testing.T) {
	for _, file := range []string{"config.toml", "config-inmemory.toml", "config-multitenant.toml"} {
		t.Run(file, func(t *testing.T) {
			cfg := clone.Clone(&config.DefaultConfig)
			require.NoError(t, cfg.LoadFromFile("../../config/manager/"+file))
			require.NoError(t, cfg.Validate())
			assert.Equal(t, "dev", cfg.Api.Auth.Mode)
			assert.Equal(t, "nats", cfg.Transport.Type)
			assert.Equal(t, []string{"nats://nats:4222"}, cfg.Nats.Urls)
			assert.True(t, cfg.Events.RelayEnabled)
			assert.True(t, cfg.Events.EnsureStream, "the manager creates the event stream in development")
		})
	}
}

// The legacy pages have no authentication: they stay off unless the configuration
// switches them on, each with its own setting.
func TestLegacyPagesAreOffUnlessEnabled(t *testing.T) {
	assert.False(t, config.DefaultConfig.Api.EnableTransactionsPage)
	assert.False(t, config.DefaultConfig.Api.EnableAdminUI)

	for name, tc := range map[string]struct {
		toml             string
		wantTransactions bool
		wantAdminUI      bool
	}{
		"not set":            {toml: ""},
		"other api settings": {toml: "[api]\naddr = \":9410\"\ninternal_addr = \":9412\"\n"},
		"transactions page":  {toml: "[api]\nenable_transactions_page = true\n", wantTransactions: true},
		"admin pages":        {toml: "[api]\nenable_admin_ui = true\n", wantAdminUI: true},
		"both":               {toml: "[api]\nenable_transactions_page = true\nenable_admin_ui = true\n", wantTransactions: true, wantAdminUI: true},
		"switched off":       {toml: "[api]\nenable_transactions_page = false\nenable_admin_ui = false\n"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := newTestConfig(t)
			cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
			require.NoError(t, cfg.Load(strings.NewReader(tc.toml)))
			require.NoError(t, cfg.Validate())
			assert.Equal(t, tc.wantTransactions, cfg.Api.EnableTransactionsPage)
			assert.Equal(t, tc.wantAdminUI, cfg.Api.EnableAdminUI)

			// the manager is set up with what the configuration says
			settings, err := config.Configure(context.Background(), cfg)
			require.NoError(t, err)
			assert.Equal(t, tc.wantTransactions, settings.Api.EnableTransactionsPage)
			assert.Equal(t, tc.wantAdminUI, settings.Api.EnableAdminUI)
		})
	}
}

// The schema of the tenant config tells the platform which heartbeat interval a tenant
// has when it sets none: the default of this configuration.
func TestTenantConfigSchemaShowsTheDefaultHeartbeatInterval(t *testing.T) {
	var schema struct {
		Properties struct {
			HeartbeatIntervalS struct {
				Default int `json:"default"`
			} `json:"heartbeat_interval_s"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(tenancy.ConfigSchema(), &schema))

	global, err := time.ParseDuration(config.DefaultConfig.Ocpp.HeartbeatInterval)
	require.NoError(t, err)
	assert.Equal(t, int(global.Seconds()), schema.Properties.HeartbeatIntervalS.Default)
}
