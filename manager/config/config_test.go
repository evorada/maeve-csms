// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	clone "github.com/huandu/go-clone/generic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/config"
	"github.com/thoughtworks/maeve-csms/manager/entitlements"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
)

// newTestConfig returns the default configuration with API authentication in dev mode,
// so that it can be configured without a platform.
func newTestConfig(t *testing.T) *config.BaseConfig {
	t.Helper()

	t.Setenv("ENVIRONMENT", "dev")
	cfg := clone.Clone(&config.DefaultConfig)
	cfg.Api.Auth.Mode = "dev"
	return cfg
}

func TestConfigure(t *testing.T) {
	cfg := newTestConfig(t)
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
	assert.NotNil(t, settings.AuthVerifier)
}

func TestConfigureFirestoreStorage(t *testing.T) {
	_ = os.Setenv("FIRESTORE_EMULATOR_HOST", "localhost:8080")

	cfg := newTestConfig(t)
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
	cfg := newTestConfig(t)
	cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
	cfg.Storage.Type = "in_memory"

	settings, err := config.Configure(context.TODO(), cfg)
	require.NoError(t, err)
	require.NotNil(t, settings.Storage)
}

func TestConfigureOcspContractCertValidator(t *testing.T) {
	cfg := newTestConfig(t)
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

	cfg := newTestConfig(t)
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
	cfg := newTestConfig(t)
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
	cfg := newTestConfig(t)
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

	cfg := newTestConfig(t)
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
	cfg := newTestConfig(t)
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
	cfg := newTestConfig(t)
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

	cfg := newTestConfig(t)
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
	cfg := newTestConfig(t)
	cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
	cfg.ChargeStationCertProvider.Type = "default"

	settings, err := config.Configure(context.TODO(), cfg)
	require.NoError(t, err)
	require.NotNil(t, settings.ChargeStationCertProviderService)
}

func TestConfigureKwHTariffService(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
	cfg.TariffService.Type = "kwh"

	settings, err := config.Configure(context.TODO(), cfg)
	require.NoError(t, err)
	require.NotNil(t, settings.ContractCertProviderService)
}

func TestConfigureReservationExpiryInterval(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		cfg := newTestConfig(t)
		cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}

		settings, err := config.Configure(context.TODO(), cfg)
		require.NoError(t, err)
		assert.Equal(t, time.Minute, settings.ReservationExpiryInterval)
		assert.IsType(t, &jobs.LocalRunner{}, settings.JobRunner)
	})

	t.Run("overridden", func(t *testing.T) {
		cfg := newTestConfig(t)
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
			cfg := newTestConfig(t)
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
		cfg := newTestConfig(t)
		cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}

		settings, err := config.Configure(context.TODO(), cfg)
		require.NoError(t, err)
		assert.Equal(t, "00000000-0000-0000-0000-000000000000", settings.DefaultOrgId)
		assert.Equal(t, store.LegacyOrgId, settings.DefaultOrgId)

		// a charge station without an org belongs to the default org
		require.NotNil(t, settings.TenantResolver)
		orgId, err := settings.TenantResolver.OrgIdForStation(context.TODO(), "cs-unknown")
		require.NoError(t, err)
		assert.Equal(t, store.LegacyOrgId, orgId)
	})

	t.Run("overridden", func(t *testing.T) {
		cfg := newTestConfig(t)
		cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
		err := cfg.Load(strings.NewReader("[tenancy]\ndefault_org_id = \"0193b2c4-0000-7000-8000-00000000000a\"\n"))
		require.NoError(t, err)

		settings, err := config.Configure(context.TODO(), cfg)
		require.NoError(t, err)
		assert.Equal(t, "0193b2c4-0000-7000-8000-00000000000a", settings.DefaultOrgId)

		// the resolver reads the charge stations of the configured storage
		require.NotNil(t, settings.TenantResolver)
		require.NoError(t, settings.Storage.SetChargeStationAuth(context.TODO(), "cs-with-org", &store.ChargeStationAuth{
			OrgId: "0193b2c4-0000-7000-8000-0000000000b2",
		}))
		orgId, err := settings.TenantResolver.OrgIdForStation(context.TODO(), "cs-with-org")
		require.NoError(t, err)
		assert.Equal(t, "0193b2c4-0000-7000-8000-0000000000b2", orgId)
		orgId, err = settings.TenantResolver.OrgIdForStation(context.TODO(), "cs-unknown")
		require.NoError(t, err)
		assert.Equal(t, "0193b2c4-0000-7000-8000-00000000000a", orgId)
	})

	for name, orgId := range map[string]string{
		"not a uuid": "acme",
		"too short":  "0193b2c4-0000-7000-8000",
		"empty":      "",
	} {
		t.Run(name, func(t *testing.T) {
			cfg := newTestConfig(t)
			cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
			cfg.Tenancy.DefaultOrgId = orgId

			require.Error(t, cfg.Validate())

			settings, err := config.Configure(context.TODO(), cfg)
			require.Error(t, err)
			assert.Nil(t, settings)
		})
	}
}

const multiTenantConfig = `
[tenancy]
mode = "multi"
cache_ttl = "45s"

[[tenancy.bootstrap_tenants]]
org_id = "0193b2c4-0000-7000-8000-0000000000a1"
slug = "dev1"
org_key = "dev1"
hostnames = ["localhost"]

[[tenancy.bootstrap_tenants]]
org_id = "0193b2c4-0000-7000-8000-0000000000b2"
slug = "dev2"
org_key = "dev2"
hostnames = ["127.0.0.1", "dev2.example"]
`

func TestConfigureTenancyMode(t *testing.T) {
	newConfig := func(t *testing.T, toml string) *config.BaseConfig {
		cfg := newTestConfig(t)
		cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
		require.NoError(t, cfg.Load(strings.NewReader(toml)))
		return cfg
	}

	t.Run("default is single: the host is ignored and the key is the station id", func(t *testing.T) {
		cfg := newConfig(t, "")
		assert.Equal(t, "single", cfg.Tenancy.Mode)
		assert.Equal(t, "30s", cfg.Tenancy.CacheTTL)
		assert.Equal(t, "localhost:9412", cfg.Api.InternalAddr)

		settings, err := config.Configure(context.TODO(), cfg)
		require.NoError(t, err)
		assert.Equal(t, tenancy.ModeSingle, settings.Tenancy)
		require.NotNil(t, settings.TenantResolver)
		assert.Equal(t, tenancy.ModeSingle, settings.TenantResolver.Mode)

		// in single mode a caller of the default org reaches the default org, another
		// caller nothing
		require.NotNil(t, settings.TenantScoper)
		assert.Same(t, settings.TenantResolver, settings.TenantScoper.Resolver)
		orgIds, err := settings.TenantScoper.OrgIds(context.TODO(), &tenancy.Principal{OrgId: store.LegacyOrgId, OrgScope: tenancy.OrgScopeSubtree})
		require.NoError(t, err)
		assert.Equal(t, []string{store.LegacyOrgId}, orgIds)
		orgIds, err = settings.TenantScoper.OrgIds(context.TODO(), &tenancy.Principal{OrgId: "0193b2c4-0000-7000-8000-0000000000a1", OrgScope: tenancy.OrgScopeSubtree})
		require.NoError(t, err)
		assert.Empty(t, orgIds)

		res, err := settings.TenantResolver.ResolveStation(context.TODO(), "unknown.example", "CP001")
		require.NoError(t, err)
		assert.Equal(t, &tenancy.StationResolution{OrgId: store.LegacyOrgId, StationKey: "CP001"}, res)

		tenants, err := settings.Storage.ListTenants(context.TODO(), "", 10)
		require.NoError(t, err)
		assert.Empty(t, tenants)

		// nothing is limited in single mode
		require.NotNil(t, settings.Entitlements)
		assert.Equal(t, tenancy.ModeSingle, settings.Entitlements.Mode)
	})

	t.Run("multi: the bootstrap tenants are parsed and stored, and their hosts resolve", func(t *testing.T) {
		cfg := newConfig(t, multiTenantConfig)
		assert.Equal(t, []config.BootstrapTenantConfig{
			{OrgId: "0193b2c4-0000-7000-8000-0000000000a1", Slug: "dev1", OrgKey: "dev1", Hostnames: []string{"localhost"}},
			{OrgId: "0193b2c4-0000-7000-8000-0000000000b2", Slug: "dev2", OrgKey: "dev2", Hostnames: []string{"127.0.0.1", "dev2.example"}},
		}, cfg.Tenancy.BootstrapTenants)
		// the default org is not changed by the other keys of the section
		assert.Equal(t, store.LegacyOrgId, cfg.Tenancy.DefaultOrgId)

		settings, err := config.Configure(context.TODO(), cfg)
		require.NoError(t, err)
		assert.Equal(t, tenancy.ModeMulti, settings.Tenancy)
		require.NotNil(t, settings.TenantResolver)
		assert.Equal(t, tenancy.ModeMulti, settings.TenantResolver.Mode)
		assert.Equal(t, 45*time.Second, settings.TenantResolver.CacheTTL)

		tenants, err := settings.Storage.ListTenants(context.TODO(), "", 10)
		require.NoError(t, err)
		require.Len(t, tenants, 2)

		// the scoper asks the tenants of the storage: a caller with the scope subtree
		// reaches the tenant that its org is, a caller of another org nothing
		require.NotNil(t, settings.TenantScoper)
		assert.Equal(t, tenancy.ModeMulti, settings.TenantScoper.Mode)
		assert.Same(t, settings.TenantResolver, settings.TenantScoper.Resolver)
		orgIds, err := settings.TenantScoper.OrgIds(context.TODO(), &tenancy.Principal{OrgId: tenants[0].OrgId, OrgScope: tenancy.OrgScopeSubtree})
		require.NoError(t, err)
		assert.Equal(t, []string{tenants[0].OrgId}, orgIds)
		orgIds, err = settings.TenantScoper.OrgIds(context.TODO(), &tenancy.Principal{OrgId: store.LegacyOrgId, OrgScope: tenancy.OrgScopeSubtree})
		require.NoError(t, err)
		assert.Empty(t, orgIds)
		assert.Equal(t, "dev1", tenants[0].OrgKey)
		assert.Equal(t, store.OrgPathLabel("0193b2c4-0000-7000-8000-0000000000a1"), tenants[0].Path)
		assert.Equal(t, []string{"127.0.0.1", "dev2.example"}, tenants[1].Hostnames)

		res, err := settings.TenantResolver.ResolveStation(context.TODO(), "localhost:9310", "CP001")
		require.NoError(t, err)
		assert.Equal(t, "dev1.CP001", res.StationKey)
		assert.Equal(t, "0193b2c4-0000-7000-8000-0000000000a1", res.OrgId)
		res, err = settings.TenantResolver.ResolveStation(context.TODO(), "127.0.0.1:9310", "CP001")
		require.NoError(t, err)
		assert.Equal(t, "dev2.CP001", res.StationKey)
		_, err = settings.TenantResolver.ResolveStation(context.TODO(), "gateway:9310", "CP001")
		assert.ErrorIs(t, err, tenancy.ErrUnknownHost)

		// a station without an org belongs to the tenant of its key
		orgId, err := settings.TenantResolver.OrgIdForStation(context.TODO(), "dev2.CP001")
		require.NoError(t, err)
		assert.Equal(t, "0193b2c4-0000-7000-8000-0000000000b2", orgId)

		// the limits are the ones of the tenants of the storage, counted in the storage
		require.NotNil(t, settings.Entitlements)
		assert.Equal(t, tenancy.ModeMulti, settings.Entitlements.Mode)
		require.NoError(t, settings.Entitlements.CheckConnectors(context.TODO(), tenants[0].OrgId, 1))
		limited := *tenants[0]
		limited.EntitlementsVersion = 1
		limited.Entitlements = json.RawMessage(`{"version":1,"limits":{"cpms.connectors.max":{"value":1,"enforcement":"hard","scope":"org"}}}`)
		require.NoError(t, settings.Storage.PutTenant(context.TODO(), &limited))
		require.NoError(t, settings.Entitlements.CheckConnectors(context.TODO(), tenants[0].OrgId, 1))
		require.NoError(t, settings.Storage.SetChargeStationAuth(context.TODO(), "dev1.CP001", &store.ChargeStationAuth{OrgId: tenants[0].OrgId}))
		require.NoError(t, settings.Storage.SetConnectorStatus(context.TODO(), "dev1.CP001", 1, &store.ConnectorStatus{
			ChargeStationId: "dev1.CP001", ConnectorId: 1, Status: store.ConnectorStatusAvailable,
		}))
		var exceeded *entitlements.LimitExceededError
		require.ErrorAs(t, settings.Entitlements.CheckConnectors(context.TODO(), tenants[0].OrgId, 1), &exceeded)
		assert.Equal(t, int64(1), exceeded.Current)
	})

	t.Run("multi without bootstrap tenants starts with no tenant", func(t *testing.T) {
		settings, err := config.Configure(context.TODO(), newConfig(t, "[tenancy]\nmode = \"multi\"\n"))
		require.NoError(t, err)
		_, err = settings.TenantResolver.ResolveStation(context.TODO(), "localhost", "CP001")
		assert.ErrorIs(t, err, tenancy.ErrUnknownHost)
	})

	t.Run("multi with firestore storage fails", func(t *testing.T) {
		t.Setenv("FIRESTORE_EMULATOR_HOST", "localhost:8080")
		cfg := newConfig(t, multiTenantConfig)
		cfg.Storage.Type = "firestore"
		cfg.Storage.FirestoreStorage = &config.FirestoreStorageConfig{ProjectId: "test-project-id"}

		settings, err := config.Configure(context.TODO(), cfg)
		require.Error(t, err)
		assert.ErrorContains(t, err, `tenancy mode "multi" is not supported by the firestore storage`)
		assert.Nil(t, settings)
	})

	t.Run("bootstrap tenants in single mode fail", func(t *testing.T) {
		cfg := newConfig(t, multiTenantConfig)
		cfg.Tenancy.Mode = "single"

		settings, err := config.Configure(context.TODO(), cfg)
		require.Error(t, err)
		assert.ErrorContains(t, err, "bootstrap tenants")
		assert.Nil(t, settings)
	})

	invalid := map[string]func(cfg *config.BaseConfig){
		"unknown mode":                     func(cfg *config.BaseConfig) { cfg.Tenancy.Mode = "shared" },
		"empty mode":                       func(cfg *config.BaseConfig) { cfg.Tenancy.Mode = "" },
		"mode in upper case":               func(cfg *config.BaseConfig) { cfg.Tenancy.Mode = "Multi" },
		"cache ttl that is not a duration": func(cfg *config.BaseConfig) { cfg.Tenancy.CacheTTL = "soon" },
		"cache ttl of zero":                func(cfg *config.BaseConfig) { cfg.Tenancy.CacheTTL = "0s" },
		"negative cache ttl":               func(cfg *config.BaseConfig) { cfg.Tenancy.CacheTTL = "-30s" },
		"empty cache ttl":                  func(cfg *config.BaseConfig) { cfg.Tenancy.CacheTTL = "" },
		"bootstrap tenant without org id":  func(cfg *config.BaseConfig) { cfg.Tenancy.BootstrapTenants[0].OrgId = "" },
		"bootstrap tenant org id no uuid":  func(cfg *config.BaseConfig) { cfg.Tenancy.BootstrapTenants[1].OrgId = "dev2" },
		"bootstrap tenant bad org key":     func(cfg *config.BaseConfig) { cfg.Tenancy.BootstrapTenants[1].OrgKey = "DEV2" },
		"bootstrap tenant without org key": func(cfg *config.BaseConfig) { cfg.Tenancy.BootstrapTenants[1].OrgKey = "" },
		"bootstrap tenant bad slug":        func(cfg *config.BaseConfig) { cfg.Tenancy.BootstrapTenants[0].Slug = "Dev 1" },
		"bootstrap tenant bad hostname":    func(cfg *config.BaseConfig) { cfg.Tenancy.BootstrapTenants[0].Hostnames = []string{"localhost:9310"} },
		"bootstrap tenants with one host": func(cfg *config.BaseConfig) {
			cfg.Tenancy.BootstrapTenants[1].Hostnames = []string{"localhost"}
		},
		"bootstrap tenants with one org key": func(cfg *config.BaseConfig) { cfg.Tenancy.BootstrapTenants[1].OrgKey = "dev1" },
		"empty internal address":             func(cfg *config.BaseConfig) { cfg.Api.InternalAddr = "" },
	}
	for name, change := range invalid {
		t.Run(name, func(t *testing.T) {
			cfg := newConfig(t, multiTenantConfig)
			change(cfg)

			settings, err := config.Configure(context.TODO(), cfg)
			require.Error(t, err)
			assert.Nil(t, settings)
		})
	}
}

func TestConfigureApiAuth(t *testing.T) {
	// the default configuration with the given TOML on top, without the dev mode of
	// the other tests
	newConfig := func(t *testing.T, toml string) *config.BaseConfig {
		cfg := clone.Clone(&config.DefaultConfig)
		cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
		require.NoError(t, cfg.Load(strings.NewReader(toml)))
		return cfg
	}

	t.Run("the default is to verify tokens, which needs the key set of the platform", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "dev")
		cfg := newConfig(t, "")
		assert.Equal(t, "jwt", cfg.Api.Auth.Mode)
		assert.Equal(t, "platform", cfg.Api.Auth.Issuer)
		assert.Equal(t, "maeve", cfg.Api.Auth.Audience)
		assert.Equal(t, "5m", cfg.Api.Auth.MinRefreshInterval)
		assert.Equal(t, "5s", cfg.Api.Auth.ClockSkew)
		assert.Equal(t, "120s", cfg.Api.Auth.MaxTokenLifetime)

		err := cfg.Validate()
		require.Error(t, err)
		assert.ErrorContains(t, err, "JWKSURL")

		settings, err := config.Configure(context.TODO(), cfg)
		require.Error(t, err)
		assert.Nil(t, settings)
	})

	t.Run("jwt mode without a key set URL", func(t *testing.T) {
		cfg := newConfig(t, "[api.auth]\nmode = \"jwt\"\n")
		err := cfg.Validate()
		require.Error(t, err)
		assert.ErrorContains(t, err, "JWKSURL")
	})

	t.Run("what is not valid", func(t *testing.T) {
		for name, toml := range map[string]string{
			"unknown mode":          "[api.auth]\nmode = \"none\"\n",
			"empty mode":            "[api.auth]\nmode = \"\"\njwks_url = \"https://platform.example.com/jwks.json\"\n",
			"key set URL is no URL": "[api.auth]\njwks_url = \"platform\"\n",
			"key set in a file":     "[api.auth]\njwks_url = \"file:///etc/jwks.json\"\n",
			"no issuer":             "[api.auth]\njwks_url = \"https://platform.example.com/jwks.json\"\nissuer = \"\"\n",
			"no audience":           "[api.auth]\njwks_url = \"https://platform.example.com/jwks.json\"\naudience = \"\"\n",
			"dev org is not a UUID": "[api.auth]\nmode = \"dev\"\n[api.auth.dev]\norg_id = \"acme\"\n",
			"unknown dev org scope": "[api.auth]\nmode = \"dev\"\n[api.auth.dev]\norg_scope = \"all\"\n",
			"refresh interval typo": "[api.auth]\njwks_url = \"http://127.0.0.1:1/jwks.json\"\nmin_refresh_interval = \"soon\"\n",
			"no refresh interval":   "[api.auth]\njwks_url = \"http://127.0.0.1:1/jwks.json\"\nmin_refresh_interval = \"0s\"\n",
			"negative clock skew":   "[api.auth]\njwks_url = \"http://127.0.0.1:1/jwks.json\"\nclock_skew = \"-1s\"\n",
			"clock skew typo":       "[api.auth]\njwks_url = \"http://127.0.0.1:1/jwks.json\"\nclock_skew = \"5\"\n",
			"no token lifetime":     "[api.auth]\njwks_url = \"http://127.0.0.1:1/jwks.json\"\nmax_token_lifetime = \"0s\"\n",
			"token lifetime typo":   "[api.auth]\njwks_url = \"http://127.0.0.1:1/jwks.json\"\nmax_token_lifetime = \"two minutes\"\n",
			"user name in the URL":  "[api.auth]\njwks_url = \"http://manager@127.0.0.1:1/jwks.json\"\n",
		} {
			t.Run(name, func(t *testing.T) {
				t.Setenv("ENVIRONMENT", "dev")
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				settings, err := config.Configure(ctx, newConfig(t, toml))
				require.Error(t, err)
				assert.Nil(t, settings)
			})
		}
	})

	t.Run("dev mode needs ENVIRONMENT=dev", func(t *testing.T) {
		for _, environment := range []string{"", "production", "development", "DEV"} {
			t.Setenv("ENVIRONMENT", environment)
			cfg := newConfig(t, "[api.auth]\nmode = \"dev\"\n")
			require.NoError(t, cfg.Validate())

			settings, err := config.Configure(context.TODO(), cfg)
			require.Error(t, err, environment)
			assert.ErrorContains(t, err, "ENVIRONMENT")
			assert.Nil(t, settings)
		}
	})

	t.Run("dev mode: the caller of every request by default", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "dev")
		settings, err := config.Configure(context.TODO(), newConfig(t, "[api.auth]\nmode = \"dev\"\n"))
		require.NoError(t, err)

		verifier, ok := settings.AuthVerifier.(tenancy.DevVerifier)
		require.Truef(t, ok, "got a %T", settings.AuthVerifier)
		assert.Equal(t, tenancy.Principal{
			Subject:  "user:dev",
			OrgId:    store.LegacyOrgId,
			OrgScope: tenancy.OrgScopeSubtree,
			Perms:    tenancy.AllPermissions,
		}, verifier.Principal)

		p, err := settings.AuthVerifier.Verify(context.TODO(), "")
		require.NoError(t, err)
		assert.Equal(t, store.LegacyOrgId, p.OrgId)
		assert.True(t, p.Has(tenancy.PermStationWrite))
		assert.False(t, p.IsService())
	})

	t.Run("dev mode: the org of the caller is the default org", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "dev")
		settings, err := config.Configure(context.TODO(), newConfig(t,
			"[api.auth]\nmode = \"dev\"\n[tenancy]\ndefault_org_id = \"0193b2c4-0000-7000-8000-00000000000a\"\n"))
		require.NoError(t, err)

		p, err := settings.AuthVerifier.Verify(context.TODO(), "")
		require.NoError(t, err)
		assert.Equal(t, "0193b2c4-0000-7000-8000-00000000000a", p.OrgId)
	})

	t.Run("dev mode: a configured caller", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "dev")
		settings, err := config.Configure(context.TODO(), newConfig(t, `
[api.auth]
mode = "dev"

[api.auth.dev]
org_id = "0193b2c4-0000-7000-8000-00000000000b"
org_scope = "self"
perms = ["cpms.station.read"]
`))
		require.NoError(t, err)

		p, err := settings.AuthVerifier.Verify(context.TODO(), "")
		require.NoError(t, err)
		assert.Equal(t, &tenancy.Principal{
			Subject:  "user:dev",
			OrgId:    "0193b2c4-0000-7000-8000-00000000000b",
			OrgScope: tenancy.OrgScopeSelf,
			Perms:    []string{tenancy.PermStationRead},
		}, p)
	})

	t.Run("dev mode: a caller without permissions", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "dev")
		settings, err := config.Configure(context.TODO(), newConfig(t,
			"[api.auth]\nmode = \"dev\"\n[api.auth.dev]\nperms = []\n"))
		require.NoError(t, err)

		p, err := settings.AuthVerifier.Verify(context.TODO(), "")
		require.NoError(t, err)
		assert.Empty(t, p.Perms)
	})

	t.Run("jwt mode verifies the tokens of the platform", func(t *testing.T) {
		// ENVIRONMENT=dev does not turn the verification off
		t.Setenv("ENVIRONMENT", "dev")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		platform := testutil.NewPlatform(t)

		settings, err := config.Configure(ctx, newConfig(t, "[api.auth]\njwks_url = \""+platform.JWKSURL()+"\"\n"))
		require.NoError(t, err)
		require.IsType(t, &tenancy.JWKSVerifier{}, settings.AuthVerifier)

		p, err := settings.AuthVerifier.Verify(ctx, platform.Token(time.Now()))
		require.NoError(t, err)
		assert.Equal(t, "user:alice", p.Subject)

		_, err = settings.AuthVerifier.Verify(ctx, "")
		assert.Equal(t, tenancy.ErrMissingToken, err)

		expired := platform.Claims(time.Now().Add(-10 * time.Minute))
		_, err = settings.AuthVerifier.Verify(ctx, platform.Sign(expired))
		assert.ErrorIs(t, err, tenancy.ErrInvalidToken)

		// a lifetime that the platform does not use: longer than max_token_lifetime
		longLived := platform.Claims(time.Now())
		longLived["exp"] = time.Now().Add(121 * time.Second).Unix()
		_, err = settings.AuthVerifier.Verify(ctx, platform.Sign(longLived))
		assert.ErrorIs(t, err, tenancy.ErrInvalidToken)
	})

	t.Run("jwt mode pins the configured issuer and audience", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		platform := testutil.NewPlatform(t)

		for _, pin := range []string{"issuer = \"platform2\"", "audience = \"maeve2\""} {
			settings, err := config.Configure(ctx, newConfig(t, "[api.auth]\njwks_url = \""+platform.JWKSURL()+"\"\n"+pin+"\n"))
			require.NoError(t, err)

			_, err = settings.AuthVerifier.Verify(ctx, platform.Token(time.Now()))
			assert.ErrorIs(t, err, tenancy.ErrInvalidToken, pin)
		}
	})

	t.Run("jwt mode starts without the platform", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		platform := testutil.NewPlatform(t)
		platform.SetDown(true)

		settings, err := config.Configure(ctx, newConfig(t, "[api.auth]\njwks_url = \""+platform.JWKSURL()+"\"\n"))
		require.NoError(t, err)

		_, err = settings.AuthVerifier.Verify(ctx, platform.Token(time.Now()))
		assert.Equal(t, tenancy.ErrKeysUnavailable, err)
	})
}
