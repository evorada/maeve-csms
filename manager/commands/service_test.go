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
)

func newCommand(action, payload string) *store.Command {
	return &store.Command{OrgId: testOrgId, StationKey: testStation, Action: action, Payload: json.RawMessage(payload)}
}

func TestEnqueueStoresTheCommand(t *testing.T) {
	f := newFixture(t)
	f.conn.set(testStation, false)
	f.service.NewId = func() string { return "0193b2c4-0000-7000-8000-00000000c001" }

	in := newCommand("Reset", `{"type":"Soft"}`)
	in.Actor = ptr("user:alice;act=svc:platform")
	in.Jti = ptr("jti-1")
	cmd, err := f.service.Enqueue(context.Background(), in)
	require.NoError(t, err)

	want := &store.Command{
		Id:          "0193b2c4-0000-7000-8000-00000000c001",
		OrgId:       testOrgId,
		StationKey:  testStation,
		OcppVersion: "ocpp1.6",
		Action:      "Reset",
		Payload:     json.RawMessage(`{"type":"Soft"}`),
		Status:      store.CommandStatusQueued,
		Actor:       ptr("user:alice;act=svc:platform"),
		Jti:         ptr("jti-1"),
		CreatedAt:   testStart,
		ExpiresAt:   testStart.Add(5 * time.Minute),
	}
	cmd.CreatedAt, cmd.ExpiresAt = cmd.CreatedAt.UTC(), cmd.ExpiresAt.UTC()
	assert.Equal(t, want, cmd)
	stored := f.get(cmd.Id)
	stored.CreatedAt, stored.ExpiresAt = stored.CreatedAt.UTC(), stored.ExpiresAt.UTC()
	assert.Equal(t, want, stored)

	// the caller's command is not changed
	assert.Empty(t, in.Id)
	assert.Empty(t, in.OcppVersion)
	assert.Empty(t, in.Status)
}

func TestEnqueueGivesNewCommandsUUIDv7Ids(t *testing.T) {
	f := newFixture(t)
	first := f.enqueue("ClearCache", `{}`)
	second := f.enqueue("ClearCache", `{}`)
	assert.NotEqual(t, first.Id, second.Id)
	assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, first.Id)
}

func TestEnqueueTakesAnEmptyPayloadAsAnEmptyObject(t *testing.T) {
	f := newFixture(t)
	cmd, err := f.service.Enqueue(context.Background(), newCommand("ClearCache", ""))
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(cmd.Payload))
}

func TestEnqueueRefusesIncompleteCommands(t *testing.T) {
	for name, cmd := range map[string]*store.Command{
		"no command":     nil,
		"no org id":      {StationKey: testStation, Action: "ClearCache"},
		"no station key": {OrgId: testOrgId, Action: "ClearCache"},
		"no action":      {OrgId: testOrgId, StationKey: testStation},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			_, err := f.service.Enqueue(context.Background(), cmd)
			assert.ErrorIs(t, err, commands.ErrMissingField)
			assert.Empty(t, f.emitter.sent())
		})
	}
}

func TestEnqueueRefusesWhatCannotBeSent(t *testing.T) {
	for name, tc := range map[string]struct {
		cmd  *store.Command
		want error
	}{
		"unknown action":            {newCommand("Teleport", `{}`), commands.ErrUnknownAction},
		"call of the station":       {newCommand("BootNotification", `{}`), commands.ErrUnknownAction},
		"invalid payload":           {newCommand("Reset", `{"type":"Warm"}`), commands.ErrInvalidPayload},
		"missing field":             {newCommand("UnlockConnector", `{}`), commands.ErrInvalidPayload},
		"payload that is not json":  {newCommand("Reset", `type=Soft`), commands.ErrInvalidPayload},
		"station that never booted": {&store.Command{OrgId: testOrgId, StationKey: "k7f2.NEW", Action: "Reset", Payload: json.RawMessage(`{"type":"Soft"}`)}, commands.ErrUnknownOcppVersion},
		"version without catalogue": {&store.Command{OrgId: testOrgId, StationKey: testStation, OcppVersion: "ocpp2.0.1", Action: "Reset", Payload: json.RawMessage(`{"type":"Soft"}`)}, commands.ErrUnknownAction},
		"expiry in the past":        {withExpiry(newCommand("Reset", `{"type":"Soft"}`), testStart.Add(-time.Second)), commands.ErrInvalidExpiry},
		"expiry now":                {withExpiry(newCommand("Reset", `{"type":"Soft"}`), testStart), commands.ErrInvalidExpiry},
		"expiry beyond the max ttl": {withExpiry(newCommand("Reset", `{"type":"Soft"}`), testStart.Add(168*time.Hour+time.Second)), commands.ErrInvalidExpiry},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			_, err := f.service.Enqueue(context.Background(), tc.cmd)
			assert.ErrorIs(t, err, tc.want)

			stored, _, err := f.store.ListCommands(context.Background(), store.CommandFilter{})
			require.NoError(t, err)
			assert.Empty(t, stored, "nothing is stored")
			assert.Empty(t, f.emitter.sent(), "nothing reaches the station")
		})
	}

	t.Run("the payload error names the schema problem", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.service.Enqueue(context.Background(), newCommand("Reset", `{"type":"Warm"}`))
		assert.ErrorContains(t, err, "type")
	})

	t.Run("a station with another version", func(t *testing.T) {
		f := newFixture(t)
		require.NoError(t, f.store.SetChargeStationRuntimeDetails(context.Background(), testStation,
			&store.ChargeStationRuntimeDetails{OcppVersion: "2.0.1"}))
		_, err := f.service.Enqueue(context.Background(), newCommand("Reset", `{"type":"Soft"}`))
		assert.ErrorIs(t, err, commands.ErrUnknownAction, "no 2.0.1 actions yet")

		require.NoError(t, f.store.SetChargeStationRuntimeDetails(context.Background(), testStation,
			&store.ChargeStationRuntimeDetails{OcppVersion: "1.5"}))
		_, err = f.service.Enqueue(context.Background(), newCommand("Reset", `{"type":"Soft"}`))
		assert.ErrorIs(t, err, commands.ErrUnknownOcppVersion)
	})
}

func withExpiry(cmd *store.Command, at time.Time) *store.Command {
	cmd.ExpiresAt = at
	return cmd
}

func TestEnqueueSetsTheExpiry(t *testing.T) {
	for name, tc := range map[string]struct {
		action, payload string
		ttl             map[string]time.Duration
		expiresAt       time.Time
		want            time.Duration
	}{
		"default":                    {action: "ClearCache", payload: `{}`, want: time.Hour},
		"catalogue default":          {action: "GetDiagnostics", payload: `{"location":"ftp://x"}`, want: 24 * time.Hour},
		"configured for the action":  {action: "ClearCache", payload: `{}`, ttl: map[string]time.Duration{"ClearCache": 10 * time.Minute}, want: 10 * time.Minute},
		"configured over catalogue":  {action: "Reset", payload: `{"type":"Soft"}`, ttl: map[string]time.Duration{"Reset": 2 * time.Minute}, want: 2 * time.Minute},
		"given by the caller":        {action: "Reset", payload: `{"type":"Soft"}`, expiresAt: testStart.Add(3 * time.Hour), want: 3 * time.Hour},
		"given, exactly the max ttl": {action: "Reset", payload: `{"type":"Soft"}`, expiresAt: testStart.Add(168 * time.Hour), want: 168 * time.Hour},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.conn.set(testStation, false)
			f.service.Config.TTL = tc.ttl
			cmd, err := f.service.Enqueue(context.Background(), withExpiry(newCommand(tc.action, tc.payload), tc.expiresAt))
			require.NoError(t, err)
			assert.Equal(t, testStart.Add(tc.want), cmd.ExpiresAt.UTC())
		})
	}
}

// preprocessorFunc is a Preprocessor.
type preprocessorFunc func(ctx context.Context, cmd *store.Command) error

func (f preprocessorFunc) Preprocess(ctx context.Context, cmd *store.Command) error {
	return f(ctx, cmd)
}

func TestPreprocessorsRunBeforeTheCheck(t *testing.T) {
	f := newFixture(t)
	f.service.NewId = func() string { return "0193b2c4-0000-7000-8000-00000000c002" }
	var seen []string
	f.service.Preprocessors = []commands.Preprocessor{
		preprocessorFunc(func(_ context.Context, cmd *store.Command) error {
			seen = append(seen, cmd.Id+" "+cmd.CreatedAt.UTC().Format(time.RFC3339)+" "+string(cmd.Payload))
			// turns a payload that does not match the schema into one that does
			cmd.Payload = json.RawMessage(`{"type":"Soft"}`)
			return nil
		}),
		preprocessorFunc(func(_ context.Context, cmd *store.Command) error {
			seen = append(seen, string(cmd.Payload))
			cmd.Progress = json.RawMessage(`{"state":"prepared"}`)
			return nil
		}),
	}

	cmd, err := f.service.Enqueue(context.Background(), newCommand("Reset", `{"type":"Later"}`))
	require.NoError(t, err)
	assert.Equal(t, []string{"0193b2c4-0000-7000-8000-00000000c002 2026-11-27T12:00:00Z {\"type\":\"Later\"}", `{"type":"Soft"}`}, seen)
	assert.JSONEq(t, `{"type":"Soft"}`, string(cmd.Payload))
	assert.JSONEq(t, `{"state":"prepared"}`, string(f.get(cmd.Id).Progress))

	t.Run("an error stops the enqueue", func(t *testing.T) {
		f := newFixture(t)
		f.service.Preprocessors = []commands.Preprocessor{preprocessorFunc(func(context.Context, *store.Command) error {
			return errors.New("no such file")
		})}
		_, err := f.service.Enqueue(context.Background(), newCommand("Reset", `{"type":"Soft"}`))
		assert.ErrorContains(t, err, "no such file")
		assert.Empty(t, f.emitter.sent())
	})

	t.Run("its payload is still checked", func(t *testing.T) {
		f := newFixture(t)
		f.service.Preprocessors = []commands.Preprocessor{preprocessorFunc(func(_ context.Context, cmd *store.Command) error {
			cmd.Payload = json.RawMessage(`{"type":"Warm"}`)
			return nil
		})}
		_, err := f.service.Enqueue(context.Background(), newCommand("Reset", `{"type":"Soft"}`))
		assert.ErrorIs(t, err, commands.ErrInvalidPayload)
	})
}

func TestCancel(t *testing.T) {
	t.Run("a queued command", func(t *testing.T) {
		f := newFixture(t)
		f.conn.set(testStation, false)
		cmd := f.enqueue("Reset", `{"type":"Soft"}`)
		f.clock.Step(time.Minute)

		cancelled, err := f.service.Cancel(context.Background(), cmd.Id)
		require.NoError(t, err)
		require.NotNil(t, cancelled)
		assert.Equal(t, store.CommandStatusCancelled, cancelled.Status)
		require.NotNil(t, cancelled.CompletedAt)
		assert.Equal(t, testStart.Add(time.Minute), cancelled.CompletedAt.UTC())
		assert.Equal(t, store.CommandStatusCancelled, f.get(cmd.Id).Status)

		completed := f.completedEvents()
		require.Len(t, completed, 1)
		assertCompletedEvent(t, completed[0], cancelled, false)

		// it is never sent
		f.conn.set(testStation, true)
		require.NoError(t, f.dispatcher.Sweep(context.Background()))
		assert.Empty(t, f.emitter.sent())

		_, err = f.service.Cancel(context.Background(), cmd.Id)
		assert.ErrorIs(t, err, commands.ErrNotCancellable)
		assert.Empty(t, f.completedEvents())
	})

	t.Run("a sent command", func(t *testing.T) {
		f := newFixture(t)
		cmd := f.enqueue("Reset", `{"type":"Soft"}`)
		require.Equal(t, store.CommandStatusSent, cmd.Status)

		_, err := f.service.Cancel(context.Background(), cmd.Id)
		assert.ErrorIs(t, err, commands.ErrNotCancellable)
		assert.Equal(t, store.CommandStatusSent, f.get(cmd.Id).Status)
		assert.Empty(t, f.completedEvents())
	})

	t.Run("an unknown command", func(t *testing.T) {
		f := newFixture(t)
		cancelled, err := f.service.Cancel(context.Background(), "0193b2c4-0000-7000-8000-00000000ffff")
		require.NoError(t, err)
		assert.Nil(t, cancelled)
	})
}

func TestGetAndList(t *testing.T) {
	f := newFixture(t)
	f.conn.set(testStation, false)
	first := f.enqueue("Reset", `{"type":"Soft"}`)
	f.clock.Step(time.Second)
	second := f.enqueue("ClearCache", `{}`)

	got, err := f.service.Get(context.Background(), first.Id)
	require.NoError(t, err)
	assert.Equal(t, first.Id, got.Id)

	got, err = f.service.Get(context.Background(), "0193b2c4-0000-7000-8000-00000000ffff")
	require.NoError(t, err)
	assert.Nil(t, got)

	list, total, err := f.service.List(context.Background(), store.CommandFilter{OrgId: testOrgId, StationKey: testStation})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	require.Len(t, list, 2)
	assert.Equal(t, second.Id, list[0].Id)
	assert.Equal(t, first.Id, list[1].Id)
}

func TestEnqueueWithoutADispatcherOnlyStores(t *testing.T) {
	f := newFixture(t)
	f.service.Dispatcher = nil
	cmd := f.enqueue("Reset", `{"type":"Soft"}`)
	assert.Equal(t, store.CommandStatusQueued, cmd.Status)
	assert.Empty(t, f.emitter.sent())
}
