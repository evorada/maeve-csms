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
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	testingclock "k8s.io/utils/clock/testing"
)

func TestEnqueueSendsToAConnectedStationWithTheCommandIdAsMessageId(t *testing.T) {
	f := newFixture(t)

	cmd := f.enqueue("Reset", `{"type":"Soft"}`)

	// sent within the call of Enqueue
	sent := f.emitter.sent()
	require.Len(t, sent, 1)
	assert.Equal(t, transport.OcppVersion16, sent[0].version)
	assert.Equal(t, testStation, sent[0].stationKey)
	assert.Equal(t, &transport.Message{
		MessageType:    transport.MessageTypeCall,
		MessageId:      cmd.Id,
		Action:         "Reset",
		RequestPayload: json.RawMessage(`{"type":"Soft"}`),
	}, sent[0].msg)

	assert.Equal(t, store.CommandStatusSent, cmd.Status)
	require.NotNil(t, cmd.SentAt)
	assert.Equal(t, testStart, cmd.SentAt.UTC())
	assert.Empty(t, f.completedEvents())
}

func TestCommandsOfADisconnectedStationWaitForItsConnection(t *testing.T) {
	f := newFixture(t)
	f.conn.set(testStation, false)

	cmd := f.enqueue("Reset", `{"type":"Soft"}`)
	assert.Equal(t, store.CommandStatusQueued, cmd.Status)
	assert.Nil(t, cmd.SentAt)

	// the sweep does not send to a disconnected station either
	require.NoError(t, f.dispatcher.Sweep(context.Background()))
	require.NoError(t, f.dispatcher.Kick(context.Background(), testStation))
	assert.Empty(t, f.emitter.sent())
	assert.Equal(t, store.CommandStatusQueued, f.get(cmd.Id).Status)

	f.conn.set(testStation, true)
	f.dispatcher.OnStationConnected(context.Background(), testStation)

	// nothing before the delay: the station may send its BootNotification first
	f.clock.Step(4 * time.Second)
	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, f.emitter.sent())

	f.clock.Step(time.Second)
	sent := f.waitSent(1)
	assert.Equal(t, cmd.Id, sent[0].msg.MessageId)
	assert.Equal(t, store.CommandStatusSent, f.get(cmd.Id).Status)
}

func TestTheNextCommandIsSentOnlyAfterTheAnswerToThePreviousOne(t *testing.T) {
	f := newFixture(t)

	first := f.enqueue("Reset", `{"type":"Soft"}`)
	f.clock.Step(time.Millisecond)
	second := f.enqueue("ClearCache", `{}`)
	f.clock.Step(time.Millisecond)
	third := f.enqueue("UnlockConnector", `{"connectorId":1}`)

	assert.Equal(t, store.CommandStatusSent, first.Status)
	assert.Equal(t, store.CommandStatusQueued, second.Status)
	assert.Equal(t, store.CommandStatusQueued, third.Status)

	// neither a kick nor a sweep sends a second command while one waits for its answer
	require.NoError(t, f.dispatcher.Kick(context.Background(), testStation))
	require.NoError(t, f.dispatcher.Sweep(context.Background()))
	require.Len(t, f.emitter.sent(), 1)

	assert.True(t, f.dispatcher.ObserveCallResult(context.Background(), testStation, result(first, `{"status":"Accepted"}`)))
	sent := f.waitSent(2)
	assert.Equal(t, second.Id, sent[1].msg.MessageId)
	assert.Equal(t, "ClearCache", sent[1].msg.Action)

	assert.True(t, f.dispatcher.ObserveCallError(context.Background(), testStation,
		callError(f.get(second.Id), transport.ErrorNotSupported, "no cache")))
	sent = f.waitSent(3)
	assert.Equal(t, third.Id, sent[2].msg.MessageId)
}

func TestCallResultsCompleteTheCommand(t *testing.T) {
	for name, tc := range map[string]struct {
		action, payload, response string
		want                      store.CommandStatus
	}{
		"accepted":             {"Reset", `{"type":"Soft"}`, `{"status":"Accepted"}`, store.CommandStatusAccepted},
		"rejected":             {"Reset", `{"type":"Hard"}`, `{"status":"Rejected"}`, store.CommandStatusRejected},
		"unlocked":             {"UnlockConnector", `{"connectorId":1}`, `{"status":"Unlocked"}`, store.CommandStatusAccepted},
		"no status field":      {"GetConfiguration", `{}`, `{"configurationKey":[{"key":"A","readonly":true}]}`, store.CommandStatusAccepted},
		"tracked datatransfer": {"DataTransfer", `{"vendorId":"v"}`, `{"status":"Accepted","data":"x"}`, store.CommandStatusAccepted},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			cmd := f.enqueue(tc.action, tc.payload)
			sentAt := *cmd.SentAt
			f.clock.Step(2 * time.Second)

			assert.True(t, f.dispatcher.ObserveCallResult(context.Background(), testStation, result(cmd, tc.response)))

			done := f.get(cmd.Id)
			assert.Equal(t, tc.want, done.Status)
			assert.JSONEq(t, tc.response, string(done.Result))
			assert.Nil(t, done.ErrorCode)
			assert.Nil(t, done.ErrorDescription)
			require.NotNil(t, done.SentAt, "the completion keeps the time the command was sent")
			assert.Equal(t, sentAt, *done.SentAt)
			require.NotNil(t, done.CompletedAt)
			assert.Equal(t, testStart.Add(2*time.Second), done.CompletedAt.UTC())

			completed := f.completedEvents()
			require.Len(t, completed, 1)
			assertCompletedEvent(t, completed[0], done, false)
		})
	}
}

func TestACallErrorFailsTheCommand(t *testing.T) {
	f := newFixture(t)
	cmd := f.enqueue("Reset", `{"type":"Soft"}`)

	assert.True(t, f.dispatcher.ObserveCallError(context.Background(), testStation,
		callError(cmd, transport.ErrorNotImplemented, "Reset is not implemented")))

	done := f.get(cmd.Id)
	assert.Equal(t, store.CommandStatusFailed, done.Status)
	assert.Equal(t, "NotImplemented", deref(done.ErrorCode))
	assert.Equal(t, "Reset is not implemented", deref(done.ErrorDescription))
	assert.Nil(t, done.Result)
	assert.Equal(t, cmd.SentAt, done.SentAt)
	require.NotNil(t, done.CompletedAt)

	completed := f.completedEvents()
	require.Len(t, completed, 1)
	assertCompletedEvent(t, completed[0], done, false)
}

func TestAResultThatDoesNotMatchTheSchemaFailsTheCommand(t *testing.T) {
	f := newFixture(t)
	cmd := f.enqueue("Reset", `{"type":"Soft"}`)

	assert.True(t, f.dispatcher.ObserveCallResult(context.Background(), testStation, result(cmd, `{"status":"Maybe"}`)))

	done := f.get(cmd.Id)
	assert.Equal(t, store.CommandStatusFailed, done.Status)
	assert.Equal(t, commands.ErrorCodeFormatViolation, deref(done.ErrorCode))
	assert.NotEmpty(t, deref(done.ErrorDescription))
	assert.Nil(t, done.Result)

	completed := f.completedEvents()
	require.Len(t, completed, 1)
	assertCompletedEvent(t, completed[0], done, false)
}

func TestAnswersToOtherCallsAreNotTracked(t *testing.T) {
	f := newFixture(t)
	cmd := f.enqueue("Reset", `{"type":"Soft"}`)

	for name, msg := range map[string]*transport.Message{
		"unknown message id": {MessageType: transport.MessageTypeCallResult, Action: "Reset",
			MessageId: "a3bb189e-8bf9-3888-9912-ace4e6543002", ResponsePayload: json.RawMessage(`{"status":"Accepted"}`)},
		"message id that is not a uuid": {MessageType: transport.MessageTypeCallResult, Action: "Reset",
			MessageId: "42", ResponsePayload: json.RawMessage(`{"status":"Accepted"}`)},
		"no message id": {MessageType: transport.MessageTypeCallResult, Action: "Reset",
			ResponsePayload: json.RawMessage(`{"status":"Accepted"}`)},
		"another action": {MessageType: transport.MessageTypeCallResult, Action: "ClearCache",
			MessageId: cmd.Id, ResponsePayload: json.RawMessage(`{"status":"Accepted"}`)},
	} {
		t.Run(name, func(t *testing.T) {
			assert.False(t, f.dispatcher.ObserveCallResult(context.Background(), testStation, msg))
			errMsg := *msg
			errMsg.MessageType = transport.MessageTypeCallError
			errMsg.ErrorCode = transport.ErrorInternalError
			assert.False(t, f.dispatcher.ObserveCallError(context.Background(), testStation, &errMsg))
		})
	}

	t.Run("another station", func(t *testing.T) {
		assert.False(t, f.dispatcher.ObserveCallResult(context.Background(), "k7f2.CP002", result(cmd, `{"status":"Accepted"}`)))
		assert.False(t, f.dispatcher.ObserveCallError(context.Background(), "k7f2.CP002",
			callError(cmd, transport.ErrorInternalError, "")))
	})

	// the command still waits for its answer
	got := f.get(cmd.Id)
	assert.Equal(t, store.CommandStatusSent, got.Status)
	assert.Nil(t, got.Result)
	assert.Nil(t, got.CompletedAt)
	assert.Empty(t, f.completedEvents())
}

func TestASecondAnswerChangesNothing(t *testing.T) {
	f := newFixture(t)
	cmd := f.enqueue("Reset", `{"type":"Soft"}`)

	assert.True(t, f.dispatcher.ObserveCallResult(context.Background(), testStation, result(cmd, `{"status":"Accepted"}`)))
	f.clock.Step(time.Second)
	// still the answer to a command: tracked, but nothing changes
	assert.True(t, f.dispatcher.ObserveCallResult(context.Background(), testStation, result(cmd, `{"status":"Rejected"}`)))
	assert.True(t, f.dispatcher.ObserveCallError(context.Background(), testStation, callError(cmd, transport.ErrorInternalError, "")))

	done := f.get(cmd.Id)
	assert.Equal(t, store.CommandStatusAccepted, done.Status)
	assert.Equal(t, testStart, done.CompletedAt.UTC())
	assert.Len(t, f.completedEvents(), 1)
}

func TestTheSweepTimesOutCommandsThatExpireInTheQueue(t *testing.T) {
	f := newFixture(t)
	f.conn.set(testStation, false)

	reset := f.enqueue("Reset", `{"type":"Soft"}`) // expires after 5 min (catalogue)
	clear := f.enqueue("ClearCache", `{}`)         // expires after 1 h (default)
	assert.Equal(t, testStart.Add(5*time.Minute), reset.ExpiresAt.UTC())
	assert.Equal(t, testStart.Add(time.Hour), clear.ExpiresAt.UTC())

	f.clock.Step(5*time.Minute - time.Second)
	require.NoError(t, f.dispatcher.Sweep(context.Background()))
	assert.Equal(t, store.CommandStatusQueued, f.get(reset.Id).Status)

	f.clock.Step(time.Second)
	require.NoError(t, f.dispatcher.Sweep(context.Background()))
	done := f.get(reset.Id)
	assert.Equal(t, store.CommandStatusTimeout, done.Status)
	assert.Equal(t, commands.ErrorCodeQueueExpired, deref(done.ErrorCode))
	assert.Nil(t, done.SentAt)
	require.NotNil(t, done.CompletedAt)
	assert.Equal(t, testStart.Add(5*time.Minute), done.CompletedAt.UTC())
	assert.Equal(t, store.CommandStatusQueued, f.get(clear.Id).Status)

	completed := f.completedEvents()
	require.Len(t, completed, 1)
	assertCompletedEvent(t, completed[0], done, false)

	// a second sweep changes nothing
	require.NoError(t, f.dispatcher.Sweep(context.Background()))
	assert.Empty(t, f.completedEvents())
	assert.Empty(t, f.emitter.sent())
}

func TestTheSweepTimesOutCommandsWithoutAnAnswerAndSendsTheNext(t *testing.T) {
	f := newFixture(t)
	first := f.enqueue("Reset", `{"type":"Soft"}`)
	second := f.enqueue("ClearCache", `{}`)

	f.clock.Step(30*time.Second - time.Millisecond)
	require.NoError(t, f.dispatcher.Sweep(context.Background()))
	assert.Equal(t, store.CommandStatusSent, f.get(first.Id).Status)
	require.Len(t, f.emitter.sent(), 1)

	f.clock.Step(time.Millisecond)
	require.NoError(t, f.dispatcher.Sweep(context.Background()))
	timedOut := f.get(first.Id)
	assert.Equal(t, store.CommandStatusTimeout, timedOut.Status)
	assert.Equal(t, commands.ErrorCodeResponseTimeout, deref(timedOut.ErrorCode))
	assert.Equal(t, first.SentAt, timedOut.SentAt)
	require.NotNil(t, timedOut.CompletedAt)

	// the next command goes out in the same sweep
	sent := f.emitter.sent()
	require.Len(t, sent, 2)
	assert.Equal(t, second.Id, sent[1].msg.MessageId)

	completed := f.completedEvents()
	require.Len(t, completed, 1)
	assertCompletedEvent(t, completed[0], timedOut, false)

	t.Run("a late answer completes the command again", func(t *testing.T) {
		f.clock.Step(time.Minute)
		assert.True(t, f.dispatcher.ObserveCallResult(context.Background(), testStation, result(first, `{"status":"Accepted"}`)))

		late := f.get(first.Id)
		assert.Equal(t, store.CommandStatusAccepted, late.Status)
		assert.JSONEq(t, `{"status":"Accepted"}`, string(late.Result))
		assert.Equal(t, first.SentAt, late.SentAt)
		assert.Equal(t, testStart.Add(90*time.Second), late.CompletedAt.UTC())

		completed := f.completedEvents()
		require.Len(t, completed, 1)
		assertCompletedEvent(t, completed[0], late, true)

		// the second command still waits for its own answer
		assert.Equal(t, store.CommandStatusSent, f.get(second.Id).Status)
		assert.Len(t, f.emitter.sent(), 2)
	})
}

func TestTheSweepLeavesAnsweredCommandsAlone(t *testing.T) {
	f := newFixture(t)
	cmd := f.enqueue("Reset", `{"type":"Soft"}`)
	assert.True(t, f.dispatcher.ObserveCallResult(context.Background(), testStation, result(cmd, `{"status":"Accepted"}`)))
	require.Len(t, f.completedEvents(), 1)

	f.clock.Step(10 * time.Minute)
	require.NoError(t, f.dispatcher.Sweep(context.Background()))

	assert.Equal(t, store.CommandStatusAccepted, f.get(cmd.Id).Status)
	assert.Empty(t, f.completedEvents())
}

func TestAFailedSendPutsTheCommandBackInTheQueue(t *testing.T) {
	f := newFixture(t)
	f.emitter.setErr(errors.New("broker down"))

	// Enqueue logs the failure: the command is stored and queued
	cmd := f.enqueue("Reset", `{"type":"Soft"}`)
	assert.Equal(t, store.CommandStatusQueued, cmd.Status)
	assert.Nil(t, cmd.SentAt)

	err := f.dispatcher.Kick(context.Background(), testStation)
	assert.ErrorContains(t, err, "broker down")
	got := f.get(cmd.Id)
	assert.Equal(t, store.CommandStatusQueued, got.Status)
	assert.Nil(t, got.SentAt)
	assert.Empty(t, f.completedEvents())

	// the sweep sends it once the broker is back
	f.emitter.setErr(nil)
	f.clock.Step(5 * time.Second)
	require.NoError(t, f.dispatcher.Sweep(context.Background()))
	sent := f.waitSent(1)
	assert.Equal(t, cmd.Id, sent[0].msg.MessageId)
	got = f.get(cmd.Id)
	assert.Equal(t, store.CommandStatusSent, got.Status)
	require.NotNil(t, got.SentAt)
	assert.Equal(t, testStart.Add(5*time.Second), got.SentAt.UTC())
}

// resolverFunc is a PayloadResolver.
type resolverFunc func(ctx context.Context, cmd *store.Command) (json.RawMessage, error)

func (f resolverFunc) Resolve(ctx context.Context, cmd *store.Command) (json.RawMessage, error) {
	return f(ctx, cmd)
}

func TestResolversBuildThePayloadThatIsSent(t *testing.T) {
	f := newFixture(t)
	var seen []string
	f.dispatcher.Resolvers = []commands.PayloadResolver{
		resolverFunc(func(_ context.Context, cmd *store.Command) (json.RawMessage, error) {
			seen = append(seen, string(cmd.Payload))
			return json.RawMessage(`{"vendorId":"v","data":"https://files.example.com/a"}`), nil
		}),
		resolverFunc(func(_ context.Context, cmd *store.Command) (json.RawMessage, error) {
			seen = append(seen, string(cmd.Payload))
			return json.RawMessage(`{"vendorId":"v","data":"https://files.example.com/a?sig=1"}`), nil
		}),
	}

	cmd := f.enqueue("DataTransfer", `{"vendorId":"v","data":"file:a"}`)

	sent := f.waitSent(1)
	assert.JSONEq(t, `{"vendorId":"v","data":"https://files.example.com/a?sig=1"}`, string(sent[0].msg.RequestPayload))
	assert.Equal(t, []string{`{"vendorId":"v","data":"file:a"}`, `{"vendorId":"v","data":"https://files.example.com/a"}`}, seen)
	// the stored payload is the one that was enqueued
	assert.JSONEq(t, `{"vendorId":"v","data":"file:a"}`, string(f.get(cmd.Id).Payload))
}

func TestACommandWhosePayloadCannotBeBuiltFailsAndTheNextIsSent(t *testing.T) {
	f := newFixture(t)
	f.conn.set(testStation, false)
	f.dispatcher.Resolvers = []commands.PayloadResolver{
		resolverFunc(func(_ context.Context, cmd *store.Command) (json.RawMessage, error) {
			if cmd.Action == "DataTransfer" {
				return nil, errors.New("file gone")
			}
			return cmd.Payload, nil
		}),
	}
	broken := f.enqueue("DataTransfer", `{"vendorId":"v"}`)
	next := f.enqueue("ClearCache", `{}`)

	f.conn.set(testStation, true)
	require.NoError(t, f.dispatcher.Kick(context.Background(), testStation))

	failed := f.get(broken.Id)
	assert.Equal(t, store.CommandStatusFailed, failed.Status)
	assert.Equal(t, commands.ErrorCodePayloadResolution, deref(failed.ErrorCode))
	assert.Equal(t, "file gone", deref(failed.ErrorDescription))
	completed := f.completedEvents()
	require.Len(t, completed, 1)
	assertCompletedEvent(t, completed[0], failed, false)

	sent := f.emitter.sent()
	require.Len(t, sent, 1)
	assert.Equal(t, next.Id, sent[0].msg.MessageId)
}

func TestTheSweepSendsTheCommandsOfEveryConnectedStation(t *testing.T) {
	f := newFixture(t)
	stations := []string{"k7f2.CP001", "k7f2.CP002", "x9z1.CP001"}
	for _, key := range stations {
		f.conn.set(key, false)
		require.NoError(t, f.store.SetChargeStationRuntimeDetails(context.Background(), key,
			&store.ChargeStationRuntimeDetails{OcppVersion: "1.6"}))
	}
	ids := map[string]string{}
	for _, key := range stations {
		cmd, err := f.service.Enqueue(context.Background(), &store.Command{OrgId: testOrgId, StationKey: key,
			Action: "ClearCache"})
		require.NoError(t, err)
		ids[key] = cmd.Id
	}
	assert.Empty(t, f.emitter.sent())

	// missed connection events: the stations are connected, nobody kicked them
	f.conn.set("k7f2.CP001", true)
	f.conn.set("x9z1.CP001", true)
	require.NoError(t, f.dispatcher.Sweep(context.Background()))

	sentTo := map[string]string{}
	for _, call := range f.emitter.sent() {
		sentTo[call.stationKey] = call.msg.MessageId
	}
	assert.Equal(t, map[string]string{"k7f2.CP001": ids["k7f2.CP001"], "x9z1.CP001": ids["x9z1.CP001"]}, sentTo)
}

func TestPurgeDeletesOldCompletedCommands(t *testing.T) {
	f := newFixture(t)
	old := f.enqueue("Reset", `{"type":"Soft"}`)
	assert.True(t, f.dispatcher.ObserveCallResult(context.Background(), testStation, result(old, `{"status":"Accepted"}`)))

	f.clock.Step(90*24*time.Hour + time.Second)
	recent := f.enqueue("ClearCache", `{}`)
	f.waitSent(2)
	assert.True(t, f.dispatcher.ObserveCallResult(context.Background(), testStation, result(recent, `{"status":"Accepted"}`)))
	f.conn.set(testStation, false)
	queued := f.enqueue("UnlockConnector", `{"connectorId":1}`)
	f.clock.Step(time.Hour)

	require.NoError(t, f.dispatcher.Purge(context.Background()))

	gone, err := f.store.GetCommand(context.Background(), old.Id)
	require.NoError(t, err)
	assert.Nil(t, gone, "completed more than 90 days ago")
	assert.Equal(t, store.CommandStatusAccepted, f.get(recent.Id).Status)
	assert.Equal(t, store.CommandStatusQueued, f.get(queued.Id).Status)
}

func TestJobs(t *testing.T) {
	f := newFixture(t)

	sweep := f.dispatcher.SweepJob()
	assert.Equal(t, "commands-sweep", sweep.Name)
	assert.Equal(t, 5*time.Second, sweep.Interval)
	require.NotNil(t, sweep.Run)
	assert.NoError(t, sweep.Run(context.Background()))

	purge := f.dispatcher.PurgeJob()
	assert.Equal(t, "commands-purge", purge.Name)
	assert.Equal(t, time.Hour, purge.Interval)
	assert.NoError(t, purge.Run(context.Background()))

	// zero intervals are the defaults
	assert.Equal(t, commands.DefaultSweepInterval, (&commands.Dispatcher{}).SweepJob().Interval)
}

func TestStatusStoreConnectivity(t *testing.T) {
	f := newFixture(t)
	conn := commands.StatusStoreConnectivity{Store: f.store}

	connected, err := conn.IsConnected(context.Background(), testStation)
	require.NoError(t, err)
	assert.False(t, connected, "no status yet")

	require.NoError(t, f.store.SetChargeStationStatus(context.Background(), testStation,
		&store.ChargeStationStatus{ChargeStationId: testStation, Connected: true}))
	connected, err = conn.IsConnected(context.Background(), testStation)
	require.NoError(t, err)
	assert.True(t, connected)

	require.NoError(t, f.store.SetChargeStationStatus(context.Background(), testStation,
		&store.ChargeStationStatus{ChargeStationId: testStation, Connected: false}))
	connected, err = conn.IsConnected(context.Background(), testStation)
	require.NoError(t, err)
	assert.False(t, connected)
}

// answeringStore lets the station answer a command between the sweep's list of overdue
// commands and its change of their status.
type answeringStore struct {
	store.Engine
	afterList func()
}

func (s *answeringStore) ListOverdueSentCommands(ctx context.Context, sentBefore time.Time, limit int) ([]*store.Command, error) {
	overdue, err := s.Engine.ListOverdueSentCommands(ctx, sentBefore, limit)
	if s.afterList != nil && len(overdue) > 0 {
		answer := s.afterList
		s.afterList = nil
		answer()
	}
	return overdue, err
}

func TestTheSweepLeavesACommandAnsweredWhileItSweeps(t *testing.T) {
	f := newFixture(t)
	cmd := f.enqueue("Reset", `{"type":"Soft"}`)
	f.clock.Step(time.Minute)

	answering := &answeringStore{Engine: f.store}
	answering.afterList = func() {
		assert.True(t, f.dispatcher.ObserveCallResult(context.Background(), testStation, result(cmd, `{"status":"Accepted"}`)))
	}
	f.dispatcher.Store = answering

	require.NoError(t, f.dispatcher.Sweep(context.Background()))

	done := f.get(cmd.Id)
	assert.Equal(t, store.CommandStatusAccepted, done.Status)
	assert.Nil(t, done.ErrorCode)
	completed := f.completedEvents()
	require.Len(t, completed, 1)
	assertCompletedEvent(t, completed[0], done, false)
}

func TestAGetConfigurationThroughTheQueueUpdatesTheSettings(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	router := f.router()
	cmd := f.enqueue("GetConfiguration", `{"key":["HeartbeatInterval","MeterValueSampleInterval","Unknown"]}`)

	router.Handle(ctx, testStation, result(f.get(cmd.Id), `{"configurationKey":[`+
		`{"key":"HeartbeatInterval","readonly":false,"value":"300"},`+
		`{"key":"MeterValueSampleInterval","readonly":false,"value":"60"}],"unknownKey":["Unknown"]}`))

	assert.Equal(t, store.CommandStatusAccepted, f.get(cmd.Id).Status)
	settings, err := f.store.LookupChargeStationSettings(ctx, testStation)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, map[string]*store.ChargeStationSetting{
		"HeartbeatInterval":        {Value: "300", Status: store.ChargeStationSettingStatusAccepted},
		"MeterValueSampleInterval": {Value: "60", Status: store.ChargeStationSettingStatusAccepted},
	}, withoutSendAfter(settings.Settings))
}

func TestAChangeConfigurationThroughTheQueueRecordsThatItNeedsAReboot(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	router := f.router()
	cmd := f.enqueue("ChangeConfiguration", `{"key":"HeartbeatInterval","value":"60"}`)
	f.waitSent(1)

	router.Handle(ctx, testStation, result(f.get(cmd.Id), `{"status":"RebootRequired"}`))

	assert.Equal(t, store.CommandStatusAccepted, f.get(cmd.Id).Status)
	settings, err := f.store.LookupChargeStationSettings(ctx, testStation)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, map[string]*store.ChargeStationSetting{
		"HeartbeatInterval": {Value: "60", Status: store.ChargeStationSettingStatusRebootRequired},
	}, withoutSendAfter(settings.Settings))
	// no key waits any more: the station is asked for a BootNotification
	sent := f.waitSent(2)
	assert.Equal(t, "TriggerMessage", sent[1].msg.Action)
	assert.JSONEq(t, `{"requestedMessage":"BootNotification"}`, string(sent[1].msg.RequestPayload))
}

// withoutSendAfter returns the settings without their time of the next send, which the
// store sets.
func withoutSendAfter(settings map[string]*store.ChargeStationSetting) map[string]*store.ChargeStationSetting {
	out := make(map[string]*store.ChargeStationSetting, len(settings))
	for key, setting := range settings {
		out[key] = &store.ChargeStationSetting{Value: setting.Value, Status: setting.Status}
	}
	return out
}

func TestTheConnectDelayHoldsCommandsForAStationThatHasJustConnected(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// the station's connectivity comes from its status, as in the manager
	f.dispatcher.Connectivity = commands.StatusStoreConnectivity{Store: f.store}
	applied, err := f.store.RecordStationConnected(ctx, testStation, "conn-1", f.clock.Now())
	require.NoError(t, err)
	require.True(t, applied)

	cmd := f.enqueue("Reset", `{"type":"Soft"}`)
	assert.Equal(t, store.CommandStatusQueued, cmd.Status, "a station that has just connected gets no command")

	require.NoError(t, f.dispatcher.Sweep(ctx))
	assert.Empty(t, f.emitter.sent(), "the sweep waits for the connect delay too")

	f.clock.Step(5 * time.Second)
	require.NoError(t, f.dispatcher.Sweep(ctx))
	sent := f.waitSent(1)
	assert.Equal(t, cmd.Id, sent[0].msg.MessageId)
}

func TestAStationThatTalkedAfterConnectingIsSettled(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.dispatcher.Connectivity = commands.StatusStoreConnectivity{Store: f.store}
	_, err := f.store.RecordStationConnected(ctx, testStation, "conn-1", f.clock.Now())
	require.NoError(t, err)
	// a BootNotification or a Heartbeat on the new connection
	f.clock.Step(time.Second)
	require.NoError(t, f.store.UpdateHeartbeat(ctx, testStation, f.clock.Now()))

	cmd := f.enqueue("Reset", `{"type":"Soft"}`)

	sent := f.waitSent(1)
	assert.Equal(t, cmd.Id, sent[0].msg.MessageId)
}

func TestStatusStoreConnectivityIsSettled(t *testing.T) {
	ctx := context.Background()
	clk := testingclock.NewFakeClock(testStart)
	engine := inmemory.NewStore(clk)
	conn := commands.StatusStoreConnectivity{Store: engine}
	delay := 5 * time.Second

	settled, err := conn.IsSettled(ctx, testStation, clk.Now(), delay)
	require.NoError(t, err)
	assert.False(t, settled, "a station that never connected")

	_, err = engine.RecordStationConnected(ctx, testStation, "conn-1", clk.Now())
	require.NoError(t, err)
	settled, err = conn.IsSettled(ctx, testStation, clk.Now().Add(delay-time.Millisecond), delay)
	require.NoError(t, err)
	assert.False(t, settled, "connected for less than the delay")

	settled, err = conn.IsSettled(ctx, testStation, clk.Now().Add(delay), delay)
	require.NoError(t, err)
	assert.True(t, settled, "connected for the delay")

	_, err = engine.RecordStationDisconnected(ctx, testStation, "conn-1", clk.Now().Add(time.Minute))
	require.NoError(t, err)
	settled, err = conn.IsSettled(ctx, testStation, clk.Now().Add(time.Hour), delay)
	require.NoError(t, err)
	assert.False(t, settled, "a disconnected station")
}
