// SPDX-License-Identifier: Apache-2.0

package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/transport"
)

const testSecret = "0123456789abcdef0123456789abcdef01234567"

func TestRedactPayload(t *testing.T) {
	for name, tc := range map[string]struct {
		payload string
		paths   []string
		want    string
		changed bool
	}{
		"a top-level value": {
			payload: `{"key":"AuthorizationKey","value":"` + testSecret + `"}`, paths: []string{"value"},
			want: `{"key":"AuthorizationKey","value":"***"}`, changed: true,
		},
		"a value in an array": {
			payload: `{"setVariableData":[{"attributeValue":"` + testSecret + `","component":{"name":"SecurityCtrlr"},"variable":{"name":"BasicAuthPassword"}}]}`,
			paths:   []string{"setVariableData.0.attributeValue"},
			want:    `{"setVariableData":[{"attributeValue":"***","component":{"name":"SecurityCtrlr"},"variable":{"name":"BasicAuthPassword"}}]}`, changed: true,
		},
		"an element of an array and an object": {
			payload: `{"a":["x","y"],"b":{"c":{"d":1}}}`, paths: []string{"a.1", "b.c"},
			want: `{"a":["x","***"],"b":{"c":"***"}}`, changed: true,
		},
		"numbers are kept as they are": {
			payload: `{"n":12345678901234567890,"f":0.1,"s":"x"}`, paths: []string{"s"},
			want: `{"f":0.1,"n":12345678901234567890,"s":"***"}`, changed: true,
		},
		"paths that are not there": {
			payload: `{"key":"HeartbeatInterval","value":"60","list":[1]}`, paths: []string{"secret", "value.x", "list.1", "list.-1", "list.a", "key.0"},
			want: `{"key":"HeartbeatInterval","value":"60","list":[1]}`,
		},
		"redacted already": {
			payload: `{"key":"AuthorizationKey","value":"***"}`, paths: []string{"value"},
			want: `{"key":"AuthorizationKey","value":"***"}`,
		},
		"no paths": {
			payload: `{"value":"` + testSecret + `"}`,
			want:    `{"value":"` + testSecret + `"}`,
		},
		"not a JSON document": {
			payload: `{"value":"` + testSecret, paths: []string{"value"},
			want: `"***"`, changed: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, changed := commands.RedactPayload(json.RawMessage(tc.payload), tc.paths)
			assert.JSONEq(t, tc.want, string(got))
			assert.Equal(t, tc.changed, changed)
			if tc.payload != tc.want {
				assert.NotContains(t, string(got), testSecret)
			}
		})
	}
}

// enqueueSecret queues a change of the AuthorizationKey of the test station whose value
// is a secret.
func (f *fixture) enqueueSecret() *store.Command {
	f.t.Helper()
	cmd, err := f.service.Enqueue(context.Background(), &store.Command{
		OrgId:       testOrgId,
		StationKey:  testStation,
		Action:      "ChangeConfiguration",
		Payload:     json.RawMessage(`{"key":"AuthorizationKey","value":"` + testSecret + `"}`),
		RedactPaths: []string{"value"},
		Actor:       ptr("user:alice"),
	})
	require.NoError(f.t, err)
	return cmd
}

// assertNoSecret checks that the stored command and the events stored so far do not
// have the secret.
func (f *fixture) assertNoSecret(id string) {
	f.t.Helper()
	stored := f.get(id)
	assert.JSONEq(f.t, `{"key":"AuthorizationKey","value":"***"}`, string(stored.Payload))
	assert.Equal(f.t, []string{"value"}, stored.RedactPaths)
	for _, ev := range f.events.Take(f.t) {
		assert.NotContains(f.t, string(ev.Data), testSecret, ev.Type)
	}
}

func TestTheSecretOfACommandIsSentAndThenReplaced(t *testing.T) {
	f := newFixture(t)
	cmd := f.enqueueSecret()

	sent := f.waitSent(1)
	assert.JSONEq(t, `{"key":"AuthorizationKey","value":"`+testSecret+`"}`, string(sent[0].msg.RequestPayload),
		"the station gets the secret")
	f.assertNoSecret(cmd.Id)

	require.True(t, f.dispatcher.ObserveCallResult(context.Background(), testStation, result(cmd, `{"status":"Accepted"}`)))
	assert.Equal(t, store.CommandStatusAccepted, f.get(cmd.Id).Status)
	f.assertNoSecret(cmd.Id)
}

func TestTheSecretOfACommandThatIsNotSentIsReplacedWhenItEnds(t *testing.T) {
	t.Run("expired in the queue", func(t *testing.T) {
		f := newFixture(t)
		f.conn.set(testStation, false)
		cmd := f.enqueueSecret()
		assert.Contains(t, string(f.get(cmd.Id).Payload), testSecret, "kept until it is sent")

		f.clock.Step(2 * time.Hour)
		require.NoError(t, f.dispatcher.Sweep(context.Background()))
		assert.Equal(t, store.CommandStatusTimeout, f.get(cmd.Id).Status)
		f.assertNoSecret(cmd.Id)
	})

	t.Run("cancelled", func(t *testing.T) {
		f := newFixture(t)
		f.conn.set(testStation, false)
		cmd := f.enqueueSecret()

		_, err := f.service.Cancel(context.Background(), cmd.Id)
		require.NoError(t, err)
		assert.Equal(t, store.CommandStatusCancelled, f.get(cmd.Id).Status)
		f.assertNoSecret(cmd.Id)
	})

	t.Run("a send that fails keeps it for the next one", func(t *testing.T) {
		f := newFixture(t)
		f.emitter.setErr(errors.New("the broker is down"))
		cmd := f.enqueueSecret()
		assert.Equal(t, store.CommandStatusQueued, f.get(cmd.Id).Status)
		assert.Contains(t, string(f.get(cmd.Id).Payload), testSecret)

		f.emitter.setErr(nil)
		require.NoError(t, f.dispatcher.Kick(context.Background(), testStation))
		sent := f.waitSent(1)
		assert.Contains(t, string(sent[0].msg.RequestPayload), testSecret)
		f.assertNoSecret(cmd.Id)
	})
}

// completions records the commands that a completion hook was told about.
type completions struct {
	mu    sync.Mutex
	told  []string
	panic bool
}

func (c *completions) AfterCompletion(_ context.Context, cmd *store.Command) {
	c.mu.Lock()
	c.told = append(c.told, cmd.Action+" "+string(cmd.Status))
	c.mu.Unlock()
	if c.panic {
		panic("hook broke")
	}
}

func (c *completions) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.told...)
}

func TestCompletionHooksAreToldEveryFinalStatus(t *testing.T) {
	ctx := context.Background()
	newHooked := func(t *testing.T) (*fixture, *completions, *completions) {
		f := newFixture(t)
		broken, hook := &completions{panic: true}, &completions{}
		f.dispatcher.AfterCompletion = []commands.CompletionHook{broken, hook}
		return f, broken, hook
	}

	t.Run("answers of the station", func(t *testing.T) {
		f, broken, hook := newHooked(t)
		accepted := f.enqueue("Reset", `{"type":"Soft"}`)
		require.True(t, f.dispatcher.ObserveCallResult(ctx, testStation, result(accepted, `{"status":"Accepted"}`)))
		rejected := f.enqueue("Reset", `{"type":"Hard"}`)
		f.waitStatus(rejected.Id, store.CommandStatusSent)
		require.True(t, f.dispatcher.ObserveCallResult(ctx, testStation, result(rejected, `{"status":"Rejected"}`)))
		failed := f.enqueue("ClearCache", `{}`)
		f.waitStatus(failed.Id, store.CommandStatusSent)
		require.True(t, f.dispatcher.ObserveCallError(ctx, testStation, callError(failed, transport.ErrorNotImplemented, "no")))

		assert.Equal(t, []string{"Reset accepted", "Reset rejected", "ClearCache failed"}, hook.all())
		assert.Equal(t, hook.all(), broken.all(), "a hook that panics does not stop the next one")

		// an answer that changes nothing tells nobody
		assert.True(t, f.dispatcher.ObserveCallResult(ctx, testStation, result(accepted, `{"status":"Accepted"}`)))
		assert.Len(t, hook.all(), 3)
	})

	t.Run("a timeout and the answer that comes late", func(t *testing.T) {
		f, _, hook := newHooked(t)
		cmd := f.enqueue("Reset", `{"type":"Soft"}`)
		f.clock.Step(31 * time.Second)
		require.NoError(t, f.dispatcher.Sweep(ctx))
		assert.Equal(t, []string{"Reset timeout"}, hook.all())

		require.True(t, f.dispatcher.ObserveCallResult(ctx, testStation, result(cmd, `{"status":"Accepted"}`)))
		assert.Equal(t, []string{"Reset timeout", "Reset accepted"}, hook.all())
	})

	t.Run("expiry in the queue and cancellation", func(t *testing.T) {
		f, _, hook := newHooked(t)
		f.conn.set(testStation, false)
		expired := f.enqueue("Reset", `{"type":"Soft"}`)
		cancelled := f.enqueue("ClearCache", `{}`)
		_, err := f.service.Cancel(ctx, cancelled.Id)
		require.NoError(t, err)
		f.clock.Step(10 * time.Minute)
		require.NoError(t, f.dispatcher.Sweep(ctx))

		assert.Equal(t, store.CommandStatusTimeout, f.get(expired.Id).Status)
		assert.Equal(t, []string{"ClearCache cancelled", "Reset timeout"}, hook.all())
	})
}
