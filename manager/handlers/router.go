// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"

	"github.com/santhosh-tekuri/jsonschema"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
)

// Router is the primary implementation of the transport.Router interface.
type Router struct {
	Emitter          transport.Emitter          // used to send responses to the gateway
	SchemaFS         fs.FS                      // used to obtain schema files
	OcppVersion      transport.OcppVersion      // the OCPP version that this router supports
	CallRoutes       map[string]CallRoute       // the set of routes for incoming calls (indexed by action)
	CallResultRoutes map[string]CallResultRoute // the set of routes for call results (indexed by action)
	CallErrorHandler CallErrorHandler           // optional: invoked for every CallError after the route's ErrorHandler
}

func (r Router) Handle(ctx context.Context, chargeStationId string, msg *transport.Message) {
	span := trace.SpanFromContext(ctx)

	err := r.route(ctx, chargeStationId, msg)
	if err != nil {
		slog.Error("unable to route message", slog.String("chargeStationId", chargeStationId), slog.String("action", msg.Action), "err", err)
		span.SetStatus(codes.Error, "routing request failed")
		span.RecordError(err)

		// only emit an error on a call (the charge station will not be expecting any response message)
		if msg.MessageType == transport.MessageTypeCall {
			var mqttError *transport.Error
			var errMsg *transport.Message
			if errors.As(err, &mqttError) {
				errMsg = transport.NewErrorMessage(msg.Action, msg.MessageId, mqttError.ErrorCode, mqttError.WrappedError)
			} else {
				errMsg = transport.NewErrorMessage(msg.Action, msg.MessageId, transport.ErrorInternalError, err)
			}
			err = r.Emitter.Emit(ctx, r.OcppVersion, chargeStationId, errMsg)
			if err != nil {
				slog.Error("unable to emit error message", "err", err)
			}
		}
	} else {
		span.SetStatus(codes.Ok, "ok")
	}
}

func (r Router) route(ctx context.Context, chargeStationId string, message *transport.Message) error {
	switch message.MessageType {
	case transport.MessageTypeCall:
		route, ok := r.CallRoutes[message.Action]
		if !ok {
			return fmt.Errorf("routing request: %w", transport.NewError(transport.ErrorNotImplemented, fmt.Errorf("%s not implemented", message.Action)))
		}
		err := schemas.Validate(message.RequestPayload, r.SchemaFS, route.RequestSchema)
		if err != nil {
			return fmt.Errorf("validating %s request: %w", message.Action, schemaValidationError(route.RequestSchema, err))
		}
		req := route.NewRequest()
		err = json.Unmarshal(message.RequestPayload, &req)
		if err != nil {
			return fmt.Errorf("unmarshalling %s request payload: %w", message.Action, err)
		}
		resp, err := route.Handler.HandleCall(ctx, chargeStationId, req)
		if err != nil {
			return err
		}
		if resp == nil {
			return fmt.Errorf("no response or error for %s", message.Action)
		}
		responseJson, err := json.Marshal(resp)
		if err != nil {
			return fmt.Errorf("marshalling %s call response: %w", message.Action, err)
		}
		err = schemas.Validate(responseJson, r.SchemaFS, route.ResponseSchema)
		if err != nil {
			mqttErr := transport.NewError(transport.ErrorPropertyConstraintViolation, err)
			slog.Warn("response not valid", slog.String("action", message.Action), mqttErr)
		}
		out := &transport.Message{
			MessageType:     transport.MessageTypeCallResult,
			Action:          message.Action,
			MessageId:       message.MessageId,
			ResponsePayload: responseJson,
		}
		err = r.Emitter.Emit(ctx, r.OcppVersion, chargeStationId, out)
		if err != nil {
			return fmt.Errorf("sending call response: %w", err)
		}
	case transport.MessageTypeCallResult:
		route, ok := r.CallResultRoutes[message.Action]
		if !ok {
			return fmt.Errorf("routing request: %w", transport.NewError(transport.ErrorNotImplemented, fmt.Errorf("%s result not implemented", message.Action)))
		}
		err := schemas.Validate(message.RequestPayload, r.SchemaFS, route.RequestSchema)
		if err != nil {
			return fmt.Errorf("validating %s request: %w", message.Action, err)
		}
		err = schemas.Validate(message.ResponsePayload, r.SchemaFS, route.ResponseSchema)
		if err != nil {
			return fmt.Errorf("validating %s response: %w", message.Action, schemaValidationError(route.ResponseSchema, err))
		}
		req := route.NewRequest()
		err = json.Unmarshal(message.RequestPayload, &req)
		if err != nil {
			return fmt.Errorf("unmarshalling %s request payload: %w", message.Action, err)
		}
		resp := route.NewResponse()
		err = json.Unmarshal(message.ResponsePayload, &resp)
		if err != nil {
			return fmt.Errorf("unmarshalling %s response payload: %v", message.Action, err)
		}
		err = route.Handler.HandleCallResult(ctx, chargeStationId, req, resp, message.State)
		if err != nil {
			return err
		}
	case transport.MessageTypeCallError:
		return r.routeCallError(ctx, chargeStationId, message)
	}

	return nil
}

// routeCallError delivers a CallError sent by a charge station in reply to a CSMS-initiated
// Call: first to the ErrorHandler of the route for the action of that Call, then to the
// router-wide CallErrorHandler. Nothing is sent back to the charge station.
func (r Router) routeCallError(ctx context.Context, chargeStationId string, message *transport.Message) error {
	callErr := &CallError{
		MessageId:        message.MessageId,
		Action:           message.Action,
		ErrorCode:        message.ErrorCode,
		ErrorDescription: message.ErrorDescription,
	}

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("call_error.action", callErr.Action),
		attribute.String("call_error.code", string(callErr.ErrorCode)),
		attribute.String("call_error.description", callErr.ErrorDescription))

	// the request is the CSMS's own Call, so it is not validated against the schema
	var req ocpp.Request
	route, ok := r.CallResultRoutes[message.Action]
	if ok && len(message.RequestPayload) > 0 {
		req = route.NewRequest()
		err := json.Unmarshal(message.RequestPayload, &req)
		if err != nil {
			slog.Warn("unable to unmarshal request payload of call error",
				slog.String("chargeStationId", chargeStationId),
				slog.String("action", callErr.Action),
				slog.String("messageId", callErr.MessageId),
				"err", err)
			req = nil
		}
	}

	handled := false
	var errs []error
	if ok && route.ErrorHandler != nil {
		handled = true
		err := route.ErrorHandler.HandleCallError(ctx, chargeStationId, req, callErr, message.State)
		if err != nil {
			errs = append(errs, fmt.Errorf("handling %s call error: %w", callErr.Action, err))
		}
	}
	if r.CallErrorHandler != nil {
		handled = true
		err := r.CallErrorHandler.HandleCallError(ctx, chargeStationId, req, callErr, message.State)
		if err != nil {
			errs = append(errs, fmt.Errorf("handling call error: %w", err))
		}
	}

	if !handled {
		slog.Warn("unhandled call error",
			slog.String("chargeStationId", chargeStationId),
			slog.String("action", callErr.Action),
			slog.String("messageId", callErr.MessageId),
			slog.String("code", string(callErr.ErrorCode)),
			slog.String("description", callErr.ErrorDescription))
		return nil
	}

	return errors.Join(errs...)
}

// schemaValidationError maps an error returned by schemas.Validate. A payload that
// violates its schema is a FormatViolation. Any other error means that the schema
// itself could not be loaded or compiled: that is a fault in the CSMS, so it is left
// to the default InternalError mapping in Handle and carries the schema path so that
// the missing or broken file shows up in the error log.
func schemaValidationError(schemaFile string, err error) error {
	var validationErr *jsonschema.ValidationError
	if errors.As(err, &validationErr) {
		return transport.NewError(transport.ErrorFormatViolation, err)
	}
	return fmt.Errorf("schema %s: %w", schemaFile, err)
}
