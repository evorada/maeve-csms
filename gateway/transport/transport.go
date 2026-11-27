// SPDX-License-Identifier: Apache-2.0

// Package transport connects the websockets of the charge stations to the CSMS through
// a message broker. The implementations live in the sub-packages.
package transport

import (
	"context"

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
	// Done is closed when the session breaks down on its own and can no longer
	// deliver messages to the station: the station socket must then be closed, so
	// that the station connects again. Close does not close it, and a nil channel
	// (a session that cannot break down) is never ready.
	Done() <-chan struct{}
	// Close stops delivery and releases the session.
	Close(ctx context.Context) error
}
