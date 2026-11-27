// SPDX-License-Identifier: Apache-2.0

package tenancy

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/google/uuid"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// maxHostnameLength is the longest DNS name in its text form.
const maxHostnameLength = 253

var (
	slugPattern   = regexp.MustCompile(`^[a-z0-9-]{3,32}$`)
	orgKeyPattern = regexp.MustCompile(`^[a-z0-9]{4,8}$`)
	// labels of letters, digits and hyphens, separated by dots; no port, no trailing dot
	hostnamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)
)

// BootstrapTenant is a tenant from the configuration of the manager, for deployments
// that have no console to provision tenants.
type BootstrapTenant struct {
	OrgId     string // UUID
	Slug      string
	OrgKey    string
	Hostnames []string
}

// The values of IdentityError.Field.
const (
	IdentityFieldSlug      = "slug"
	IdentityFieldOrgKey    = "org_key"
	IdentityFieldHostnames = "hostnames"
)

// IdentityError is the error of ValidateTenantIdentity: it says which of the names of a
// tenant is not valid.
type IdentityError struct {
	// Field is one of the IdentityField constants.
	Field   string
	message string
}

func (e *IdentityError) Error() string {
	return e.message
}

// ValidateTenantIdentity checks what names a tenant: the slug (^[a-z0-9-]{3,32}$), the
// org key (^[a-z0-9]{4,8}$) and the hostnames (lowercase DNS names without a port). The
// error is an *IdentityError.
func ValidateTenantIdentity(slug, orgKey string, hostnames []string) error {
	if !slugPattern.MatchString(slug) {
		return &IdentityError{Field: IdentityFieldSlug, message: fmt.Sprintf("invalid slug %q: must match %s", slug, slugPattern)}
	}
	if !orgKeyPattern.MatchString(orgKey) {
		return &IdentityError{Field: IdentityFieldOrgKey, message: fmt.Sprintf("invalid org key %q: must match %s", orgKey, orgKeyPattern)}
	}
	for _, hostname := range hostnames {
		if len(hostname) > maxHostnameLength || !hostnamePattern.MatchString(hostname) {
			return &IdentityError{Field: IdentityFieldHostnames, message: fmt.Sprintf("invalid hostname %q: must be a lowercase DNS name without a port", hostname)}
		}
	}
	return nil
}

// Bootstrap puts the configured tenants in the tenant store: each one is created, or
// updated when it is already there (the slug and the org key of an existing tenant
// cannot change). A tenant is a root org (path store.OrgPathLabel(orgId)) with empty
// entitlements and config at version 0. Nothing is written when one of the tenants is
// not valid.
func Bootstrap(ctx context.Context, tenants store.TenantStore, cfg []BootstrapTenant) error {
	if len(cfg) == 0 {
		return nil
	}
	if tenants == nil {
		return errors.New("bootstrap tenants: no tenant store")
	}

	toPut := make([]*store.Tenant, 0, len(cfg))
	seen := make(map[string]bool, len(cfg))
	for _, tenant := range cfg {
		id, err := uuid.Parse(tenant.OrgId)
		if err != nil {
			return fmt.Errorf("bootstrap tenant %q: invalid org id: %w", tenant.OrgId, err)
		}
		orgId := id.String()
		if seen[orgId] {
			return fmt.Errorf("bootstrap tenant %s: configured more than once", orgId)
		}
		seen[orgId] = true
		if err := ValidateTenantIdentity(tenant.Slug, tenant.OrgKey, tenant.Hostnames); err != nil {
			return fmt.Errorf("bootstrap tenant %s: %w", orgId, err)
		}
		toPut = append(toPut, &store.Tenant{
			OrgId:     orgId,
			Path:      store.OrgPathLabel(orgId),
			Slug:      tenant.Slug,
			OrgKey:    tenant.OrgKey,
			Hostnames: tenant.Hostnames,
		})
	}

	for _, tenant := range toPut {
		if err := tenants.PutTenant(ctx, tenant); err != nil {
			return fmt.Errorf("bootstrap tenant %s: %w", tenant.OrgId, err)
		}
	}
	return nil
}
