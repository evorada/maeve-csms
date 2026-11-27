// SPDX-License-Identifier: Apache-2.0

package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/transport"
)

// answered enqueues a command, waits until it is sent and has the station answer it
// with the response. It returns the command as stored after the answer.
func (f *fixture) answered(action, payload, response string) *store.Command {
	f.t.Helper()
	cmd := f.enqueue(action, payload)
	sent := f.waitStatus(cmd.Id, store.CommandStatusSent)
	require.True(f.t, f.dispatcher.ObserveCallResult(context.Background(), testStation, result(sent, response)))
	return f.get(cmd.Id)
}

// progress returns the stored progress of a command, nil when it has none.
func (f *fixture) progress(id string) *commands.Progress {
	f.t.Helper()
	cmd := f.get(id)
	if cmd.Progress == nil {
		return nil
	}
	var progress commands.Progress
	require.NoError(f.t, json.Unmarshal(cmd.Progress, &progress))
	return &progress
}

// progressEvents returns the command.progress events stored since the last call (the
// other events stored since then are dropped).
func (f *fixture) progressEvents() []*store.OutboxEvent {
	f.t.Helper()
	var progress []*store.OutboxEvent
	for _, ev := range f.events.Take(f.t) {
		if ev.Type == events.TypeCommandProgress {
			progress = append(progress, ev)
		}
	}
	return progress
}

// assertOneProgressEvent checks that the progress of cmd changed once since the last
// call, with its command.progress event.
func (f *fixture) assertOneProgressEvent(cmd *store.Command) {
	f.t.Helper()
	stored := f.get(cmd.Id)
	evs := f.progressEvents()
	require.Len(f.t, evs, 1, "one command.progress event per change of the progress")
	ev := evs[0]
	assert.Equal(f.t, events.TypeCommandProgress, ev.Type)
	assert.Equal(f.t, stored.OrgId, ev.OrgId)
	assert.Equal(f.t, "urn:cpms:"+stored.OrgId+":command/"+stored.Id, ev.Subject)
	assert.Equal(f.t, "user:alice", ev.Actor, "the actor of the command")
	require.NotNil(f.t, stored.ProgressAt)
	assert.Equal(f.t, stored.ProgressAt.UTC(), ev.Time)

	data := eventstest.Data[events.CommandProgressData](f.t, ev)
	assert.Equal(f.t, stored.Id, data.CommandId)
	assert.Equal(f.t, stored.OrgId, data.OrgId)
	assert.Equal(f.t, stored.StationKey, data.StationKey)
	assert.Equal(f.t, events.StationId(stored.StationKey), data.StationId)
	assert.Equal(f.t, stored.Action, data.Action)
	assert.Equal(f.t, string(stored.Status), data.Status)
	assert.JSONEq(f.t, string(stored.Progress), string(data.Progress))
	assert.Equal(f.t, stored.ProgressAt.UTC(), data.ProgressAt)
}

// assertNoProgressEvent checks that no progress changed since the last call.
func (f *fixture) assertNoProgressEvent() {
	f.t.Helper()
	assert.Empty(f.t, f.progressEvents())
}

func at(d time.Duration) *time.Time {
	t := testStart.Add(d)
	return &t
}

func TestAnAcceptedRemoteStartIsLinkedToTheTransactionItBegan(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	cmd := f.answered("RemoteStartTransaction", `{"idTag":"DEADBEEF","connectorId":1}`, `{"status":"Accepted"}`)
	require.Equal(t, store.CommandStatusAccepted, cmd.Status)

	assert.Equal(t, &commands.Progress{State: commands.ProgressAwaitingTransaction, Deadline: at(5 * time.Minute)}, f.progress(cmd.Id))
	assert.JSONEq(t, `{"state":"awaiting_transaction","deadline":"2026-11-27T12:05:00Z"}`, string(f.get(cmd.Id).Progress))
	f.assertOneProgressEvent(cmd)

	f.clock.Step(time.Minute)
	transactionId := ocpp16.ConvertToUUID(42)
	for name, start := range map[string]func(){
		"other connector": func() { f.tracker.OnTransactionStarted(ctx, testStation, 2, "DEADBEEF", transactionId, 42) },
		"other idTag":     func() { f.tracker.OnTransactionStarted(ctx, testStation, 1, "CAFEBABE", transactionId, 42) },
		"other station":   func() { f.tracker.OnTransactionStarted(ctx, "k7f2.CP002", 1, "DEADBEEF", transactionId, 42) },
	} {
		start()
		assert.Equal(t, commands.ProgressAwaitingTransaction, f.progress(cmd.Id).State, name)
		f.assertNoProgressEvent()
	}

	f.tracker.OnTransactionStarted(ctx, testStation, 1, "DEADBEEF", transactionId, 42)

	assert.Equal(t, &commands.Progress{State: commands.ProgressTransactionStarted, TransactionId: transactionId,
		OcppTransactionId: ptr(42), ConnectorId: ptr(1)}, f.progress(cmd.Id))
	assert.JSONEq(t, `{"state":"transaction_started","transactionId":"`+transactionId+`","ocppTransactionId":42,"connectorId":1}`,
		string(f.get(cmd.Id).Progress))
	assert.Equal(t, testStart.Add(time.Minute), f.get(cmd.Id).ProgressAt.UTC())
	assert.Equal(t, store.CommandStatusAccepted, f.get(cmd.Id).Status, "the status stays")
	f.assertOneProgressEvent(cmd)

	t.Run("a remote start begins one transaction", func(t *testing.T) {
		f.tracker.OnTransactionStarted(ctx, testStation, 1, "DEADBEEF", ocpp16.ConvertToUUID(43), 43)
		assert.Equal(t, 42, *f.progress(cmd.Id).OcppTransactionId)
		f.assertNoProgressEvent()
	})

	t.Run("the sweep leaves a started transaction alone", func(t *testing.T) {
		f.clock.Step(time.Hour)
		require.NoError(t, f.tracker.Sweep(ctx))
		assert.Equal(t, commands.ProgressTransactionStarted, f.progress(cmd.Id).State)
		f.assertNoProgressEvent()
	})
}

func TestARemoteStartWithoutConnectorIsLinkedToTheTransactionOfAnyConnector(t *testing.T) {
	f := newFixture(t)
	cmd := f.answered("RemoteStartTransaction", `{"idTag":"DEADBEEF"}`, `{"status":"Accepted"}`)
	f.progressEvents()

	f.tracker.OnTransactionStarted(context.Background(), testStation, 3, "DEADBEEF", ocpp16.ConvertToUUID(7), 7)

	assert.Equal(t, &commands.Progress{State: commands.ProgressTransactionStarted, TransactionId: ocpp16.ConvertToUUID(7),
		OcppTransactionId: ptr(7), ConnectorId: ptr(3)}, f.progress(cmd.Id))
	f.assertOneProgressEvent(cmd)
}

func TestTheNewestWaitingRemoteStartOfTheConnectorIsLinked(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	first := f.answered("RemoteStartTransaction", `{"idTag":"DEADBEEF","connectorId":1}`, `{"status":"Accepted"}`)
	f.clock.Step(time.Second)
	second := f.answered("RemoteStartTransaction", `{"idTag":"DEADBEEF","connectorId":1}`, `{"status":"Accepted"}`)
	f.clock.Step(time.Second)
	otherConnector := f.answered("RemoteStartTransaction", `{"idTag":"DEADBEEF","connectorId":2}`, `{"status":"Accepted"}`)
	f.progressEvents()

	f.tracker.OnTransactionStarted(ctx, testStation, 1, "DEADBEEF", ocpp16.ConvertToUUID(1), 1)

	assert.Equal(t, commands.ProgressAwaitingTransaction, f.progress(first.Id).State)
	assert.Equal(t, commands.ProgressTransactionStarted, f.progress(second.Id).State)
	assert.Equal(t, commands.ProgressAwaitingTransaction, f.progress(otherConnector.Id).State,
		"a newer remote start of another connector is skipped")
	f.assertOneProgressEvent(second)

	f.tracker.OnTransactionStarted(ctx, testStation, 1, "DEADBEEF", ocpp16.ConvertToUUID(2), 2)

	assert.Equal(t, 2, *f.progress(first.Id).OcppTransactionId, "then the older one")
	assert.Equal(t, 1, *f.progress(second.Id).OcppTransactionId)
	f.assertOneProgressEvent(first)
}

func TestARemoteStartWithoutTransactionEndsAfterItsWindow(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	cmd := f.answered("RemoteStartTransaction", `{"idTag":"DEADBEEF","connectorId":1}`, `{"status":"Accepted"}`)
	f.progressEvents()

	f.clock.Step(5 * time.Minute)
	require.NoError(t, f.dispatcher.SweepJob().Run(ctx))
	assert.Equal(t, commands.ProgressAwaitingTransaction, f.progress(cmd.Id).State, "the deadline itself is still in time")
	f.assertNoProgressEvent()

	f.clock.Step(time.Second)
	require.NoError(t, f.dispatcher.SweepJob().Run(ctx), "the sweep job of the queue ends the waits")

	assert.Equal(t, &commands.Progress{State: commands.ProgressNoTransaction, Deadline: at(5 * time.Minute)}, f.progress(cmd.Id))
	assert.Equal(t, testStart.Add(5*time.Minute+time.Second), f.get(cmd.Id).ProgressAt.UTC())
	assert.Equal(t, store.CommandStatusAccepted, f.get(cmd.Id).Status)
	f.assertOneProgressEvent(cmd)

	require.NoError(t, f.tracker.Sweep(ctx))
	f.assertNoProgressEvent()

	// a transaction that starts after the window is not linked
	f.tracker.OnTransactionStarted(ctx, testStation, 1, "DEADBEEF", ocpp16.ConvertToUUID(9), 9)
	assert.Equal(t, commands.ProgressNoTransaction, f.progress(cmd.Id).State)
	f.assertNoProgressEvent()
}

func TestOnlyAcceptedCommandsAreFollowed(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	rejectedStart := f.answered("RemoteStartTransaction", `{"idTag":"DEADBEEF","connectorId":1}`, `{"status":"Rejected"}`)
	rejectedStop := f.answered("RemoteStopTransaction", `{"transactionId":42}`, `{"status":"Rejected"}`)
	rejectedAvailability := f.answered("ChangeAvailability", `{"connectorId":1,"type":"Inoperative"}`, `{"status":"Rejected"}`)
	failedStart := f.enqueue("RemoteStartTransaction", `{"idTag":"DEADBEEF","connectorId":1}`)
	f.waitStatus(failedStart.Id, store.CommandStatusSent)
	require.True(t, f.dispatcher.ObserveCallError(ctx, testStation, callError(f.get(failedStart.Id), transport.ErrorInternalError, "boom")))

	f.tracker.OnTransactionStarted(ctx, testStation, 1, "DEADBEEF", ocpp16.ConvertToUUID(42), 42)
	f.tracker.OnTransactionStopped(ctx, testStation, 42, "Remote")
	f.tracker.OnConnectorStatus(ctx, testStation, 1, "Unavailable")
	f.clock.Step(time.Hour)
	require.NoError(t, f.tracker.Sweep(ctx))

	for _, cmd := range []*store.Command{rejectedStart, rejectedStop, rejectedAvailability, f.get(failedStart.Id)} {
		assert.NotEqual(t, store.CommandStatusAccepted, f.get(cmd.Id).Status)
		assert.Nil(t, f.get(cmd.Id).Progress, cmd.Action)
		assert.Nil(t, f.get(cmd.Id).ProgressAt, cmd.Action)
	}
	f.assertNoProgressEvent()

	t.Run("an accepted command of another action", func(t *testing.T) {
		reset := f.answered("Reset", `{"type":"Soft"}`, `{"status":"Accepted"}`)
		assert.Nil(t, f.get(reset.Id).Progress)
		f.assertNoProgressEvent()
	})

	t.Run("an accepted command of OCPP 2.0.1", func(t *testing.T) {
		cmd := &store.Command{Id: uuid.NewString(), OrgId: testOrgId, StationKey: testStation, OcppVersion: "ocpp2.0.1",
			Action: "ChangeAvailability", Payload: json.RawMessage(`{"operationalStatus":"Inoperative"}`),
			Status: store.CommandStatusQueued, CreatedAt: f.clock.Now(), ExpiresAt: f.clock.Now().Add(time.Hour)}
		require.NoError(t, f.store.CreateCommand(ctx, cmd))
		accepted := *cmd
		accepted.Status = store.CommandStatusAccepted
		accepted.Result = json.RawMessage(`{"status":"Accepted"}`)

		f.tracker.AfterResult(ctx, &accepted)

		assert.Nil(t, f.get(cmd.Id).Progress)
		f.assertNoProgressEvent()
	})
}

func TestALateAcceptedRemoteStartIsFollowed(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	cmd := f.enqueue("RemoteStartTransaction", `{"idTag":"DEADBEEF","connectorId":1}`)
	sent := f.waitStatus(cmd.Id, store.CommandStatusSent)
	f.clock.Step(31 * time.Second)
	require.NoError(t, f.dispatcher.Sweep(ctx))
	require.Equal(t, store.CommandStatusTimeout, f.get(cmd.Id).Status)

	require.True(t, f.dispatcher.ObserveCallResult(ctx, testStation, result(sent, `{"status":"Accepted"}`)))

	assert.Equal(t, store.CommandStatusAccepted, f.get(cmd.Id).Status)
	assert.Equal(t, &commands.Progress{State: commands.ProgressAwaitingTransaction, Deadline: at(31*time.Second + 5*time.Minute)},
		f.progress(cmd.Id), "the window starts with the answer")
	f.assertOneProgressEvent(cmd)

	t.Run("a duplicate answer does not start it again", func(t *testing.T) {
		f.clock.Step(time.Minute)
		require.True(t, f.dispatcher.ObserveCallResult(ctx, testStation, result(sent, `{"status":"Accepted"}`)))
		assert.Equal(t, at(31*time.Second+5*time.Minute), f.progress(cmd.Id).Deadline)
		f.assertNoProgressEvent()
	})
}

func TestAnAcceptedRemoteStopRecordsTheEndOfItsTransaction(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	cmd := f.answered("RemoteStopTransaction", `{"transactionId":42}`, `{"status":"Accepted"}`)

	assert.Equal(t, &commands.Progress{State: commands.ProgressAwaitingStop, Deadline: at(2 * time.Minute)}, f.progress(cmd.Id))
	f.assertOneProgressEvent(cmd)

	f.tracker.OnTransactionStopped(ctx, testStation, 43, "Remote")
	f.tracker.OnTransactionStopped(ctx, "k7f2.CP002", 42, "Remote")
	assert.Equal(t, commands.ProgressAwaitingStop, f.progress(cmd.Id).State, "another transaction or station")
	f.assertNoProgressEvent()

	f.clock.Step(30 * time.Second)
	f.tracker.OnTransactionStopped(ctx, testStation, 42, "Remote")

	assert.Equal(t, &commands.Progress{State: commands.ProgressTransactionStopped, OcppTransactionId: ptr(42),
		StopReason: "Remote"}, f.progress(cmd.Id))
	assert.JSONEq(t, `{"state":"transaction_stopped","ocppTransactionId":42,"stopReason":"Remote"}`, string(f.get(cmd.Id).Progress))
	f.assertOneProgressEvent(cmd)

	f.clock.Step(time.Hour)
	require.NoError(t, f.tracker.Sweep(ctx))
	assert.Equal(t, commands.ProgressTransactionStopped, f.progress(cmd.Id).State)
	f.assertNoProgressEvent()
}

func TestARemoteStopWhoseTransactionDoesNotEndIsNotConfirmed(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	cmd := f.answered("RemoteStopTransaction", `{"transactionId":42}`, `{"status":"Accepted"}`)
	f.progressEvents()

	f.clock.Step(2*time.Minute + time.Second)
	require.NoError(t, f.tracker.Sweep(ctx))

	assert.Equal(t, &commands.Progress{State: commands.ProgressStopNotConfirmed, Deadline: at(2 * time.Minute)}, f.progress(cmd.Id))
	f.assertOneProgressEvent(cmd)

	f.tracker.OnTransactionStopped(ctx, testStation, 42, "Local")
	assert.Equal(t, commands.ProgressStopNotConfirmed, f.progress(cmd.Id).State)
	f.assertNoProgressEvent()
}

func TestAnAcceptedChangeOfAvailabilityIsAppliedByTheStatusItAsksFor(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	inoperative := f.answered("ChangeAvailability", `{"connectorId":1,"type":"Inoperative"}`, `{"status":"Accepted"}`)

	assert.Equal(t, &commands.Progress{State: commands.ProgressAwaitingStatus, Deadline: at(2 * time.Minute)}, f.progress(inoperative.Id))
	f.assertOneProgressEvent(inoperative)

	f.tracker.OnConnectorStatus(ctx, testStation, 1, "Available")
	f.tracker.OnConnectorStatus(ctx, testStation, 1, "Charging")
	f.tracker.OnConnectorStatus(ctx, testStation, 2, "Unavailable")
	f.tracker.OnConnectorStatus(ctx, "k7f2.CP002", 1, "Unavailable")
	assert.Equal(t, commands.ProgressAwaitingStatus, f.progress(inoperative.Id).State, "not the status it asks for, or not its connector")
	f.assertNoProgressEvent()

	f.clock.Step(10 * time.Second)
	f.tracker.OnConnectorStatus(ctx, testStation, 1, "Unavailable")

	assert.Equal(t, &commands.Progress{State: commands.ProgressApplied, ConnectorId: ptr(1), ConnectorStatus: "Unavailable"},
		f.progress(inoperative.Id))
	assert.JSONEq(t, `{"state":"applied","connectorId":1,"connectorStatus":"Unavailable"}`, string(f.get(inoperative.Id).Progress))
	f.assertOneProgressEvent(inoperative)

	f.tracker.OnConnectorStatus(ctx, testStation, 1, "Unavailable")
	f.assertNoProgressEvent()

	t.Run("operative", func(t *testing.T) {
		operative := f.answered("ChangeAvailability", `{"connectorId":2,"type":"Operative"}`, `{"status":"Accepted"}`)
		f.progressEvents()

		f.tracker.OnConnectorStatus(ctx, testStation, 2, "Unavailable")
		assert.Equal(t, commands.ProgressAwaitingStatus, f.progress(operative.Id).State)
		f.assertNoProgressEvent()

		f.tracker.OnConnectorStatus(ctx, testStation, 2, "Available")
		assert.Equal(t, &commands.Progress{State: commands.ProgressApplied, ConnectorId: ptr(2), ConnectorStatus: "Available"},
			f.progress(operative.Id))
		f.assertOneProgressEvent(operative)
	})

	t.Run("the whole station is applied by the first connector that reports the status", func(t *testing.T) {
		station := f.answered("ChangeAvailability", `{"connectorId":0,"type":"Inoperative"}`, `{"status":"Accepted"}`)
		f.progressEvents()

		f.tracker.OnConnectorStatus(ctx, testStation, 3, "Available")
		assert.Equal(t, commands.ProgressAwaitingStatus, f.progress(station.Id).State)

		f.tracker.OnConnectorStatus(ctx, testStation, 3, "Unavailable")
		assert.Equal(t, &commands.Progress{State: commands.ProgressApplied, ConnectorId: ptr(3), ConnectorStatus: "Unavailable"},
			f.progress(station.Id))
		f.assertOneProgressEvent(station)

		f.tracker.OnConnectorStatus(ctx, testStation, 4, "Unavailable")
		assert.Equal(t, 3, *f.progress(station.Id).ConnectorId)
		f.assertNoProgressEvent()
	})
}

func TestAChangeOfAvailabilityWithoutItsStatusIsNotApplied(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	cmd := f.answered("ChangeAvailability", `{"connectorId":1,"type":"Inoperative"}`, `{"status":"Accepted"}`)
	f.progressEvents()

	f.clock.Step(2*time.Minute + time.Second)
	require.NoError(t, f.tracker.Sweep(ctx))

	assert.Equal(t, &commands.Progress{State: commands.ProgressNotApplied, Deadline: at(2 * time.Minute)}, f.progress(cmd.Id))
	f.assertOneProgressEvent(cmd)

	f.tracker.OnConnectorStatus(ctx, testStation, 1, "Unavailable")
	assert.Equal(t, commands.ProgressNotApplied, f.progress(cmd.Id).State)
	f.assertNoProgressEvent()
}

func TestAScheduledChangeOfAvailabilityWaitsForItsStatusWithoutDeadline(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	cmd := f.answered("ChangeAvailability", `{"connectorId":1,"type":"Inoperative"}`, `{"status":"Scheduled"}`)
	require.Equal(t, store.CommandStatusAccepted, cmd.Status)

	assert.Equal(t, &commands.Progress{State: commands.ProgressScheduled}, f.progress(cmd.Id))
	assert.JSONEq(t, `{"state":"scheduled"}`, string(f.get(cmd.Id).Progress))
	f.assertOneProgressEvent(cmd)

	// the running transaction goes on: the change waits
	f.tracker.OnConnectorStatus(ctx, testStation, 1, "Charging")
	f.clock.Step(time.Hour)
	require.NoError(t, f.tracker.Sweep(ctx))
	assert.Equal(t, commands.ProgressScheduled, f.progress(cmd.Id).State)
	f.assertNoProgressEvent()

	f.tracker.OnConnectorStatus(ctx, testStation, 1, "Unavailable")

	assert.Equal(t, &commands.Progress{State: commands.ProgressApplied, ConnectorId: ptr(1), ConnectorStatus: "Unavailable"},
		f.progress(cmd.Id))
	f.assertOneProgressEvent(cmd)
}

// waitingRemoteStart stores an accepted remote start created at createdAt whose
// progress waits for its transaction until deadline.
func (f *fixture) waitingRemoteStart(stationKey string, createdAt, deadline time.Time) string {
	f.t.Helper()
	ctx := context.Background()
	cmd := &store.Command{Id: uuid.NewString(), OrgId: testOrgId, StationKey: stationKey, OcppVersion: "ocpp1.6",
		Action: "RemoteStartTransaction", Payload: json.RawMessage(`{"idTag":"DEADBEEF"}`), Status: store.CommandStatusQueued,
		CreatedAt: createdAt, ExpiresAt: createdAt.Add(5 * time.Minute)}
	require.NoError(f.t, f.store.CreateCommand(ctx, cmd))
	_, err := f.store.TransitionCommand(ctx, cmd.Id, []store.CommandStatus{store.CommandStatusQueued},
		store.CommandUpdate{Status: store.CommandStatusAccepted, CompletedAt: &createdAt})
	require.NoError(f.t, err)
	progress, err := json.Marshal(commands.Progress{State: commands.ProgressAwaitingTransaction, Deadline: &deadline})
	require.NoError(f.t, err)
	_, err = f.store.SetCommandProgress(ctx, cmd.Id, progress, createdAt, nil)
	require.NoError(f.t, err)
	return cmd.Id
}

func TestTheSweepEndsEveryWaitPastItsDeadlinePageByPage(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	now := testStart.Add(time.Hour)
	f.clock.SetTime(now)

	var due, notDue []string
	for i := 0; i < 130; i++ {
		createdAt := now.Add(-time.Duration(i) * 10 * time.Second)
		station := fmt.Sprintf("k7f2.CP%03d", i)
		if i%10 == 3 {
			// created among the others, but still waiting
			notDue = append(notDue, f.waitingRemoteStart(station, createdAt, now.Add(time.Minute)))
			continue
		}
		due = append(due, f.waitingRemoteStart(station, createdAt, now.Add(-time.Second)))
	}
	// created before the longest window plus an hour: no longer looked at
	tooOld := f.waitingRemoteStart("k7f2.OLD", now.Add(-time.Hour-5*time.Minute-time.Second), now.Add(-time.Second))

	require.NoError(t, f.tracker.Sweep(ctx))

	for _, id := range due {
		assert.Equal(t, commands.ProgressNoTransaction, f.progress(id).State, id)
	}
	for _, id := range append(notDue, tooOld) {
		assert.Equal(t, commands.ProgressAwaitingTransaction, f.progress(id).State, id)
	}
	assert.Len(t, f.progressEvents(), len(due), "one event per command that the sweep moved")
}

// brokenProgressStore fails the lists and the progress writes of the commands.
type brokenProgressStore struct {
	store.CommandStore
}

var errCommandStore = errors.New("the command store is down")

func (brokenProgressStore) ListCommands(context.Context, store.CommandFilter) ([]*store.Command, int, error) {
	return nil, 0, errCommandStore
}

func (brokenProgressStore) SetCommandProgress(context.Context, string, json.RawMessage, time.Time, *store.OutboxEvent) (*store.Command, error) {
	return nil, errCommandStore
}

func TestTheTrackerLogsTheErrorsOfTheStore(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	cmd := f.answered("RemoteStartTransaction", `{"idTag":"DEADBEEF","connectorId":1}`, `{"status":"Accepted"}`)
	f.progressEvents()
	broken := &commands.Tracker{Store: brokenProgressStore{f.store}, Tx: f.events.Tx, Events: f.events.Events,
		Clock: f.clock, Config: f.tracker.Config}

	assert.NotPanics(t, func() {
		broken.AfterResult(ctx, f.get(cmd.Id))
		broken.OnTransactionStarted(ctx, testStation, 1, "DEADBEEF", ocpp16.ConvertToUUID(1), 1)
		broken.OnTransactionStopped(ctx, testStation, 1, "Local")
		broken.OnConnectorStatus(ctx, testStation, 1, "Unavailable")
	})
	f.clock.Step(time.Hour)
	assert.ErrorIs(t, broken.Sweep(ctx), errCommandStore, "the sweep job reports them")

	assert.Equal(t, commands.ProgressAwaitingTransaction, f.progress(cmd.Id).State)
	f.assertNoProgressEvent()
}

// panickingHook panics on every result.
type panickingHook struct{}

func (panickingHook) AfterResult(context.Context, *store.Command) {
	panic("hook broke")
}

func TestAHookThatPanicsDoesNotStopTheDispatcher(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.dispatcher.AfterResult = []commands.ResultHook{panickingHook{}, f.tracker}
	cmd := f.enqueue("RemoteStartTransaction", `{"idTag":"DEADBEEF","connectorId":1}`)
	next := f.enqueue("Reset", `{"type":"Soft"}`)

	require.True(t, f.dispatcher.ObserveCallResult(ctx, testStation, result(f.get(cmd.Id), `{"status":"Accepted"}`)))

	assert.Equal(t, store.CommandStatusAccepted, f.get(cmd.Id).Status)
	require.Len(t, f.completedEvents(), 1)
	assert.Equal(t, commands.ProgressAwaitingTransaction, f.progress(cmd.Id).State, "the next hook is still told")
	f.waitStatus(next.Id, store.CommandStatusSent)
}
