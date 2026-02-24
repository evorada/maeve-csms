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
	return fmt.Errorf("SetChargeStationDataTransfer not implemented for Firestore")
}

func (s *Store) LookupChargeStationDataTransfer(ctx context.Context, chargeStationId string) (*store.ChargeStationDataTransfer, error) {
	return nil, fmt.Errorf("LookupChargeStationDataTransfer not implemented for Firestore")
}

func (s *Store) ListChargeStationDataTransfers(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationDataTransfer, error) {
	return nil, fmt.Errorf("ListChargeStationDataTransfers not implemented for Firestore")
}

func (s *Store) DeleteChargeStationDataTransfer(ctx context.Context, chargeStationId string) error {
	return fmt.Errorf("DeleteChargeStationDataTransfer not implemented for Firestore")
}

// ChargeStationClearCacheStore stub implementations

func (s *Store) SetChargeStationClearCache(ctx context.Context, chargeStationId string, clearCache *store.ChargeStationClearCache) error {
	return fmt.Errorf("SetChargeStationClearCache not implemented for Firestore")
}

func (s *Store) LookupChargeStationClearCache(ctx context.Context, chargeStationId string) (*store.ChargeStationClearCache, error) {
	return nil, fmt.Errorf("LookupChargeStationClearCache not implemented for Firestore")
}

func (s *Store) ListChargeStationClearCaches(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationClearCache, error) {
	return nil, fmt.Errorf("ListChargeStationClearCaches not implemented for Firestore")
}

func (s *Store) DeleteChargeStationClearCache(ctx context.Context, chargeStationId string) error {
	return fmt.Errorf("DeleteChargeStationClearCache not implemented for Firestore")
}

// ChargeStationChangeAvailabilityStore stub implementations

func (s *Store) SetChargeStationChangeAvailability(ctx context.Context, chargeStationId string, changeAvailability *store.ChargeStationChangeAvailability) error {
	return fmt.Errorf("SetChargeStationChangeAvailability not implemented for Firestore")
}

func (s *Store) LookupChargeStationChangeAvailability(ctx context.Context, chargeStationId string) (*store.ChargeStationChangeAvailability, error) {
	return nil, fmt.Errorf("LookupChargeStationChangeAvailability not implemented for Firestore")
}

func (s *Store) ListChargeStationChangeAvailabilities(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationChangeAvailability, error) {
	return nil, fmt.Errorf("ListChargeStationChangeAvailabilities not implemented for Firestore")
}

func (s *Store) DeleteChargeStationChangeAvailability(ctx context.Context, chargeStationId string) error {
	return fmt.Errorf("DeleteChargeStationChangeAvailability not implemented for Firestore")
}

// QueryMeterValues stub implementation
func (s *Store) QueryMeterValues(ctx context.Context, filter store.MeterValuesFilter) (*store.MeterValuesResult, error) {
	return nil, fmt.Errorf("QueryMeterValues not implemented for Firestore")
}
