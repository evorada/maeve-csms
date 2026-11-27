// SPDX-License-Identifier: Apache-2.0

// Package transport connects the websockets of the charge stations to the CSMS through
// a message broker. The implementations live in the sub-packages.
package transport

import (
	"context"
	"time"

	"github.com/thoughtworks/maeve-csms/gateway/pipe"
)

// Transport connects station websockets to the CSMS.
type Transport interface {
	// Open starts the CSMS side of one station socket. deliver is called for every
	// message the CSMS sends to the station; msg.Context carries the trace context.
	// ctx is the context of the station's connection and carries its span. Open
	// returns once the session can carry messages, or with an error when that is not
	// the case within the connect timeout of the transport.
	Open(ctx context.Context, stationKey, ocppProtocol string, deliver func(msg *pipe.GatewayMessage)) (Session, error)
	// Close releases the resources that the sessions share; sessions must be closed
	// first.
	Close() error
}

// Session is the CSMS side of one station socket.
type Session interface {
	// Send publishes a station → CSMS message.
	Send(ctx context.Context, msg *pipe.GatewayMessage) error
	// SendConnectionEvent publishes an event about the station socket for the CSMS. It
	// does not wait for longer than ctx allows.
	SendConnectionEvent(ctx context.Context, ev *ConnectionEvent) error
	// Done is closed when the session breaks down on its own and can no longer
	// deliver messages to the station: the station socket must then be closed, so
	// that the station connects again. Close does not close it, and a nil channel
	// (a session that cannot break down) is never ready.
	Done() <-chan struct{}
	// Close stops delivery and releases the session.
	Close(ctx context.Context) error
}

// The types of the connection events.
const (
	// ConnectionEventConnected is sent once the station socket is open and its session
	// can carry messages.
	ConnectionEventConnected = "connected"
	// ConnectionEventAlive is sent at an interval while the socket stays open and the
	// station answers the pings of the gateway.
	ConnectionEventAlive = "alive"
	// ConnectionEventDisconnected is sent once the socket has closed.
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

// ConnectionEvent tells the CSMS that a station socket has opened, is still open or
// has closed. The manager decodes the same JSON.
type ConnectionEvent struct {
	// Type is one of ConnectionEventConnected, ConnectionEventAlive and
	// ConnectionEventDisconnected.
	Type string `json:"type"`
	// StationKey is the key of the station: "{org_key}.{stationId}", or the station id
	// in single tenancy mode.
	StationKey string `json:"station_key"`
	// OcppVersion is the websocket subprotocol, e.g. "ocpp1.6".
	OcppVersion string `json:"ocpp_version"`
	// ConnectionId is a random 128-bit hex id of the socket: all events of one socket
	// carry it.
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
