// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The Firestore backend has no charge station registry: its stations are only the auth
// details and the runtime details (OCPP version).

func (s *Store) LookupChargeStation(context.Context, string) (*store.ChargeStation, error) {
	return nil, fmt.Errorf("LookupChargeStation: %w", store.ErrNotImplemented)
}

func (s *Store) UpdateChargeStation(context.Context, *store.ChargeStation) error {
	return fmt.Errorf("UpdateChargeStation: %w", store.ErrNotImplemented)
}

func (s *Store) SetChargeStationState(context.Context, string, store.StationState, *string) error {
	return fmt.Errorf("SetChargeStationState: %w", store.ErrNotImplemented)
}

func (s *Store) ListChargeStations(context.Context, store.ChargeStationFilter) ([]*store.ChargeStation, string, error) {
	return nil, "", fmt.Errorf("ListChargeStations: %w", store.ErrNotImplemented)
}

func (s *Store) CountChargeStations(context.Context, string, ...store.StationState) (int, error) {
	return 0, fmt.Errorf("CountChargeStations: %w", store.ErrNotImplemented)
}
