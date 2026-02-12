// SPDX-License-Identifier: Apache-2.0

package store

import "context"

const (
	LocalAuthListUpdateTypeFull         = "Full"
	LocalAuthListUpdateTypeDifferential = "Differential"
)

const (
	IdTagStatusAccepted     = "Accepted"
	IdTagStatusBlocked      = "Blocked"
	IdTagStatusExpired      = "Expired"
	IdTagStatusInvalid      = "Invalid"
	IdTagStatusConcurrentTx = "ConcurrentTx"
)

// IdTagInfo contains authorization information for a local auth list entry
type IdTagInfo struct {
	Status      string
	ExpiryDate  *string
	ParentIdTag *string
}

// LocalAuthListEntry represents a single entry in the local authorization list
type LocalAuthListEntry struct {
	IdTag     string
	IdTagInfo *IdTagInfo
}

// LocalAuthListStore defines the interface for local authorization list management
type LocalAuthListStore interface {
	// GetLocalListVersion returns the current version of the local auth list for a charge station.
	// Returns 0 if no list has been set.
	GetLocalListVersion(ctx context.Context, chargeStationId string) (int, error)

	// UpdateLocalAuthList updates the local authorization list for a charge station.
	// updateType is either "Full" (replace entire list) or "Differential" (add/update/remove entries).
	// For differential updates, entries with nil IdTagInfo are removed.
	UpdateLocalAuthList(ctx context.Context, chargeStationId string, version int, updateType string, entries []*LocalAuthListEntry) error

	// GetLocalAuthList returns all entries in the local authorization list for a charge station.
	GetLocalAuthList(ctx context.Context, chargeStationId string) ([]*LocalAuthListEntry, error)
}
