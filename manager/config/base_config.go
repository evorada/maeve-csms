// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/go-playground/validator/v10"
	"github.com/pelletier/go-toml/v2"
	"github.com/thoughtworks/maeve-csms/manager/connectivity"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// BaseConfig provides the data structures that represent the configuration
// and provides the ability to load the configuration from a TOML file.
type BaseConfig struct {
	Api                       ApiSettingsConfig               `mapstructure:"api" toml:"api" validate:"required"`
	Transport                 TransportConfig                 `mapstructure:"transport" toml:"transport" validate:"required"`
	Ocpp                      OcppSettingsConfig              `mapstructure:"ocpp" toml:"ocpp" validate:"required"`
	Observability             ObservabilitySettingsConfig     `mapstructure:"observability" toml:"observability" validate:"required"`
	Storage                   StorageConfig                   `mapstructure:"storage" toml:"storage" validate:"required"`
	ContractCertValidator     ContractCertValidatorConfig     `mapstructure:"contract_cert_validator" toml:"contract_cert_validator" validate:"required"`
	ContractCertProvider      ContractCertProviderConfig      `mapstructure:"contract_cert_provider" toml:"contract_cert_provider" validate:"required"`
	ChargeStationCertProvider ChargeStationCertProviderConfig `mapstructure:"charge_station_cert_provider" toml:"charge_station_cert_provider" validate:"required"`
	TariffService             TariffServiceConfig             `mapstructure:"tariff_service" toml:"tariff_service" validate:"required"`
	Jobs                      JobsConfig                      `mapstructure:"jobs" toml:"jobs"`
	Tenancy                   TenancyConfig                   `mapstructure:"tenancy" toml:"tenancy"`
	Events                    EventsConfig                    `mapstructure:"events" toml:"events"`
	Connectivity              ConnectivityConfig              `mapstructure:"connectivity" toml:"connectivity"`
	Ocpi                      *OcpiConfig                     `mapstructure:"ocpi,omitempty" toml:"ocpi,omitempty"`
	// Nats is validated by Validate only when NATS is used.
	Nats *NatsConfig `mapstructure:"nats,omitempty" toml:"nats,omitempty" validate:"-"`
}

// DefaultConfig provides the default configuration. The configuration
// read from the TOML file will overlay this configuration.
var DefaultConfig = BaseConfig{
	Api: ApiSettingsConfig{
		Addr:         "localhost:9410",
		InternalAddr: "localhost:9412",
		Host:         "localhost",
		WsPort:       80,
		WssPort:      443,
		OrgName:      "Thoughtworks",
		Auth: AuthConfig{
			Mode:               "jwt",
			Issuer:             "platform",
			Audience:           "maeve",
			MinRefreshInterval: "5m",
			ClockSkew:          "5s",
			MaxTokenLifetime:   "120s",
			Dev: AuthDevConfig{
				OrgScope: "subtree",
			},
		},
	},
	// NATS is the default transport. The MQTT settings stay, so that type = "mqtt"
	// works without further keys.
	Transport: TransportConfig{
		Type: "nats",
		Mqtt: &MqttSettingsConfig{
			Urls:              []string{"mqtt://localhost:1883"},
			Prefix:            "cs",
			Group:             "manager",
			ConnectTimeout:    "10s",
			ConnectRetryDelay: "1s",
			KeepAliveInterval: "10s",
		},
	},
	Ocpp: OcppSettingsConfig{
		HeartbeatInterval: "5m",
		Ocpp16Enabled:     true,
		Ocpp201Enabled:    true,
	},
	Observability: ObservabilitySettingsConfig{
		LogFormat: "text",
	},
	Storage: StorageConfig{
		Type: "in_memory",
	},
	ContractCertValidator: ContractCertValidatorConfig{
		Type: "ocsp",
		Ocsp: &OcspContractCertValidatorConfig{
			RootCertProvider: RootCertProviderConfig{
				Type: "file",
				File: &FileRootCertProviderConfig{
					FileNames: []string{"root_ca.pem"},
				},
			},
			MaxAttempts: 1,
		},
	},
	ContractCertProvider: ContractCertProviderConfig{
		Type: "default",
	},
	ChargeStationCertProvider: ChargeStationCertProviderConfig{
		Type: "default",
	},
	TariffService: TariffServiceConfig{
		Type: "kwh",
	},
	Jobs: JobsConfig{
		Runner:                    "nats_kv",
		LeaseBucket:               jobs.DefaultLeaseBucket,
		LeaseTTL:                  jobs.DefaultLeaseTTL.String(),
		EnsureBucket:              false,
		BucketReplicas:            1,
		SyncInterval:              "1m",
		SyncRetryAfter:            "2m",
		ReservationExpiryInterval: "1m",
	},
	Tenancy: TenancyConfig{
		DefaultOrgId: store.LegacyOrgId,
		Mode:         "single",
		CacheTTL:     "30s",
	},
	Events: EventsConfig{
		Enabled:         true,
		RelayEnabled:    true,
		SubjectPrefix:   events.DefaultSubjectPrefix,
		Stream:          events.DefaultStreamName,
		StreamReplicas:  1,
		StreamMaxAge:    "720h",
		RelayInterval:   "1s",
		RelayBatchSize:  events.DefaultRelayBatchSize,
		OutboxRetention: "720h",
		PurgeInterval:   "1h",
	},
	Connectivity: ConnectivityConfig{
		LastSeenWriteInterval: connectivity.DefaultSeenWriteEvery.String(),
		StaleAfter:            connectivity.DefaultStaleAfter.String(),
		SweepInterval:         connectivity.DefaultSweepInterval.String(),
	},
	Nats: &NatsConfig{
		Urls: []string{"nats://localhost:4222"},
	},
}

// Load reads TOML configuration from a reader.
func (c *BaseConfig) Load(reader io.Reader) error {
	decoder := toml.NewDecoder(reader)
	err := decoder.Decode(c)
	if err != nil {
		return err
	}
	c.replaceDefaults()
	return nil
}

// LoadFromFile reads TOML configuration from a file.
func (c *BaseConfig) LoadFromFile(configFile string) error {
	//#nosec G304 - only files specified by the person running the application will be loaded
	f, err := os.Open(configFile)
	if err != nil {
		return err
	}
	err = c.Load(bufio.NewReader(f))
	c.replaceDefaults()
	return err
}

// replaceDefaults removes any default configuration (defined in DefaultConfig)
// that has been replaced by configuration loaded from the file: this happens
// when the configured `type` is different from the default
func (c *BaseConfig) replaceDefaults() {
	switch c.ContractCertValidator.Type {
	case "ocsp":
		if c.ContractCertValidator.Ocsp != nil {
			if c.ContractCertValidator.Ocsp.RootCertProvider.Type != "file" {
				c.ContractCertValidator.Ocsp.RootCertProvider.File = nil
			}
		}
	}
}

// Validate ensures that the configuration is structurally valid.
func (c *BaseConfig) Validate() error {
	validate := validator.New()

	err := validate.Struct(c)
	if err != nil {
		return err
	}

	if c.usesNats() {
		if c.Nats == nil {
			switch {
			case c.Transport.Type == "nats":
				return fmt.Errorf("the [nats] section is required with transport type %q", c.Transport.Type)
			case c.relaysEvents():
				return errors.New("the [nats] section is required to relay the domain events to JetStream (events.relay_enabled)")
			default:
				return errors.New(`the [nats] section is required to run the background jobs with jobs runner "nats_kv" (use "local" for a single instance without NATS)`)
			}
		}
		err = validate.Struct(c.Nats)
		if err != nil {
			return err
		}
	}
	return nil
}

// usesNats reports whether something needs the connection to NATS: the NATS transport,
// the relay of the domain events or the leases of the background jobs.
func (c *BaseConfig) usesNats() bool {
	return c.Transport.Type == "nats" || c.relaysEvents() || c.Jobs.usesLeasedRunner()
}
