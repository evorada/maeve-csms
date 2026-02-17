// SPDX-License-Identifier: Apache-2.0

package inmemory

import (
	"context"
	"sort"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

type meterValueKey struct {
	chargeStationId string
	evseId          int
}

// StoreMeterValues stores meter values received from a charge station.
func (s *Store) StoreMeterValues(_ context.Context, chargeStationId string, evseId int, transactionId string, meterValues []store.MeterValue) error {
	s.Lock()
	defer s.Unlock()

	key := meterValueKey{
		chargeStationId: chargeStationId,
		evseId:          evseId,
	}

	if s.meterValues == nil {
		s.meterValues = make(map[meterValueKey][]store.StoredMeterValue)
	}

	// Append new meter values
	for _, mv := range meterValues {
		s.meterValues[key] = append(s.meterValues[key], store.StoredMeterValue{
			ChargeStationId: chargeStationId,
			EvseId:          evseId,
			TransactionId:   transactionId,
			MeterValue:      mv,
		})
	}

	// Sort by timestamp (descending)
	sort.Slice(s.meterValues[key], func(i, j int) bool {
		return s.meterValues[key][i].MeterValue.Timestamp > s.meterValues[key][j].MeterValue.Timestamp
	})

	return nil
}

// GetMeterValues retrieves meter values for a specific charge station and EVSE.
func (s *Store) GetMeterValues(_ context.Context, chargeStationId string, evseId int, limit int) ([]store.StoredMeterValue, error) {
	s.Lock()
	defer s.Unlock()

	key := meterValueKey{
		chargeStationId: chargeStationId,
		evseId:          evseId,
	}

	values := s.meterValues[key]
	if values == nil {
		return []store.StoredMeterValue{}, nil
	}

	if limit > 0 && len(values) > limit {
		return values[:limit], nil
	}

	return values, nil
}
