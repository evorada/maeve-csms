// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/transport"
)

// CallHandler is the interface implemented by handlers that are designed to process an OCPP Call.
type CallHandler interface {
	// HandleCall receives the charge station identifier and the OCPP request message
	// and returns either the OCPP response message or an error.
	HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (response ocpp.Response, err error)
}

// CallHandlerFunc allows a plain function to be used as a CallHandler.
type CallHandlerFunc func(ctx context.Context, chargeStationId string, request ocpp.Request) (response ocpp.Response, err error)

func (ch CallHandlerFunc) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	return ch(ctx, chargeStationId, request)
}

// CallRoute is the configuration that is used by the Router for processing an OCPP Call.
// In the Router this is indexed by the OCPP Action.
type CallRoute struct {
	NewRequest     func() ocpp.Request // Function used for creating an empty request
	RequestSchema  string              // JSON schema file that corresponds to the request data structure
	ResponseSchema string              // JSON schema file that corresponds to the response data structure
	Handler        CallHandler         // Function to process a call
}

// CallResultHandler is the interface implemented by the handlers that are designed to process an OCPP CallResult.
type CallResultHandler interface {
	// HandleCallResult receives the charge station id, OCPP Request message and OCPP Response message
	// along with any cached state. It may return an error.
	HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error
}

// CallResultHandlerFunc allows a plain function to be used as a CallResultHandler
type CallResultHandlerFunc func(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error

func (crh CallResultHandlerFunc) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	return crh(ctx, chargeStationId, request, response, state)
}

// CallError is an OCPP CallError returned by a charge station in reply to a
// CSMS-initiated Call.
type CallError struct {
	MessageId        string
	Action           string // action of the original Call
	ErrorCode        transport.ErrorCode
	ErrorDescription string
}

func (e *CallError) Error() string {
	return fmt.Sprintf("%s %s: %s: %s", e.Action, e.MessageId, e.ErrorCode, e.ErrorDescription)
}

// CallErrorHandler is the interface implemented by the handlers that are designed to process an OCPP CallError.
type CallErrorHandler interface {
	// HandleCallError receives the charge station id, the OCPP Request message of the original Call
	// and the CallError along with any cached state. The request is the original Call payload
	// unmarshalled with the route's NewRequest, or nil if no route exists or the payload
	// cannot be decoded. It may return an error.
	HandleCallError(ctx context.Context, chargeStationId string, request ocpp.Request, callError *CallError, state any) error
}

// CallErrorHandlerFunc allows a plain function to be used as a CallErrorHandler
type CallErrorHandlerFunc func(ctx context.Context, chargeStationId string, request ocpp.Request, callError *CallError, state any) error

func (f CallErrorHandlerFunc) HandleCallError(ctx context.Context, chargeStationId string, request ocpp.Request, callError *CallError, state any) error {
	return f(ctx, chargeStationId, request, callError, state)
}

// CallResultRoute is the configuration that is used by the Router for process an OCPP CallResult.
// In the Router this is indexed by the OCPP Action (of the corresponding Call).
type CallResultRoute struct {
	NewRequest     func() ocpp.Request  // Function used for creating an empty request
	NewResponse    func() ocpp.Response // Function used for creating an empty response
	RequestSchema  string               // JSON schema file that corresponds to the request data structure
	ResponseSchema string               // JSON schema file that corresponds to the response data structure
	Handler        CallResultHandler    // Function to process a call result
	ErrorHandler   CallErrorHandler     // optional: invoked when the station answers the Call with a CallError
}

// CallMaker is the interface used by handlers (and other parts of the system) that want to initiate
// an OCPP call from the CSMS.
type CallMaker interface {
	// Send receives the charge station id and the request to send. It may return an error.
	Send(ctx context.Context, chargeStationId string, request ocpp.Request) error
}

// RawCallMaker sends a pre-serialised OCPP call with a caller-chosen MessageId, so that
// the CallResult or CallError of the station, which echoes the MessageId, can be tied
// back to whoever sent the call.
type RawCallMaker interface {
	SendRaw(ctx context.Context, chargeStationId, messageId, action string, payload json.RawMessage) error
}

// CallOutcomeObserver sees every CallResult and CallError before the per-action routing.
// It returns true when the message answered a call that it tracks. It must not fail or
// hold up the handling of the message: errors are its own to log.
type CallOutcomeObserver interface {
	ObserveCallResult(ctx context.Context, chargeStationId string, msg *transport.Message) bool
	ObserveCallError(ctx context.Context, chargeStationId string, msg *transport.Message) bool
}
