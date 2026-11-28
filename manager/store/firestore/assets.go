// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

var _ store.Engine = (*Store)(nil)

// The Firestore backend has no native asset model (locations, EVSEs, connectors).

func (s *Store) CreateAssetLocation(context.Context, *store.AssetLocation) error {
	return fmt.Errorf("CreateAssetLocation: %w", store.ErrNotImplemented)
}

func (s *Store) UpdateAssetLocation(context.Context, *store.AssetLocation) error {
	return fmt.Errorf("UpdateAssetLocation: %w", store.ErrNotImplemented)
}

func (s *Store) LookupAssetLocation(context.Context, string) (*store.AssetLocation, error) {
	return nil, fmt.Errorf("LookupAssetLocation: %w", store.ErrNotImplemented)
}

func (s *Store) LookupAssetLocationByOcpiId(context.Context, string, string) (*store.AssetLocation, error) {
	return nil, fmt.Errorf("LookupAssetLocationByOcpiId: %w", store.ErrNotImplemented)
}

func (s *Store) ListAssetLocations(context.Context, store.AssetLocationFilter) ([]*store.AssetLocation, string, error) {
	return nil, "", fmt.Errorf("ListAssetLocations: %w", store.ErrNotImplemented)
}

func (s *Store) DeleteAssetLocation(context.Context, string) error {
	return fmt.Errorf("DeleteAssetLocation: %w", store.ErrNotImplemented)
}

func (s *Store) CreateAssetEvse(context.Context, *store.AssetEvse) error {
	return fmt.Errorf("CreateAssetEvse: %w", store.ErrNotImplemented)
}

func (s *Store) UpdateAssetEvse(context.Context, *store.AssetEvse) error {
	return fmt.Errorf("UpdateAssetEvse: %w", store.ErrNotImplemented)
}

func (s *Store) LookupAssetEvse(context.Context, string) (*store.AssetEvse, error) {
	return nil, fmt.Errorf("LookupAssetEvse: %w", store.ErrNotImplemented)
}

func (s *Store) LookupAssetEvseByOcpp(context.Context, string, int) (*store.AssetEvse, error) {
	return nil, fmt.Errorf("LookupAssetEvseByOcpp: %w", store.ErrNotImplemented)
}

func (s *Store) ListAssetEvsesForLocation(context.Context, string) ([]*store.AssetEvse, error) {
	return nil, fmt.Errorf("ListAssetEvsesForLocation: %w", store.ErrNotImplemented)
}

func (s *Store) ListAssetEvsesForStation(context.Context, string) ([]*store.AssetEvse, error) {
	return nil, fmt.Errorf("ListAssetEvsesForStation: %w", store.ErrNotImplemented)
}

func (s *Store) DeleteAssetEvse(context.Context, string) error {
	return fmt.Errorf("DeleteAssetEvse: %w", store.ErrNotImplemented)
}

func (s *Store) CreateAssetConnector(context.Context, *store.AssetConnector) error {
	return fmt.Errorf("CreateAssetConnector: %w", store.ErrNotImplemented)
}

func (s *Store) UpdateAssetConnector(context.Context, *store.AssetConnector) error {
	return fmt.Errorf("UpdateAssetConnector: %w", store.ErrNotImplemented)
}

func (s *Store) LookupAssetConnector(context.Context, string) (*store.AssetConnector, error) {
	return nil, fmt.Errorf("LookupAssetConnector: %w", store.ErrNotImplemented)
}

func (s *Store) LookupAssetConnectorByOcpp(context.Context, string, int, int) (*store.AssetConnector, error) {
	return nil, fmt.Errorf("LookupAssetConnectorByOcpp: %w", store.ErrNotImplemented)
}

func (s *Store) DeleteAssetConnector(context.Context, string) error {
	return fmt.Errorf("DeleteAssetConnector: %w", store.ErrNotImplemented)
}

func (s *Store) MoveChargeStationToLocation(context.Context, string, string) error {
	return fmt.Errorf("MoveChargeStationToLocation: %w", store.ErrNotImplemented)
}

func (s *Store) DecommissionChargeStation(context.Context, string, *string) error {
	return fmt.Errorf("DecommissionChargeStation: %w", store.ErrNotImplemented)
}

func (s *Store) TransferChargeStation(context.Context, store.TransferParams) (*store.TransferResult, error) {
	return nil, fmt.Errorf("TransferChargeStation: %w", store.ErrNotImplemented)
}
