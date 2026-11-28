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
// commands to the stations emits.
type CommandCompletedData struct {
	CommandId        string          `json:"command_id"`
	StationKey       string          `json:"station_key"`
	StationId        string          `json:"station_id"`
	Action           string          `json:"action"`
	Status           string          `json:"status"` // final status of the command
	ErrorCode        string          `json:"error_code,omitempty"`
	ErrorDescription string          `json:"error_description,omitempty"`
	Result           json.RawMessage `json:"result,omitempty"`
	CompletedAt      time.Time       `json:"completed_at"`
}
