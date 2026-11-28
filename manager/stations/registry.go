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

// Decommissioned reports whether the station was taken out of service. A decommissioned
// station keeps its record and its authentication details, but may no longer connect, and
// its id is not registered again. A station without a record is not decommissioned.
func Decommissioned(ctx context.Context, s store.ChargeStationRegistryStore, stationKey string) (bool, error) {
	record, err := Lookup(ctx, s, stationKey)
	if err != nil {
		return false, err
	}
	return record.State == store.StationStateDecommissioned, nil
}

func syntheticRecord(stationKey string) *store.ChargeStation {
	return &store.ChargeStation{
		StationKey: stationKey,
		State:      store.StationStateActive,
	}
}
