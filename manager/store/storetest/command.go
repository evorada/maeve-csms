// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// commandT0 is the time of the first command of the command tests. Every time is a
// whole second, so that PostgreSQL stores it exactly.
var commandT0 = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

// errCommandTest is what the command tests return to make a transaction fail.
var errCommandTest = errors.New("stop")

// commandAt is commandT0 plus the given number of minutes.
func commandAt(minutes int) time.Time {
	return commandT0.Add(time.Duration(minutes) * time.Minute)
}

// testCommandId is a fixed UUID per number; a higher number sorts after a lower one.
func testCommandId(n int) string {
	return fmt.Sprintf("0193b2c4-0000-7000-8000-%012d", n)
}

// testCommand is a queued Reset of orgA, created `minutes` after commandT0 and expiring
// an hour later.
func testCommand(n int, stationKey string, minutes int) *store.Command {
	return &store.Command{
		Id:          testCommandId(n),
		OrgId:       orgA,
		StationKey:  stationKey,
		OcppVersion: "ocpp1.6",
		Action:      "Reset",
		Payload:     json.RawMessage(`{"type":"Soft"}`),
		Status:      store.CommandStatusQueued,
		CreatedAt:   commandAt(minutes),
		ExpiresAt:   commandAt(minutes + 60),
	}
}

// testCommandEvent is an outbox event about a command.
func testCommandEvent(commandId string) *store.OutboxEvent {
	ev := testOutboxEvent(orgA, "command.completed", commandId)
	ev.Subject = "urn:cpms:" + orgA + ":command/" + commandId
	return ev
}

func createCommands(t *testing.T, s store.Engine, commands ...*store.Command) {
	t.Helper()
	for _, cmd := range commands {
		require.NoError(t, s.CreateCommand(context.Background(), cmd))
	}
}

func getCommand(t *testing.T, s store.Engine, id string) *store.Command {
	t.Helper()
	cmd, err := s.GetCommand(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, cmd, "command %s", id)
	return cmd
}

func claim(t *testing.T, s store.Engine, stationKey string, now time.Time) *store.Command {
	t.Helper()
	cmd, err := s.ClaimNextCommand(context.Background(), stationKey, now)
	require.NoError(t, err)
	return cmd
}

// claimedId claims the next command of the station and returns its id, "" when there
// is none.
func claimedId(t *testing.T, s store.Engine, stationKey string, now time.Time) string {
	t.Helper()
	if cmd := claim(t, s, stationKey, now); cmd != nil {
		return cmd.Id
	}
	return ""
}

// mustClaim claims the next command of the station; the test stops when there is none.
func mustClaim(t *testing.T, s store.Engine, stationKey string, now time.Time) *store.Command {
	t.Helper()
	cmd := claim(t, s, stationKey, now)
	require.NotNil(t, cmd, "nothing claimed for %s", stationKey)
	return cmd
}

func listCommands(t *testing.T, s store.Engine, filter store.CommandFilter) ([]string, int) {
	t.Helper()
	commands, total, err := s.ListCommands(context.Background(), filter)
	require.NoError(t, err)
	return commandIds(commands), total
}

func commandIds(commands []*store.Command) []string {
	ids := make([]string, len(commands))
	for i, cmd := range commands {
		ids[i] = cmd.Id
	}
	return ids
}

// assertSameCommand compares two commands field by field: JSON documents by their
// content (PostgreSQL rewrites them) and times by the instant.
func assertSameCommand(t *testing.T, want, got *store.Command) {
	t.Helper()
	if assert.NotNil(t, got) {
		assert.Equal(t, normalizedCommand(t, want), normalizedCommand(t, got))
	}
}

func normalizedCommand(t *testing.T, cmd *store.Command) *store.Command {
	t.Helper()
	n := *cmd
	n.Payload = normalizedJSON(t, cmd.Payload)
	n.Result = normalizedJSON(t, cmd.Result)
	n.Progress = normalizedJSON(t, cmd.Progress)
	n.CreatedAt = cmd.CreatedAt.UTC()
	n.ExpiresAt = cmd.ExpiresAt.UTC()
	n.ProgressAt = utcOrNil(cmd.ProgressAt)
	n.SentAt = utcOrNil(cmd.SentAt)
	n.CompletedAt = utcOrNil(cmd.CompletedAt)
	return &n
}

func normalizedJSON(t *testing.T, document json.RawMessage) json.RawMessage {
	t.Helper()
	if document == nil {
		return nil
	}
	var v any
	require.NoError(t, json.Unmarshal(document, &v))
	normalized, err := json.Marshal(v)
	require.NoError(t, err)
	return normalized
}

func utcOrNil(at *time.Time) *time.Time {
	if at == nil {
		return nil
	}
	utc := at.UTC()
	return &utc
}

// TestCommandStore checks a CommandStore: the round trip of a command, the list
// filters, the claim of the next command of a station (at most one in flight), the
// compare-and-set transitions with their outbox events, and the lists and the purge of
// the dispatcher.
func TestCommandStore(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()

	t.Run("create and get round trip", func(t *testing.T) {
		s := newStore(t)
		// a station key of the widest kind: org key, dot, station id (80 characters)
		full := testCommand(1, "k7f2."+strings.Repeat("x", 75), 0)
		full.OcppVersion = "ocpp2.0.1"
		full.Action = "RequestStartTransaction"
		full.Payload = json.RawMessage(`{"idToken":{"idToken":"DEADBEEF","type":"ISO14443"},"remoteStartId":7}`)
		full.Result = json.RawMessage(`{"status":"Accepted"}`)
		full.ErrorCode = ptr("FormatViolation")
		full.ErrorDescription = ptr("bad payload")
		full.Progress = json.RawMessage(`{"state":"awaiting_transaction"}`)
		full.ProgressAt = ptr(commandAt(2))
		full.Actor = ptr("user:alice;act=svc:billing")
		full.Jti = ptr("jti-1")
		full.SentAt = ptr(commandAt(1))
		full.CompletedAt = ptr(commandAt(3))
		minimal := testCommand(2, "k7f2.cs001", 0)
		createCommands(t, s, full, minimal)

		assertSameCommand(t, full, getCommand(t, s, full.Id))
		got := getCommand(t, s, minimal.Id)
		assertSameCommand(t, minimal, got)
		assert.Nil(t, got.Result)
		assert.Nil(t, got.Progress)
		assert.Nil(t, got.ErrorCode)
		assert.Nil(t, got.Actor)
		assert.Nil(t, got.SentAt)
		assert.Nil(t, got.CompletedAt)

		unknown, err := s.GetCommand(ctx, testCommandId(99))
		assert.NoError(t, err)
		assert.Nil(t, unknown)
		notAUUID, err := s.GetCommand(ctx, "not-a-uuid")
		assert.NoError(t, err)
		assert.Nil(t, notAUUID, "an id that is not a UUID names no command")
	})

	t.Run("stored command does not share memory with the caller", func(t *testing.T) {
		s := newStore(t)
		cmd := testCommand(1, "k7f2.cs001", 0)
		cmd.Actor = ptr("user:alice")
		createCommands(t, s, cmd)
		cmd.Payload[2] = 'X'
		*cmd.Actor = "user:mallory"

		got := getCommand(t, s, cmd.Id)
		got.Payload[2] = 'Y'
		*got.Actor = "user:eve"

		again := getCommand(t, s, cmd.Id)
		assert.JSONEq(t, `{"type":"Soft"}`, string(again.Payload))
		assert.Equal(t, "user:alice", *again.Actor)
	})

	t.Run("create refuses a command that is not queued, a second one with the same id and ids that are not UUIDs", func(t *testing.T) {
		s := newStore(t)
		sent := testCommand(1, "k7f2.cs001", 0)
		sent.Status = store.CommandStatusSent
		assert.Error(t, s.CreateCommand(ctx, sent))
		noStatus := testCommand(1, "k7f2.cs001", 0)
		noStatus.Status = ""
		assert.Error(t, s.CreateCommand(ctx, noStatus))
		unknown, err := s.GetCommand(ctx, sent.Id)
		require.NoError(t, err)
		assert.Nil(t, unknown, "nothing was stored")

		createCommands(t, s, testCommand(1, "k7f2.cs001", 0))
		again := testCommand(1, "k7f2.cs002", 5)
		assert.ErrorIs(t, s.CreateCommand(ctx, again), store.ErrAlreadyExists)
		assert.Equal(t, "k7f2.cs001", getCommand(t, s, again.Id).StationKey, "the first command is kept")

		badId := testCommand(2, "k7f2.cs001", 0)
		badId.Id = "cmd-2"
		assert.Error(t, s.CreateCommand(ctx, badId))
		badOrg := testCommand(3, "k7f2.cs001", 0)
		badOrg.OrgId = "org-a"
		assert.Error(t, s.CreateCommand(ctx, badOrg))
	})

	t.Run("list filters, orders, pages and counts", func(t *testing.T) {
		s := newStore(t)
		c1 := testCommand(1, "k7f2.cs001", 0)
		c1.Payload = json.RawMessage(`{"transactionId":42}`)
		c1.Action = "RemoteStopTransaction"
		c2 := testCommand(2, "k7f2.cs001", 10)
		c3 := testCommand(3, "k7f2.cs002", 20)
		c3.Action = "UnlockConnector"
		c3.Payload = json.RawMessage(`{"connectorId":1}`)
		c4 := testCommand(4, "q9x1.cs001", 30)
		c4.OrgId = orgB
		// same creation time as c4: the higher id comes first
		c5 := testCommand(5, "k7f2.cs001", 30)
		c5.Action = "RemoteStopTransaction"
		c5.Payload = json.RawMessage(`{"transactionId":43}`)
		createCommands(t, s, c1, c2, c3, c4, c5)
		_, err := s.TransitionCommand(ctx, c2.Id, []store.CommandStatus{store.CommandStatusQueued},
			store.CommandUpdate{Status: store.CommandStatusCancelled, CompletedAt: ptr(commandAt(11))})
		require.NoError(t, err)

		ids := func(commands ...*store.Command) []string { return commandIds(commands) }

		page, total := listCommands(t, s, store.CommandFilter{})
		assert.Equal(t, ids(c5, c4, c3, c2, c1), page, "created_at DESC, id DESC")
		assert.Equal(t, 5, total)

		page, total = listCommands(t, s, store.CommandFilter{OrgId: orgA})
		assert.Equal(t, ids(c5, c3, c2, c1), page)
		assert.Equal(t, 4, total)
		page, _ = listCommands(t, s, store.CommandFilter{OrgId: orgB})
		assert.Equal(t, ids(c4), page)

		page, _ = listCommands(t, s, store.CommandFilter{StationKey: "k7f2.cs001"})
		assert.Equal(t, ids(c5, c2, c1), page)
		page, _ = listCommands(t, s, store.CommandFilter{OrgId: orgB, StationKey: "k7f2.cs001"})
		assert.Empty(t, page)

		page, _ = listCommands(t, s, store.CommandFilter{Actions: []string{"RemoteStopTransaction", "UnlockConnector"}})
		assert.Equal(t, ids(c5, c3, c1), page)
		page, _ = listCommands(t, s, store.CommandFilter{Statuses: []store.CommandStatus{store.CommandStatusCancelled}})
		assert.Equal(t, ids(c2), page)
		page, _ = listCommands(t, s, store.CommandFilter{Statuses: []store.CommandStatus{store.CommandStatusQueued, store.CommandStatusSent}})
		assert.Equal(t, ids(c5, c4, c3, c1), page)

		page, _ = listCommands(t, s, store.CommandFilter{PayloadContains: json.RawMessage(`{"transactionId":42}`)})
		assert.Equal(t, ids(c1), page)
		page, _ = listCommands(t, s, store.CommandFilter{PayloadContains: json.RawMessage(`{"transactionId":43}`)})
		assert.Equal(t, ids(c5), page)
		page, _ = listCommands(t, s, store.CommandFilter{PayloadContains: json.RawMessage(`{"transactionId":44}`)})
		assert.Empty(t, page)
		page, _ = listCommands(t, s, store.CommandFilter{PayloadContains: json.RawMessage(`{"type":"Soft"}`)})
		assert.Equal(t, ids(c4, c2), page)

		page, total = listCommands(t, s, store.CommandFilter{CreatedAfter: ptr(commandAt(10)), CreatedBefore: ptr(commandAt(30))})
		assert.Equal(t, ids(c3), page, "both bounds are exclusive")
		assert.Equal(t, 1, total)
		page, _ = listCommands(t, s, store.CommandFilter{CreatedAfter: ptr(commandAt(9))})
		assert.Equal(t, ids(c5, c4, c3, c2), page)
		page, _ = listCommands(t, s, store.CommandFilter{CreatedBefore: ptr(commandAt(11))})
		assert.Equal(t, ids(c2, c1), page)

		page, total = listCommands(t, s, store.CommandFilter{Limit: 2})
		assert.Equal(t, ids(c5, c4), page)
		assert.Equal(t, 5, total, "the total ignores limit and offset")
		page, total = listCommands(t, s, store.CommandFilter{Limit: 2, Offset: 2})
		assert.Equal(t, ids(c3, c2), page)
		assert.Equal(t, 5, total)
		page, _ = listCommands(t, s, store.CommandFilter{Limit: 2, Offset: 4})
		assert.Equal(t, ids(c1), page)
		page, total = listCommands(t, s, store.CommandFilter{Offset: 10})
		assert.Empty(t, page)
		assert.Equal(t, 5, total)
		page, _ = listCommands(t, s, store.CommandFilter{Limit: -1, Offset: -1})
		assert.Equal(t, ids(c5, c4, c3, c2, c1), page, "no limit and no offset")

		page, total = listCommands(t, s, store.CommandFilter{OrgId: orgA, StationKey: "k7f2.cs001",
			Actions: []string{"RemoteStopTransaction"}, Statuses: []store.CommandStatus{store.CommandStatusQueued},
			PayloadContains: json.RawMessage(`{"transactionId":43}`), CreatedAfter: ptr(commandAt(0))})
		assert.Equal(t, ids(c5), page, "all filters together")
		assert.Equal(t, 1, total)

		_, _, err = s.ListCommands(ctx, store.CommandFilter{OrgId: "org-a"})
		assert.Error(t, err, "an org id that is not a UUID")
		_, _, err = s.ListCommands(ctx, store.CommandFilter{PayloadContains: json.RawMessage(`[42]`)})
		assert.Error(t, err, "a payload filter that is not a JSON object")
	})

	t.Run("list filters on the top-level keys of the progress", func(t *testing.T) {
		s := newStore(t)
		c1 := testCommand(1, "k7f2.cs001", 0)
		c1.Action = "RemoteStartTransaction"
		c1.Payload = json.RawMessage(`{"idTag":"DEADBEEF","connectorId":1}`)
		c2 := testCommand(2, "k7f2.cs001", 10)
		c2.Action = "RemoteStartTransaction"
		c2.Payload = json.RawMessage(`{"idTag":"DEADBEEF"}`)
		c3 := testCommand(3, "k7f2.cs001", 20)
		c3.Action = "RemoteStartTransaction"
		c3.Payload = json.RawMessage(`{"idTag":"CAFEBABE"}`)
		c4 := testCommand(4, "k7f2.cs002", 30) // no progress
		createCommands(t, s, c1, c2, c3, c4)
		for id, progress := range map[string]string{
			c1.Id: `{"state":"awaiting_transaction","deadline":"2026-11-27T12:05:00Z"}`,
			c2.Id: `{"state":"transaction_started","transactionId":"t-1","ocppTransactionId":7,"connectorId":2}`,
			c3.Id: `{"state":"awaiting_transaction","deadline":"2026-11-27T12:25:00Z"}`,
		} {
			_, err := s.SetCommandProgress(ctx, id, json.RawMessage(progress), commandAt(40), nil)
			require.NoError(t, err)
		}
		ids := func(commands ...*store.Command) []string { return commandIds(commands) }

		page, total := listCommands(t, s, store.CommandFilter{ProgressContains: json.RawMessage(`{"state":"awaiting_transaction"}`)})
		assert.Equal(t, ids(c3, c1), page)
		assert.Equal(t, 2, total)
		page, _ = listCommands(t, s, store.CommandFilter{ProgressContains: json.RawMessage(`{"state":"transaction_started","ocppTransactionId":7}`)})
		assert.Equal(t, ids(c2), page, "every key must be there with an equal value")
		page, _ = listCommands(t, s, store.CommandFilter{ProgressContains: json.RawMessage(`{"state":"transaction_started","ocppTransactionId":8}`)})
		assert.Empty(t, page)
		page, _ = listCommands(t, s, store.CommandFilter{ProgressContains: json.RawMessage(`{"state":"no_transaction"}`)})
		assert.Empty(t, page)
		page, _ = listCommands(t, s, store.CommandFilter{ProgressContains: json.RawMessage(`{}`)})
		assert.Equal(t, ids(c3, c2, c1), page, "an empty object matches every command with progress, not those without")

		page, total = listCommands(t, s, store.CommandFilter{StationKey: "k7f2.cs001",
			PayloadContains:  json.RawMessage(`{"idTag":"DEADBEEF"}`),
			ProgressContains: json.RawMessage(`{"state":"awaiting_transaction"}`)})
		assert.Equal(t, ids(c1), page, "with the payload filter")
		assert.Equal(t, 1, total)

		_, _, err := s.ListCommands(ctx, store.CommandFilter{ProgressContains: json.RawMessage(`"awaiting_transaction"`)})
		assert.Error(t, err, "a progress filter that is not a JSON object")
	})

	t.Run("claim takes the oldest queued command and sets sent_at", func(t *testing.T) {
		s := newStore(t)
		// created in this order: 3, then 1 and 2 at the same time (the lower id first)
		createCommands(t, s,
			testCommand(2, "k7f2.cs001", 10),
			testCommand(3, "k7f2.cs001", 5),
			testCommand(1, "k7f2.cs001", 10))
		now := commandAt(20)

		cmd := claim(t, s, "k7f2.cs001", now)
		require.NotNil(t, cmd)
		assert.Equal(t, testCommandId(3), cmd.Id)
		assert.Equal(t, store.CommandStatusSent, cmd.Status)
		assertTime(t, now, cmd.SentAt, "SentAt")
		stored := getCommand(t, s, cmd.Id)
		assert.Equal(t, store.CommandStatusSent, stored.Status)
		assertTime(t, now, stored.SentAt, "stored SentAt")

		assert.Nil(t, claim(t, s, "k7f2.cs001", now), "one command in flight per station")

		complete(t, s, testCommandId(3))
		assert.Equal(t, testCommandId(1), claimedId(t, s, "k7f2.cs001", now))
		complete(t, s, testCommandId(1))
		assert.Equal(t, testCommandId(2), claimedId(t, s, "k7f2.cs001", now))
		complete(t, s, testCommandId(2))
		assert.Nil(t, claim(t, s, "k7f2.cs001", now), "nothing queued")
	})

	t.Run("claim skips expired commands", func(t *testing.T) {
		s := newStore(t)
		expired := testCommand(1, "k7f2.cs001", 0)
		expired.ExpiresAt = commandAt(15)
		createCommands(t, s, expired, testCommand(2, "k7f2.cs001", 5))

		assert.Equal(t, testCommandId(2), claimedId(t, s, "k7f2.cs001", commandAt(15)), "expires_at = now is expired")
		assert.Equal(t, store.CommandStatusQueued, getCommand(t, s, testCommandId(1)).Status)
	})

	t.Run("claims of different stations are independent", func(t *testing.T) {
		s := newStore(t)
		createCommands(t, s, testCommand(1, "k7f2.cs001", 0), testCommand(2, "k7f2.cs002", 5))

		assert.Equal(t, testCommandId(1), claimedId(t, s, "k7f2.cs001", commandAt(10)))
		assert.Equal(t, testCommandId(2), claimedId(t, s, "k7f2.cs002", commandAt(10)))
		assert.Nil(t, claim(t, s, "k7f2.cs003", commandAt(10)))
	})

	t.Run("concurrent claims of one station: exactly one gets a command", func(t *testing.T) {
		s := newStore(t)
		createCommands(t, s,
			testCommand(1, "k7f2.cs001", 0),
			testCommand(2, "k7f2.cs001", 1),
			testCommand(3, "k7f2.cs001", 2))

		const claimers = 10
		var (
			wg      sync.WaitGroup
			mu      sync.Mutex
			claimed []*store.Command
			errs    []error
		)
		start := make(chan struct{})
		for range claimers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				cmd, err := s.ClaimNextCommand(ctx, "k7f2.cs001", commandAt(10))
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					errs = append(errs, err)
				}
				if cmd != nil {
					claimed = append(claimed, cmd)
				}
			}()
		}
		close(start)
		wg.Wait()

		assert.Empty(t, errs)
		if assert.Len(t, claimed, 1) {
			assert.Equal(t, testCommandId(1), claimed[0].Id, "the oldest")
		}
		sent, _ := listCommands(t, s, store.CommandFilter{Statuses: []store.CommandStatus{store.CommandStatusSent}})
		assert.Equal(t, []string{testCommandId(1)}, sent)
	})

	t.Run("transition sent to accepted sets the fields and appends the event", func(t *testing.T) {
		s := newStore(t)
		createCommands(t, s, testCommand(1, "k7f2.cs001", 0))
		claimed := mustClaim(t, s, "k7f2.cs001", commandAt(1))
		ev := testCommandEvent(claimed.Id)

		got, err := s.TransitionCommand(ctx, claimed.Id, []store.CommandStatus{store.CommandStatusSent, store.CommandStatusTimeout},
			store.CommandUpdate{
				Status:      store.CommandStatusAccepted,
				Result:      json.RawMessage(`{"status":"Accepted"}`),
				SentAt:      claimed.SentAt,
				CompletedAt: ptr(commandAt(2)),
				Event:       ev,
			})
		require.NoError(t, err)
		want := testCommand(1, "k7f2.cs001", 0)
		want.Status = store.CommandStatusAccepted
		want.Result = json.RawMessage(`{"status":"Accepted"}`)
		want.SentAt = ptr(commandAt(1))
		want.CompletedAt = ptr(commandAt(2))
		assertSameCommand(t, want, got)
		assertSameCommand(t, want, getCommand(t, s, claimed.Id))

		assert.Equal(t, []string{ev.Id}, outboxIds(relayAll(t, s)), "one event, appended with the update")
		assert.NotZero(t, ev.Seq)
	})

	t.Run("transition writes the error fields and keeps the result when none is given", func(t *testing.T) {
		s := newStore(t)
		createCommands(t, s, testCommand(1, "k7f2.cs001", 0))
		claimed := mustClaim(t, s, "k7f2.cs001", commandAt(1))
		_, err := s.TransitionCommand(ctx, claimed.Id, []store.CommandStatus{store.CommandStatusSent},
			store.CommandUpdate{Status: store.CommandStatusTimeout, ErrorCode: ptr("ResponseTimeout"),
				ErrorDescription: ptr("no response"), SentAt: claimed.SentAt, CompletedAt: ptr(commandAt(2))})
		require.NoError(t, err)

		// a late result: the error fields are written as given (cleared), the result set
		got, err := s.TransitionCommand(ctx, claimed.Id, []store.CommandStatus{store.CommandStatusSent, store.CommandStatusTimeout},
			store.CommandUpdate{Status: store.CommandStatusRejected, Result: json.RawMessage(`{"status":"Rejected"}`),
				SentAt: claimed.SentAt, CompletedAt: ptr(commandAt(3))})
		require.NoError(t, err)
		assert.Equal(t, store.CommandStatusRejected, got.Status)
		assert.Nil(t, got.ErrorCode)
		assert.Nil(t, got.ErrorDescription)
		assertTime(t, commandAt(3), got.CompletedAt, "CompletedAt")

		got, err = s.TransitionCommand(ctx, claimed.Id, []store.CommandStatus{store.CommandStatusRejected},
			store.CommandUpdate{Status: store.CommandStatusFailed, ErrorCode: ptr("FormatViolation"), CompletedAt: ptr(commandAt(4))})
		require.NoError(t, err)
		assert.JSONEq(t, `{"status":"Rejected"}`, string(got.Result), "a nil result leaves the result")
		assert.Equal(t, "FormatViolation", *got.ErrorCode)
		assert.Nil(t, got.SentAt, "sent_at is written as given")
	})

	t.Run("transition from a status not in from is a conflict and appends no event", func(t *testing.T) {
		s := newStore(t)
		createCommands(t, s, testCommand(1, "k7f2.cs001", 0))

		got, err := s.TransitionCommand(ctx, testCommandId(1), []store.CommandStatus{store.CommandStatusSent, store.CommandStatusTimeout},
			store.CommandUpdate{Status: store.CommandStatusAccepted, CompletedAt: ptr(commandAt(1)), Event: testCommandEvent(testCommandId(1))})
		assert.ErrorIs(t, err, store.ErrCommandConflict)
		assert.Nil(t, got)
		got, err = s.TransitionCommand(ctx, testCommandId(1), nil,
			store.CommandUpdate{Status: store.CommandStatusCancelled, Event: testCommandEvent(testCommandId(1))})
		assert.ErrorIs(t, err, store.ErrCommandConflict, "no status matches an empty from")
		assert.Nil(t, got)

		stored := getCommand(t, s, testCommandId(1))
		assert.Equal(t, store.CommandStatusQueued, stored.Status)
		assert.Nil(t, stored.CompletedAt)
		assert.Empty(t, relayAll(t, s), "no event without the update")
	})

	t.Run("transition of an unknown command returns nil and appends no event", func(t *testing.T) {
		s := newStore(t)
		for _, id := range []string{testCommandId(99), "not-a-uuid"} {
			got, err := s.TransitionCommand(ctx, id, []store.CommandStatus{store.CommandStatusQueued},
				store.CommandUpdate{Status: store.CommandStatusCancelled, Event: testCommandEvent(testCommandId(99))})
			assert.NoError(t, err, id)
			assert.Nil(t, got, id)
		}
		assert.Empty(t, relayAll(t, s))
	})

	t.Run("transition sent to queued without sent_at makes the command claimable again", func(t *testing.T) {
		s := newStore(t)
		createCommands(t, s, testCommand(1, "k7f2.cs001", 0), testCommand(2, "k7f2.cs001", 5))
		claimed := mustClaim(t, s, "k7f2.cs001", commandAt(10))

		got, err := s.TransitionCommand(ctx, claimed.Id, []store.CommandStatus{store.CommandStatusSent},
			store.CommandUpdate{Status: store.CommandStatusQueued, SentAt: nil})
		require.NoError(t, err)
		assert.Equal(t, store.CommandStatusQueued, got.Status)
		assert.Nil(t, got.SentAt)
		assert.Nil(t, getCommand(t, s, claimed.Id).SentAt)

		again := mustClaim(t, s, "k7f2.cs001", commandAt(11))
		assert.Equal(t, claimed.Id, again.Id, "still the oldest")
		assertTime(t, commandAt(11), again.SentAt, "SentAt")
	})

	t.Run("transition to sent while another command of the station is sent is a conflict", func(t *testing.T) {
		s := newStore(t)
		createCommands(t, s, testCommand(1, "k7f2.cs001", 0), testCommand(2, "k7f2.cs001", 5), testCommand(3, "k7f2.cs002", 5))
		claim(t, s, "k7f2.cs001", commandAt(10))

		_, err := s.TransitionCommand(ctx, testCommandId(2), []store.CommandStatus{store.CommandStatusQueued},
			store.CommandUpdate{Status: store.CommandStatusSent, SentAt: ptr(commandAt(10)), Event: testCommandEvent(testCommandId(2))})
		assert.ErrorIs(t, err, store.ErrCommandConflict)
		assert.Equal(t, store.CommandStatusQueued, getCommand(t, s, testCommandId(2)).Status)
		assert.Empty(t, relayAll(t, s))

		got, err := s.TransitionCommand(ctx, testCommandId(3), []store.CommandStatus{store.CommandStatusQueued},
			store.CommandUpdate{Status: store.CommandStatusSent, SentAt: ptr(commandAt(10))})
		require.NoError(t, err, "another station")
		assert.Equal(t, store.CommandStatusSent, got.Status)
	})

	t.Run("an event of a transition in a transaction that fails is not stored", func(t *testing.T) {
		s := newStore(t)
		createCommands(t, s, testCommand(1, "k7f2.cs001", 0))

		err := s.RunInTx(ctx, func(ctx context.Context) error {
			_, err := s.TransitionCommand(ctx, testCommandId(1), []store.CommandStatus{store.CommandStatusQueued},
				store.CommandUpdate{Status: store.CommandStatusCancelled, CompletedAt: ptr(commandAt(1)), Event: testCommandEvent(testCommandId(1))})
			require.NoError(t, err)
			return errCommandTest
		})
		require.ErrorIs(t, err, errCommandTest)
		assert.Empty(t, relayAll(t, s))

		ev := testCommandEvent(testCommandId(1))
		err = s.RunInTx(ctx, func(ctx context.Context) error {
			_, err := s.SetCommandProgress(ctx, testCommandId(1), json.RawMessage(`{"state":"done"}`), commandAt(2), ev)
			return err
		})
		require.NoError(t, err)
		assert.Equal(t, []string{ev.Id}, outboxIds(relayAll(t, s)), "stored with a transaction that succeeds")
	})

	t.Run("set progress keeps the status and appends the event", func(t *testing.T) {
		s := newStore(t)
		createCommands(t, s, testCommand(1, "k7f2.cs001", 0))
		claimed := mustClaim(t, s, "k7f2.cs001", commandAt(1))
		_, err := s.TransitionCommand(ctx, claimed.Id, []store.CommandStatus{store.CommandStatusSent},
			store.CommandUpdate{Status: store.CommandStatusAccepted, SentAt: claimed.SentAt, CompletedAt: ptr(commandAt(2))})
		require.NoError(t, err)
		ev := testCommandEvent(claimed.Id)

		got, err := s.SetCommandProgress(ctx, claimed.Id, json.RawMessage(`{"state":"awaiting_transaction"}`), commandAt(3), ev)
		require.NoError(t, err)
		assert.Equal(t, store.CommandStatusAccepted, got.Status)
		assert.JSONEq(t, `{"state":"awaiting_transaction"}`, string(got.Progress))
		assertTime(t, commandAt(3), got.ProgressAt, "ProgressAt")
		assertTime(t, commandAt(2), got.CompletedAt, "CompletedAt")
		assert.Equal(t, []string{ev.Id}, outboxIds(relayAll(t, s)))

		got, err = s.SetCommandProgress(ctx, claimed.Id, json.RawMessage(`{"state":"transaction_started","transactionId":"t-1"}`), commandAt(4), nil)
		require.NoError(t, err)
		stored := getCommand(t, s, claimed.Id)
		assertSameCommand(t, got, stored)
		assert.JSONEq(t, `{"state":"transaction_started","transactionId":"t-1"}`, string(stored.Progress), "replaced, not merged")
		assertTime(t, commandAt(4), stored.ProgressAt, "ProgressAt")
		assert.Equal(t, store.CommandStatusAccepted, stored.Status)
		assert.Empty(t, relayAll(t, s), "no event when none is given")

		for _, id := range []string{testCommandId(99), "not-a-uuid"} {
			unknown, err := s.SetCommandProgress(ctx, id, json.RawMessage(`{}`), commandAt(5), testCommandEvent(id))
			assert.NoError(t, err, id)
			assert.Nil(t, unknown, id)
		}
		assert.Empty(t, relayAll(t, s), "no event for an unknown command")
	})

	t.Run("queued station keys are paged by station key and skip expired commands", func(t *testing.T) {
		s := newStore(t)
		expired := testCommand(5, "k7f2.cs005", 0)
		expired.ExpiresAt = commandAt(20)
		createCommands(t, s,
			testCommand(1, "k7f2.cs003", 0),
			testCommand(2, "k7f2.cs001", 0),
			testCommand(3, "k7f2.cs003", 5),
			testCommand(4, "k7f2.cs002", 0),
			expired,
			testCommand(6, "k7f2.cs004", 0),
		)
		claim(t, s, "k7f2.cs004", commandAt(10))
		now := commandAt(20)

		list := func(after string, limit int) []string {
			keys, err := s.ListQueuedStationKeys(ctx, now, after, limit)
			require.NoError(t, err)
			return keys
		}
		assert.Equal(t, []string{"k7f2.cs001", "k7f2.cs002"}, list("", 2))
		assert.Equal(t, []string{"k7f2.cs003"}, list("k7f2.cs002", 2), "cs004 has nothing queued, cs005 only expired")
		assert.Empty(t, list("k7f2.cs003", 2))
		assert.Equal(t, []string{"k7f2.cs001", "k7f2.cs002", "k7f2.cs003"}, list("", 10))

		_, err := s.ListQueuedStationKeys(ctx, now, "", 0)
		assert.Error(t, err)
	})

	t.Run("expired queued commands, oldest expiry first", func(t *testing.T) {
		s := newStore(t)
		first := testCommand(1, "k7f2.cs001", 0)
		first.ExpiresAt = commandAt(10)
		second := testCommand(2, "k7f2.cs002", 0)
		second.ExpiresAt = commandAt(5)
		atNow := testCommand(3, "k7f2.cs003", 0)
		atNow.ExpiresAt = commandAt(20)
		later := testCommand(4, "k7f2.cs001", 0)
		later.ExpiresAt = commandAt(21)
		sentExpired := testCommand(5, "k7f2.cs004", 0)
		sentExpired.ExpiresAt = commandAt(5)
		createCommands(t, s, first, second, atNow, later, sentExpired)
		claim(t, s, "k7f2.cs004", commandAt(1))

		expired, err := s.ListExpiredQueuedCommands(ctx, commandAt(20), 10)
		require.NoError(t, err)
		assert.Equal(t, []string{second.Id, first.Id, atNow.Id}, commandIds(expired), "expires_at <= now, queued only")
		assertSameCommand(t, second, expired[0])

		expired, err = s.ListExpiredQueuedCommands(ctx, commandAt(20), 2)
		require.NoError(t, err)
		assert.Equal(t, []string{second.Id, first.Id}, commandIds(expired))

		_, err = s.ListExpiredQueuedCommands(ctx, commandAt(20), 0)
		assert.Error(t, err)
	})

	t.Run("overdue sent commands, oldest first", func(t *testing.T) {
		s := newStore(t)
		createCommands(t, s,
			testCommand(1, "k7f2.cs001", 0),
			testCommand(2, "k7f2.cs002", 0),
			testCommand(3, "k7f2.cs003", 0),
			testCommand(4, "k7f2.cs004", 0))
		claim(t, s, "k7f2.cs001", commandAt(10))
		claim(t, s, "k7f2.cs002", commandAt(5))
		claim(t, s, "k7f2.cs003", commandAt(11))

		overdue, err := s.ListOverdueSentCommands(ctx, commandAt(10), 10)
		require.NoError(t, err)
		assert.Equal(t, []string{testCommandId(2), testCommandId(1)}, commandIds(overdue), "sent_at <= sentBefore, sent only")
		assert.Equal(t, store.CommandStatusSent, overdue[0].Status)

		overdue, err = s.ListOverdueSentCommands(ctx, commandAt(10), 1)
		require.NoError(t, err)
		assert.Equal(t, []string{testCommandId(2)}, commandIds(overdue))

		_, err = s.ListOverdueSentCommands(ctx, commandAt(10), 0)
		assert.Error(t, err)
	})

	t.Run("delete completed removes only terminal commands completed before the cut-off, up to limit", func(t *testing.T) {
		s := newStore(t)
		terminal := []store.CommandStatus{store.CommandStatusAccepted, store.CommandStatusRejected,
			store.CommandStatusFailed, store.CommandStatusTimeout, store.CommandStatusCancelled}
		for i, status := range terminal {
			createCommands(t, s, testCommand(i+1, "k7f2.cs001", 0))
			_, err := s.TransitionCommand(ctx, testCommandId(i+1), []store.CommandStatus{store.CommandStatusQueued},
				store.CommandUpdate{Status: status, CompletedAt: ptr(commandAt(10 + i))})
			require.NoError(t, err)
		}
		// completed at the cut-off: kept
		createCommands(t, s, testCommand(6, "k7f2.cs002", 0))
		_, err := s.TransitionCommand(ctx, testCommandId(6), []store.CommandStatus{store.CommandStatusQueued},
			store.CommandUpdate{Status: store.CommandStatusAccepted, CompletedAt: ptr(commandAt(20))})
		require.NoError(t, err)
		// a queued and a sent command with an old completed_at: never deleted
		createCommands(t, s, testCommand(7, "k7f2.cs003", 0), testCommand(8, "k7f2.cs004", 0))
		_, err = s.TransitionCommand(ctx, testCommandId(7), []store.CommandStatus{store.CommandStatusQueued},
			store.CommandUpdate{Status: store.CommandStatusQueued, CompletedAt: ptr(commandAt(1))})
		require.NoError(t, err)
		_, err = s.TransitionCommand(ctx, testCommandId(8), []store.CommandStatus{store.CommandStatusQueued},
			store.CommandUpdate{Status: store.CommandStatusSent, SentAt: ptr(commandAt(1)), CompletedAt: ptr(commandAt(1))})
		require.NoError(t, err)

		deleted, err := s.DeleteCompletedCommandsBefore(ctx, commandAt(20), 3)
		require.NoError(t, err)
		assert.Equal(t, 3, deleted)
		remaining, _ := listCommands(t, s, store.CommandFilter{})
		assert.ElementsMatch(t, []string{testCommandId(4), testCommandId(5), testCommandId(6), testCommandId(7), testCommandId(8)},
			remaining, "the oldest completed go first")

		deleted, err = s.DeleteCompletedCommandsBefore(ctx, commandAt(20), 10)
		require.NoError(t, err)
		assert.Equal(t, 2, deleted)
		remaining, _ = listCommands(t, s, store.CommandFilter{})
		assert.ElementsMatch(t, []string{testCommandId(6), testCommandId(7), testCommandId(8)}, remaining)

		deleted, err = s.DeleteCompletedCommandsBefore(ctx, commandAt(20), 10)
		require.NoError(t, err)
		assert.Zero(t, deleted)

		_, err = s.DeleteCompletedCommandsBefore(ctx, commandAt(20), 0)
		assert.Error(t, err)
	})
}

// complete moves a sent command to accepted.
func complete(t *testing.T, s store.Engine, id string) {
	t.Helper()
	_, err := s.TransitionCommand(context.Background(), id, []store.CommandStatus{store.CommandStatusSent},
		store.CommandUpdate{Status: store.CommandStatusAccepted, CompletedAt: ptr(commandT0)})
	require.NoError(t, err)
}
