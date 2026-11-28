// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"
	"time"

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

func (s *Store) RecordOnboardingConnection(context.Context, string, string, string, string, int, time.Time) (bool, error) {
	return false, fmt.Errorf("RecordOnboardingConnection: %w", store.ErrNotImplemented)
}

func (s *Store) RecordOnboardingBoot(context.Context, string, time.Time) (*store.StationOnboarding, bool, error) {
	return nil, false, fmt.Errorf("RecordOnboardingBoot: %w", store.ErrNotImplemented)
}

func (s *Store) SetOnboarding(context.Context, string, *store.StationOnboarding, *string) error {
	return fmt.Errorf("SetOnboarding: %w", store.ErrNotImplemented)
}

func (s *Store) CompleteApproval(context.Context, string, *store.PendingApproval, *store.ChargeStationAuth, bool) error {
	return fmt.Errorf("CompleteApproval: %w", store.ErrNotImplemented)
}

func (s *Store) RejectChargeStation(context.Context, string, string) error {
	return fmt.Errorf("RejectChargeStation: %w", store.ErrNotImplemented)
}
