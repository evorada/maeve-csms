// SPDX-License-Identifier: Apache-2.0

package transport

import (
	"context"
	"time"
)

// The types of the connection events that the gateway publishes.
const (
	// ConnectionEventConnected: the station socket is open and can carry messages.
	ConnectionEventConnected = "connected"
	// ConnectionEventAlive: the socket is still open and the station answers the pings
	// of the gateway; sent at an interval.
	ConnectionEventAlive = "alive"
	// ConnectionEventDisconnected: the socket has closed.
	ConnectionEventDisconnected = "disconnected"
)

// The reasons of a disconnected event.
const (
	// DisconnectReasonClosed: the station closed the socket, or the socket failed.
	DisconnectReasonClosed = "closed"
	// DisconnectReasonPingTimeout: the gateway closed the socket because the station
	// did not answer a ping in time.
	DisconnectReasonPingTimeout = "ping_timeout"
	// DisconnectReasonGatewayShutdown: the gateway closed the socket because it shuts
	// down.
	DisconnectReasonGatewayShutdown = "gateway_shutdown"
)

// ConnectionEvent tells that a station socket on a gateway has opened, is still open or
// has closed. The gateway encodes the same JSON.
type ConnectionEvent struct {
	// Type is one of ConnectionEventConnected, ConnectionEventAlive and
	// ConnectionEventDisconnected.
	Type string `json:"type"`
	// StationKey is the key of the station: "{org_key}.{stationId}", or the station id
	// in single tenancy mode.
	StationKey string `json:"station_key"`
	// OcppVersion is the websocket subprotocol, e.g. "ocpp1.6".
	OcppVersion string `json:"ocpp_version"`
	// ConnectionId is a random 128-bit hex id of the socket, the same in all its events.
	ConnectionId string `json:"connection_id"`
	// GatewayId names the gateway instance that holds the socket.
	GatewayId string `json:"gateway_id"`
	// At is the time of the event on the clock of the gateway, in UTC.
	At time.Time `json:"at"`
	// RemoteAddr is the address of the station as the gateway sees it.
	RemoteAddr string `json:"remote_addr,omitempty"`
	// CloseCode is the websocket close status of a disconnected socket, -1 when the
	// socket ended without one.
	CloseCode int `json:"close_code,omitempty"`
	// Reason is why a socket disconnected: DisconnectReasonClosed,
	// DisconnectReasonPingTimeout or DisconnectReasonGatewayShutdown.
	Reason string `json:"reason,omitempty"`
}

// ConnectionEventHandler handles the connection events of the station sockets.
type ConnectionEventHandler interface {
	// HandleConnectionEvent handles one event. The events of one station arrive in the
	// order the listener receives them; the listener has checked that ev.StationKey is
	// the key of the station that the event was published for.
	HandleConnectionEvent(ctx context.Context, ev *ConnectionEvent)
}

// ConnectionEventHandlerFunc allows a plain function to be used as a
// ConnectionEventHandler.
type ConnectionEventHandlerFunc func(ctx context.Context, ev *ConnectionEvent)

func (f ConnectionEventHandlerFunc) HandleConnectionEvent(ctx context.Context, ev *ConnectionEvent) {
	f(ctx, ev)
}

// ConnectionEventListener is implemented by the listeners that can receive the
// connection events of the gateways.
type ConnectionEventListener interface {
	// ListenConnectionEvents subscribes to the connection events of all stations, shared
	// with the other manager replicas so that each event is handled once, and hands them
	// to the handler until the returned Connection is disconnected.
	ListenConnectionEvents(ctx context.Context, handler ConnectionEventHandler) (Connection, error)
}
