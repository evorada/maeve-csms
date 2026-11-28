// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

const (
	uniqueViolationCode = "23505"
	tenantsPrimaryKey   = "tenants_pkey"
)

// tenantUniqueConstraints are the constraints that stop two tenants from sharing a slug,
// an org key or a hostname.
var tenantUniqueConstraints = map[string]bool{
	"tenants_slug_unique":    true,
	"tenants_org_key_unique": true,
	"tenant_hostnames_pkey":  true,
}

// PutTenant inserts or updates a tenant and replaces its hostnames, in one transaction.
func (s *Store) PutTenant(ctx context.Context, tenant *store.Tenant) error {
	orgId, err := pgUUID(tenant.OrgId)
	if err != nil {
		return fmt.Errorf("put tenant: invalid org id %q: %w", tenant.OrgId, err)
	}
	var parentId pgtype.UUID
	if tenant.ParentId != nil {
		parentId, err = pgUUID(*tenant.ParentId)
		if err != nil {
			return fmt.Errorf("put tenant %s: invalid parent id %q: %w", uuidString(orgId), *tenant.ParentId, err)
		}
	}
	parties := tenant.Parties
	if parties == nil {
		parties = []store.TenantParty{}
	}
	partiesJson, err := json.Marshal(parties)
	if err != nil {
		return fmt.Errorf("put tenant %s: marshal parties: %w", uuidString(orgId), err)
	}
	entitlements, err := tenantDocument(tenant.Entitlements)
	if err != nil {
		return fmt.Errorf("put tenant %s: entitlements: %w", uuidString(orgId), err)
	}
	config, err := tenantDocument(tenant.Config)
	if err != nil {
		return fmt.Errorf("put tenant %s: config: %w", uuidString(orgId), err)
	}

	put := tenantPut{
		insert: InsertTenantParams{
			OrgID:               orgId,
			ParentID:            parentId,
			Path:                tenant.Path,
			Slug:                tenant.Slug,
			OrgKey:              tenant.OrgKey,
			Parties:             partiesJson,
			EntitlementsVersion: tenant.EntitlementsVersion,
			Entitlements:        entitlements,
			ConfigVersion:       tenant.ConfigVersion,
			Config:              config,
		},
		hostnames: tenantHostnames(tenant.Hostnames),
	}

	err = s.putTenant(ctx, put)
	if isUniqueViolation(err, tenantsPrimaryKey) {
		// Another caller inserted the same tenant after this one looked for it (for
		// example two instances that provision the same tenant when they start). The
		// row is there now, so the second attempt updates it.
		err = s.putTenant(ctx, put)
	}
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode && tenantUniqueConstraints[pgErr.ConstraintName] {
		return fmt.Errorf("put tenant %s: %s: %w", uuidString(orgId), pgErr.ConstraintName, store.ErrTenantConflict)
	}
	return fmt.Errorf("put tenant %s: %w", uuidString(orgId), err)
}

type tenantPut struct {
	insert    InsertTenantParams
	hostnames []string // normalized, unique, sorted
}

func (s *Store) putTenant(ctx context.Context, put tenantPut) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := New(tx)

	// The row lock makes concurrent puts of one tenant run one after the other.
	existing, err := qtx.GetTenantIdentityForUpdate(ctx, put.insert.OrgID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// status, created_at and updated_at take their column defaults
		if err := qtx.InsertTenant(ctx, put.insert); err != nil {
			return fmt.Errorf("insert: %w", err)
		}
	case err != nil:
		return fmt.Errorf("lock: %w", err)
	case existing.Slug != put.insert.Slug || existing.OrgKey != put.insert.OrgKey:
		return store.ErrTenantImmutable
	default:
		if err := qtx.UpdateTenant(ctx, UpdateTenantParams{
			OrgID:               put.insert.OrgID,
			ParentID:            put.insert.ParentID,
			Path:                put.insert.Path,
			Parties:             put.insert.Parties,
			EntitlementsVersion: put.insert.EntitlementsVersion,
			Entitlements:        put.insert.Entitlements,
			ConfigVersion:       put.insert.ConfigVersion,
			Config:              put.insert.Config,
		}); err != nil {
			return fmt.Errorf("update: %w", err)
		}
	}

	if err := qtx.DeleteTenantHostnames(ctx, put.insert.OrgID); err != nil {
		return fmt.Errorf("delete hostnames: %w", err)
	}
	// The hostnames are sorted, so two tenants that claim the same hostnames at the same
	// time wait for each other in the same order instead of deadlocking.
	for _, hostname := range put.hostnames {
		if err := qtx.InsertTenantHostname(ctx, InsertTenantHostnameParams{
			Hostname: hostname,
			OrgID:    put.insert.OrgID,
		}); err != nil {
			return fmt.Errorf("insert hostname %s: %w", hostname, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (s *Store) GetTenant(ctx context.Context, orgId string) (*store.Tenant, error) {
	id, err := pgUUID(orgId)
	if err != nil {
		return nil, fmt.Errorf("get tenant: invalid org id %q: %w", orgId, err)
	}
	row, err := s.readQueries(ctx).GetTenant(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get tenant %s: %w", orgId, err)
	}
	return tenantFromRow(row)
}

func (s *Store) GetTenantByHost(ctx context.Context, host string) (*store.Tenant, error) {
	row, err := s.readQueries(ctx).GetTenantByHost(ctx, store.NormalizeHost(host))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get tenant by host %s: %w", host, err)
	}
	return tenantFromRow(GetTenantRow(row))
}

func (s *Store) GetTenantByOrgKey(ctx context.Context, orgKey string) (*store.Tenant, error) {
	row, err := s.readQueries(ctx).GetTenantByOrgKey(ctx, orgKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get tenant by org key %s: %w", orgKey, err)
	}
	return tenantFromRow(GetTenantRow(row))
}

func (s *Store) ListTenants(ctx context.Context, afterOrgId string, limit int) ([]*store.Tenant, error) {
	var after pgtype.UUID // NULL: start at the beginning
	if afterOrgId != "" {
		var err error
		after, err = pgUUID(afterOrgId)
		if err != nil {
			return nil, fmt.Errorf("list tenants: invalid org id %q: %w", afterOrgId, err)
		}
	}
	if limit <= 0 {
		return []*store.Tenant{}, nil
	}
	pageSize, err := safeIntToInt32(limit)
	if err != nil {
		return nil, fmt.Errorf("list tenants: limit: %w", err)
	}

	rows, err := s.readQueries(ctx).ListTenants(ctx, ListTenantsParams{AfterOrgID: after, PageSize: pageSize})
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	tenants := make([]*store.Tenant, 0, len(rows))
	for _, row := range rows {
		tenant, err := tenantFromRow(GetTenantRow(row))
		if err != nil {
			return nil, err
		}
		tenants = append(tenants, tenant)
	}
	return tenants, nil
}

func (s *Store) ListSubtreeOrgIds(ctx context.Context, orgId string) ([]string, error) {
	id, err := pgUUID(orgId)
	if err != nil {
		return nil, fmt.Errorf("list subtree org ids: invalid org id %q: %w", orgId, err)
	}
	// The label comes from a parsed UUID, so it cannot carry lquery syntax.
	rows, err := s.readQueries(ctx).ListSubtreeOrgIds(ctx, ListSubtreeOrgIdsParams{
		OrgID:  id,
		Lquery: "*." + store.OrgPathLabel(uuidString(id)) + ".*",
	})
	if err != nil {
		return nil, fmt.Errorf("list subtree org ids of %s: %w", orgId, err)
	}
	orgIds := make([]string, 0, len(rows))
	for _, row := range rows {
		orgIds = append(orgIds, uuidString(row))
	}
	return orgIds, nil
}

func (s *Store) SetTenantStatus(ctx context.Context, orgId string, status store.TenantStatus) (bool, error) {
	id, err := pgUUID(orgId)
	if err != nil {
		return false, fmt.Errorf("set tenant status: invalid org id %q: %w", orgId, err)
	}
	switch status {
	case store.TenantStatusActive, store.TenantStatusSuspended, store.TenantStatusCancelled:
	default:
		return false, fmt.Errorf("set tenant status of %s: invalid status %q", orgId, status)
	}
	rows, err := s.writeQueries(ctx).SetTenantStatus(ctx, SetTenantStatusParams{OrgID: id, Status: string(status)})
	if err != nil {
		return false, fmt.Errorf("set tenant status of %s: %w", orgId, err)
	}
	return rows > 0, nil
}

// isUniqueViolation reports whether err is a violation of the named unique constraint.
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode && pgErr.ConstraintName == constraint
}

// tenantDocument returns the JSON document to store: {} for an empty one.
func tenantDocument(document json.RawMessage) ([]byte, error) {
	if len(bytes.TrimSpace(document)) == 0 {
		return []byte("{}"), nil
	}
	if !json.Valid(document) {
		return nil, errors.New("not a valid JSON document")
	}
	return document, nil
}

// tenantHostnames returns the normalized hostnames, without duplicates, sorted.
func tenantHostnames(hostnames []string) []string {
	seen := make(map[string]bool, len(hostnames))
	normalized := make([]string, 0, len(hostnames))
	for _, hostname := range hostnames {
		hostname = store.NormalizeHost(hostname)
		if hostname == "" || seen[hostname] {
			continue
		}
		seen[hostname] = true
		normalized = append(normalized, hostname)
	}
	sort.Strings(normalized)
	return normalized
}

func tenantFromRow(row GetTenantRow) (*store.Tenant, error) {
	tenant := &store.Tenant{
		OrgId:               uuidString(row.OrgID),
		Path:                row.Path,
		Slug:                row.Slug,
		OrgKey:              row.OrgKey,
		Hostnames:           row.Hostnames,
		Parties:             []store.TenantParty{},
		EntitlementsVersion: row.EntitlementsVersion,
		Entitlements:        row.Entitlements,
		ConfigVersion:       row.ConfigVersion,
		Config:              row.Config,
		Status:              store.TenantStatus(row.Status),
		CreatedAt:           fromPgTimestamptz(row.CreatedAt),
		UpdatedAt:           fromPgTimestamptz(row.UpdatedAt),
	}
	if row.ParentID.Valid {
		parentId := uuidString(row.ParentID)
		tenant.ParentId = &parentId
	}
	if tenant.Hostnames == nil {
		tenant.Hostnames = []string{}
	}
	if err := json.Unmarshal(row.Parties, &tenant.Parties); err != nil {
		return nil, fmt.Errorf("tenant %s: unmarshal parties: %w", tenant.OrgId, err)
	}
	return tenant, nil
}
