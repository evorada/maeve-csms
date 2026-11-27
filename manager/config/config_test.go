// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	clone "github.com/huandu/go-clone/generic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/config"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func TestConfigure(t *testing.T) {
	cfg := clone.Clone(&config.DefaultConfig)
	cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}

	settings, err := config.Configure(context.TODO(), cfg)
	require.NoError(t, err)

	wantApiSettings := config.ApiSettings{
		Addr:    "localhost:9410",
		Host:    "localhost",
		WsPort:  80,
		WssPort: 443,
		OrgName: "Thoughtworks",
	}

	assert.Equal(t, wantApiSettings, settings.Api)
	assert.NotNil(t, settings.Tracer)
	assert.NotNil(t, settings.TracerProvider)
	assert.NotNil(t, settings.Storage)
	assert.NotNil(t, settings.MsgEmitter)
	assert.NotNil(t, settings.MsgListener)
	assert.NotNil(t, settings.Ocpp16Handler)
	assert.NotNil(t, settings.Ocpp201Handler)
	assert.NotNil(t, settings.ContractCertValidationService)
	assert.NotNil(t, settings.ContractCertProviderService)
	assert.NotNil(t, settings.ChargeStationCertProviderService)
	assert.NotNil(t, settings.TariffService)
}

func TestConfigureFirestoreStorage(t *testing.T) {
	_ = os.Setenv("FIRESTORE_EMULATOR_HOST", "localhost:8080")

	cfg := clone.Clone(&config.DefaultConfig)
	cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
	cfg.Storage.Type = "firestore"
	cfg.Storage.FirestoreStorage = &config.FirestoreStorageConfig{
		ProjectId: "test-project-id",
	}

	settings, err := config.Configure(context.TODO(), cfg)
	require.NoError(t, err)
	require.NotNil(t, settings.Storage)
}

func TestConfigureInMemoryStorage(t *testing.T) {
	cfg := clone.Clone(&config.DefaultConfig)
	cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
	cfg.Storage.Type = "in_memory"

	settings, err := config.Configure(context.TODO(), cfg)
	require.NoError(t, err)
	require.NotNil(t, settings.Storage)
}

func TestConfigureOcspContractCertValidator(t *testing.T) {
	cfg := clone.Clone(&config.DefaultConfig)
	cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}

	settings, err := config.Configure(context.TODO(), cfg)
	require.NoError(t, err)
	require.NotNil(t, settings.ContractCertValidationService)
}

func TestConfigureOpcpContractCertProvider(t *testing.T) {
	_ = os.Setenv("TEST_OPCP_TOKEN", "test-token")
	defer func() {
		_ = os.Unsetenv("TEST_OPCP_TOKEN")
	}()

	cfg := clone.Clone(&config.DefaultConfig)
	cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
	cfg.ContractCertProvider.Type = "opcp"
	cfg.ContractCertProvider.Opcp = &config.OpcpContractCertProviderConfig{
		Url: "http://localhost:8080",
		HttpAuth: config.HttpAuthConfig{
			Type: "env_token",
			EnvToken: &config.EnvHttpTokenConfig{
				EnvVar: "TEST_OPCP_TOKEN",
			},
		},
	}

	settings, err := config.Configure(context.TODO(), cfg)
	require.NoError(t, err)
	require.NotNil(t, settings.ContractCertProviderService)
}

func TestConfigureOcspContractCertProviderWithCompositeRootCertificateProvider(t *testing.T) {
	cfg := clone.Clone(&config.DefaultConfig)
	cfg.ContractCertValidator.Ocsp.RootCertProvider.Type = "composite"
	cfg.ContractCertValidator.Ocsp.RootCertProvider.Composite = &config.CompositeRootCertProviderConfig{
		Providers: []config.RootCertProviderConfig{
			{
				Type: "file",
				File: &config.FileRootCertProviderConfig{
					FileNames: []string{"testdata/root_ca.pem"},
				},
			},
			{
				Type: "file",
				File: &config.FileRootCertProviderConfig{
					FileNames: []string{"testdata/ca.pem"},
				},
			},
		},
	}

	settings, err := config.Configure(context.TODO(), cfg)
	require.NoError(t, err)
	require.NotNil(t, settings.ContractCertValidationService)
}

func TestConfigureDefaultContractCertProvider(t *testing.T) {
	cfg := clone.Clone(&config.DefaultConfig)
	cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
	cfg.ContractCertProvider.Type = "default"

	settings, err := config.Configure(context.TODO(), cfg)
	require.NoError(t, err)
	require.NotNil(t, settings.ContractCertProviderService)
}

func TestConfigureOpcpChargeStationCertProvider(t *testing.T) {
	_ = os.Setenv("TEST_OPCP_TOKEN", "test-token")
	defer func() {
		_ = os.Unsetenv("TEST_OPCP_TOKEN")
	}()

	cfg := clone.Clone(&config.DefaultConfig)
	cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
	cfg.ChargeStationCertProvider.Type = "opcp"
	cfg.ChargeStationCertProvider.Opcp = &config.OpcpChargeStationCertProviderConfig{
		Url: "http://localhost:8080",
		HttpAuth: config.HttpAuthConfig{
			Type: "env_token",
			EnvToken: &config.EnvHttpTokenConfig{
				EnvVar: "TEST_OPCP_TOKEN",
			},
		},
	}

	settings, err := config.Configure(context.TODO(), cfg)
	require.NoError(t, err)
	require.NotNil(t, settings.ChargeStationCertProviderService)
}

func TestConfigureLocalChargeStationCertProviderWithFile(t *testing.T) {
	cfg := clone.Clone(&config.DefaultConfig)
	cfg.ChargeStationCertProvider.Type = "local"
	cfg.ChargeStationCertProvider.Local = &config.LocalChargeStationCertProviderConfig{
		CertificateSource: &config.LocalSourceConfig{
			Type: "file",
			File: "testdata/ca.pem",
		},
		PrivateKeySource: &config.LocalSourceConfig{
			Type: "file",
			File: "testdata/ca.key",
		},
	}

	settings, err := config.Configure(context.TODO(), cfg)
	require.NoError(t, err)
	require.NotNil(t, settings.ChargeStationCertProviderService)
}

func TestConfigureLocalChargeStationCertProviderWithGoogleCloudSecret(t *testing.T) {
	cfg := clone.Clone(&config.DefaultConfig)
	cfg.ChargeStationCertProvider.Type = "local"
	cfg.ChargeStationCertProvider.Local = &config.LocalChargeStationCertProviderConfig{
		CertificateSource: &config.LocalSourceConfig{
			Type:              "google_cloud_secret",
			GoogleCloudSecret: "project/12345678/secrets/certificate/versions/1",
		},
		PrivateKeySource: &config.LocalSourceConfig{
			Type:              "google_cloud_secret",
			GoogleCloudSecret: "project/12345678/secrets/privatekey/versions/1",
		},
	}

	settings, err := config.Configure(context.TODO(), cfg)
	require.NoError(t, err)
	require.NotNil(t, settings.ChargeStationCertProviderService)
}

func TestConfigureDelegatingChargeStationCertProvider(t *testing.T) {
	_ = os.Setenv("TEST_OPCP_TOKEN", "test-token")
	defer func() {
		_ = os.Unsetenv("TEST_OPCP_TOKEN")
	}()

	cfg := clone.Clone(&config.DefaultConfig)
	cfg.ChargeStationCertProvider.Type = "delegating"
	cfg.ChargeStationCertProvider.Delegating = &config.DelegatingChargeStationCertProviderConfig{
		V2G: &config.ChargeStationCertProviderConfig{
			Type: "opcp",
			Opcp: &config.OpcpChargeStationCertProviderConfig{
				Url: "http://localhost:8080",
				HttpAuth: config.HttpAuthConfig{
					Type: "env_token",
					EnvToken: &config.EnvHttpTokenConfig{
						EnvVar: "TEST_OPCP_TOKEN",
					},
				},
			},
		},
		CSO: &config.ChargeStationCertProviderConfig{
			Type: "local",
			Local: &config.LocalChargeStationCertProviderConfig{
				CertificateSource: &config.LocalSourceConfig{
					Type: "file",
					File: "testdata/ca.pem",
				},
				PrivateKeySource: &config.LocalSourceConfig{
					Type: "file",
					File: "testdata/ca.key",
				},
			},
		},
	}

	settings, err := config.Configure(context.TODO(), cfg)
	require.NoError(t, err)
	require.NotNil(t, settings.ChargeStationCertProviderService)
}

func TestConfigureDefaultChargeStationCertProvider(t *testing.T) {
	cfg := clone.Clone(&config.DefaultConfig)
	cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
	cfg.ChargeStationCertProvider.Type = "default"

	settings, err := config.Configure(context.TODO(), cfg)
	require.NoError(t, err)
	require.NotNil(t, settings.ChargeStationCertProviderService)
}

func TestConfigureKwHTariffService(t *testing.T) {
	cfg := clone.Clone(&config.DefaultConfig)
	cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
	cfg.TariffService.Type = "kwh"

	settings, err := config.Configure(context.TODO(), cfg)
	require.NoError(t, err)
	require.NotNil(t, settings.ContractCertProviderService)
}

func TestConfigureReservationExpiryInterval(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		cfg := clone.Clone(&config.DefaultConfig)
		cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}

		settings, err := config.Configure(context.TODO(), cfg)
		require.NoError(t, err)
		assert.Equal(t, time.Minute, settings.ReservationExpiryInterval)
		assert.IsType(t, &jobs.LocalRunner{}, settings.JobRunner)
	})

	t.Run("overridden", func(t *testing.T) {
		cfg := clone.Clone(&config.DefaultConfig)
		cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
		err := cfg.Load(strings.NewReader("[jobs]\nreservation_expiry_interval = \"90s\"\n"))
		require.NoError(t, err)

		settings, err := config.Configure(context.TODO(), cfg)
		require.NoError(t, err)
		assert.Equal(t, 90*time.Second, settings.ReservationExpiryInterval)
	})

	for name, interval := range map[string]string{
		"not a duration": "every minute",
		"no unit":        "60",
		"zero":           "0s",
		"negative":       "-1m",
		"empty":          "",
	} {
		t.Run(name, func(t *testing.T) {
			cfg := clone.Clone(&config.DefaultConfig)
			cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
			cfg.Jobs.ReservationExpiryInterval = interval

			settings, err := config.Configure(context.TODO(), cfg)
			require.Error(t, err)
			assert.Nil(t, settings)
		})
	}
}

func TestConfigureDefaultOrgId(t *testing.T) {
	t.Run("default is the legacy org", func(t *testing.T) {
		cfg := clone.Clone(&config.DefaultConfig)
		cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}

		settings, err := config.Configure(context.TODO(), cfg)
		require.NoError(t, err)
		assert.Equal(t, "00000000-0000-0000-0000-000000000000", settings.DefaultOrgId)
		assert.Equal(t, store.LegacyOrgId, settings.DefaultOrgId)
	})

	t.Run("overridden", func(t *testing.T) {
		cfg := clone.Clone(&config.DefaultConfig)
		cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
		err := cfg.Load(strings.NewReader("[tenancy]\ndefault_org_id = \"0193b2c4-0000-7000-8000-00000000000a\"\n"))
		require.NoError(t, err)

		settings, err := config.Configure(context.TODO(), cfg)
		require.NoError(t, err)
		assert.Equal(t, "0193b2c4-0000-7000-8000-00000000000a", settings.DefaultOrgId)
	})

	for name, orgId := range map[string]string{
		"not a uuid": "evorada",
		"too short":  "0193b2c4-0000-7000-8000",
		"empty":      "",
	} {
		t.Run(name, func(t *testing.T) {
			cfg := clone.Clone(&config.DefaultConfig)
			cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
			cfg.Tenancy.DefaultOrgId = orgId

			require.Error(t, cfg.Validate())

			settings, err := config.Configure(context.TODO(), cfg)
			require.Error(t, err)
			assert.Nil(t, settings)
		})
	}
}
