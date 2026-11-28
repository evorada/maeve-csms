// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The Firestore backend has no tariffs.

func (s *Store) CreateTariff(context.Context, *store.Tariff, *store.TariffVersion) error {
	return fmt.Errorf("CreateTariff: %w", store.ErrNotImplemented)
}

func (s *Store) AddTariffVersion(context.Context, string, int, *store.TariffVersion) error {
	return fmt.Errorf("AddTariffVersion: %w", store.ErrNotImplemented)
}

func (s *Store) RenameTariff(context.Context, string, string) error {
	return fmt.Errorf("RenameTariff: %w", store.ErrNotImplemented)
}

func (s *Store) LookupTariff(context.Context, string) (*store.Tariff, error) {
	return nil, fmt.Errorf("LookupTariff: %w", store.ErrNotImplemented)
}

func (s *Store) LookupTariffByOcpiId(context.Context, string, string) (*store.Tariff, error) {
	return nil, fmt.Errorf("LookupTariffByOcpiId: %w", store.ErrNotImplemented)
}

func (s *Store) LookupTariffVersion(context.Context, string, int) (*store.TariffVersion, error) {
	return nil, fmt.Errorf("LookupTariffVersion: %w", store.ErrNotImplemented)
}

func (s *Store) ListTariffs(context.Context, store.TariffFilter) ([]*store.Tariff, string, error) {
	return nil, "", fmt.Errorf("ListTariffs: %w", store.ErrNotImplemented)
}

func (s *Store) ListTariffVersions(context.Context, string) ([]*store.TariffVersion, error) {
	return nil, fmt.Errorf("ListTariffVersions: %w", store.ErrNotImplemented)
}

func (s *Store) DeleteTariff(context.Context, string) error {
	return fmt.Errorf("DeleteTariff: %w", store.ErrNotImplemented)
}
