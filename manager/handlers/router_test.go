// SPDX-License-Identifier: Apache-2.0

package handlers_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers"
	handlers201 "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"k8s.io/utils/clock"

	"github.com/stretchr/testify/assert"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
)

var heartbeatMsg = transport.Message{
	Action:         "Heartbeat",
	MessageType:    transport.MessageTypeCall,
	RequestPayload: []byte("{}"),
}

var resultMsg = transport.Message{
	Action:          "Result",
	MessageType:     transport.MessageTypeCallResult,
	RequestPayload:  []byte("{}"),
	ResponsePayload: []byte("{}"),
}

func TestRouterHandlesCall(t *testing.T) {
	emitter := new(FakeEmitter)

	router := handlers.Router{
		Emitter:     emitter,
		SchemaFS:    schemas.OcppSchemas,
		OcppVersion: transport.OcppVersion201,
		CallRoutes: map[string]handlers.CallRoute{
			"Heartbeat": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.HeartbeatRequestJson) },
				RequestSchema:  "ocpp201/HeartbeatRequest.json",
				ResponseSchema: "ocpp201/HeartbeatResponse.json",
				Handler: handlers201.HeartbeatHandler{
					Clock: clock.RealClock{},
				},
			},
		},
	}

	router.Handle(context.Background(), "id", &heartbeatMsg)

	assert.Equal(t, "id", emitter.chargeStationId)
	assert.Equal(t, transport.MessageTypeCallResult, emitter.msg.MessageType)
	assert.Equal(t, "Heartbeat", emitter.msg.Action)
	assert.Equal(t, "", emitter.msg.MessageId)
	assert.NotNil(t, emitter.msg.ResponsePayload)
}

func TestRouterErrorWhenNoCallRoute(t *testing.T) {
	emitter := new(FakeEmitter)

	router := handlers.Router{
		Emitter:     emitter,
		SchemaFS:    schemas.OcppSchemas,
		OcppVersion: transport.OcppVersion201,
		CallRoutes:  map[string]handlers.CallRoute{},
	}

	router.Handle(context.Background(), "id", &heartbeatMsg)

	assert.Equal(t, "id", emitter.chargeStationId)
	assert.Equal(t, transport.MessageTypeCallError, emitter.msg.MessageType)
	assert.Equal(t, "Heartbeat", emitter.msg.Action)
	assert.Equal(t, "", emitter.msg.MessageId)
	assert.Equal(t, transport.ErrorNotImplemented, emitter.msg.ErrorCode)
	assert.Nil(t, emitter.msg.ResponsePayload)
}

func TestRouterErrorWhenCallRequestPayloadIsInvalid(t *testing.T) {
	emitter := new(FakeEmitter)

	router := handlers.Router{
		Emitter:  emitter,
		SchemaFS: schemas.OcppSchemas,
		CallRoutes: map[string]handlers.CallRoute{
			"Heartbeat": {
				NewRequest:     func() ocpp.Request { return new(noUnmarshalRequest) },
				RequestSchema:  "ocpp201/HeartbeatRequest.json",
				ResponseSchema: "ocpp201/HeartbeatResponse.json",
				Handler: handlers201.HeartbeatHandler{
					Clock: clock.RealClock{},
				},
			},
		},
	}

	// the heartbeat request schema does not allow additional properties
	var heartbeatMsgWithInvalidPayload = transport.Message{
		Action:         "Heartbeat",
		MessageType:    transport.MessageTypeCall,
		RequestPayload: []byte(`{"unexpected": true}`),
	}

	router.Handle(context.Background(), "id", &heartbeatMsgWithInvalidPayload)

	require.NotNil(t, emitter.msg)
	assert.Equal(t, "id", emitter.chargeStationId)
	assert.Equal(t, transport.MessageTypeCallError, emitter.msg.MessageType)
	assert.Equal(t, "Heartbeat", emitter.msg.Action)
	assert.Equal(t, "", emitter.msg.MessageId)
	assert.Equal(t, transport.ErrorFormatViolation, emitter.msg.ErrorCode)
	assert.Nil(t, emitter.msg.ResponsePayload)
}

func TestRouterInternalErrorWhenSchemaMissing(t *testing.T) {
	tracer, exporter := testutil.GetTracer()

	emitter := new(FakeEmitter)

	handlerCalled := false
	handler := func(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
		handlerCalled = true
		return new(ocpp201.HeartbeatResponseJson), nil
	}

	router := handlers.Router{
		Emitter:  emitter,
		SchemaFS: schemas.OcppSchemas,
		CallRoutes: map[string]handlers.CallRoute{
			"Heartbeat": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.HeartbeatRequestJson) },
				RequestSchema:  "ocpp201/DoesNotExist.json",
				ResponseSchema: "ocpp201/HeartbeatResponse.json",
				Handler:        handlers.CallHandlerFunc(handler),
			},
		},
	}

	func() {
		ctx, span := tracer.Start(context.Background(), "test")
		defer span.End()
		router.Handle(ctx, "id", &heartbeatMsg)
	}()

	// a schema that cannot be loaded is a fault in the CSMS, not in the message
	require.NotNil(t, emitter.msg)
	assert.Equal(t, "id", emitter.chargeStationId)
	assert.Equal(t, transport.MessageTypeCallError, emitter.msg.MessageType)
	assert.Equal(t, "Heartbeat", emitter.msg.Action)
	assert.Equal(t, transport.ErrorInternalError, emitter.msg.ErrorCode)
	assert.Nil(t, emitter.msg.ResponsePayload)
	assert.False(t, handlerCalled)

	// the schema path is part of the recorded error
	require.Greater(t, len(exporter.GetSpans()), 0)
	assert.Equal(t, codes.Error, exporter.GetSpans()[0].Status.Code)
	require.Greater(t, len(exporter.GetSpans()[0].Events), 0)
	testutil.AssertAttributes(t, exporter.GetSpans()[0].Events[0].Attributes, map[string]any{
		"exception.type": "*fmt.wrapError",
		"exception.message": func(val attribute.Value) bool {
			return strings.Contains(val.AsString(), "validating Heartbeat request: schema ocpp201/DoesNotExist.json")
		},
	})
}

func TestRouterErrorWhenCantUnmarshallCallRequestPayload(t *testing.T) {
	emitter := new(FakeEmitter)

	router := handlers.Router{
		Emitter:  emitter,
		SchemaFS: os.DirFS("testdata"),
		CallRoutes: map[string]handlers.CallRoute{
			"MyCall": {
				NewRequest:     func() ocpp.Request { return new(noUnmarshalRequest) },
				RequestSchema:  "schemas/EmptySchema.json",
				ResponseSchema: "schemas/EmptySchema.json",
				Handler: handlers201.HeartbeatHandler{
					Clock: clock.RealClock{},
				},
			},
		},
	}

	var heartbeatMsgWithEmptyPayload = transport.Message{
		Action:         "MyCall",
		MessageType:    transport.MessageTypeCall,
		RequestPayload: []byte("{}"),
	}

	router.Handle(context.Background(), "id", &heartbeatMsgWithEmptyPayload)

	assert.Equal(t, "id", emitter.chargeStationId)
	assert.Equal(t, transport.MessageTypeCallError, emitter.msg.MessageType)
	assert.Equal(t, "MyCall", emitter.msg.Action)
	assert.Equal(t, "", emitter.msg.MessageId)
	assert.Equal(t, transport.ErrorInternalError, emitter.msg.ErrorCode)
	assert.Nil(t, emitter.msg.ResponsePayload)
}

func TestRouterErrorWhenCallHandlerErrors(t *testing.T) {
	emitter := new(FakeEmitter)

	handler := func(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
		return nil, errors.New("handler error")
	}

	router := handlers.Router{
		Emitter:  emitter,
		SchemaFS: schemas.OcppSchemas,
		CallRoutes: map[string]handlers.CallRoute{
			"Heartbeat": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.HeartbeatRequestJson) },
				RequestSchema:  "ocpp201/HeartbeatRequest.json",
				ResponseSchema: "ocpp201/HeartbeatResponse.json",
				Handler:        handlers.CallHandlerFunc(handler),
			},
		},
	}

	router.Handle(context.Background(), "id", &heartbeatMsg)

	assert.Equal(t, "id", emitter.chargeStationId)
	assert.Equal(t, transport.MessageTypeCallError, emitter.msg.MessageType)
	assert.Equal(t, "Heartbeat", emitter.msg.Action)
	assert.Equal(t, "", emitter.msg.MessageId)
	assert.Equal(t, transport.ErrorInternalError, emitter.msg.ErrorCode)
	assert.Nil(t, emitter.msg.ResponsePayload)
}

func TestRouterErrorWhenErrorMarshallingCallHandlerResponse(t *testing.T) {
	emitter := new(FakeEmitter)

	handler := func(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
		return new(noMarshalResponse), nil
	}

	router := handlers.Router{
		Emitter:  emitter,
		SchemaFS: schemas.OcppSchemas,
		CallRoutes: map[string]handlers.CallRoute{
			"Heartbeat": {
				NewRequest:     func() ocpp.Request { return new(ocpp201.HeartbeatRequestJson) },
				RequestSchema:  "ocpp201/HeartbeatRequest.json",
				ResponseSchema: "ocpp201/HeartbeatResponse.json",
				Handler:        handlers.CallHandlerFunc(handler),
			},
		},
	}

	router.Handle(context.Background(), "id", &heartbeatMsg)

	assert.Equal(t, "id", emitter.chargeStationId)
	assert.Equal(t, transport.MessageTypeCallError, emitter.msg.MessageType)
	assert.Equal(t, "Heartbeat", emitter.msg.Action)
	assert.Equal(t, "", emitter.msg.MessageId)
	assert.Equal(t, transport.ErrorInternalError, emitter.msg.ErrorCode)
	assert.Nil(t, emitter.msg.ResponsePayload)
}

func TestRouterHandlesCallResult(t *testing.T) {
	tracer, exporter := testutil.GetTracer()

	emitter := new(FakeEmitter)

	handler := func(ctx context.Context,
		chargeStationId string,
		request ocpp.Request,
		response ocpp.Response,
		state any) error {
		return nil
	}

	router := handlers.Router{
		Emitter:  emitter,
		SchemaFS: os.DirFS("testdata"),
		CallResultRoutes: map[string]handlers.CallResultRoute{
			"Result": {
				NewRequest:     func() ocpp.Request { return new(fakeRequest) },
				NewResponse:    func() ocpp.Response { return new(fakeResponse) },
				RequestSchema:  "schemas/EmptySchema.json",
				ResponseSchema: "schemas/EmptySchema.json",
				Handler:        handlers.CallResultHandlerFunc(handler),
			},
		},
	}

	func() {
		ctx, span := tracer.Start(context.Background(), "test")
		defer span.End()
		router.Handle(ctx, "id", &resultMsg)
	}()

	// for a call response the emitter should never be called
	assert.False(t, emitter.called)

	// check that no error was produced using telemetry
	assert.Greater(t, len(exporter.GetSpans()), 0)
	assert.Equal(t, codes.Ok, exporter.GetSpans()[0].Status.Code)
}

func TestRouterErrorWhenNoCallResultRoute(t *testing.T) {
	tracer, exporter := testutil.GetTracer()

	emitter := new(FakeEmitter)

	router := handlers.Router{
		Emitter:          emitter,
		SchemaFS:         os.DirFS("testdata"),
		CallResultRoutes: map[string]handlers.CallResultRoute{},
	}

	func() {
		ctx, span := tracer.Start(context.Background(), "test")
		defer span.End()
		router.Handle(ctx, "id", &resultMsg)
	}()

	// for a call response the emitter should never be called
	assert.False(t, emitter.called)

	// check that an error was produced using telemetry
	require.Greater(t, len(exporter.GetSpans()), 0)
	assert.Equal(t, codes.Error, exporter.GetSpans()[0].Status.Code)
	require.Greater(t, len(exporter.GetSpans()[0].Events), 0)
	testutil.AssertAttributes(t, exporter.GetSpans()[0].Events[0].Attributes, map[string]any{
		"exception.type":    "*fmt.wrapError",
		"exception.message": "routing request: NotImplemented: Result result not implemented",
	})
}

func TestRouterErrorWhenInvalidCallResultRequestPayload(t *testing.T) {
	tracer, exporter := testutil.GetTracer()

	emitter := new(FakeEmitter)

	handler := func(ctx context.Context,
		chargeStationId string,
		request ocpp.Request,
		response ocpp.Response,
		state any) error {
		return nil
	}

	router := handlers.Router{
		Emitter:  emitter,
		SchemaFS: os.DirFS("testdata"),
		CallResultRoutes: map[string]handlers.CallResultRoute{
			"MyCallResult": {
				NewRequest:     func() ocpp.Request { return nil },
				NewResponse:    func() ocpp.Response { return nil },
				RequestSchema:  "schemas/RequiredFieldSchema.json",
				ResponseSchema: "schemas/EmptySchema.json",
				Handler:        handlers.CallResultHandlerFunc(handler),
			},
		},
	}

	var resultWithEmptyRequestPayload = transport.Message{
		Action:          "MyCallResult",
		MessageType:     transport.MessageTypeCallResult,
		RequestPayload:  []byte("{}"),
		ResponsePayload: []byte("{}"),
	}

	func() {
		ctx, span := tracer.Start(context.Background(), "test")
		defer span.End()

		router.Handle(ctx, "id", &resultWithEmptyRequestPayload)
	}()

	// for a call response the emitter should never be called
	assert.False(t, emitter.called)

	// check that an error was produced using telemetry
	require.Greater(t, len(exporter.GetSpans()), 0)
	assert.Equal(t, codes.Error, exporter.GetSpans()[0].Status.Code)
	require.Greater(t, len(exporter.GetSpans()[0].Events), 0)
	testutil.AssertAttributes(t, exporter.GetSpans()[0].Events[0].Attributes, map[string]any{
		"exception.type": "*fmt.wrapError",
		"exception.message": func(val attribute.Value) bool {
			return strings.Contains(val.AsString(), "validating MyCallResult request")
		},
	})
}

func TestRouterErrorWhenCantUnmarshallCallResultRequestPayload(t *testing.T) {
	tracer, exporter := testutil.GetTracer()

	emitter := new(FakeEmitter)

	handler := func(ctx context.Context,
		chargeStationId string,
		request ocpp.Request,
		response ocpp.Response,
		state any) error {
		return nil
	}

	router := handlers.Router{
		Emitter:  emitter,
		SchemaFS: os.DirFS("testdata"),
		CallResultRoutes: map[string]handlers.CallResultRoute{
			"MyCallResult": {
				NewRequest:     func() ocpp.Request { return nil },
				NewResponse:    func() ocpp.Response { return nil },
				RequestSchema:  "schemas/EmptySchema.json",
				ResponseSchema: "schemas/EmptySchema.json",
				Handler:        handlers.CallResultHandlerFunc(handler),
			},
		},
	}

	var resultWithEmptyRequestPayload = transport.Message{
		Action:          "MyCallResult",
		MessageType:     transport.MessageTypeCallResult,
		RequestPayload:  []byte("{}"),
		ResponsePayload: []byte("{}"),
	}

	func() {
		ctx, span := tracer.Start(context.Background(), "test")
		defer span.End()
		router.Handle(ctx, "id", &resultWithEmptyRequestPayload)
	}()

	// for a call response the emitter should never be called
	assert.False(t, emitter.called)

	// check that an error was produced using telemetry
	require.Greater(t, len(exporter.GetSpans()), 0)
	assert.Equal(t, codes.Error, exporter.GetSpans()[0].Status.Code)
	require.Greater(t, len(exporter.GetSpans()[0].Events), 0)
	testutil.AssertAttributes(t, exporter.GetSpans()[0].Events[0].Attributes, map[string]any{
		"exception.type": "*fmt.wrapError",
		"exception.message": func(val attribute.Value) bool {
			return strings.Contains(val.AsString(), "unmarshalling MyCallResult request payload")
		},
	})
}

func TestRouterErrorWhenInvalidCallResultResponsePayload(t *testing.T) {
	tracer, exporter := testutil.GetTracer()

	emitter := new(FakeEmitter)

	handler := func(ctx context.Context,
		chargeStationId string,
		request ocpp.Request,
		response ocpp.Response,
		state any) error {
		return nil
	}

	router := handlers.Router{
		Emitter:  emitter,
		SchemaFS: os.DirFS("testdata"),
		CallResultRoutes: map[string]handlers.CallResultRoute{
			"MyCallResult": {
				NewRequest:     func() ocpp.Request { return nil },
				NewResponse:    func() ocpp.Response { return nil },
				RequestSchema:  "schemas/EmptySchema.json",
				ResponseSchema: "schemas/RequiredFieldSchema.json",
				Handler:        handlers.CallResultHandlerFunc(handler),
			},
		},
	}

	var resultWithEmptyRequestPayload = transport.Message{
		Action:          "MyCallResult",
		MessageType:     transport.MessageTypeCallResult,
		RequestPayload:  []byte("{}"),
		ResponsePayload: []byte("{}"),
	}

	func() {
		ctx, span := tracer.Start(context.Background(), "test")
		defer span.End()
		router.Handle(ctx, "id", &resultWithEmptyRequestPayload)
	}()

	// for a call response the emitter should never be called
	assert.False(t, emitter.called)

	// check that an error was produced using telemetry
	require.Greater(t, len(exporter.GetSpans()), 0)
	assert.Equal(t, codes.Error, exporter.GetSpans()[0].Status.Code)
	require.Greater(t, len(exporter.GetSpans()[0].Events), 0)
	testutil.AssertAttributes(t, exporter.GetSpans()[0].Events[0].Attributes, map[string]any{
		"exception.type": "*fmt.wrapError",
		"exception.message": func(val attribute.Value) bool {
			return strings.Contains(val.AsString(), "validating MyCallResult response: FormatViolation")
		},
	})
}

func TestRouterErrorWhenCallResultResponseSchemaMissing(t *testing.T) {
	tracer, exporter := testutil.GetTracer()

	emitter := new(FakeEmitter)

	handlerCalled := false
	handler := func(ctx context.Context,
		chargeStationId string,
		request ocpp.Request,
		response ocpp.Response,
		state any) error {
		handlerCalled = true
		return nil
	}

	router := handlers.Router{
		Emitter:  emitter,
		SchemaFS: os.DirFS("testdata"),
		CallResultRoutes: map[string]handlers.CallResultRoute{
			"Result": {
				NewRequest:     func() ocpp.Request { return new(fakeRequest) },
				NewResponse:    func() ocpp.Response { return new(fakeResponse) },
				RequestSchema:  "schemas/EmptySchema.json",
				ResponseSchema: "schemas/DoesNotExist.json",
				Handler:        handlers.CallResultHandlerFunc(handler),
			},
		},
	}

	func() {
		ctx, span := tracer.Start(context.Background(), "test")
		defer span.End()
		router.Handle(ctx, "id", &resultMsg)
	}()

	// for a call response the emitter should never be called
	assert.False(t, emitter.called)
	assert.False(t, handlerCalled)

	// check that the error names the schema and is not reported as a format violation
	require.Greater(t, len(exporter.GetSpans()), 0)
	assert.Equal(t, codes.Error, exporter.GetSpans()[0].Status.Code)
	require.Greater(t, len(exporter.GetSpans()[0].Events), 0)
	testutil.AssertAttributes(t, exporter.GetSpans()[0].Events[0].Attributes, map[string]any{
		"exception.type": "*fmt.wrapError",
		"exception.message": func(val attribute.Value) bool {
			msg := val.AsString()
			return strings.Contains(msg, "validating Result response: schema schemas/DoesNotExist.json") &&
				!strings.Contains(msg, string(transport.ErrorFormatViolation))
		},
	})
}

func TestRouterErrorWhenCantUnmarshallCallResultResponsePayload(t *testing.T) {
	tracer, exporter := testutil.GetTracer()

	emitter := new(FakeEmitter)

	handler := func(ctx context.Context,
		chargeStationId string,
		request ocpp.Request,
		response ocpp.Response,
		state any) error {
		return nil
	}

	router := handlers.Router{
		Emitter:  emitter,
		SchemaFS: os.DirFS("testdata"),
		CallResultRoutes: map[string]handlers.CallResultRoute{
			"MyCallResult": {
				NewRequest:     func() ocpp.Request { return new(fakeRequest) },
				NewResponse:    func() ocpp.Response { return new(noUnmarshalResponse) },
				RequestSchema:  "schemas/EmptySchema.json",
				ResponseSchema: "schemas/EmptySchema.json",
				Handler:        handlers.CallResultHandlerFunc(handler),
			},
		},
	}

	var resultWithEmptyResponsePayload = transport.Message{
		Action:          "MyCallResult",
		MessageType:     transport.MessageTypeCallResult,
		RequestPayload:  []byte("{}"),
		ResponsePayload: []byte("{}"),
	}

	func() {
		ctx, span := tracer.Start(context.Background(), "test")
		defer span.End()
		router.Handle(ctx, "id", &resultWithEmptyResponsePayload)
	}()

	// for a call response the emitter should never be called
	assert.False(t, emitter.called)

	// check that an error was produced using telemetry
	require.Greater(t, len(exporter.GetSpans()), 0)
	assert.Equal(t, codes.Error, exporter.GetSpans()[0].Status.Code)
	require.Greater(t, len(exporter.GetSpans()[0].Events), 0)
	testutil.AssertAttributes(t, exporter.GetSpans()[0].Events[0].Attributes, map[string]any{
		"exception.type": "*errors.errorString",
		"exception.message": func(val attribute.Value) bool {
			return strings.Contains(val.AsString(), "unmarshalling MyCallResult response payload")
		},
	})
}

func TestRouterErrorWhenCallResultHandlerErrors(t *testing.T) {
	tracer, exporter := testutil.GetTracer()

	emitter := new(FakeEmitter)

	handler := func(ctx context.Context,
		chargeStationId string,
		request ocpp.Request,
		response ocpp.Response,
		state any) error {
		return errors.New("handler error")
	}

	router := handlers.Router{
		Emitter:  emitter,
		SchemaFS: os.DirFS("testdata"),
		CallResultRoutes: map[string]handlers.CallResultRoute{
			"Result": {
				NewRequest:     func() ocpp.Request { return new(fakeRequest) },
				NewResponse:    func() ocpp.Response { return new(fakeResponse) },
				RequestSchema:  "schemas/EmptySchema.json",
				ResponseSchema: "schemas/EmptySchema.json",
				Handler:        handlers.CallResultHandlerFunc(handler),
			},
		},
	}

	func() {
		ctx, span := tracer.Start(context.Background(), "test")
		defer span.End()
		router.Handle(ctx, "id", &resultMsg)
	}()

	// for a call response the emitter should never be called
	assert.False(t, emitter.called)

	// check that an error was produced using telemetry
	require.Greater(t, len(exporter.GetSpans()), 0)
	assert.Equal(t, codes.Error, exporter.GetSpans()[0].Status.Code)
	require.Greater(t, len(exporter.GetSpans()[0].Events), 0)
	testutil.AssertAttributes(t, exporter.GetSpans()[0].Events[0].Attributes, map[string]any{
		"exception.type": "*errors.errorString",
		"exception.message": func(val attribute.Value) bool {
			return strings.Contains(val.AsString(), "handler error")
		},
	})
}

type fakeRequest struct{}

func (*fakeRequest) IsRequest() {}

type fakeResponse struct{}

func (*fakeResponse) IsResponse() {}

type noUnmarshalRequest struct{}

func (*noUnmarshalRequest) IsRequest() {}

func (*noUnmarshalRequest) UnmarshalJSON(_ []byte) error {
	return errors.New("expected to fail")
}

type noUnmarshalResponse struct{}

func (*noUnmarshalResponse) IsResponse() {}

func (*noUnmarshalResponse) UnmarshalJSON(_ []byte) error {
	return errors.New("expected to fail")
}

type noMarshalResponse struct{}

func (*noMarshalResponse) IsResponse() {}

func (*noMarshalResponse) MarshalJSON() ([]byte, error) {
	return nil, errors.New("expected to fail")
}

// callErrorRequest is the request of the CSMS-initiated call that the call error tests answer.
type callErrorRequest struct {
	Key string `json:"key"`
}

func (*callErrorRequest) IsRequest() {}

func newCallErrorMsg(requestPayload string) *transport.Message {
	return &transport.Message{
		MessageType:      transport.MessageTypeCallError,
		Action:           "Result",
		MessageId:        "msg-001",
		RequestPayload:   []byte(requestPayload),
		ErrorCode:        transport.ErrorNotSupported,
		ErrorDescription: "key is not supported",
		State:            []byte(`"some state"`),
	}
}

// callErrorCall records the arguments of one call to a call error handler.
type callErrorCall struct {
	handler         string
	chargeStationId string
	request         ocpp.Request
	requestIsNil    bool
	callError       *handlers.CallError
	state           any
}

// recordCallErrors returns a call error handler that appends its arguments to calls and returns err.
func recordCallErrors(calls *[]callErrorCall, name string, err error) handlers.CallErrorHandler {
	return handlers.CallErrorHandlerFunc(func(ctx context.Context,
		chargeStationId string,
		request ocpp.Request,
		callError *handlers.CallError,
		state any) error {
		*calls = append(*calls, callErrorCall{
			handler:         name,
			chargeStationId: chargeStationId,
			request:         request,
			requestIsNil:    request == nil,
			callError:       callError,
			state:           state,
		})
		return err
	})
}

func callErrorRoutes(errorHandler handlers.CallErrorHandler) map[string]handlers.CallResultRoute {
	return map[string]handlers.CallResultRoute{
		"Result": {
			NewRequest:     func() ocpp.Request { return new(callErrorRequest) },
			NewResponse:    func() ocpp.Response { return new(fakeResponse) },
			RequestSchema:  "schemas/EmptySchema.json",
			ResponseSchema: "schemas/EmptySchema.json",
			Handler: handlers.CallResultHandlerFunc(func(context.Context, string, ocpp.Request, ocpp.Response, any) error {
				return errors.New("the call result handler must not be called for a call error")
			}),
			ErrorHandler: errorHandler,
		},
	}
}

func TestRouterDeliversCallErrorToRouteErrorHandler(t *testing.T) {
	tracer, exporter := testutil.GetTracer()

	emitter := new(FakeEmitter)

	var calls []callErrorCall
	router := handlers.Router{
		Emitter:          emitter,
		SchemaFS:         os.DirFS("testdata"),
		CallResultRoutes: callErrorRoutes(recordCallErrors(&calls, "route", nil)),
	}

	msg := newCallErrorMsg(`{"key":"HeartbeatInterval"}`)
	func() {
		ctx, span := tracer.Start(context.Background(), "test")
		defer span.End()
		router.Handle(ctx, "cs001", msg)
	}()

	require.Len(t, calls, 1)
	assert.Equal(t, "cs001", calls[0].chargeStationId)
	assert.Equal(t, &callErrorRequest{Key: "HeartbeatInterval"}, calls[0].request)
	assert.Equal(t, &handlers.CallError{
		MessageId:        "msg-001",
		Action:           "Result",
		ErrorCode:        transport.ErrorNotSupported,
		ErrorDescription: "key is not supported",
	}, calls[0].callError)
	assert.Equal(t, "Result msg-001: NotSupported: key is not supported", calls[0].callError.Error())
	assert.Equal(t, any(msg.State), calls[0].state)

	// a call error is never answered
	assert.False(t, emitter.called)

	// the call error was handled: no error is reported and the span describes the call error
	require.Greater(t, len(exporter.GetSpans()), 0)
	assert.Equal(t, codes.Ok, exporter.GetSpans()[0].Status.Code)
	assert.Empty(t, exporter.GetSpans()[0].Events)
	testutil.AssertAttributes(t, exporter.GetSpans()[0].Attributes, map[string]any{
		"call_error.action":      "Result",
		"call_error.code":        "NotSupported",
		"call_error.description": "key is not supported",
	})
}

func TestRouterCallsRouterWideCallErrorHandlerAfterRouteErrorHandler(t *testing.T) {
	emitter := new(FakeEmitter)

	var calls []callErrorCall
	router := handlers.Router{
		Emitter:          emitter,
		SchemaFS:         os.DirFS("testdata"),
		CallResultRoutes: callErrorRoutes(recordCallErrors(&calls, "route", nil)),
		CallErrorHandler: recordCallErrors(&calls, "router", nil),
	}

	msg := newCallErrorMsg(`{"key":"HeartbeatInterval"}`)
	router.Handle(context.Background(), "cs001", msg)

	require.Len(t, calls, 2)
	assert.Equal(t, "route", calls[0].handler)
	assert.Equal(t, "router", calls[1].handler)

	// the router-wide handler gets the same arguments as the route's handler
	assert.Equal(t, "cs001", calls[1].chargeStationId)
	assert.Equal(t, &callErrorRequest{Key: "HeartbeatInterval"}, calls[1].request)
	assert.Same(t, calls[0].callError, calls[1].callError)
	assert.Equal(t, any(msg.State), calls[1].state)

	assert.False(t, emitter.called)
}

func TestRouterIgnoresCallErrorWithNoRouteAndNoCallErrorHandler(t *testing.T) {
	tracer, exporter := testutil.GetTracer()

	emitter := new(FakeEmitter)

	router := handlers.Router{
		Emitter:          emitter,
		SchemaFS:         os.DirFS("testdata"),
		CallResultRoutes: map[string]handlers.CallResultRoute{},
	}

	func() {
		ctx, span := tracer.Start(context.Background(), "test")
		defer span.End()
		router.Handle(ctx, "cs001", newCallErrorMsg(`{"key":"HeartbeatInterval"}`))
	}()

	assert.False(t, emitter.called)

	// an unhandled call error is not a routing failure
	require.Greater(t, len(exporter.GetSpans()), 0)
	assert.Equal(t, codes.Ok, exporter.GetSpans()[0].Status.Code)
	assert.Empty(t, exporter.GetSpans()[0].Events)
}

func TestRouterIgnoresCallErrorWhenRouteHasNoErrorHandler(t *testing.T) {
	tracer, exporter := testutil.GetTracer()

	emitter := new(FakeEmitter)

	router := handlers.Router{
		Emitter:          emitter,
		SchemaFS:         os.DirFS("testdata"),
		CallResultRoutes: callErrorRoutes(nil),
	}

	func() {
		ctx, span := tracer.Start(context.Background(), "test")
		defer span.End()
		router.Handle(ctx, "cs001", newCallErrorMsg(`{"key":"HeartbeatInterval"}`))
	}()

	assert.False(t, emitter.called)

	require.Greater(t, len(exporter.GetSpans()), 0)
	assert.Equal(t, codes.Ok, exporter.GetSpans()[0].Status.Code)
	assert.Empty(t, exporter.GetSpans()[0].Events)
}

func TestRouterCallsRouterWideCallErrorHandlerWithNilRequestWhenNoRoute(t *testing.T) {
	emitter := new(FakeEmitter)

	var calls []callErrorCall
	router := handlers.Router{
		Emitter:          emitter,
		SchemaFS:         os.DirFS("testdata"),
		CallResultRoutes: map[string]handlers.CallResultRoute{},
		CallErrorHandler: recordCallErrors(&calls, "router", nil),
	}

	router.Handle(context.Background(), "cs001", newCallErrorMsg(`{"key":"HeartbeatInterval"}`))

	require.Len(t, calls, 1)
	assert.Equal(t, "router", calls[0].handler)
	assert.Equal(t, "cs001", calls[0].chargeStationId)
	assert.True(t, calls[0].requestIsNil, "expected a nil request, got %#v", calls[0].request)
	assert.Equal(t, "Result", calls[0].callError.Action)
	assert.Equal(t, "msg-001", calls[0].callError.MessageId)
	assert.Equal(t, transport.ErrorNotSupported, calls[0].callError.ErrorCode)

	assert.False(t, emitter.called)
}

func TestRouterCallsCallErrorHandlersWithNilRequestWhenRequestPayloadCantBeDecoded(t *testing.T) {
	for name, requestPayload := range map[string]string{
		"malformed": `{"key":`,
		"missing":   ``,
	} {
		t.Run(name, func(t *testing.T) {
			tracer, exporter := testutil.GetTracer()

			emitter := new(FakeEmitter)

			var calls []callErrorCall
			router := handlers.Router{
				Emitter:          emitter,
				SchemaFS:         os.DirFS("testdata"),
				CallResultRoutes: callErrorRoutes(recordCallErrors(&calls, "route", nil)),
				CallErrorHandler: recordCallErrors(&calls, "router", nil),
			}

			func() {
				ctx, span := tracer.Start(context.Background(), "test")
				defer span.End()
				router.Handle(ctx, "cs001", newCallErrorMsg(requestPayload))
			}()

			require.Len(t, calls, 2)
			for _, call := range calls {
				assert.True(t, call.requestIsNil, "%s handler: expected a nil request, got %#v", call.handler, call.request)
				assert.Equal(t, "msg-001", call.callError.MessageId)
			}

			assert.False(t, emitter.called)

			// the handlers still ran, so this is not a routing failure
			require.Greater(t, len(exporter.GetSpans()), 0)
			assert.Equal(t, codes.Ok, exporter.GetSpans()[0].Status.Code)
		})
	}
}

func TestRouterErrorWhenCallErrorHandlersError(t *testing.T) {
	tracer, exporter := testutil.GetTracer()

	emitter := new(FakeEmitter)

	var calls []callErrorCall
	router := handlers.Router{
		Emitter:          emitter,
		SchemaFS:         os.DirFS("testdata"),
		CallResultRoutes: callErrorRoutes(recordCallErrors(&calls, "route", errors.New("route handler error"))),
		CallErrorHandler: recordCallErrors(&calls, "router", errors.New("router handler error")),
	}

	func() {
		ctx, span := tracer.Start(context.Background(), "test")
		defer span.End()
		router.Handle(ctx, "cs001", newCallErrorMsg(`{"key":"HeartbeatInterval"}`))
	}()

	// the router-wide handler is still called when the route's handler fails
	require.Len(t, calls, 2)

	// the charge station is not expecting any response to its call error
	assert.False(t, emitter.called)

	// check that both errors were reported using telemetry
	require.Greater(t, len(exporter.GetSpans()), 0)
	assert.Equal(t, codes.Error, exporter.GetSpans()[0].Status.Code)
	require.Greater(t, len(exporter.GetSpans()[0].Events), 0)
	testutil.AssertAttributes(t, exporter.GetSpans()[0].Events[0].Attributes, map[string]any{
		"exception.type": "*errors.joinError",
		"exception.message": func(val attribute.Value) bool {
			msg := val.AsString()
			return strings.Contains(msg, "route handler error") && strings.Contains(msg, "router handler error")
		},
	})
}
