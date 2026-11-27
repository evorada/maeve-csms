// SPDX-License-Identifier: Apache-2.0

package tenancy

import (
	"context"
	"slices"
	"strings"
)

// OrgScope says how far below its org a caller may reach.
type OrgScope string

const (
	// OrgScopeSelf limits the caller to its own org.
	OrgScopeSelf OrgScope = "self"
	// OrgScopeSubtree lets the caller reach its org and the orgs below it.
	OrgScopeSubtree OrgScope = "subtree"
)

// servicePrefix starts the subject of a service of the console.
const servicePrefix = "svc:"

// Principal is the caller of a request, as stated by a token that the console minted
// and that the manager verified.
type Principal struct {
	Subject  string // "user:…", "machine:…" or "svc:…"
	Actor    string // who acts as the subject (act.sub), "" when nobody does
	OrgId    string // UUID
	OrgScope OrgScope
	Perms    []string
	Jti      string
}

// Has reports whether the caller was given the permission.
func (p *Principal) Has(perm string) bool {
	if p == nil {
		return false
	}
	return slices.Contains(p.Perms, perm)
}

// IsService reports whether the caller is a service of the console and not a person
// or a machine of a tenant.
func (p *Principal) IsService() bool {
	if p == nil {
		return false
	}
	return strings.HasPrefix(p.Subject, servicePrefix)
}

type principalKey struct{}

// WithPrincipal returns a context that carries the caller of the request.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext returns the caller that Middleware verified for the request. It reports
// false when the request was not authenticated.
func FromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(*Principal)
	if !ok || p == nil {
		return nil, false
	}
	return p, true
}
