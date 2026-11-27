// SPDX-License-Identifier: Apache-2.0

// Package stations answers questions about a charge station from its registry record:
// its lifecycle state and what it allows, with the behaviour of a station that has no
// record (a backend without a registry, or a station that predates it) built in.
package stations

import (
	"context"
	"errors"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// Lookup returns the registry record of the station, or a synthetic active record when
// the backend has no registry (Firestore: ErrNotImplemented) or the station has no
// record. The synthetic record has the station key and state active, and nothing else:
// the station behaves as before there was a registry (it may charge, and only the tokens
// that the token store knows are accepted). Any other store error is returned.
func Lookup(ctx context.Context, s store.ChargeStationRegistryStore, stationKey string) (*store.ChargeStation, error) {
	record, err := s.LookupChargeStation(ctx, stationKey)
	if errors.Is(err, store.ErrNotImplemented) {
		return syntheticRecord(stationKey), nil
	}
	if err != nil {
		return nil, fmt.Errorf("look up the registry record of %s: %w", stationKey, err)
	}
	if record == nil {
		return syntheticRecord(stationKey), nil
	}
	return record, nil
}

func syntheticRecord(stationKey string) *store.ChargeStation {
	return &store.ChargeStation{
		StationKey: stationKey,
		State:      store.StationStateActive,
	}
}
