// SPDX-License-Identifier: Apache-2.0

package entitlements

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
)

// errorLimitExceeded starts the text of every LimitExceededError: the error code that
// the callers of the API see.
const errorLimitExceeded = "entitlement_limit_exceeded"

// LimitExceededError says that creating what was asked for would go beyond a hard limit.
type LimitExceededError struct {
	// Key is the limit, for example LimitConnectorsMax.
	Key string
	// Limit is the value of the limit, Current what exists in the orgs that share it
	// and Requested what was to be added.
	Limit, Current, Requested int64
}

func (e *LimitExceededError) Error() string {
	return fmt.Sprintf("%s: %s (limit %d, current %d, requested %d)", errorLimitExceeded, e.Key, e.Limit, e.Current, e.Requested)
}

// ConnectorCounter counts the connectors of the charge stations of the given orgs. A
// store.StatusStore is one.
type ConnectorCounter interface {
	CountConnectorsForOrgs(ctx context.Context, orgIds []string) (int, error)
}

// Enforcer checks the hard limits of the entitlements of a tenant. A nil Enforcer
// limits nothing.
type Enforcer struct {
	// Tenants gives the entitlement document of an org and the tenants below an org.
	Tenants store.TenantStore
	// Counter says how much of a limit is used.
	Counter ConnectorCounter
	// Mode is ModeSingle when empty. Nothing is limited in single mode.
	Mode tenancy.Mode
}

// CheckConnectors returns a *LimitExceededError when adding additional connectors to
// the org would go beyond a hard cpms.connectors.max of its tenant, and nil when they
// may be added. Any other error means that the check could not be made: the caller
// must add nothing then. The package comment says what is allowed when the limit
// cannot be read.
//
// The entitlement document is read from the tenant store at every call, so a new limit
// applies at once on every instance of the manager.
func (e *Enforcer) CheckConnectors(ctx context.Context, orgId string, additional int) error {
	if e == nil || e.Mode != tenancy.ModeMulti || additional <= 0 {
		return nil
	}
	if e.Tenants == nil || e.Counter == nil {
		return errors.New("entitlements: no tenant store or no connector counter")
	}

	tenant, err := e.Tenants.GetTenant(ctx, orgId)
	if errors.Is(err, store.ErrNotImplemented) {
		slog.Warn("the storage backend has no tenants, no limit is enforced", "org", orgId)
		return nil
	}
	if err != nil {
		return fmt.Errorf("lookup tenant of org %s: %w", orgId, err)
	}
	if tenant == nil {
		return nil
	}

	doc, err := Parse(tenant.Entitlements)
	if err != nil {
		slog.Error("entitlements cannot be read, no limit is enforced",
			"org", tenant.OrgId, "entitlementsVersion", tenant.EntitlementsVersion, "err", err)
		return nil
	}
	limit, ok := doc.Limits[LimitConnectorsMax]
	if !ok || limit.Enforcement != EnforcementHard {
		return nil
	}
	if err := limit.validate(); err != nil {
		slog.Error("limit cannot be applied, it is not enforced", "limit", LimitConnectorsMax,
			"org", tenant.OrgId, "entitlementsVersion", tenant.EntitlementsVersion, "err", err)
		return nil
	}

	current, err := e.countConnectors(ctx, tenant.OrgId, limit)
	if errors.Is(err, store.ErrNotImplemented) {
		slog.Warn("the storage backend cannot count connectors, the limit is not enforced",
			"limit", LimitConnectorsMax, "org", tenant.OrgId)
		return nil
	}
	if err != nil {
		return err
	}

	if int64(current)+int64(additional) > limit.Value {
		return &LimitExceededError{
			Key:       LimitConnectorsMax,
			Limit:     limit.Value,
			Current:   int64(current),
			Requested: int64(additional),
		}
	}
	return nil
}

// countConnectors counts the connectors of the orgs that share the limit of the org.
func (e *Enforcer) countConnectors(ctx context.Context, orgId string, limit Limit) (int, error) {
	orgIds := []string{orgId}
	switch limit.Scope {
	case ScopeOrg:
	case ScopeSubtree:
		pool := orgId
		if limit.PoolOrgId != "" {
			pool = limit.PoolOrgId
		}
		var err error
		orgIds, err = e.Tenants.ListSubtreeOrgIds(ctx, pool)
		if err != nil {
			return 0, fmt.Errorf("list orgs that share %s below %s: %w", LimitConnectorsMax, pool, err)
		}
	default:
		slog.Warn("limit has an unknown scope, counting the org alone",
			"limit", LimitConnectorsMax, "org", orgId, "scope", limit.Scope)
	}

	current, err := e.Counter.CountConnectorsForOrgs(ctx, orgIds)
	if err != nil {
		return 0, fmt.Errorf("count connectors for %s of org %s: %w", LimitConnectorsMax, orgId, err)
	}
	return current, nil
}

// validate says why a limit cannot be applied as it is written; nil when it can.
func (l Limit) validate() error {
	if l.Value < 0 {
		return fmt.Errorf("value %d is negative", l.Value)
	}
	if l.Scope == ScopeSubtree && l.PoolOrgId != "" {
		// the tenant store takes nothing else for an org id
		if _, err := uuid.Parse(l.PoolOrgId); err != nil {
			return fmt.Errorf("pool_org_id %q is not a UUID", l.PoolOrgId)
		}
	}
	return nil
}
