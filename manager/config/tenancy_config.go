// SPDX-License-Identifier: Apache-2.0

package config

// TenancyConfig configures the orgs (tenants) that the manager serves.
type TenancyConfig struct {
	// DefaultOrgId is the org (UUID) that owns the rows from before orgs and
	// everything that is created without a tenant.
	DefaultOrgId string `mapstructure:"default_org_id" toml:"default_org_id" validate:"required,uuid"`
}
