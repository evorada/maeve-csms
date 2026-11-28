// SPDX-License-Identifier: Apache-2.0

package stations

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// ChargingAllowed reports whether a transaction may start at the station: only an active
// station may charge, a pending or decommissioned one may not. A station without a
// record, a backend without a registry (ErrNotImplemented) and a nil registry allow it,
// as before there was a registry. Any other error of the registry is returned.
func ChargingAllowed(ctx context.Context, registry store.ChargeStationRegistryStore, stationKey string) (bool, error) {
	if registry == nil {
		return true, nil
	}
	record, err := Lookup(ctx, registry, stationKey)
	if err != nil {
		return false, err
	}
	return record.State == store.StationStateActive, nil
}
