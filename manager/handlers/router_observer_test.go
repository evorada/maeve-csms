// SPDX-License-Identifier: Apache-2.0

package handlers_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"go.opentelemetry.io/otel/codes"
)

// recordingObserver is a CallOutcomeObserver that appends what it sees to steps and
// answers with tracked (or panics when panicWith is set).
type recordingObserver struct {
	steps     *[]string
	tracked   bool
	panicWith any
	seen      []*transport.Message
}

func (o *recordingObserver) observe(kind string, chargeStationId string, msg *transport.Message) bool {
	*o.steps = append(*o.steps, kind+" "+chargeStationId+" "+msg.MessageId)
	o.seen = append(o.seen, msg)
	if o.panicWith != nil {
		panic(o.panicWith)
	}
	return o.tracked
}

func (o *recordingObserver) ObserveCallResult(_ context.Context, chargeStationId string, msg *transport.Message) bool {
	return o.observe("observer result", chargeStationId, msg)
}

func (o *recordingObserver) ObserveCallError(_ context.Context, chargeStationId string, msg *transport.Message) bool {
	return o.observe("observer error", chargeStationId, msg)
}

// resultRoutes is a route for the action "Result" whose handlers append to steps.
func resultRoutes(steps *[]string) map[string]handlers.CallResultRoute {
	return map[string]handlers.CallResultRoute{
		"Result": {
			NewRequest:     func() ocpp.Request { return new(callErrorRequest) },
			NewResponse:    func() ocpp.Response { return new(fakeResponse) },
			RequestSchema:  "schemas/EmptySchema.json",
			ResponseSchema: "schemas/EmptySchema.json",
			Handler: handlers.CallResultHandlerFunc(func(_ context.Context, chargeStationId string, _ ocpp.Request, _ ocpp.Response, _ any) error {
				*steps = append(*steps, "route result "+chargeStationId)
				return nil
			}),
			ErrorHandler: handlers.CallErrorHandlerFunc(func(_ context.Context, chargeStationId string, _ ocpp.Request, _ *handlers.CallError, _ any) error {
				*steps = append(*steps, "route error "+chargeStationId)
				return nil
			}),
		},
	}
}

// routeAndReport routes msg inside a span and returns the status of that span.
func routeAndReport(t *testing.T, router handlers.Router, msg *transport.Message) (codes.Code, string) {
	t.Helper()
	tracer, exporter := testutil.GetTracer()
	func() {
		ctx, span := tracer.Start(context.Background(), "test")
		defer span.End()
		router.Handle(ctx, "cs001", msg)
	}()
	spans := exporter.GetSpans()
	require.NotEmpty(t, spans)
	if len(spans[0].Events) > 0 {
		for _, attr := range spans[0].Events[0].Attributes {
			if attr.Key == "exception.message" {
				return spans[0].Status.Code, attr.Value.AsString()
			}
		}
	}
	return spans[0].Status.Code, ""
}

func TestRouterShowsCallResultsToTheObserverBeforeTheRoute(t *testing.T) {
	for name, tracked := range map[string]bool{"tracked": true, "not tracked": false} {
		t.Run(name, func(t *testing.T) {
			var steps []string
			observer := &recordingObserver{steps: &steps, tracked: tracked}
			emitter := new(FakeEmitter)
			router := handlers.Router{
				Emitter:          emitter,
				SchemaFS:         os.DirFS("testdata"),
				CallResultRoutes: resultRoutes(&steps),
				Observer:         observer,
			}

			msg := &transport.Message{MessageType: transport.MessageTypeCallResult, Action: "Result", MessageId: "msg-001",
				RequestPayload: []byte(`{}`), ResponsePayload: []byte(`{}`)}
			code, _ := routeAndReport(t, router, msg)

			// the per-action handler still runs, after the observer
			assert.Equal(t, []string{"observer result cs001 msg-001", "route result cs001"}, steps)
			assert.Same(t, msg, observer.seen[0])
			assert.Equal(t, codes.Ok, code)
			assert.False(t, emitter.called)
		})
	}
}

func TestRouterShowsCallErrorsToTheObserverBeforeTheHandlers(t *testing.T) {
	for name, tracked := range map[string]bool{"tracked": true, "not tracked": false} {
		t.Run(name, func(t *testing.T) {
			var steps []string
			router := handlers.Router{
				Emitter:          new(FakeEmitter),
				SchemaFS:         os.DirFS("testdata"),
				CallResultRoutes: resultRoutes(&steps),
				CallErrorHandler: handlers.CallErrorHandlerFunc(func(_ context.Context, chargeStationId string, _ ocpp.Request, _ *handlers.CallError, _ any) error {
					steps = append(steps, "router-wide error "+chargeStationId)
					return nil
				}),
				Observer: &recordingObserver{steps: &steps, tracked: tracked},
			}

			code, _ := routeAndReport(t, router, newCallErrorMsg(`{}`))

			assert.Equal(t, []string{"observer error cs001 msg-001", "route error cs001", "router-wide error cs001"}, steps)
			assert.Equal(t, codes.Ok, code)
		})
	}
}

func TestRouterAcceptsATrackedCallResultWithoutARoute(t *testing.T) {
	var steps []string
	router := handlers.Router{
		Emitter:          new(FakeEmitter),
		SchemaFS:         os.DirFS("testdata"),
		CallResultRoutes: map[string]handlers.CallResultRoute{},
		Observer:         &recordingObserver{steps: &steps, tracked: true},
	}

	code, message := routeAndReport(t, router, &resultMsg)

	assert.Equal(t, []string{"observer result cs001 "}, steps)
	assert.Equal(t, codes.Ok, code)
	assert.Empty(t, message)
}

func TestRouterRefusesAnUntrackedCallResultWithoutARoute(t *testing.T) {
	var steps []string
	router := handlers.Router{
		Emitter:          new(FakeEmitter),
		SchemaFS:         os.DirFS("testdata"),
		CallResultRoutes: map[string]handlers.CallResultRoute{},
		Observer:         &recordingObserver{steps: &steps, tracked: false},
	}

	code, message := routeAndReport(t, router, &resultMsg)

	assert.Equal(t, []string{"observer result cs001 "}, steps)
	assert.Equal(t, codes.Error, code)
	assert.Equal(t, "routing request: NotImplemented: Result result not implemented", message)
}

func TestRouterRoutesAsBeforeWhenTheObserverPanics(t *testing.T) {
	t.Run("call result with a route", func(t *testing.T) {
		var steps []string
		router := handlers.Router{
			Emitter:          new(FakeEmitter),
			SchemaFS:         os.DirFS("testdata"),
			CallResultRoutes: resultRoutes(&steps),
			Observer:         &recordingObserver{steps: &steps, tracked: true, panicWith: "observer bug"},
		}

		code, _ := routeAndReport(t, router, &transport.Message{MessageType: transport.MessageTypeCallResult,
			Action: "Result", MessageId: "msg-001", RequestPayload: []byte(`{}`), ResponsePayload: []byte(`{}`)})

		assert.Equal(t, []string{"observer result cs001 msg-001", "route result cs001"}, steps)
		assert.Equal(t, codes.Ok, code)
	})

	t.Run("call result without a route counts as untracked", func(t *testing.T) {
		var steps []string
		router := handlers.Router{
			Emitter:          new(FakeEmitter),
			SchemaFS:         os.DirFS("testdata"),
			CallResultRoutes: map[string]handlers.CallResultRoute{},
			Observer:         &recordingObserver{steps: &steps, tracked: true, panicWith: "observer bug"},
		}

		code, message := routeAndReport(t, router, &resultMsg)

		assert.Equal(t, codes.Error, code)
		assert.Equal(t, "routing request: NotImplemented: Result result not implemented", message)
	})

	t.Run("call error", func(t *testing.T) {
		var steps []string
		router := handlers.Router{
			Emitter:          new(FakeEmitter),
			SchemaFS:         os.DirFS("testdata"),
			CallResultRoutes: resultRoutes(&steps),
			Observer:         &recordingObserver{steps: &steps, tracked: true, panicWith: "observer bug"},
		}

		code, _ := routeAndReport(t, router, newCallErrorMsg(`{}`))

		assert.Equal(t, []string{"observer error cs001 msg-001", "route error cs001"}, steps)
		assert.Equal(t, codes.Ok, code)
	})
}

func TestRouterNeverShowsCallsToTheObserver(t *testing.T) {
	var steps []string
	emitter := new(FakeEmitter)
	router := handlers.Router{
		Emitter:     emitter,
		SchemaFS:    os.DirFS("testdata"),
		OcppVersion: transport.OcppVersion16,
		CallRoutes: map[string]handlers.CallRoute{
			"Heartbeat": {
				NewRequest:     func() ocpp.Request { return new(fakeRequest) },
				RequestSchema:  "schemas/EmptySchema.json",
				ResponseSchema: "schemas/EmptySchema.json",
				Handler: handlers.CallHandlerFunc(func(context.Context, string, ocpp.Request) (ocpp.Response, error) {
					return new(fakeResponse), nil
				}),
			},
		},
		Observer: &recordingObserver{steps: &steps, tracked: true, panicWith: "must not be called"},
	}

	code, _ := routeAndReport(t, router, &transport.Message{MessageType: transport.MessageTypeCall,
		Action: "Heartbeat", MessageId: "hb-1", RequestPayload: []byte(`{}`)})

	assert.Empty(t, steps)
	assert.Equal(t, codes.Ok, code)
	require.True(t, emitter.called)
	assert.Equal(t, transport.MessageTypeCallResult, emitter.msg.MessageType)
	assert.Equal(t, "hb-1", emitter.msg.MessageId)
}
