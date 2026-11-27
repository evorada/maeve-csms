// SPDX-License-Identifier: Apache-2.0

package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/connectivity"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/transport"
)

// router returns the OCPP 1.6 router of the manager with the dispatcher of f as its
// observer, sending through the emitter of f.
func (f *fixture) router() transport.MessageHandler {
	return ocpp16.NewRouter(f.emitter, f.clock, f.store, events.NopEmitter{}, &tenancy.Resolver{Stations: f.store},
		nil, nil, nil, nil, time.Minute, schemas.OcppSchemas, f.dispatcher)
}

// responses returns the CallResults and CallErrors that the router sent to the station.
func (f *fixture) responses() []*transport.Message {
	var responses []*transport.Message
	for _, call := range f.emitter.sent() {
		if call.msg.MessageType != transport.MessageTypeCall {
			responses = append(responses, call.msg)
		}
	}
	return responses
}

func TestStationMessagesAreHandledAsBeforeWhileACommandWaitsForItsAnswer(t *testing.T) {
	f := newFixture(t)
	router := f.router()
	cmd := f.enqueue("Reset", `{"type":"Soft"}`)
	require.Equal(t, store.CommandStatusSent, cmd.Status)

	router.Handle(context.Background(), testStation, &transport.Message{MessageType: transport.MessageTypeCall,
		Action: "Heartbeat", MessageId: "hb-1", RequestPayload: json.RawMessage(`{}`)})
	router.Handle(context.Background(), testStation, &transport.Message{MessageType: transport.MessageTypeCall,
		Action: "StatusNotification", MessageId: "sn-1",
		RequestPayload: json.RawMessage(`{"connectorId":1,"errorCode":"NoError","status":"Available"}`)})

	responses := f.responses()
	require.Len(t, responses, 2)
	assert.Equal(t, transport.MessageTypeCallResult, responses[0].MessageType)
	assert.Equal(t, "hb-1", responses[0].MessageId)
	assert.Contains(t, string(responses[0].ResponsePayload), "currentTime")
	assert.Equal(t, transport.MessageTypeCallResult, responses[1].MessageType)
	assert.Equal(t, "sn-1", responses[1].MessageId)
	assert.JSONEq(t, `{}`, string(responses[1].ResponsePayload))

	status, err := f.store.GetConnectorStatus(context.Background(), testStation, 1)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, store.ConnectorStatusAvailable, status.Status)

	// the command still waits for its answer, and no other command went out
	assert.Equal(t, store.CommandStatusSent, f.get(cmd.Id).Status)
	assert.Len(t, f.emitter.sent(), 3)
}

func TestTheRouterCompletesCommandsAndStillRunsTheResultHandlers(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	router := f.router()
	require.NoError(t, f.store.SetChargeStationTriggerMessage(ctx, testStation, &store.ChargeStationTriggerMessage{
		ChargeStationId: testStation, TriggerMessage: store.TriggerMessageStatusNotification,
		TriggerStatus: store.TriggerStatusPending,
	}))
	cmd := f.enqueue("TriggerMessage", `{"requestedMessage":"StatusNotification"}`)

	router.Handle(ctx, testStation, result(cmd, `{"status":"Accepted"}`))

	assert.Equal(t, store.CommandStatusAccepted, f.get(cmd.Id).Status)
	require.Len(t, f.completedEvents(), 1)
	// the result handler of the action ran too: an accepted trigger is deleted
	trigger, err := f.store.LookupChargeStationTriggerMessage(ctx, testStation)
	require.NoError(t, err)
	assert.Nil(t, trigger)
	assert.Empty(t, f.responses(), "a CallResult is never answered")

	t.Run("call error", func(t *testing.T) {
		next := f.enqueue("TriggerMessage", `{"requestedMessage":"Heartbeat"}`)
		f.waitSent(2)

		router.Handle(ctx, testStation, callError(f.get(next.Id), transport.ErrorNotImplemented, "no triggers"))

		failed := f.get(next.Id)
		assert.Equal(t, store.CommandStatusFailed, failed.Status)
		assert.Equal(t, "NotImplemented", deref(failed.ErrorCode))
		trigger, err := f.store.LookupChargeStationTriggerMessage(ctx, testStation)
		require.NoError(t, err)
		require.NotNil(t, trigger, "the error handler of the action ran too")
		assert.Equal(t, store.TriggerStatusNotImplemented, trigger.TriggerStatus)
	})
}

func TestTheRouterAcceptsTheResultOfACommandWithoutAResultRoute(t *testing.T) {
	f := newFixture(t)
	router := f.router()
	cmd := f.enqueue("CertificateSigned", `{"certificateChain":"-----BEGIN CERTIFICATE-----"}`)

	router.Handle(context.Background(), testStation, result(cmd, `{"status":"Accepted"}`))

	assert.Equal(t, store.CommandStatusAccepted, f.get(cmd.Id).Status)
	assert.Empty(t, f.responses())
}

func TestAnswersToTheCallsOfTheSyncJobsKeepTheirHandling(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	router := f.router()
	cmd := f.enqueue("Reset", `{"type":"Soft"}`)
	require.NoError(t, f.store.SetChargeStationTriggerMessage(ctx, testStation, &store.ChargeStationTriggerMessage{
		ChargeStationId: testStation, TriggerMessage: store.TriggerMessageStatusNotification,
		TriggerStatus: store.TriggerStatusPending,
	}))

	// the trigger sync job sends with a random UUID as MessageId
	router.Handle(ctx, testStation, &transport.Message{MessageType: transport.MessageTypeCallResult,
		Action: "TriggerMessage", MessageId: "6f1c2d3e-4b5a-4c6d-8e7f-9a0b1c2d3e4f",
		RequestPayload:  json.RawMessage(`{"requestedMessage":"StatusNotification"}`),
		ResponsePayload: json.RawMessage(`{"status":"Rejected"}`)})

	trigger, err := f.store.LookupChargeStationTriggerMessage(ctx, testStation)
	require.NoError(t, err)
	require.NotNil(t, trigger)
	assert.Equal(t, store.TriggerStatusRejected, trigger.TriggerStatus)
	assert.Equal(t, store.CommandStatusSent, f.get(cmd.Id).Status, "the command is not touched")
	assert.Empty(t, f.completedEvents())
}

// failingCommandStore is a store whose GetCommand fails.
type failingCommandStore struct {
	store.Engine
}

func (failingCommandStore) GetCommand(context.Context, string) (*store.Command, error) {
	return nil, errors.New("database down")
}

func TestAStoreErrorOfTheObserverDoesNotStopTheHandling(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.dispatcher.Store = failingCommandStore{Engine: f.store}
	router := f.router()
	require.NoError(t, f.store.SetChargeStationTriggerMessage(ctx, testStation, &store.ChargeStationTriggerMessage{
		ChargeStationId: testStation, TriggerMessage: store.TriggerMessageStatusNotification,
		TriggerStatus: store.TriggerStatusPending,
	}))

	msg := &transport.Message{MessageType: transport.MessageTypeCallResult, Action: "TriggerMessage",
		MessageId:       "0193b2c4-0000-7000-8000-00000000c003",
		RequestPayload:  json.RawMessage(`{"requestedMessage":"StatusNotification"}`),
		ResponsePayload: json.RawMessage(`{"status":"Accepted"}`)}
	assert.False(t, f.dispatcher.ObserveCallResult(ctx, testStation, msg))
	router.Handle(ctx, testStation, msg)

	trigger, err := f.store.LookupChargeStationTriggerMessage(ctx, testStation)
	require.NoError(t, err)
	assert.Nil(t, trigger, "the result handler ran")

	// and the calls of the station are answered
	router.Handle(ctx, testStation, &transport.Message{MessageType: transport.MessageTypeCall,
		Action: "Heartbeat", MessageId: "hb-2", RequestPayload: json.RawMessage(`{}`)})
	responses := f.responses()
	require.Len(t, responses, 1)
	assert.Equal(t, "hb-2", responses[0].MessageId)
	assert.Equal(t, transport.MessageTypeCallResult, responses[0].MessageType)
}

func TestABootNotificationSendsTheQueuedCommands(t *testing.T) {
	f := newFixture(t)
	// connected as the store says: a station that never sent anything is not
	f.dispatcher.Connectivity = commands.StatusStoreConnectivity{Store: f.store}
	router := f.router()

	cmd := f.enqueue("Reset", `{"type":"Soft"}`)
	assert.Equal(t, store.CommandStatusQueued, cmd.Status)
	assert.Empty(t, f.emitter.sent())

	router.Handle(context.Background(), testStation, &transport.Message{MessageType: transport.MessageTypeCall,
		Action: "BootNotification", MessageId: "boot-1",
		RequestPayload: json.RawMessage(`{"chargePointVendor":"V","chargePointModel":"M"}`)})

	sent := f.waitSent(2)
	var call, response *transport.Message
	for _, s := range sent {
		if s.msg.MessageType == transport.MessageTypeCall {
			call = s.msg
		} else {
			response = s.msg
		}
	}
	require.NotNil(t, response)
	assert.Equal(t, "boot-1", response.MessageId)
	assert.Contains(t, string(response.ResponsePayload), `"status":"Accepted"`)
	require.NotNil(t, call)
	assert.Equal(t, cmd.Id, call.MessageId)
	assert.Equal(t, "Reset", call.Action)
	f.waitStatus(cmd.Id, store.CommandStatusSent)
}

func TestTheConnectionOfAStationSendsItsQueuedCommands(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.dispatcher.Connectivity = commands.StatusStoreConnectivity{Store: f.store}
	tracker := &connectivity.Tracker{Store: f.store, Tx: f.store, Events: events.NopEmitter{}, Clock: f.clock,
		OnConnected: f.dispatcher}

	cmd := f.enqueue("Reset", `{"type":"Soft"}`)
	assert.Equal(t, store.CommandStatusQueued, cmd.Status)

	tracker.HandleConnectionEvent(ctx, &transport.ConnectionEvent{Type: transport.ConnectionEventConnected,
		StationKey: testStation, OcppVersion: "ocpp1.6", ConnectionId: "conn-1", At: testStart})
	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, f.emitter.sent(), "not before the connect dispatch delay")

	f.clock.Step(5 * time.Second)
	sent := f.waitSent(1)
	assert.Equal(t, cmd.Id, sent[0].msg.MessageId)

	t.Run("an event that does not apply sends nothing", func(t *testing.T) {
		f := newFixture(t)
		f.conn.set(testStation, false)
		tracker := &connectivity.Tracker{Store: f.store, Tx: f.store, Events: events.NopEmitter{}, Clock: f.clock,
			OnConnected: f.dispatcher}
		applied, err := f.store.RecordStationConnected(ctx, testStation, "conn-2", testStart.Add(time.Minute))
		require.NoError(t, err)
		require.True(t, applied)
		f.enqueue("Reset", `{"type":"Soft"}`)
		f.conn.set(testStation, true)

		// an older connection than the recorded one
		tracker.HandleConnectionEvent(ctx, &transport.ConnectionEvent{Type: transport.ConnectionEventConnected,
			StationKey: testStation, ConnectionId: "conn-1", At: testStart})
		f.clock.Step(5 * time.Second)
		time.Sleep(50 * time.Millisecond)
		assert.Empty(t, f.emitter.sent())
	})
}

var (
	_ connectivity.ConnectionObserver = (*commands.Dispatcher)(nil)
	_ handlers.CallOutcomeObserver    = (*commands.Dispatcher)(nil)
	_ ocpp16.CommandKicker            = (*commands.Dispatcher)(nil)
)
