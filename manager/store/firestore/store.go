// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"k8s.io/utils/clock"
)

type Store struct {
	client *firestore.Client
	clock  clock.PassiveClock
}

func NewStore(ctx context.Context, gcloudProject string, clock clock.PassiveClock) (store.Engine, error) {
	client, err := firestore.NewClient(ctx, gcloudProject)
	if err != nil {
		return nil, fmt.Errorf("create new firestore client in %s: %w", gcloudProject, err)
	}

	return &Store{
		client: client,
		clock:  clock,
	}, nil
}

// ChargeStationDataTransferStore stub implementations

func (s *Store) SetChargeStationDataTransfer(ctx context.Context, chargeStationId string, dataTransfer *store.ChargeStationDataTransfer) error {
	return fmt.Errorf("SetChargeStationDataTransfer: %w", store.ErrNotImplemented)
}

func (s *Store) LookupChargeStationDataTransfer(ctx context.Context, chargeStationId string) (*store.ChargeStationDataTransfer, error) {
	return nil, fmt.Errorf("LookupChargeStationDataTransfer: %w", store.ErrNotImplemented)
}

func (s *Store) ListChargeStationDataTransfers(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationDataTransfer, error) {
	return nil, fmt.Errorf("ListChargeStationDataTransfers: %w", store.ErrNotImplemented)
}

func (s *Store) DeleteChargeStationDataTransfer(ctx context.Context, chargeStationId string) error {
	return fmt.Errorf("DeleteChargeStationDataTransfer: %w", store.ErrNotImplemented)
}

// ChargeStationClearCacheStore stub implementations

func (s *Store) SetChargeStationClearCache(ctx context.Context, chargeStationId string, clearCache *store.ChargeStationClearCache) error {
	return fmt.Errorf("SetChargeStationClearCache: %w", store.ErrNotImplemented)
}

func (s *Store) LookupChargeStationClearCache(ctx context.Context, chargeStationId string) (*store.ChargeStationClearCache, error) {
	return nil, fmt.Errorf("LookupChargeStationClearCache: %w", store.ErrNotImplemented)
}

func (s *Store) ListChargeStationClearCaches(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationClearCache, error) {
	return nil, fmt.Errorf("ListChargeStationClearCaches: %w", store.ErrNotImplemented)
}

func (s *Store) DeleteChargeStationClearCache(ctx context.Context, chargeStationId string) error {
	return fmt.Errorf("DeleteChargeStationClearCache: %w", store.ErrNotImplemented)
}

// ChargeStationChangeAvailabilityStore stub implementations

func (s *Store) SetChargeStationChangeAvailability(ctx context.Context, chargeStationId string, changeAvailability *store.ChargeStationChangeAvailability) error {
	return fmt.Errorf("SetChargeStationChangeAvailability: %w", store.ErrNotImplemented)
}

func (s *Store) LookupChargeStationChangeAvailability(ctx context.Context, chargeStationId string) (*store.ChargeStationChangeAvailability, error) {
	return nil, fmt.Errorf("LookupChargeStationChangeAvailability: %w", store.ErrNotImplemented)
}

func (s *Store) ListChargeStationChangeAvailabilities(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationChangeAvailability, error) {
	return nil, fmt.Errorf("ListChargeStationChangeAvailabilities: %w", store.ErrNotImplemented)
}

func (s *Store) DeleteChargeStationChangeAvailability(ctx context.Context, chargeStationId string) error {
	return fmt.Errorf("DeleteChargeStationChangeAvailability: %w", store.ErrNotImplemented)
}

// QueryMeterValues stub implementation
func (s *Store) QueryMeterValues(ctx context.Context, filter store.MeterValuesFilter) (*store.MeterValuesResult, error) {
	return nil, fmt.Errorf("QueryMeterValues: %w", store.ErrNotImplemented)
}
