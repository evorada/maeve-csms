// SPDX-License-Identifier: Apache-2.0

package config

// NatsConfig configures the connection to NATS ([nats]). It is only read, and only
// validated, when something uses NATS: the NATS transport or the relay of the domain
// events to JetStream.
//
// The credentials and keys are files (in Kubernetes, mounted from the secret store);
// user, password and token are meant for local development.
type NatsConfig struct {
	Urls           []string `mapstructure:"urls" toml:"urls" validate:"required,min=1,dive,required"`
	Name           string   `mapstructure:"name" toml:"name"`
	CredsFile      string   `mapstructure:"creds_file" toml:"creds_file"`
	NkeySeedFile   string   `mapstructure:"nkey_seed_file" toml:"nkey_seed_file"`
	User           string   `mapstructure:"user" toml:"user"`
	Password       string   `mapstructure:"password" toml:"password"`
	Token          string   `mapstructure:"token" toml:"token"`
	TlsCaFile      string   `mapstructure:"tls_ca_file" toml:"tls_ca_file"`
	TlsCertFile    string   `mapstructure:"tls_cert_file" toml:"tls_cert_file" validate:"required_with=TlsKeyFile"`
	TlsKeyFile     string   `mapstructure:"tls_key_file" toml:"tls_key_file" validate:"required_with=TlsCertFile"`
	ConnectTimeout string   `mapstructure:"connect_timeout" toml:"connect_timeout"`
	ReconnectWait  string   `mapstructure:"reconnect_wait" toml:"reconnect_wait"`
	MaxReconnects  *int     `mapstructure:"max_reconnects" toml:"max_reconnects"`
}

// NatsTransportConfig configures the NATS transport ([transport.nats]). Empty values
// take the defaults: prefix "ocpp", queue group "maeve-manager", 16 workers.
type NatsTransportConfig struct {
	SubjectPrefix string `mapstructure:"subject_prefix" toml:"subject_prefix"`
	QueueGroup    string `mapstructure:"queue_group" toml:"queue_group"`
	Workers       int    `mapstructure:"workers" toml:"workers" validate:"omitempty,min=1,max=1024"`
}
