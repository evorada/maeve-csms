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

// OnboardingStatus is where the approval of a pending station is.
type OnboardingStatus string

const (
	// OnboardingAwaitingApproval is a pending station that nobody has approved yet.
	OnboardingAwaitingApproval OnboardingStatus = "awaiting_approval"
	// OnboardingApproving is a pending station whose new password is being sent to it.
	OnboardingApproving OnboardingStatus = "approving"
	// OnboardingCredentialsFailed is a pending station that did not take its new password.
	OnboardingCredentialsFailed OnboardingStatus = "credentials_failed"
)

// The transports that a pending station connects with.
const (
	OnboardingTransportWs  = "ws"  // no TLS
	OnboardingTransportWss = "wss" // TLS
)

// The credentials that a pending station connects with.
const (
	// OnboardingCredentialOpen is a connection without TLS and without a password, which
	// a tenant can allow for the onboarding of its stations.
	OnboardingCredentialOpen = "open"
	// OnboardingCredentialOnboardingPassword is the onboarding password of the tenant.
	OnboardingCredentialOnboardingPassword = "onboarding_password"
	// OnboardingCredentialPendingPassword is the new password of the station whose
	// approval is in flight.
	OnboardingCredentialPendingPassword = "pending_password"
)

// OnboardingSecurityProfile returns the security profile of a pending station that
// connects with a transport: profile 1 (no TLS) for ws, profile 2 (TLS) for wss. ok is
// false for any other transport.
func OnboardingSecurityProfile(transport string) (profile SecurityProfile, ok bool) {
	switch transport {
	case OnboardingTransportWs:
		return UnsecuredTransportWithBasicAuth, true
	case OnboardingTransportWss:
		return TLSWithBasicAuth, true
	}
	return 0, false
}

// ValidOnboardingCredential reports whether credential is one of the credentials that a
// pending station connects with.
func ValidOnboardingCredential(credential string) bool {
	switch credential {
	case OnboardingCredentialOpen, OnboardingCredentialOnboardingPassword, OnboardingCredentialPendingPassword:
		return true
	}
	return false
}

// PendingApproval is what an operator approved a pending station with while its new
// password is sent to it: the approval is completed with it (CompleteApproval) once the
// station has taken the password.
type PendingApproval struct {
	LocationId      string
	DisplayName     *string
	AllowAnyToken   bool
	MaxCapacityKw   *float64
	SecurityProfile SecurityProfile
	// Evses are the EVSEs of the station with their connectors, created when the
	// approval is completed. Only the fields that the owner of an EVSE sets are kept.
	Evses []*AssetEvse
	// RequestedBy is the actor who approved the station ("user:…", "svc:…").
	RequestedBy string
	RequestedAt time.Time
}

// StationOnboarding is what the CSMS knows about a pending station: a station that
// connected without being registered.
type StationOnboarding struct {
	FirstSeenAt time.Time
	// LastSeenAt is the time of the last connection or BootNotification of the station.
	LastSeenAt time.Time
	// Transport is the transport of the last connection (OnboardingTransportWs/Wss).
	Transport string
	// Credential is what the station gave on its last connection (OnboardingCredential*).
	Credential string
	Status     OnboardingStatus
	// NotifiedAt is when the station.pending event of the station was emitted; nil
	// before. It is emitted once while the station is pending.
	NotifiedAt *time.Time
	// Error says why the station did not take its new password.
	Error *string
	// CommandId is the command that sends the new password to the station.
	CommandId *string
	// Approval is the approval that waits for the station to take its new password; nil
	// before an approval.
	Approval *PendingApproval
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
	// Onboarding is what is known about a pending station; nil for the stations that
	// were registered.
	Onboarding *StationOnboarding
	// SharedCredentials is set for a station that was approved with the onboarding
	// password of its tenant, which other stations know too.
	SharedCredentials bool
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
	// RecordOnboardingConnection records that a station that was not registered by an
	// operator connected at the time at, to a host of the org orgId, with the transport
	// and the credential (OnboardingTransport*, OnboardingCredential*). A station that
	// does not exist is created in state pending, in the org, with the security profile
	// of the transport (OnboardingSecurityProfile), no password and an onboarding that
	// awaits approval; created is true then. A pending station gets the time, transport
	// and credential of the connection as its last ones.
	//
	// A new station while the org has maxPending or more pending stations is
	// ErrLimitExceeded, and nothing is written. A station that exists and is not pending
	// (registered, approved or decommissioned) is ErrInvalidState: it is never made
	// pending.
	RecordOnboardingConnection(ctx context.Context, stationKey, orgId, transport, credential string, maxPending int, at time.Time) (created bool, err error)
	// RecordOnboardingBoot records a BootNotification of a pending station at the time
	// at: LastSeenAt becomes at and, when the station.pending event of the station has not
	// been emitted (NotifiedAt is nil), NotifiedAt becomes at too and notify is true. Of
	// the boots of one station that are recorded at the same time, one at most gets
	// notify. It returns the onboarding as it is stored now. A station that does not
	// exist is ErrNotFound, one that is not pending ErrInvalidState.
	RecordOnboardingBoot(ctx context.Context, stationKey string, at time.Time) (onboarding *StationOnboarding, notify bool, err error)
	// SetOnboarding replaces the onboarding of a pending station and the hash of its
	// pending password (nil clears it). A station that does not exist is ErrNotFound, one
	// that is not pending ErrInvalidState, and nothing is written then.
	SetOnboarding(ctx context.Context, stationKey string, onboarding *StationOnboarding, pendingPasswordHash *string) error
	// CompleteApproval makes a pending station active as the approval says: it creates the
	// EVSEs of the approval with their connectors at its location (in the org of the
	// station; the EVSEs are filled in as CreateAssetEvse fills them), writes the
	// location, display name, capacity and AllowAnyToken of the station, its security
	// profile, password hash and InvalidUsernameAllowed from auth, clears its pending
	// password and its onboarding, and sets SharedCredentials. A station that does not
	// exist is ErrNotFound, one that is not pending ErrInvalidState; a location that does
	// not exist is ErrNotFound and an EVSE whose ids are taken ErrAlreadyExists. Nothing
	// is written when it fails.
	CompleteApproval(ctx context.Context, stationKey string, approval *PendingApproval, auth *ChargeStationAuth, sharedCredentials bool) error
	// RejectChargeStation decommissions a pending station that an operator refused: its
	// state becomes decommissioned with the reason "rejected: <reason>" ("rejected"
	// without one), and its pending password is cleared. A station that does not exist is
	// ErrNotFound, one that is not pending ErrInvalidState.
	RejectChargeStation(ctx context.Context, stationKey string, reason string) error
}

// RejectedStateReason is the state reason of a station that was rejected for the reason.
func RejectedStateReason(reason string) string {
	if reason == "" {
		return "rejected"
	}
	return "rejected: " + reason
}
