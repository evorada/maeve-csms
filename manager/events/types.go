// SPDX-License-Identifier: Apache-2.0

package events

import (
	"strconv"

	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// The types of the domain events. They have no product prefix: the relay publishes
// "session.completed" as "cpms.session.completed".
const (
	TypeStationRegistered      = "station.registered"
	TypeStationConnected       = "station.connected"
	TypeStationDisconnected    = "station.disconnected"
	TypeConnectorStatusChanged = "connector.status_changed"
	TypeSessionStarted         = "session.started"
	TypeSessionUpdated         = "session.updated"
	TypeSessionCompleted       = "session.completed"
	TypeCommandCompleted       = "command.completed"
	TypeCommandProgress        = "command.progress"
	TypeLocationUpdated        = "location.updated"
	TypeStationDecommissioned  = "station.decommissioned"
	TypeStationPending         = "station.pending"
	TypeStationApproved        = "station.approved"
	TypeStationApprovalFailed  = "station.approval_failed"
	TypeStationRejected        = "station.rejected"
	TypeStationTransferred     = "station.transferred"
)

// The types of the resources that the events are about, with the form of their ids.
const (
	ResourceStation   = "station"   // id: station key
	ResourceConnector = "connector" // id: "<station_key>#<connector_id>" (ConnectorResourceId)
	ResourceSession   = "session"   // id: the id that the transaction is stored with
	ResourceCommand   = "command"   // id: command id
	ResourceLocation  = "location"  // id: asset location id
)

// AllTypes lists every type of the catalogue.
var AllTypes = []string{
	TypeStationRegistered,
	TypeStationConnected,
	TypeStationDisconnected,
	TypeConnectorStatusChanged,
	TypeSessionStarted,
	TypeSessionUpdated,
	TypeSessionCompleted,
	TypeCommandCompleted,
	TypeCommandProgress,
	TypeLocationUpdated,
	TypeStationDecommissioned,
	TypeStationPending,
	TypeStationApproved,
	TypeStationApprovalFailed,
	TypeStationRejected,
	TypeStationTransferred,
}

// The changes that a location.updated event reports.
const (
	LocationChangeCreated = "created"
	LocationChangeUpdated = "updated"
	LocationChangeDeleted = "deleted"
	// LocationChangeTransferred is a location that moved to another org with its charge
	// station: the event is emitted in the old and in the new org.
	LocationChangeTransferred = "transferred"
)

// The ways in which the password of an approved station is set (StationApprovedData).
const (
	// PasswordModeRotate sends a new password of the station to it.
	PasswordModeRotate = "rotate"
	// PasswordModeManual gives the new password of the station to the operator, who sets
	// it on the station.
	PasswordModeManual = "manual"
	// PasswordModeKeepOnboarding keeps the onboarding password of the tenant, which the
	// station connected with.
	PasswordModeKeepOnboarding = "keep_onboarding"
)

// DisconnectReasonStale is the reason of a station.disconnected event when the station
// showed no sign of life for too long and the manager marked it disconnected. The other
// reasons are those of the gateway (transport.DisconnectReason*).
const DisconnectReasonStale = "stale"

// StationId returns the part of a station key after the org key ("k7f2.CP001" →
// "CP001"). A key without an org key is returned unchanged. The key is split at its
// first ".", as tenancy.SplitStationKey does, so in single tenancy mode the id of a
// station whose id has a "." loses the part before it.
func StationId(stationKey string) string {
	_, stationId, _ := tenancy.SplitStationKey(stationKey)
	return stationId
}

// StationActor returns the actor of the changes that a charge station causes.
func StationActor(stationKey string) string {
	return "station:" + stationKey
}

// ConnectorResourceId returns the resource id of a connector of a charge station:
// "<station_key>#<connector_id>".
func ConnectorResourceId(stationKey string, connectorId int) string {
	return stationKey + "#" + strconv.Itoa(connectorId)
}
