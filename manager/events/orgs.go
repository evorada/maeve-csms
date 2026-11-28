// SPDX-License-Identifier: Apache-2.0

package events

import (
	"context"
	"errors"
)

// OrgResolver gives the org that owns a charge station. The manager uses the tenant
// resolver (tenancy.Resolver): the org of the station itself, else the tenant of the
// org key of its station key, else the default org.
type OrgResolver interface {
	OrgIdForStation(ctx context.Context, stationKey string) (string, error)
}

// ErrUnknownOrg is returned when the org of an event cannot be found.
var ErrUnknownOrg = errors.New("unknown org")

// StaticOrgResolver gives every charge station the same org: for tests and single-tenant
// development. It returns ErrUnknownOrg when OrgId is empty.
type StaticOrgResolver struct{ OrgId string }

func (r StaticOrgResolver) OrgIdForStation(context.Context, string) (string, error) {
	if r.OrgId == "" {
		return "", ErrUnknownOrg
	}
	return r.OrgId, nil
}
