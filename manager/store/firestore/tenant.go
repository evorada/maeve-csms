// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// TenantStore is not implemented by the Firestore backend.

func (s *Store) PutTenant(_ context.Context, _ *store.Tenant) error {
	return fmt.Errorf("PutTenant: %w", store.ErrNotImplemented)
}

func (s *Store) GetTenant(_ context.Context, _ string) (*store.Tenant, error) {
	return nil, fmt.Errorf("GetTenant: %w", store.ErrNotImplemented)
}

func (s *Store) GetTenantByHost(_ context.Context, _ string) (*store.Tenant, error) {
	return nil, fmt.Errorf("GetTenantByHost: %w", store.ErrNotImplemented)
}

func (s *Store) GetTenantByOrgKey(_ context.Context, _ string) (*store.Tenant, error) {
	return nil, fmt.Errorf("GetTenantByOrgKey: %w", store.ErrNotImplemented)
}

func (s *Store) ListTenants(_ context.Context, _ string, _ int) ([]*store.Tenant, error) {
	return nil, fmt.Errorf("ListTenants: %w", store.ErrNotImplemented)
}

func (s *Store) ListSubtreeOrgIds(_ context.Context, _ string) ([]string, error) {
	return nil, fmt.Errorf("ListSubtreeOrgIds: %w", store.ErrNotImplemented)
}

func (s *Store) SetTenantStatus(_ context.Context, _ string, _ store.TenantStatus) (bool, error) {
	return false, fmt.Errorf("SetTenantStatus: %w", store.ErrNotImplemented)
}
