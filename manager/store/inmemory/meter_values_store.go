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

// QueryMeterValues retrieves meter values with advanced filtering and pagination.
func (s *Store) QueryMeterValues(_ context.Context, filter store.MeterValuesFilter) (*store.MeterValuesResult, error) {
	s.Lock()
	defer s.Unlock()

	var allValues []store.StoredMeterValue

	// Collect all meter values for the charge station
	for key, values := range s.meterValues {
		if key.chargeStationId != filter.ChargeStationId {
			continue
		}

		for _, mv := range values {
			// Apply filters
			if filter.ConnectorId != nil && mv.EvseId != *filter.ConnectorId {
				continue
			}
			if filter.TransactionId != nil && mv.TransactionId != *filter.TransactionId {
				continue
			}
			if filter.StartTime != nil && mv.MeterValue.Timestamp < *filter.StartTime {
				continue
			}
			if filter.EndTime != nil && mv.MeterValue.Timestamp > *filter.EndTime {
				continue
			}

			allValues = append(allValues, mv)
		}
	}

	// Sort by timestamp descending
	sort.Slice(allValues, func(i, j int) bool {
		return allValues[i].MeterValue.Timestamp > allValues[j].MeterValue.Timestamp
	})

	total := len(allValues)

	// Apply pagination
	start := filter.Offset
	if start > len(allValues) {
		start = len(allValues)
	}
	end := start + filter.Limit
	if end > len(allValues) {
		end = len(allValues)
	}

	result := &store.MeterValuesResult{
		MeterValues: allValues[start:end],
		Total:       total,
	}

	return result, nil
}
