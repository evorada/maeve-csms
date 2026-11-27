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
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/config"
	"github.com/thoughtworks/maeve-csms/manager/entitlements"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"github.com/thoughtworks/maeve-csms/manager/transport/mqtt"
	natstransport "github.com/thoughtworks/maeve-csms/manager/transport/nats"
	"github.com/thoughtworks/maeve-csms/manager/transport/nats/natstest"
)

// newTestConfig returns the default configuration with API authentication in dev mode,
// so that it can be configured without a platform, with the MQTT transport, without the
// relay of the domain events, with the local job runner and without a [nats] section:
// nothing opens a connection in Configure, so the tests of the other settings need no
// NATS server. TestConfigureNatsTransport covers the default transport,
// TestConfigureEventRelay the relay, TestConfigureJobRunner the leased job runner.
func newTestConfig(t *testing.T) *config.BaseConfig {
	t.Helper()

	cfg := newDefaultTestConfig(t)
	cfg.Transport.Type = "mqtt"
	cfg.Events.RelayEnabled = false
	cfg.Jobs.Runner = "local"
	cfg.Nats = nil
	return cfg
}

// newDefaultTestConfig returns the default configuration with API authentication in
// dev mode.
func newDefaultTestConfig(t *testing.T) *config.BaseConfig {
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
	assert.NotNil(t, settings.Events)
	assert.Nil(t, settings.EventRelay)
	assert.Equal(t, &config.OutboxPurgeSettings{Retention: 720 * time.Hour, Interval: time.Hour}, settings.OutboxPurge)
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
	// Firestore has no outbox: events are off although they are enabled
	assert.True(t, cfg.Events.Enabled)
	assert.Equal(t, events.NopEmitter{}, settings.Events)
	// nor does it track connectivity: the manager starts without
	assert.Nil(t, settings.Connectivity)
	// nothing to relay or purge
	assert.Nil(t, settings.EventRelay)
	assert.Nil(t, settings.OutboxPurge)
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

func TestConfigureJobRunner(t *testing.T) {
	// MQTT transport and no relay, so that only the job runner can need NATS
	newConfig := func(t *testing.T, toml string) *config.BaseConfig {
		cfg := newDefaultTestConfig(t)
		cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
		cfg.Transport.Type = "mqtt"
		cfg.Events.RelayEnabled = false
		require.NoError(t, cfg.Load(strings.NewReader(toml)))
		return cfg
	}
	configure := func(t *testing.T, cfg *config.BaseConfig) *config.Config {
		settings, err := config.Configure(context.TODO(), cfg)
		require.NoError(t, err)
		if settings.NatsConn != nil {
			t.Cleanup(settings.NatsConn.Close)
		}
		return settings
	}
	// runJob runs a job on the runner until it has run once.
	runJob := func(t *testing.T, runner jobs.Runner) {
		ran := make(chan struct{}, 1)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- runner.Run(ctx, []jobs.Job{{Name: "test", Interval: time.Millisecond, Run: func(context.Context) error {
				select {
				case ran <- struct{}{}:
				default:
				}
				return nil
			}}})
		}()
		select {
		case <-ran:
		case <-time.After(10 * time.Second):
			t.Fatal("the job did not run")
		}
		cancel()
		require.NoError(t, <-done)
	}

	t.Run("defaults", func(t *testing.T) {
		cfg := newDefaultTestConfig(t)
		assert.Equal(t, config.JobsConfig{
			Runner:                    "nats_kv",
			LeaseBucket:               "maeve_job_leases",
			LeaseTTL:                  "15s",
			EnsureBucket:              false,
			BucketReplicas:            1,
			SyncInterval:              "1m",
			SyncRetryAfter:            "2m",
			ReservationExpiryInterval: "1m",
		}, cfg.Jobs)
	})

	t.Run("the leased runner needs the nats section", func(t *testing.T) {
		for name, toml := range map[string]string{"default": "", "nats_kv": "[jobs]\nrunner = \"nats_kv\"\n"} {
			t.Run(name, func(t *testing.T) {
				cfg := newConfig(t, toml)
				cfg.Nats = nil
				err := cfg.Validate()
				require.Error(t, err)
				assert.ErrorContains(t, err, "[nats] section is required to run the background jobs")

				settings, err := config.Configure(context.TODO(), cfg)
				require.Error(t, err)
				assert.Nil(t, settings)
			})
		}
	})

	t.Run("local builds the local runner without nats", func(t *testing.T) {
		cfg := newConfig(t, "[jobs]\nrunner = \"local\"\nsync_interval = \"30s\"\nsync_retry_after = \"10m\"\n")
		cfg.Nats = nil
		require.NoError(t, cfg.Validate())

		settings := configure(t, cfg)
		assert.IsType(t, &jobs.LocalRunner{}, settings.JobRunner)
		assert.Nil(t, settings.NatsConn)
		assert.Equal(t, 30*time.Second, settings.SyncInterval)
		assert.Equal(t, 10*time.Minute, settings.SyncRetryAfter)
		runJob(t, settings.JobRunner)
	})

	t.Run("nats_kv with all settings creates the bucket", func(t *testing.T) {
		_, url := natstest.RunServer(t, true)
		cfg := newConfig(t, `
[nats]
urls = ["`+url+`"]

[jobs]
runner = "nats_kv"
lease_bucket = "test_leases"
lease_ttl = "9s"
ensure_bucket = true
bucket_replicas = 1
`)
		settings := configure(t, cfg)
		require.NotNil(t, settings.NatsConn, "the job runner opens the connection to NATS with the MQTT transport")
		require.IsType(t, &jobs.LeasedRunner{}, settings.JobRunner)
		runner := settings.JobRunner.(*jobs.LeasedRunner)
		assert.Equal(t, 9*time.Second, runner.TTL)
		require.IsType(t, &jobs.KVLease{}, runner.Lease)
		assert.Regexp(t, `-[0-9a-f]{6}$`, runner.Lease.(*jobs.KVLease).Instance())
		assert.Equal(t, time.Minute, settings.SyncInterval)
		assert.Equal(t, 2*time.Minute, settings.SyncRetryAfter)

		conn, err := nats.Connect(url)
		require.NoError(t, err)
		t.Cleanup(conn.Close)
		js, err := jetstream.New(conn)
		require.NoError(t, err)
		kv, err := js.KeyValue(context.TODO(), "test_leases")
		require.NoError(t, err, "the bucket was not created at startup")
		status, err := kv.Status(context.TODO())
		require.NoError(t, err)
		assert.Equal(t, 9*time.Second, status.TTL())

		runJob(t, settings.JobRunner)
		// the lease was released when the runner stopped
		_, err = kv.Get(context.TODO(), "job.test")
		assert.ErrorIs(t, err, jetstream.ErrKeyNotFound)
	})

	t.Run("a missing bucket does not stop the manager", func(t *testing.T) {
		_, url := natstest.RunServer(t, true)
		cfg := newConfig(t, "[nats]\nurls = [\""+url+"\"]\n")
		assert.False(t, cfg.Jobs.EnsureBucket)

		settings := configure(t, cfg)
		require.IsType(t, &jobs.LeasedRunner{}, settings.JobRunner)
		conn, err := nats.Connect(url)
		require.NoError(t, err)
		t.Cleanup(conn.Close)
		js, err := jetstream.New(conn)
		require.NoError(t, err)
		_, err = js.KeyValue(context.TODO(), "maeve_job_leases")
		require.ErrorIs(t, err, jetstream.ErrBucketNotFound, "the bucket is only looked up")

		// once the bucket exists, the jobs run
		_, err = js.CreateKeyValue(context.TODO(), jetstream.KeyValueConfig{Bucket: "maeve_job_leases", TTL: 15 * time.Second})
		require.NoError(t, err)
		runJob(t, settings.JobRunner)
	})

	t.Run("nats not reachable at startup", func(t *testing.T) {
		cfg := newConfig(t, "[nats]\nurls = [\"nats://127.0.0.1:1\"]\nconnect_timeout = \"100ms\"\n")
		start := time.Now()
		settings := configure(t, cfg)
		assert.Less(t, time.Since(start), 3*time.Second, "the manager does not wait for the bucket")
		assert.IsType(t, &jobs.LeasedRunner{}, settings.JobRunner)
		assert.False(t, settings.NatsConn.IsConnected())
	})

	for name, tc := range map[string]struct {
		toml    string
		wantErr string
	}{
		"unknown runner":            {toml: "runner = \"cron\"", wantErr: "Runner"},
		"bad lease_ttl":             {toml: "lease_ttl = \"a while\"", wantErr: "lease ttl"},
		"zero lease_ttl":            {toml: "lease_ttl = \"0s\"", wantErr: "lease ttl must be positive"},
		"lease_ttl below 100ms":     {toml: "lease_ttl = \"50ms\"", wantErr: "at least 100ms"},
		"bad lease_bucket":          {toml: "lease_bucket = \"job.leases\"", wantErr: "not a valid bucket name"},
		"empty lease_bucket":        {toml: "lease_bucket = \"\"", wantErr: "not a valid bucket name"},
		"zero bucket_replicas":      {toml: "bucket_replicas = 0", wantErr: "BucketReplicas"},
		"too many bucket_replicas":  {toml: "bucket_replicas = 6", wantErr: "BucketReplicas"},
		"empty sync_interval":       {toml: "sync_interval = \"\"", wantErr: "sync interval"},
		"zero sync_interval":        {toml: "sync_interval = \"0s\"", wantErr: "sync interval must be positive"},
		"negative sync_retry_after": {toml: "sync_retry_after = \"-1m\"", wantErr: "sync retry after must be positive"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := newConfig(t, "[nats]\nurls = [\"nats://127.0.0.1:1\"]\nconnect_timeout = \"100ms\"\n[jobs]\n"+tc.toml+"\n")
			settings, err := config.Configure(context.TODO(), cfg)
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.wantErr)
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

// relayAll publishes every event in the outbox of a storage and returns them.
func relayAll(t *testing.T, engine store.Engine) []*store.OutboxEvent {
	t.Helper()
	var published []*store.OutboxEvent
	_, err := engine.RelayOutboxEvents(context.TODO(), store.MaxOutboxEventListLimit,
		func(_ context.Context, events []*store.OutboxEvent) (int, error) {
			published = append(published, events...)
			return len(events), nil
		})
	require.NoError(t, err)
	return published
}

func TestConfigureEvents(t *testing.T) {
	const defaultOrgId = "0193b2c4-0000-7000-8000-00000000000a"
	newConfig := func(t *testing.T, toml string) *config.BaseConfig {
		cfg := newTestConfig(t)
		cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
		require.NoError(t, cfg.Load(strings.NewReader(toml)))
		return cfg
	}
	stationEvent := func(stationKey string) events.Event {
		return events.Event{
			Type:         "station.registered",
			StationKey:   stationKey,
			ResourceType: "station",
			ResourceId:   stationKey,
			Data:         struct{}{},
		}
	}

	t.Run("enabled by default: the org of a station comes from the tenant resolver", func(t *testing.T) {
		ctx := context.TODO()
		cfg := newConfig(t, "[tenancy]\ndefault_org_id = \""+defaultOrgId+"\"\n")
		assert.True(t, cfg.Events.Enabled)
		assert.Equal(t, "single", cfg.Tenancy.Mode)

		settings, err := config.Configure(ctx, cfg)
		require.NoError(t, err)
		require.IsType(t, &events.OutboxEmitter{}, settings.Events)

		// a station that moved to another org has its org in its auth details
		const otherOrgId = "0193b2c4-0000-7000-8000-0000000000b2"
		require.NoError(t, settings.Storage.SetChargeStationAuth(ctx, "cs-moved", &store.ChargeStationAuth{OrgId: otherOrgId}))

		err = settings.Storage.RunInTx(ctx, func(ctx context.Context) error {
			// single mode: the station key is the plain station id, which has no org key
			return settings.Events.Emit(ctx, stationEvent("cs001"), stationEvent("cs-moved"))
		})
		require.NoError(t, err)

		published := relayAll(t, settings.Storage)
		require.Len(t, published, 2)
		assert.Equal(t, defaultOrgId, published[0].OrgId, "a station without an org belongs to the default org")
		assert.Equal(t, "urn:cpms:"+defaultOrgId+":station/cs001", published[0].Subject)
		assert.Equal(t, "station:cs001", published[0].Actor)
		assert.Equal(t, otherOrgId, published[1].OrgId, "a station with an org belongs to it")
	})

	t.Run("disabled", func(t *testing.T) {
		cfg := newConfig(t, "[events]\nenabled = false\n")
		assert.False(t, cfg.Events.Enabled)

		settings, err := config.Configure(context.TODO(), cfg)
		require.NoError(t, err)
		assert.Equal(t, events.NopEmitter{}, settings.Events)
	})
}

func TestConfigureEventRelay(t *testing.T) {
	// the default configuration (relay enabled) with the MQTT transport, so that only the
	// relay can need NATS
	newConfig := func(t *testing.T, toml string) *config.BaseConfig {
		cfg := newDefaultTestConfig(t)
		cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
		cfg.Transport.Type = "mqtt"
		require.NoError(t, cfg.Load(strings.NewReader(toml)))
		return cfg
	}
	configure := func(t *testing.T, cfg *config.BaseConfig) *config.Config {
		settings, err := config.Configure(context.TODO(), cfg)
		require.NoError(t, err)
		if settings.NatsConn != nil {
			t.Cleanup(settings.NatsConn.Close)
		}
		return settings
	}
	emitStationEvent := func(t *testing.T, settings *config.Config) {
		err := settings.Storage.RunInTx(context.TODO(), func(ctx context.Context) error {
			return settings.Events.Emit(ctx, events.Event{
				Type:         "station.registered",
				StationKey:   "cs001",
				ResourceType: "station",
				ResourceId:   "cs001",
				Data:         struct{}{},
			})
		})
		require.NoError(t, err)
	}
	jetStream := func(t *testing.T, url string) jetstream.JetStream {
		conn, err := nats.Connect(url)
		require.NoError(t, err)
		t.Cleanup(conn.Close)
		js, err := jetstream.New(conn)
		require.NoError(t, err)
		return js
	}

	t.Run("defaults", func(t *testing.T) {
		cfg := newDefaultTestConfig(t)
		assert.Equal(t, config.EventsConfig{
			Enabled:         true,
			RelayEnabled:    true,
			SubjectPrefix:   "cpms",
			Stream:          "CPMS_EVENTS",
			EnsureStream:    false,
			StreamReplicas:  1,
			StreamMaxAge:    "720h",
			RelayInterval:   "1s",
			RelayBatchSize:  100,
			OutboxRetention: "720h",
			PurgeInterval:   "1h",
		}, cfg.Events)
	})

	t.Run("the relay needs the nats section", func(t *testing.T) {
		cfg := newConfig(t, "")
		cfg.Nats = nil
		err := cfg.Validate()
		require.Error(t, err)
		assert.ErrorContains(t, err, "[nats] section is required to relay the domain events")

		settings, err := config.Configure(context.TODO(), cfg)
		require.Error(t, err)
		assert.Nil(t, settings)
	})

	t.Run("no nats section is needed without the relay and with the local job runner", func(t *testing.T) {
		for name, toml := range map[string]string{
			"relay disabled":  "[events]\nrelay_enabled = false\n[jobs]\nrunner = \"local\"\n",
			"events disabled": "[events]\nenabled = false\n[jobs]\nrunner = \"local\"\n",
		} {
			t.Run(name, func(t *testing.T) {
				cfg := newConfig(t, toml)
				cfg.Nats = nil
				require.NoError(t, cfg.Validate())

				settings := configure(t, cfg)
				assert.Nil(t, settings.EventRelay)
				assert.Nil(t, settings.NatsConn)
				// what was published before is still purged
				assert.Equal(t, &config.OutboxPurgeSettings{Retention: 720 * time.Hour, Interval: time.Hour}, settings.OutboxPurge)
			})
		}
	})

	t.Run("firestore disables the relay", func(t *testing.T) {
		t.Setenv("FIRESTORE_EMULATOR_HOST", "localhost:8080")
		cfg := newConfig(t, "[storage]\ntype = \"firestore\"\nfirestore.project_id = \"test-project-id\"\n[jobs]\nrunner = \"local\"\n")
		cfg.Nats = nil
		assert.True(t, cfg.Events.RelayEnabled)
		require.NoError(t, cfg.Validate())

		settings := configure(t, cfg)
		assert.Nil(t, settings.EventRelay)
		assert.Nil(t, settings.OutboxPurge)
		assert.Nil(t, settings.NatsConn)
	})

	t.Run("relay with all settings creates the stream", func(t *testing.T) {
		_, url := natstest.RunServer(t, true)
		cfg := newConfig(t, `
[nats]
urls = ["`+url+`"]

[events]
subject_prefix = "csms"
stream = "CSMS_EVENTS"
ensure_stream = true
stream_replicas = 1
stream_max_age = "24h"
relay_interval = "2s"
relay_batch_size = 10
outbox_retention = "48h"
purge_interval = "30m"
`)
		settings := configure(t, cfg)
		require.NotNil(t, settings.NatsConn, "the relay opens the connection to NATS with the MQTT transport")
		require.NotNil(t, settings.EventRelay)
		assert.Equal(t, "csms", settings.EventRelay.SubjectPrefix)
		assert.Equal(t, events.Product, settings.EventRelay.Product)
		assert.Equal(t, 10, settings.EventRelay.BatchSize)
		assert.Same(t, settings.Storage, settings.EventRelay.Store)
		assert.Equal(t, 2*time.Second, settings.EventRelayInterval)
		assert.Equal(t, &config.OutboxPurgeSettings{Retention: 48 * time.Hour, Interval: 30 * time.Minute}, settings.OutboxPurge)

		js := jetStream(t, url)
		stream, err := js.Stream(context.TODO(), "CSMS_EVENTS")
		require.NoError(t, err)
		info := stream.CachedInfo()
		assert.Equal(t, []string{"csms.>"}, info.Config.Subjects)
		assert.Equal(t, 24*time.Hour, info.Config.MaxAge)
		assert.Equal(t, 2*time.Minute, info.Config.Duplicates)

		emitStationEvent(t, settings)
		require.NoError(t, settings.EventRelay.RunOnce(context.TODO()))
		msg, err := stream.GetMsg(context.TODO(), 1)
		require.NoError(t, err)
		assert.Equal(t, "csms.station.registered."+store.LegacyOrgId, msg.Subject)
		var ce events.CloudEvent
		require.NoError(t, json.Unmarshal(msg.Data, &ce))
		assert.Equal(t, "cpms.station.registered", ce.Type)
		assert.Equal(t, msg.Header.Get(jetstream.MsgIDHeader), ce.Id)
	})

	t.Run("a missing stream does not stop the manager", func(t *testing.T) {
		_, url := natstest.RunServer(t, true)
		cfg := newConfig(t, "[transport]\ntype = \"nats\"\n[nats]\nurls = [\""+url+"\"]\n")
		assert.False(t, cfg.Events.EnsureStream)

		settings := configure(t, cfg)
		require.NotNil(t, settings.EventRelay)
		js := jetStream(t, url)
		_, err := js.Stream(context.TODO(), "CPMS_EVENTS")
		require.ErrorIs(t, err, jetstream.ErrStreamNotFound, "the stream is only checked")

		// the manager answers the stations
		assertHeartbeatRoundTrip(t, settings, url, "ocpp")

		// the events wait in the outbox until the stream is there
		emitStationEvent(t, settings)
		require.Error(t, settings.EventRelay.RunOnce(context.TODO()))
		assert.Empty(t, publishedOutbox(t, settings.Storage))

		require.NoError(t, events.EnsureStream(context.TODO(), js, events.StreamConfig{
			Name: "CPMS_EVENTS", Subjects: []string{"cpms.>"}, Replicas: 1,
		}))
		require.NoError(t, settings.EventRelay.RunOnce(context.TODO()))
		assert.Len(t, publishedOutbox(t, settings.Storage), 1)
	})

	t.Run("nats not reachable at startup", func(t *testing.T) {
		cfg := newConfig(t, "[nats]\nurls = [\"nats://127.0.0.1:1\"]\nconnect_timeout = \"100ms\"\n")
		start := time.Now()
		settings := configure(t, cfg)
		assert.Less(t, time.Since(start), 3*time.Second, "the manager does not wait for the stream")
		require.NotNil(t, settings.EventRelay)
		assert.False(t, settings.NatsConn.IsConnected())
	})

	for name, tc := range map[string]struct {
		toml    string
		wantErr string
	}{
		"bad stream_max_age":        {toml: "stream_max_age = \"a month\"", wantErr: "stream_max_age"},
		"zero relay_interval":       {toml: "relay_interval = \"0s\"", wantErr: "relay_interval must be positive"},
		"negative outbox_retention": {toml: "outbox_retention = \"-1h\"", wantErr: "outbox_retention must be positive"},
		"empty purge_interval":      {toml: "purge_interval = \"\"", wantErr: "purge_interval"},
		"zero relay_batch_size":     {toml: "relay_batch_size = 0", wantErr: "RelayBatchSize"},
		"huge relay_batch_size":     {toml: "relay_batch_size = 1001", wantErr: "RelayBatchSize"},
		"zero stream_replicas":      {toml: "stream_replicas = 0", wantErr: "StreamReplicas"},
		"empty subject_prefix":      {toml: "subject_prefix = \"\"", wantErr: "SubjectPrefix"},
		"subject_prefix with a dot": {toml: "subject_prefix = \"a.b\"", wantErr: "subject_prefix"},
		"empty stream":              {toml: "stream = \"\"", wantErr: "Stream"},
		"stream with a dot":         {toml: "stream = \"CPMS.EVENTS\"", wantErr: "not a valid stream name"},
	} {
		t.Run(name, func(t *testing.T) {
			// checked although the relay is disabled
			cfg := newConfig(t, "[events]\nrelay_enabled = false\n"+tc.toml+"\n")
			settings, err := config.Configure(context.TODO(), cfg)
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.wantErr)
			assert.Nil(t, settings)
		})
	}
}

// publishedOutbox returns the published events of the outbox of a storage.
func publishedOutbox(t *testing.T, engine store.Engine) []*store.OutboxEvent {
	t.Helper()
	evs, err := engine.ListOutboxEvents(context.TODO(), store.OutboxEventFilter{})
	require.NoError(t, err)
	return evs
}

func TestConfigureConnectivity(t *testing.T) {
	newConfig := func(t *testing.T, toml string) *config.BaseConfig {
		cfg := newTestConfig(t)
		cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
		require.NoError(t, cfg.Load(strings.NewReader(toml)))
		return cfg
	}

	t.Run("defaults", func(t *testing.T) {
		settings, err := config.Configure(context.TODO(), newConfig(t, ""))
		require.NoError(t, err)

		require.NotNil(t, settings.Connectivity)
		assert.Same(t, settings.Storage, settings.Connectivity.Store)
		assert.NotNil(t, settings.Connectivity.Clock)
		assert.Equal(t, 30*time.Second, settings.Connectivity.SeenWriteEvery)
		assert.Equal(t, 5*time.Minute, settings.Connectivity.StaleAfter)
		assert.Equal(t, 30*time.Second, settings.ConnectivitySweepInterval)
	})

	t.Run("overridden", func(t *testing.T) {
		settings, err := config.Configure(context.TODO(), newConfig(t,
			"[connectivity]\nlast_seen_write_interval = \"1m\"\nstale_after = \"10m\"\nsweep_interval = \"15s\"\n"))
		require.NoError(t, err)

		require.NotNil(t, settings.Connectivity)
		assert.Equal(t, time.Minute, settings.Connectivity.SeenWriteEvery)
		assert.Equal(t, 10*time.Minute, settings.Connectivity.StaleAfter)
		assert.Equal(t, 15*time.Second, settings.ConnectivitySweepInterval)
	})

	t.Run("a short stale_after is accepted", func(t *testing.T) {
		settings, err := config.Configure(context.TODO(), newConfig(t,
			"[connectivity]\nlast_seen_write_interval = \"10s\"\nstale_after = \"1m\"\n"))
		require.NoError(t, err)
		assert.Equal(t, time.Minute, settings.Connectivity.StaleAfter)
	})

	for _, key := range []string{"last_seen_write_interval", "stale_after", "sweep_interval"} {
		for name, value := range map[string]string{
			"not a duration": "every minute",
			"no unit":        "60",
			"zero":           "0s",
			"negative":       "-1m",
			"empty":          "",
		} {
			t.Run(key+" "+name, func(t *testing.T) {
				settings, err := config.Configure(context.TODO(), newConfig(t, "[connectivity]\n"+key+" = \""+value+"\"\n"))
				require.Error(t, err)
				assert.ErrorContains(t, err, key)
				assert.Nil(t, settings)
			})
		}
	}

	for name, toml := range map[string]string{
		"stale_after equal to last_seen_write_interval":     "[connectivity]\nlast_seen_write_interval = \"5m\"\nstale_after = \"5m\"\n",
		"stale_after shorter than last_seen_write_interval": "[connectivity]\nlast_seen_write_interval = \"1m\"\nstale_after = \"30s\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			settings, err := config.Configure(context.TODO(), newConfig(t, toml))
			require.Error(t, err)
			assert.ErrorContains(t, err, "must be longer than last_seen_write_interval")
			assert.Nil(t, settings)
		})
	}
}

func TestConfigureNatsTransport(t *testing.T) {
	newConfig := func(t *testing.T, toml string) *config.BaseConfig {
		cfg := newTestConfig(t)
		cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
		require.NoError(t, cfg.Load(strings.NewReader(toml)))
		return cfg
	}
	configure := func(t *testing.T, cfg *config.BaseConfig) *config.Config {
		settings, err := config.Configure(context.TODO(), cfg)
		require.NoError(t, err)
		if settings.NatsConn != nil {
			t.Cleanup(settings.NatsConn.Close)
		}
		return settings
	}

	newDefaultConfig := func(t *testing.T, toml string) *config.BaseConfig {
		cfg := newDefaultTestConfig(t)
		cfg.ContractCertValidator.Ocsp.RootCertProvider.File.FileNames = []string{"testdata/root_ca.pem"}
		require.NoError(t, cfg.Load(strings.NewReader(toml)))
		return cfg
	}

	t.Run("nats is the default", func(t *testing.T) {
		cfg := newDefaultConfig(t, "")
		assert.Equal(t, "nats", cfg.Transport.Type)
		assert.Nil(t, cfg.Transport.Nats)
		assert.Equal(t, &config.NatsConfig{Urls: []string{"nats://localhost:4222"}}, cfg.Nats)
		require.NoError(t, cfg.Validate())

		// the default settings, with the server of the test instead of localhost
		_, url := natstest.RunServer(t, false)
		cfg = newDefaultConfig(t, "[nats]\nurls = [\""+url+"\"]\n")
		assert.Equal(t, []string{url}, cfg.Nats.Urls)

		settings := configure(t, cfg)
		require.NotNil(t, settings.NatsConn)
		assert.Equal(t, "maeve-manager", settings.NatsConn.Opts.Name)
		assert.IsType(t, &natstransport.Emitter{}, settings.MsgEmitter)
		assert.IsType(t, &natstransport.Listener{}, settings.MsgListener)

		assertHeartbeatRoundTrip(t, settings, url, "ocpp")
	})

	t.Run("mqtt needs no further keys and no nats without the event relay and the leased job runner", func(t *testing.T) {
		cfg := newDefaultConfig(t, "[transport]\ntype = \"mqtt\"\n[events]\nrelay_enabled = false\n[jobs]\nrunner = \"local\"\n")
		assert.Equal(t, "mqtt", cfg.Transport.Type)
		assert.Equal(t, []string{"mqtt://localhost:1883"}, cfg.Transport.Mqtt.Urls)
		require.NoError(t, cfg.Validate())

		settings := configure(t, cfg)
		assert.Nil(t, settings.NatsConn)
		assert.IsType(t, &mqtt.Emitter{}, settings.MsgEmitter)
		assert.IsType(t, &mqtt.Listener{}, settings.MsgListener)
	})

	t.Run("the nats section is not read with mqtt", func(t *testing.T) {
		cfg := newConfig(t, "[nats]\nurls = []\nconnect_timeout = \"not a duration\"\n")
		require.NoError(t, cfg.Validate())

		settings := configure(t, cfg)
		assert.Nil(t, settings.NatsConn)
	})

	t.Run("nats works end to end with the defaults", func(t *testing.T) {
		_, url := natstest.RunServer(t, false)
		cfg := newConfig(t, "[transport]\ntype = \"nats\"\n[nats]\nurls = [\""+url+"\"]\n")

		settings := configure(t, cfg)
		require.NotNil(t, settings.NatsConn)
		assert.True(t, settings.NatsConn.IsConnected())
		assert.Equal(t, "maeve-manager", settings.NatsConn.Opts.Name)
		assert.Equal(t, 5*time.Second, settings.NatsConn.Opts.Timeout)
		assert.Equal(t, 2*time.Second, settings.NatsConn.Opts.ReconnectWait)
		assert.Equal(t, -1, settings.NatsConn.Opts.MaxReconnect)
		assert.IsType(t, &natstransport.Emitter{}, settings.MsgEmitter)
		assert.IsType(t, &natstransport.Listener{}, settings.MsgListener)

		assertHeartbeatRoundTrip(t, settings, url, "ocpp")
	})

	t.Run("nats with all settings", func(t *testing.T) {
		_, url := natstest.RunServer(t, false)
		cfg := newConfig(t, `
[transport]
type = "nats"
nats.subject_prefix = "csms"
nats.queue_group = "replicas"
nats.workers = 4

[nats]
urls = ["`+url+`"]
name = "manager-1"
connect_timeout = "3s"
reconnect_wait = "250ms"
max_reconnects = 7
`)
		assert.Equal(t, &config.NatsTransportConfig{SubjectPrefix: "csms", QueueGroup: "replicas", Workers: 4}, cfg.Transport.Nats)

		settings := configure(t, cfg)
		assert.Equal(t, "manager-1", settings.NatsConn.Opts.Name)
		assert.Equal(t, 3*time.Second, settings.NatsConn.Opts.Timeout)
		assert.Equal(t, 250*time.Millisecond, settings.NatsConn.Opts.ReconnectWait)
		assert.Equal(t, 7, settings.NatsConn.Opts.MaxReconnect)

		assertHeartbeatRoundTrip(t, settings, url, "csms")
	})

	t.Run("invalid nats settings", func(t *testing.T) {
		for name, tc := range map[string]struct {
			toml    string
			wantErr string
		}{
			"no nats section":          {toml: "", wantErr: "[nats] section is required"},
			"no urls":                  {toml: "[nats]\nurls = []\n", wantErr: "Urls"},
			"empty url":                {toml: "[nats]\nurls = [\"\"]\n", wantErr: "Urls"},
			"bad connect timeout":      {toml: "[nats]\nurls = [\"nats://127.0.0.1:4222\"]\nconnect_timeout = \"soon\"\n", wantErr: "connect timeout"},
			"negative connect timeout": {toml: "[nats]\nurls = [\"nats://127.0.0.1:4222\"]\nconnect_timeout = \"-1s\"\n", wantErr: "connect timeout must be positive"},
			"zero reconnect wait":      {toml: "[nats]\nurls = [\"nats://127.0.0.1:4222\"]\nreconnect_wait = \"0s\"\n", wantErr: "reconnect wait must be positive"},
			"cert without key":         {toml: "[nats]\nurls = [\"nats://127.0.0.1:4222\"]\ntls_cert_file = \"cert.pem\"\n", wantErr: "TlsKeyFile"},
			"missing creds file":       {toml: "[nats]\nurls = [\"nats://127.0.0.1:4222\"]\ncreds_file = \"testdata/missing.creds\"\n", wantErr: "creds file"},
			"too many workers":         {toml: "[transport.nats]\nworkers = 2000\n[nats]\nurls = [\"nats://127.0.0.1:4222\"]\n", wantErr: "Workers"},
			"prefix with a dot":        {toml: "[transport.nats]\nsubject_prefix = \"a.b\"\n[nats]\nurls = [\"nats://127.0.0.1:4222\"]\n", wantErr: "subject prefix"},
		} {
			t.Run(name, func(t *testing.T) {
				cfg := newConfig(t, "[transport]\ntype = \"nats\"\n"+tc.toml)
				settings, err := config.Configure(context.TODO(), cfg)
				require.Error(t, err)
				assert.Nil(t, settings)
				assert.ErrorContains(t, err, tc.wantErr)
			})
		}
	})
}

// assertHeartbeatRoundTrip sends a heartbeat to the configured manager as the gateway
// would and waits for the answer.
func assertHeartbeatRoundTrip(t *testing.T, settings *config.Config, url, prefix string) {
	t.Helper()
	conn, err := settings.MsgListener.Connect(context.TODO(), transport.OcppVersion16, nil, settings.Ocpp16Handler)
	require.NoError(t, err)
	defer func() { assert.NoError(t, conn.Disconnect(context.TODO())) }()

	gateway, err := nats.Connect(url)
	require.NoError(t, err)
	defer gateway.Close()
	responses, err := gateway.SubscribeSync(prefix + ".out.ocpp16._.CP001")
	require.NoError(t, err)
	require.NoError(t, gateway.Flush())

	require.NoError(t, gateway.Publish(prefix+".in.ocpp16._.CP001",
		[]byte(`{"type":2,"action":"Heartbeat","id":"hb-1","request":{}}`)))
	got, err := responses.NextMsg(5 * time.Second)
	require.NoError(t, err)
	var resp transport.Message
	require.NoError(t, json.Unmarshal(got.Data, &resp))
	assert.Equal(t, transport.MessageTypeCallResult, resp.MessageType)
	assert.Equal(t, "hb-1", resp.MessageId)
}
