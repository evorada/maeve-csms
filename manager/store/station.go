// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"time"
)

// StationState is the lifecycle state of a charge station in the registry.
type StationState string

const (
	// StationStatePending is a station that connected without being registered and waits
	// for an operator to approve it.
	StationStatePending StationState = "pending"
	// StationStateActive is a station that may charge. Registered stations are active.
	StationStateActive StationState = "active"
	// StationStateDecommissioned is a station that was taken out of service (or rejected)
	// and is kept for its history.
	StationStateDecommissioned StationState = "decommissioned"
)

// Valid reports whether s is one of the three states.
func (s StationState) Valid() bool {
	switch s {
	case StationStatePending, StationStateActive, StationStateDecommissioned:
		return true
	}
	return false
}

// ChargeStation is the registry record of a charge station: what the operator knows
// about it on top of its authentication details (ChargeStationAuth).
type ChargeStation struct {
	StationKey     string
	OrgId          string
	LocationId     *string  // asset location (AssetLocation.Id) of the station
	DisplayName    *string  // name for people; the station key identifies the station
	MaxCapacityKw  *float64 // most power the station can deliver in total
	AllowAnyToken  bool     // accept idTags that the token store does not know
	State          StationState
	StateChangedAt time.Time
	StateReason    *string
	// Runtime is what the station reported when it booted, read-only (nil before its
	// first boot). It is written with SetChargeStationRuntimeDetails.
	Runtime   *ChargeStationRuntimeDetails
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ChargeStationFilter selects the charge stations of a list. Every set field narrows it.
type ChargeStationFilter struct {
	OrgIds     []string       // required: no org lists nothing
	LocationId *string        // stations at this asset location
	States     []StationState // empty = every state
	Vendor     *string        // vendor reported at boot, exact, ignoring case
	Model      *string        // model reported at boot, exact, ignoring case
	// Connected selects the stations whose websocket is open (true) or not (false); a
	// station without a status has never connected.
	Connected *bool
	// StatusAnyOf selects the stations with a connector (connector 0, the station as a
	// whole, included) whose current status is one of these.
	StatusAnyOf []ConnectorStatusType
	// Query is a case-insensitive substring of the station key, the display name, the
	// serial number or the ICCID, matched as plain text.
	Query *string
	// Page is the page to return. The cursor holds the station key that the previous
	// page ended with (EncodeKeyCursor).
	Page PageRequest
}

// ChargeStationRegistryStore keeps the registry records of the charge stations. A record
// exists for every station that has authentication details: SetChargeStationAuth creates
// it in state active.
type ChargeStationRegistryStore interface {
	// LookupChargeStation returns the record with its runtime details; nil, nil when the
	// station is not registered.
	LookupChargeStation(ctx context.Context, stationKey string) (*ChargeStation, error)
	// UpdateChargeStation writes LocationId, DisplayName, MaxCapacityKw and AllowAnyToken
	// of the station cs.StationKey, and cs.UpdatedAt back. A station or a location that
	// does not exist is ErrNotFound. The caller checks that the location belongs to the
	// org of the station.
	UpdateChargeStation(ctx context.Context, cs *ChargeStation) error
	// SetChargeStationState moves the station to state, with an optional reason, and sets
	// the time of the change to now. A station that does not exist is ErrNotFound.
	SetChargeStationState(ctx context.Context, stationKey string, state StationState, reason *string) error
	// ListChargeStations returns a page of the matching stations ordered by station key,
	// and the cursor of the next page ("" after the last page).
	ListChargeStations(ctx context.Context, filter ChargeStationFilter) ([]*ChargeStation, string, error)
	// CountChargeStations counts the stations of the org that are in one of the states
	// (every state when none is given).
	CountChargeStations(ctx context.Context, orgId string, states ...StationState) (int, error)
}
