// SPDX-License-Identifier: Apache-2.0

package config

type ApiSettingsConfig struct {
	Addr string `mapstructure:"addr" toml:"addr" validate:"required"`
	// InternalAddr is the address of the internal listener, which serves the lookups of
	// the gateway, the health check, the metrics, the OpenAPI document and the legacy
	// pages that are enabled. It is not authenticated and must only be reachable from
	// inside the deployment (the gateway, probes and the metrics scraper).
	InternalAddr string `mapstructure:"internal_addr" toml:"internal_addr" validate:"required"`
	// EnableTransactionsPage serves the legacy page /transactions on the internal
	// listener. The page lists the transactions of every org without authentication.
	EnableTransactionsPage bool `mapstructure:"enable_transactions_page" toml:"enable_transactions_page"`
	// EnableAdminUI serves the legacy pages below /adminui on the internal listener.
	// They register charge stations and tokens, and issue client certificates, without
	// authentication.
	EnableAdminUI bool   `mapstructure:"enable_admin_ui" toml:"enable_admin_ui"`
	Host          string `mapstructure:"host,omitempty" toml:"host,omitempty"`
	WsPort        int    `mapstructure:"ws_port,omitempty" toml:"ws_port,omitempty"`
	WssPort       int    `mapstructure:"wss_port,omitempty" toml:"wss_port,omitempty"`
	// OrgName is the organization name of the client certificates that the admin pages
	// issue. It is deprecated and for single-tenant mode only: in multi mode the
	// accepted certificate organisation names of the tenant config are used, and this
	// is only the fallback for a charge station whose tenant has none.
	OrgName string `mapstructure:"org_name,omitempty" toml:"org_name,omitempty"`
	// Auth says how the callers of /api/v0 are authenticated.
	Auth AuthConfig `mapstructure:"auth" toml:"auth"`
}

// AuthConfig configures the authentication of the callers of the API.
type AuthConfig struct {
	// Mode is "jwt" (every request needs a token that the console signed) or "dev"
	// (every request is accepted as the caller in Dev; refused at startup unless the
	// ENVIRONMENT environment variable is "dev").
	Mode string `mapstructure:"mode" toml:"mode" validate:"required,oneof=jwt dev"`
	// JWKSURL is where the console publishes the keys that sign its tokens.
	JWKSURL string `mapstructure:"jwks_url" toml:"jwks_url" validate:"required_if=Mode jwt,omitempty,http_url"`
	// Issuer is the only "iss" that is accepted.
	Issuer string `mapstructure:"issuer" toml:"issuer" validate:"required"`
	// Audience is the "aud" that a token must have.
	Audience string `mapstructure:"audience" toml:"audience" validate:"required"`
	// MinRefreshInterval is the shortest time between two requests for the keys of
	// the console, e.g. "5m".
	MinRefreshInterval string `mapstructure:"min_refresh_interval" toml:"min_refresh_interval" validate:"required"`
	// ClockSkew is the leeway for the time claims of a token, e.g. "5s".
	ClockSkew string `mapstructure:"clock_skew" toml:"clock_skew" validate:"required"`
	// MaxTokenLifetime is the longest accepted time between the issue and the expiry
	// of a token, e.g. "120s".
	MaxTokenLifetime string `mapstructure:"max_token_lifetime" toml:"max_token_lifetime" validate:"required"`
	// Dev is the caller of every request in dev mode.
	Dev AuthDevConfig `mapstructure:"dev" toml:"dev"`
}

// AuthDevConfig is the caller that every request gets in dev mode.
type AuthDevConfig struct {
	// OrgId is the org (UUID) of the caller. Empty is the default org of [tenancy].
	OrgId string `mapstructure:"org_id" toml:"org_id" validate:"omitempty,uuid"`
	// OrgScope is "self" or "subtree".
	OrgScope string `mapstructure:"org_scope" toml:"org_scope" validate:"required,oneof=self subtree"`
	// Perms are the permissions of the caller. Not set is every permission.
	Perms []string `mapstructure:"perms" toml:"perms"`
}

type OcppSettingsConfig struct {
	HeartbeatInterval string `mapstructure:"heartbeat_interval" toml:"heartbeat_interval" validate:"required"`
	Ocpp16Enabled     bool   `mapstructure:"ocpp16_enabled" toml:"ocpp16_enabled" validate:"required_without=Ocpp201Enabled"`
	Ocpp201Enabled    bool   `mapstructure:"ocpp201_enabled" toml:"ocpp201_enabled" validate:"required_without=Ocpp16Enabled"`
}

type ObservabilitySettingsConfig struct {
	LogFormat         string `mapstructure:"log_format" toml:"log_format" validate:"required"`
	OtelCollectorAddr string `mapstructure:"otel_collector_addr" toml:"otel_collector_addr"`
	TlsKeylogFile     string `mapstructure:"tls_keylog_file" toml:"tls_keylog_file"`
}
