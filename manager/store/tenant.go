// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"time"
)

type TenantStatus string

const (
	TenantStatusActive    TenantStatus = "active"
	TenantStatusSuspended TenantStatus = "suspended"
	TenantStatusCancelled TenantStatus = "cancelled"
)

// LegacyOrgId owns the rows that existed before tenancy.
const LegacyOrgId = "00000000-0000-0000-0000-000000000000"

var (
	// ErrTenantConflict is returned when the slug, the org key or a hostname of a tenant
	// is already used by another tenant.
	ErrTenantConflict = errors.New("tenant conflict: slug, org_key or hostname used by another tenant")
	// ErrTenantImmutable is returned when a put tries to change the slug or the org key
	// of an existing tenant.
	ErrTenantImmutable = errors.New("tenant slug and org_key are immutable")
)

// TenantParty is an OCPI party of a tenant.
type TenantParty struct {
	Id          string `json:"id"`
	Role        string `json:"role"`         // CPO | EMSP
	CountryCode string `json:"country_code"` // ISO 3166 alpha-2, or XX for internal identities
	PartyId     string `json:"party_id"`     // 3 chars
}

// Tenant is the local copy of an organisation that is provisioned by the platform.
type Tenant struct {
	OrgId               string  // UUID
	ParentId            *string // UUID, nil for the root
	Path                string  // ltree text, e.g. "0193b2c4...ab.0193b2c5...cd"
	Slug                string
	OrgKey              string
	Hostnames           []string // lowercase, no port, sorted on write
	Parties             []TenantParty
	EntitlementsVersion int64
	Entitlements        json.RawMessage // full entitlement document as received
	ConfigVersion       int64
	Config              json.RawMessage // full tenant config object as received
	Status              TenantStatus
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// OrgPathLabel returns the ltree label for an org: lowercase hex UUID without hyphens.
func OrgPathLabel(orgId string) string {
	return strings.ToLower(strings.ReplaceAll(orgId, "-", ""))
}

// NormalizeHost lowercases, strips a ":port" suffix and a trailing dot.
func NormalizeHost(host string) string {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.TrimSuffix(host, ".")
}

// TenantStore holds the tenants that the platform provisions. Org ids are UUIDs: they are
// accepted in any case and returned in the canonical lowercase form, and a value that is
// not a UUID is an error. Lookups return (nil, nil) when there is no such tenant.
type TenantStore interface {
	// PutTenant inserts or updates by OrgId. On insert Status is set to active; on update
	// Status is left unchanged. Entitlements/Config are replaced only when the incoming
	// version is >= the stored version (stale documents are ignored, not an error); an
	// empty document is stored as {}. Hostnames are normalized and replaced as a set.
	// Returns ErrTenantImmutable if Slug or OrgKey differ from the stored values,
	// ErrTenantConflict on a uniqueness clash with another tenant. The tenant passed in
	// is not modified.
	PutTenant(ctx context.Context, tenant *Tenant) error
	GetTenant(ctx context.Context, orgId string) (*Tenant, error)
	// GetTenantByHost normalizes host before the lookup.
	GetTenantByHost(ctx context.Context, host string) (*Tenant, error)
	GetTenantByOrgKey(ctx context.Context, orgKey string) (*Tenant, error)
	// ListTenants pages by OrgId ascending; afterOrgId "" starts at the beginning.
	ListTenants(ctx context.Context, afterOrgId string, limit int) ([]*Tenant, error)
	// ListSubtreeOrgIds returns orgId itself (if provisioned) and every provisioned tenant
	// whose path contains OrgPathLabel(orgId), ordered by org id. Works even if orgId
	// itself is not provisioned here (for example a partner org without cpms), because
	// labels are globally unique.
	ListSubtreeOrgIds(ctx context.Context, orgId string) ([]string, error)
	// SetTenantStatus returns (false, nil) if the tenant does not exist.
	SetTenantStatus(ctx context.Context, orgId string, status TenantStatus) (bool, error)
}
