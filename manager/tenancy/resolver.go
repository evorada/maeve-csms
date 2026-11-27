// SPDX-License-Identifier: Apache-2.0

package tenancy

import (
	"context"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// StationOrgResolver gives the org that owns a station key.
type StationOrgResolver interface {
	OrgIdForStation(ctx context.Context, chargeStationId string) (string, error)
}

// Resolver takes the org of a charge station from its auth details.
type Resolver struct {
	Stations store.ChargeStationAuthStore
	// DefaultOrgId is the org of a charge station that has none. Empty is the legacy
	// org (store.LegacyOrgId).
	DefaultOrgId string
}

// OrgIdForStation returns ChargeStationAuth.OrgId when the station exists and has an
// org, otherwise DefaultOrgId. A station of the legacy org has no org yet: the rows of
// the legacy org are given to the default org when the manager starts.
func (r *Resolver) OrgIdForStation(ctx context.Context, chargeStationId string) (string, error) {
	auth, err := r.Stations.LookupChargeStationAuth(ctx, chargeStationId)
	if err != nil {
		return "", fmt.Errorf("lookup org of charge station %s: %w", chargeStationId, err)
	}
	if auth != nil && auth.OrgId != "" && auth.OrgId != store.LegacyOrgId {
		return auth.OrgId, nil
	}
	if r.DefaultOrgId == "" {
		return store.LegacyOrgId, nil
	}
	return r.DefaultOrgId, nil
}
