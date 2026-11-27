// SPDX-License-Identifier: Apache-2.0

package inmemory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Store) PutTenant(_ context.Context, tenant *store.Tenant) error {
	t, err := tenantToStore(tenant)
	if err != nil {
		return err
	}

	s.Lock()
	defer s.Unlock()

	existing := s.tenants[t.OrgId]
	if existing != nil && (existing.Slug != t.Slug || existing.OrgKey != t.OrgKey) {
		return fmt.Errorf("put tenant %s: %w", t.OrgId, store.ErrTenantImmutable)
	}
	if owner, found := s.tenantsBySlug[t.Slug]; found && owner != t.OrgId {
		return fmt.Errorf("put tenant %s: slug: %w", t.OrgId, store.ErrTenantConflict)
	}
	if owner, found := s.tenantsByOrgKey[t.OrgKey]; found && owner != t.OrgId {
		return fmt.Errorf("put tenant %s: org key: %w", t.OrgId, store.ErrTenantConflict)
	}
	for _, hostname := range t.Hostnames {
		if owner, found := s.tenantsByHost[hostname]; found && owner != t.OrgId {
			return fmt.Errorf("put tenant %s: hostname %s: %w", t.OrgId, hostname, store.ErrTenantConflict)
		}
	}

	now := s.clock.Now().UTC()
	t.Status = store.TenantStatusActive
	t.CreatedAt = now
	t.UpdatedAt = now
	if existing != nil {
		t.Status = existing.Status
		t.CreatedAt = existing.CreatedAt
		// a document that is older than the stored one is ignored
		if t.EntitlementsVersion < existing.EntitlementsVersion {
			t.EntitlementsVersion = existing.EntitlementsVersion
			t.Entitlements = existing.Entitlements
		}
		if t.ConfigVersion < existing.ConfigVersion {
			t.ConfigVersion = existing.ConfigVersion
			t.Config = existing.Config
		}
		for _, hostname := range existing.Hostnames {
			delete(s.tenantsByHost, hostname)
		}
	}

	s.tenants[t.OrgId] = t
	s.tenantsBySlug[t.Slug] = t.OrgId
	s.tenantsByOrgKey[t.OrgKey] = t.OrgId
	for _, hostname := range t.Hostnames {
		s.tenantsByHost[hostname] = t.OrgId
	}
	return nil
}

func (s *Store) GetTenant(_ context.Context, orgId string) (*store.Tenant, error) {
	id, err := canonicalOrgId(orgId)
	if err != nil {
		return nil, fmt.Errorf("get tenant: %w", err)
	}

	s.Lock()
	defer s.Unlock()
	return cloneTenant(s.tenants[id]), nil
}

func (s *Store) GetTenantByHost(_ context.Context, host string) (*store.Tenant, error) {
	s.Lock()
	defer s.Unlock()
	return cloneTenant(s.tenants[s.tenantsByHost[store.NormalizeHost(host)]]), nil
}

func (s *Store) GetTenantByOrgKey(_ context.Context, orgKey string) (*store.Tenant, error) {
	s.Lock()
	defer s.Unlock()
	return cloneTenant(s.tenants[s.tenantsByOrgKey[orgKey]]), nil
}

func (s *Store) ListTenants(_ context.Context, afterOrgId string, limit int) ([]*store.Tenant, error) {
	after := ""
	if afterOrgId != "" {
		var err error
		after, err = canonicalOrgId(afterOrgId)
		if err != nil {
			return nil, fmt.Errorf("list tenants: %w", err)
		}
	}

	s.Lock()
	defer s.Unlock()

	// canonical UUIDs sort as text in the order of their bytes
	orgIds := make([]string, 0, len(s.tenants))
	for orgId := range s.tenants {
		if afterOrgId == "" || orgId > after {
			orgIds = append(orgIds, orgId)
		}
	}
	sort.Strings(orgIds)

	tenants := make([]*store.Tenant, 0)
	for _, orgId := range orgIds {
		if len(tenants) >= limit {
			break
		}
		tenants = append(tenants, cloneTenant(s.tenants[orgId]))
	}
	return tenants, nil
}

func (s *Store) ListSubtreeOrgIds(_ context.Context, orgId string) ([]string, error) {
	id, err := canonicalOrgId(orgId)
	if err != nil {
		return nil, fmt.Errorf("list subtree org ids: %w", err)
	}
	label := store.OrgPathLabel(id)

	s.Lock()
	defer s.Unlock()

	orgIds := make([]string, 0)
	for _, tenant := range s.tenants {
		if tenant.OrgId == id || pathHasLabel(tenant.Path, label) {
			orgIds = append(orgIds, tenant.OrgId)
		}
	}
	sort.Strings(orgIds)
	return orgIds, nil
}

func (s *Store) SetTenantStatus(_ context.Context, orgId string, status store.TenantStatus) (bool, error) {
	id, err := canonicalOrgId(orgId)
	if err != nil {
		return false, fmt.Errorf("set tenant status: %w", err)
	}
	switch status {
	case store.TenantStatusActive, store.TenantStatusSuspended, store.TenantStatusCancelled:
	default:
		return false, fmt.Errorf("set tenant status of %s: invalid status %q", id, status)
	}

	s.Lock()
	defer s.Unlock()

	tenant := s.tenants[id]
	if tenant == nil {
		return false, nil
	}
	tenant.Status = status
	tenant.UpdatedAt = s.clock.Now().UTC()
	return true, nil
}

// canonicalOrgId returns the lowercase text form of an org id, which must be a UUID.
func canonicalOrgId(orgId string) (string, error) {
	id, err := uuid.Parse(orgId)
	if err != nil {
		return "", fmt.Errorf("invalid org id %q: %w", orgId, err)
	}
	return id.String(), nil
}

func pathHasLabel(path, label string) bool {
	for _, pathLabel := range strings.Split(path, ".") {
		if pathLabel == label {
			return true
		}
	}
	return false
}

// tenantToStore returns the copy of a tenant that the store keeps: canonical ids,
// normalized hostnames (unique, sorted) and {} for an empty document. The status and
// the timestamps are set by PutTenant.
func tenantToStore(tenant *store.Tenant) (*store.Tenant, error) {
	orgId, err := canonicalOrgId(tenant.OrgId)
	if err != nil {
		return nil, fmt.Errorf("put tenant: %w", err)
	}
	t := &store.Tenant{
		OrgId:               orgId,
		Path:                tenant.Path,
		Slug:                tenant.Slug,
		OrgKey:              tenant.OrgKey,
		Hostnames:           make([]string, 0, len(tenant.Hostnames)),
		Parties:             append([]store.TenantParty{}, tenant.Parties...),
		EntitlementsVersion: tenant.EntitlementsVersion,
		ConfigVersion:       tenant.ConfigVersion,
	}
	if tenant.ParentId != nil {
		parentId, err := uuid.Parse(*tenant.ParentId)
		if err != nil {
			return nil, fmt.Errorf("put tenant %s: invalid parent id %q: %w", orgId, *tenant.ParentId, err)
		}
		canonical := parentId.String()
		t.ParentId = &canonical
	}
	if t.Entitlements, err = tenantDocument(tenant.Entitlements); err != nil {
		return nil, fmt.Errorf("put tenant %s: entitlements: %w", orgId, err)
	}
	if t.Config, err = tenantDocument(tenant.Config); err != nil {
		return nil, fmt.Errorf("put tenant %s: config: %w", orgId, err)
	}

	seen := make(map[string]bool, len(tenant.Hostnames))
	for _, hostname := range tenant.Hostnames {
		hostname = store.NormalizeHost(hostname)
		if hostname == "" || seen[hostname] {
			continue
		}
		seen[hostname] = true
		t.Hostnames = append(t.Hostnames, hostname)
	}
	sort.Strings(t.Hostnames)
	return t, nil
}

// tenantDocument returns a copy of the JSON document to store: {} for an empty one.
func tenantDocument(document json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(document)) == 0 {
		return json.RawMessage("{}"), nil
	}
	if !json.Valid(document) {
		return nil, errors.New("not a valid JSON document")
	}
	return bytes.Clone(document), nil
}

// cloneTenant returns a deep copy, so that callers cannot change what is stored.
func cloneTenant(tenant *store.Tenant) *store.Tenant {
	if tenant == nil {
		return nil
	}
	clone := *tenant
	if tenant.ParentId != nil {
		parentId := *tenant.ParentId
		clone.ParentId = &parentId
	}
	clone.Hostnames = append([]string{}, tenant.Hostnames...)
	clone.Parties = append([]store.TenantParty{}, tenant.Parties...)
	clone.Entitlements = bytes.Clone(tenant.Entitlements)
	clone.Config = bytes.Clone(tenant.Config)
	return &clone
}
