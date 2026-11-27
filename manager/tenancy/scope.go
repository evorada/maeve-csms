// SPDX-License-Identifier: Apache-2.0

package tenancy

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

var (
	// ErrOutOfScope is returned for an org that the caller may not reach. It is always
	// answered like something that does not exist (404).
	ErrOutOfScope = errors.New("resource outside caller scope")
	// ErrOrgRequired is returned when a caller that has no org of its own here must
	// name the org of what it creates.
	ErrOrgRequired = errors.New("orgId is required")
	// ErrTenantSuspended is returned for an org whose tenant is suspended or cancelled:
	// nothing may be created or changed for it through the API.
	ErrTenantSuspended = errors.New("tenant suspended")
)

// Scoper says which orgs a caller may reach and which org owns a charge station.
type Scoper struct {
	// Mode is ModeSingle when empty.
	Mode Mode
	// Tenants is only used in multi mode, for a caller with the scope subtree.
	Tenants store.TenantStore
	// Resolver gives the charge stations and the tenant of a station key.
	Resolver *Resolver
	// DefaultOrgId is the only org of single mode and the org of a charge station that
	// has none. Empty is the legacy org (store.LegacyOrgId).
	DefaultOrgId string
}

// OrgIds returns the orgs that the caller may reach, in lower case. It is empty for a
// caller that may reach nothing.
//
// Single mode: the default org when it is the org of the caller, otherwise nothing.
// Multi mode: the org of the caller with the scope self; the provisioned orgs of
// Tenants.ListSubtreeOrgIds with the scope subtree (the org of the caller is only part
// of it when it is a tenant here); nothing with any other scope.
//
// The answer is kept for the request when the context has a scope cache (see
// WithScopeCache). The slice must not be changed.
func (s *Scoper) OrgIds(ctx context.Context, p *Principal) ([]string, error) {
	if s == nil || p == nil {
		return nil, nil
	}
	cache, _ := ctx.Value(scopeCacheKey{}).(*scopeCache)
	if cache == nil {
		return s.orgIds(ctx, p)
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.principal == p {
		return cache.orgIds, nil
	}
	orgIds, err := s.orgIds(ctx, p)
	if err != nil {
		return nil, err
	}
	cache.principal, cache.orgIds = p, orgIds
	return orgIds, nil
}

func (s *Scoper) orgIds(ctx context.Context, p *Principal) ([]string, error) {
	orgId := normalizeOrgId(p.OrgId)
	if orgId == "" {
		return nil, nil
	}

	if s.Mode != ModeMulti {
		if orgId != s.defaultOrgId() {
			return nil, nil
		}
		return []string{orgId}, nil
	}

	switch p.OrgScope {
	case OrgScopeSelf:
		return []string{orgId}, nil
	case OrgScopeSubtree:
		if s.Tenants == nil {
			return nil, errors.New("no tenant store")
		}
		orgIds, err := s.Tenants.ListSubtreeOrgIds(ctx, orgId)
		if err != nil {
			return nil, fmt.Errorf("list orgs below %s: %w", orgId, err)
		}
		normalized := make([]string, 0, len(orgIds))
		for _, id := range orgIds {
			if id = normalizeOrgId(id); id != "" {
				normalized = append(normalized, id)
			}
		}
		return normalized, nil
	default:
		// a scope that is not known reaches nothing
		return nil, nil
	}
}

// InScope reports whether the caller may reach the org. No caller reaches the empty org.
func (s *Scoper) InScope(ctx context.Context, p *Principal, orgId string) (bool, error) {
	orgId = normalizeOrgId(orgId)
	if orgId == "" {
		return false, nil
	}
	orgIds, err := s.OrgIds(ctx, p)
	if err != nil {
		return false, err
	}
	return slices.Contains(orgIds, orgId), nil
}

// StationOrg returns the org that owns a station key and whether the charge station
// exists. A charge station that exists belongs to the org in its auth details, or to
// the default org when it has none (the legacy org counts as none). A charge station
// that does not exist yet would belong to the default org in single mode, and in multi
// mode to the tenant that has the org key that the station key starts with: the org is
// "" when there is no such tenant.
func (s *Scoper) StationOrg(ctx context.Context, stationKey string) (orgId string, exists bool, err error) {
	if s == nil || s.Resolver == nil || s.Resolver.Stations == nil {
		return "", false, errors.New("no charge station store")
	}
	auth, err := s.Resolver.Stations.LookupChargeStationAuth(ctx, stationKey)
	if err != nil {
		return "", false, fmt.Errorf("lookup org of charge station %s: %w", stationKey, err)
	}
	if auth != nil {
		return normalizeOrgId(StationOrgId(auth, s.defaultOrgId())), true, nil
	}

	if s.Mode != ModeMulti {
		return s.defaultOrgId(), false, nil
	}
	tenant, err := s.Resolver.TenantForStationKey(ctx, stationKey)
	if err != nil {
		return "", false, fmt.Errorf("lookup org of charge station %s: %w", stationKey, err)
	}
	if tenant == nil {
		return "", false, nil
	}
	return normalizeOrgId(tenant.OrgId), false, nil
}

// TargetOrg picks the org of something that the caller creates: the requested org, or
// the org of the caller when none is requested. It returns ErrOutOfScope when the
// caller may not reach that org, and ErrOrgRequired when no org is requested by a
// caller with the scope subtree whose own org is not a tenant here (multi mode): such
// a caller has to name one of the orgs below its own.
func (s *Scoper) TargetOrg(ctx context.Context, p *Principal, requested string) (string, error) {
	if s == nil || p == nil {
		return "", ErrOutOfScope
	}

	orgId := normalizeOrgId(requested)
	if orgId == "" {
		orgId = normalizeOrgId(p.OrgId)
	}
	inScope, err := s.InScope(ctx, p, orgId)
	if err != nil {
		return "", err
	}
	if inScope {
		return orgId, nil
	}
	if requested == "" && s.Mode == ModeMulti && p.OrgScope == OrgScopeSubtree {
		return "", ErrOrgRequired
	}
	return "", ErrOutOfScope
}

// CheckWritable returns ErrTenantSuspended when nothing may be created or changed for
// the org through the API, because its tenant is suspended or cancelled. An org that is
// not a tenant here is writable, and so is every org in single mode. Any other error
// means that the status of the tenant could not be read: the caller must not write then.
//
// It is only for what a caller of the API creates or changes. Reading, commands to a
// charge station and the messages of the charge stations themselves never depend on
// the status of a tenant.
//
// The status comes from the cache of the resolver: a change through the control plane
// of this manager is seen at once, a change through another instance after the cache
// TTL of the resolver.
func (s *Scoper) CheckWritable(ctx context.Context, orgId string) error {
	if s == nil {
		return errors.New("no scoper")
	}
	if s.Mode != ModeMulti {
		return nil
	}
	if s.Resolver == nil {
		return errors.New("no tenant resolver")
	}
	tenant, err := s.Resolver.TenantByOrgId(ctx, orgId)
	if err != nil {
		return err
	}
	if tenant == nil {
		return nil
	}
	switch tenant.Status {
	case store.TenantStatusSuspended, store.TenantStatusCancelled:
		return ErrTenantSuspended
	}
	return nil
}

func (s *Scoper) defaultOrgId() string {
	if s.DefaultOrgId == "" {
		return store.LegacyOrgId
	}
	return normalizeOrgId(s.DefaultOrgId)
}

// normalizeOrgId returns the form in which org ids are compared: a UUID in lower case.
func normalizeOrgId(orgId string) string {
	return strings.ToLower(orgId)
}

type scopeCacheKey struct{}

// scopeCache keeps the orgs that one caller may reach for the time of a request.
type scopeCache struct {
	mu        sync.Mutex
	principal *Principal
	orgIds    []string
}

// WithScopeCache returns a context in which Scoper.OrgIds asks the tenant store once
// for a caller. It is meant for the context of one request: the answer is not
// refreshed.
func WithScopeCache(ctx context.Context) context.Context {
	if _, ok := ctx.Value(scopeCacheKey{}).(*scopeCache); ok {
		return ctx
	}
	return context.WithValue(ctx, scopeCacheKey{}, &scopeCache{})
}

type stationOrgKey struct{}

// WithStationOrg returns a context that carries the org that owns the charge station
// of the request.
func WithStationOrg(ctx context.Context, orgId string) context.Context {
	return context.WithValue(ctx, stationOrgKey{}, orgId)
}

// StationOrgFromContext returns the org that owns the charge station of the request.
// It reports false when the org was not resolved for the request.
func StationOrgFromContext(ctx context.Context) (string, bool) {
	orgId, ok := ctx.Value(stationOrgKey{}).(string)
	if !ok || orgId == "" {
		return "", false
	}
	return orgId, true
}
