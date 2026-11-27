// SPDX-License-Identifier: Apache-2.0

package config

// TenancyConfig configures the orgs (tenants) that the manager serves.
type TenancyConfig struct {
	// DefaultOrgId is the org (UUID) that owns the rows from before orgs and
	// everything that is created without a tenant.
	DefaultOrgId string `mapstructure:"default_org_id" toml:"default_org_id" validate:"required,uuid"`
	// Mode is "single" (plain station ids, the host that a station connects to is
	// ignored) or "multi" (the host names the tenant and a station is known as
	// "{org_key}.{stationId}").
	Mode string `mapstructure:"mode" toml:"mode" validate:"required,oneof=single multi"`
	// CacheTTL is how long a tenant that was resolved from a host or an org key is
	// kept in memory, e.g. "30s".
	CacheTTL string `mapstructure:"cache_ttl" toml:"cache_ttl" validate:"required"`
	// BootstrapTenants are created or updated when the manager starts in multi mode,
	// for deployments without a console that provisions the tenants.
	BootstrapTenants []BootstrapTenantConfig `mapstructure:"bootstrap_tenants" toml:"bootstrap_tenants" validate:"dive"`
}

// BootstrapTenantConfig is one tenant of [[tenancy.bootstrap_tenants]].
type BootstrapTenantConfig struct {
	OrgId     string   `mapstructure:"org_id" toml:"org_id" validate:"required,uuid"`
	Slug      string   `mapstructure:"slug" toml:"slug" validate:"required"`
	OrgKey    string   `mapstructure:"org_key" toml:"org_key" validate:"required"`
	Hostnames []string `mapstructure:"hostnames" toml:"hostnames"`
}
