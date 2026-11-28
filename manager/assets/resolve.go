// SPDX-License-Identifier: Apache-2.0

package assets

import (
	"context"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/transport"
)

// ResolveConnector maps an OCPP (evse, connector) pair to the asset connector and its
// EVSE. OCPP 1.6 (ocppVersion "ocpp1.6"): evseId is ignored, the 1.6 connectorId selects
// the EVSE (ocpp_evse_id = connectorId) and its connector 1. OCPP 2.0.1 ("ocpp2.0.1"):
// (evseId, connectorId) as sent by the station.
//
// It returns nil for the connector when the EVSE has no such connector, and nil for both
// when the station has no such EVSE in the asset model, which is also the case for the
// station as a whole (1.6 connector 0, 2.0.1 EVSE 0).
func ResolveConnector(ctx context.Context, s store.AssetStore, stationKey, ocppVersion string, evseId, connectorId int) (*store.AssetConnector, *store.AssetEvse, error) {
	switch transport.OcppVersion(ocppVersion) {
	case transport.OcppVersion16:
		evseId, connectorId = connectorId, 1
	case transport.OcppVersion201:
	default:
		return nil, nil, fmt.Errorf("resolve connector of %s: unknown OCPP version %q", stationKey, ocppVersion)
	}
	if evseId <= 0 {
		return nil, nil, nil
	}
	evse, err := s.LookupAssetEvseByOcpp(ctx, stationKey, evseId)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve connector of %s: %w", stationKey, err)
	}
	if evse == nil {
		return nil, nil, nil
	}
	for _, c := range evse.Connectors {
		if c.OcppConnectorId == connectorId {
			return c, evse, nil
		}
	}
	return nil, evse, nil
}
