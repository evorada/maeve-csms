// SPDX-License-Identifier: Apache-2.0

package events

import (
	"encoding/json"
	"time"
)

// The payloads (CloudEvents "data") of the event types. Field names are snake_case,
// times are RFC 3339 in UTC (whoever builds a payload converts them), and nil pointers
// are left out.
//
// The station_id of a payload is StationId of its station_key.

// StationRegisteredData is the payload of station.registered.
type StationRegisteredData struct {
	StationKey      string `json:"station_key"`
	StationId       string `json:"station_id"`
	SecurityProfile int    `json:"security_profile"`
}

// StationConnectedData is the payload of station.connected: a websocket of the station
// opened on a gateway.
type StationConnectedData struct {
	StationKey   string    `json:"station_key"`
	StationId    string    `json:"station_id"`
	OcppVersion  string    `json:"ocpp_version"` // "ocpp1.6" | "ocpp2.0.1"
	ConnectionId string    `json:"connection_id"`
	ConnectedAt  time.Time `json:"connected_at"` // clock of the gateway
}

// StationDisconnectedData is the payload of station.disconnected.
type StationDisconnectedData struct {
	StationKey   string `json:"station_key"`
	StationId    string `json:"station_id"`
	ConnectionId string `json:"connection_id,omitempty"` // not known for "stale"
	// DisconnectedAt is the time of the gateway, or of the manager for "stale".
	DisconnectedAt time.Time `json:"disconnected_at"`
	Reason         string    `json:"reason"` // "closed" | "ping_timeout" | "gateway_shutdown" | "stale"
	// CloseCode is the websocket close status that the gateway reported, -1 when the
	// socket ended without one.
	CloseCode int `json:"close_code,omitempty"`
}

// ConnectorStatusChangedData is the payload of connector.status_changed: the status or
// the error code of a connector changed.
type ConnectorStatusChangedData struct {
	StationKey      string    `json:"station_key"`
	StationId       string    `json:"station_id"`
	EvseId          int       `json:"evse_id"`      // 1.6: equal to connector_id
	ConnectorId     int       `json:"connector_id"` // 0 = whole station (1.6)
	Status          string    `json:"status"`
	PreviousStatus  string    `json:"previous_status,omitempty"` // empty for the first status
	ErrorCode       string    `json:"error_code"`
	Info            *string   `json:"info,omitempty"`
	VendorId        *string   `json:"vendor_id,omitempty"`
	VendorErrorCode *string   `json:"vendor_error_code,omitempty"`
	Timestamp       time.Time `json:"timestamp"` // station timestamp, else receive time
}

// SessionStartedData is the payload of session.started.
type SessionStartedData struct {
	StationKey        string `json:"station_key"`
	StationId         string `json:"station_id"`
	TransactionId     string `json:"transaction_id"`      // id the transaction is stored with
	OcppTransactionId string `json:"ocpp_transaction_id"` // 1.6 integer as decimal string
	OcppVersion       string `json:"ocpp_version"`
	EvseId            *int   `json:"evse_id,omitempty"`
	ConnectorId       *int   `json:"connector_id,omitempty"`
	// IdToken is the token UID in clear: the events stay on the internal NATS. Consumers
	// that show it to people mask it unless the user may read sensitive data.
	IdToken      string    `json:"id_token"`
	TokenType    string    `json:"token_type"`
	IdTokenGroup *string   `json:"id_token_group,omitempty"`
	AuthStatus   string    `json:"auth_status"` // idTagInfo status returned to the station
	StartedAt    time.Time `json:"started_at"`
	MeterStartWh *int64    `json:"meter_start_wh,omitempty"`
	// ReservationId is the reservation that the station says the session terminates.
	ReservationId *int `json:"reservation_id,omitempty"`
	// TariffId and TariffVersion are the tariff version that the session is charged
	// with, and TariffSource the rule that chose it (the sources of the package tariffs).
	// A session without a tariff has only the source. All three are left out when the
	// tariff of the session was not chosen at its start.
	TariffId      *string `json:"tariff_id,omitempty"`
	TariffVersion *int    `json:"tariff_version,omitempty"`
	TariffSource  *string `json:"tariff_source,omitempty"`
}

// SessionUpdatedData is the payload of session.updated: the newest readings that a
// meter values message reported for a session.
type SessionUpdatedData struct {
	StationKey        string    `json:"station_key"`
	StationId         string    `json:"station_id"`
	TransactionId     string    `json:"transaction_id"`
	OcppTransactionId string    `json:"ocpp_transaction_id"`
	EvseId            *int      `json:"evse_id,omitempty"`
	ConnectorId       *int      `json:"connector_id,omitempty"`
	SampledAt         time.Time `json:"sampled_at"`          // time of the newest meter value
	MeterWh           *int64    `json:"meter_wh,omitempty"`  // latest energy register
	EnergyWh          *int64    `json:"energy_wh,omitempty"` // meter_wh − meter_start_wh
	PowerW            *float64  `json:"power_w,omitempty"`   // latest Power.Active.Import (kW × 1000)
	SocPercent        *float64  `json:"soc_percent,omitempty"`
}

// SessionCompletedData is the payload of session.completed.
type SessionCompletedData struct {
	StationKey        string `json:"station_key"`
	StationId         string `json:"station_id"`
	TransactionId     string `json:"transaction_id"`
	OcppTransactionId string `json:"ocpp_transaction_id"`
	EvseId            *int   `json:"evse_id,omitempty"`
	ConnectorId       *int   `json:"connector_id,omitempty"`
	// IdToken is the token UID in clear: the events stay on the internal NATS. Consumers
	// that show it to people mask it unless the user may read sensitive data.
	IdToken      string     `json:"id_token"`
	TokenType    string     `json:"token_type"`
	IdTokenGroup *string    `json:"id_token_group,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	StoppedAt    time.Time  `json:"stopped_at"`
	DurationS    *int64     `json:"duration_s,omitempty"`
	MeterStartWh *int64     `json:"meter_start_wh,omitempty"`
	MeterStopWh  *int64     `json:"meter_stop_wh,omitempty"`
	EnergyWh     *int64     `json:"energy_wh,omitempty"`
	StopReason   string     `json:"stop_reason"`
}

// CommandCompletedData is the payload of command.completed, which the dispatcher of the
// commands to the stations emits each time a command reaches a final status: accepted,
// rejected, failed, timeout or cancelled. A result that arrives after the command timed
// out moves it once more, to accepted, rejected or failed, with Late set.
type CommandCompletedData struct {
	CommandId        string          `json:"command_id"`
	StationKey       string          `json:"station_key"`
	StationId        string          `json:"station_id"`
	Action           string          `json:"action"`
	Status           string          `json:"status"` // final status of the command
	ErrorCode        string          `json:"error_code,omitempty"`
	ErrorDescription string          `json:"error_description,omitempty"`
	Result           json.RawMessage `json:"result,omitempty"` // CallResult payload
	CompletedAt      time.Time       `json:"completed_at"`
	OrgId            string          `json:"org_id"`
	OcppVersion      string          `json:"ocpp_version"` // "ocpp1.6" | "ocpp2.0.1"
	Actor            *string         `json:"actor,omitempty"`
	Jti              *string         `json:"jti,omitempty"` // of the token that created the command
	CreatedAt        time.Time       `json:"created_at"`
	SentAt           *time.Time      `json:"sent_at,omitempty"`
	Late             bool            `json:"late"`
}

// CommandProgressData is the payload of command.progress, which follows a command that
// its station accepted to its effect: the transaction that a remote start began, the
// end of the transaction that a remote stop asked for, the connector status that a
// change of availability led to. One event per change of the progress.
type CommandProgressData struct {
	CommandId  string `json:"command_id"`
	OrgId      string `json:"org_id"`
	StationId  string `json:"station_id"`
	StationKey string `json:"station_key"`
	Action     string `json:"action"`
	Status     string `json:"status"` // status of the command, accepted
	// Progress is the progress as stored on the command, e.g.
	// {"state":"transaction_started","transactionId":"…","ocppTransactionId":42,"connectorId":1}.
	Progress   json.RawMessage `json:"progress"`
	ProgressAt time.Time       `json:"progress_at"`
}

// LocationUpdatedData is the payload of location.updated: a location, an EVSE at it or a
// connector of such an EVSE was created, changed or deleted. Change is what happened to
// the connector when ConnectorUid is set, otherwise to the EVSE when EvseUid is set,
// otherwise to the location. The location fields are those that the location has after
// the change (before it, for a location that was deleted).
type LocationUpdatedData struct {
	LocationId   string `json:"location_id"`
	OcpiId       string `json:"ocpi_id"`
	PartyRef     string `json:"party_ref"`
	Change       string `json:"change"` // LocationChange*
	EvseUid      string `json:"evse_uid,omitempty"`
	ConnectorUid string `json:"connector_uid,omitempty"`
	// Publish is whether the location is published through OCPI, and PublishChanged
	// whether the change turned it on or off.
	Publish        bool `json:"publish"`
	PublishChanged bool `json:"publish_changed"`
}

// StationDecommissionedData is the payload of station.decommissioned: the station was
// taken out of service. Its EVSEs are REMOVED, it can no longer connect, and its record
// is kept. LocationId is the location of the station and Reason the reason given for the
// decommissioning, both null when there is none; Actor is who decommissioned it.
type StationDecommissionedData struct {
	StationKey string  `json:"station_key"`
	StationId  string  `json:"station_id"`
	LocationId *string `json:"location_id"`
	Reason     *string `json:"reason"`
	Actor      string  `json:"actor"`
}

// StationPendingData is the payload of station.pending: a station that is not registered
// connected, booted and waits for approval; it cannot charge until then. It is emitted at
// the first BootNotification of the station while it is pending. Vendor, model and the
// optional fields are what the station reported in that BootNotification; FirstSeenAt,
// Transport ("ws" | "wss") and Credential ("open" | "onboarding_password" |
// "pending_password") are those of its connections.
type StationPendingData struct {
	StationKey      string    `json:"station_key"`
	StationId       string    `json:"station_id"`
	Vendor          string    `json:"vendor"`
	Model           string    `json:"model"`
	SerialNumber    *string   `json:"serial_number,omitempty"`
	FirmwareVersion *string   `json:"firmware_version,omitempty"`
	Iccid           *string   `json:"iccid,omitempty"`
	Imsi            *string   `json:"imsi,omitempty"`
	FirstSeenAt     time.Time `json:"first_seen_at"`
	Transport       string    `json:"transport"`
	Credential      string    `json:"credential"`
}

// StationApprovedData is the payload of station.approved: an operator approved a pending
// station, which is active now and may charge. LocationId is where it is,
// EvseCount and ConnectorCount what it was given; PasswordMode is how its password was
// set (PasswordMode*) and SharedCredentials whether it is the onboarding password of the
// tenant. Actor is who approved it.
type StationApprovedData struct {
	StationKey        string `json:"station_key"`
	StationId         string `json:"station_id"`
	LocationId        string `json:"location_id"`
	EvseCount         int    `json:"evse_count"`
	ConnectorCount    int    `json:"connector_count"`
	PasswordMode      string `json:"password_mode"`
	SharedCredentials bool   `json:"shared_credentials"`
	Actor             string `json:"actor"`
}

// StationApprovalFailedData is the payload of station.approval_failed: a pending station
// did not take the new password of its approval, and stays pending. CommandId is the
// command that sent the password; Result is what came of it: the status the station
// answered (e.g. "Rejected", "NotSupported"), the error code of its CallError, "timeout"
// or "cancelled"; Error describes it, or is null.
type StationApprovalFailedData struct {
	StationKey string  `json:"station_key"`
	StationId  string  `json:"station_id"`
	CommandId  string  `json:"command_id"`
	Result     string  `json:"result"`
	Error      *string `json:"error"`
}

// StationRejectedData is the payload of station.rejected: an operator refused a pending
// station, which is decommissioned and can no longer connect. Reason is the reason given,
// or null; Actor is who rejected it.
type StationRejectedData struct {
	StationKey string  `json:"station_key"`
	StationId  string  `json:"station_id"`
	Reason     *string `json:"reason"`
	Actor      string  `json:"actor"`
}

// StationTransferredData is the payload of station.transferred: a charge station moved
// from one org to another, with its EVSEs and their connectors, and with its location
// when LocationMoved. It is emitted in both orgs. The station key does not change; the
// history of the station (transactions, meter values) stays with the old org.
// LocationId is where the station and its EVSEs are now (null without a location),
// ClearedTariffRefs the tariffs of the old org that the connectors, and the location
// when it moved, lost, Reason the reason given (or null) and Actor who transferred the
// station.
type StationTransferredData struct {
	StationKey        string   `json:"station_key"`
	StationId         string   `json:"station_id"`
	FromOrgId         string   `json:"from_org_id"`
	ToOrgId           string   `json:"to_org_id"`
	LocationId        *string  `json:"location_id"`
	LocationMoved     bool     `json:"location_moved"`
	EvsesMoved        int      `json:"evses_moved"`
	ConnectorsMoved   int      `json:"connectors_moved"`
	ClearedTariffRefs []string `json:"cleared_tariff_refs"`
	Actor             string   `json:"actor"`
	Reason            *string  `json:"reason"`
}
