// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	clockTest "k8s.io/utils/clock/testing"
)

// stationOfAWithoutBoot is a charge station of A that never booted: its OCPP version is
// not known.
const stationOfAWithoutBoot = "aaaa.CP002"

// sentCalls records the calls that the dispatcher sends to the charge stations.
type sentCalls struct {
	mu    sync.Mutex
	calls []*transport.Message
}

func (s *sentCalls) Emit(_ context.Context, _ transport.OcppVersion, _ string, msg *transport.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, msg)
	return nil
}

func (s *sentCalls) sent() []*transport.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*transport.Message(nil), s.calls...)
}

// connectedStations says which charge stations are connected.
type connectedStations struct {
	mu        sync.Mutex
	connected map[string]bool
}

func (c *connectedStations) IsConnected(_ context.Context, stationKey string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected[stationKey], nil
}

func (c *connectedStations) set(stationKey string, connected bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected[stationKey] = connected
}

// commandsFixture is the multi mode manager of newMultiTenant with a command queue: the
// charge stations of A and B are OCPP 1.6 stations that booted, and only the charge
// station of A is connected.
type commandsFixture struct {
	*multiTenant
	calls      *sentCalls
	connected  *connectedStations
	dispatcher *commands.Dispatcher
	service    *commands.Service
	router     *chi.Mux
}

func newCommandsFixture(t *testing.T) *commandsFixture {
	t.Helper()
	ctx := context.Background()
	m := newMultiTenant(t)
	require.NoError(t, m.engine.SetChargeStationAuth(ctx, stationOfAWithoutBoot, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgA}))
	for _, station := range []string{stationOfA, stationOfB} {
		require.NoError(t, m.engine.SetChargeStationRuntimeDetails(ctx, station, &store.ChargeStationRuntimeDetails{OcppVersion: "1.6"}))
	}

	domainEvents := &events.OutboxEmitter{Store: m.engine, Orgs: m.scoper.Resolver, Clock: m.clock, Product: events.Product}
	config := commands.Config{
		ResponseTimeout: 30 * time.Second,
		SweepInterval:   5 * time.Second,
		DefaultTTL:      time.Hour,
		MaxTTL:          168 * time.Hour,
		Retention:       90 * 24 * time.Hour,
	}
	calls := &sentCalls{}
	connected := &connectedStations{connected: map[string]bool{stationOfA: true}}
	dispatcher := &commands.Dispatcher{
		Store:        m.engine,
		Tx:           m.engine,
		Events:       domainEvents,
		Senders:      map[transport.OcppVersion]handlers.RawCallMaker{transport.OcppVersion16: ocpp16.NewCallMaker(calls)},
		Connectivity: connected,
		SchemaFS:     schemas.OcppSchemas,
		// the dispatcher only waits after a station connected, which these tests do not
		// report
		Clock:  clockTest.NewFakeClock(m.clock.Now()),
		Config: config,
	}
	service := &commands.Service{
		Store:          m.engine,
		Tx:             m.engine,
		Events:         domainEvents,
		RuntimeDetails: m.engine,
		Dispatcher:     dispatcher,
		SchemaFS:       schemas.OcppSchemas,
		Clock:          m.clock,
		Config:         config,
	}
	srv, err := api.NewServer(m.engine, m.clock, nil, domainEvents, api.WithScoper(m.scoper), api.WithCommands(service))
	require.NoError(t, err)
	router := routerOf(t, srv, m.scoper, func(r *http.Request) *tenancy.Principal {
		p, _ := tenancy.FromContext(r.Context())
		return p
	})
	return &commandsFixture{multiTenant: m, calls: calls, connected: connected, dispatcher: dispatcher, service: service, router: router}
}

// commandCaller is a person of A with every permission who uses the token "jti-a".
func commandCaller() *tenancy.Principal {
	p := callerOf(orgA, tenancy.OrgScopeSelf)
	p.Jti = "jti-a"
	return p
}

// enqueue sends a command for the charge station through the API and returns the
// answer.
func (f *commandsFixture) enqueue(p *tenancy.Principal, csId, query, body string) *httptest.ResponseRecorder {
	return serve(f.router, as(p, http.MethodPost, "/cs/"+csId+"/commands"+query, body))
}

// mustEnqueue sends a command for the charge station through the API and returns the
// command that it answered.
func (f *commandsFixture) mustEnqueue(t *testing.T, csId, body string) api.Command {
	t.Helper()
	rr := f.enqueue(commandCaller(), csId, "", body)
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	return decodeCommand(t, rr)
}

// stored returns the stored command.
func (f *commandsFixture) stored(t *testing.T, id string) *store.Command {
	t.Helper()
	cmd, err := f.engine.GetCommand(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, cmd)
	return cmd
}

// storedCommands returns how many commands are stored.
func (f *commandsFixture) storedCommands(t *testing.T) int {
	t.Helper()
	_, total, err := f.engine.ListCommands(context.Background(), store.CommandFilter{})
	require.NoError(t, err)
	return total
}

func decodeCommand(t *testing.T, rr *httptest.ResponseRecorder) api.Command {
	t.Helper()
	var cmd api.Command
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &cmd), rr.Body.String())
	return cmd
}

const resetSoft = `{"action":"Reset","payload":{"type":"Soft"}}`

func TestEnqueueCommandSendsTheCommandToTheChargeStation(t *testing.T) {
	f := newCommandsFixture(t)
	now := f.clock.Now()

	rr := f.enqueue(commandCaller(), stationOfA, "", resetSoft)
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	got := decodeCommand(t, rr)
	id := got.Id.String()
	assert.Equal(t, "/api/v0/commands/"+id, rr.Header().Get("Location"))

	// sent at once, with the command id as the OCPP message id
	sent := f.calls.sent()
	require.Len(t, sent, 1)
	assert.Equal(t, id, sent[0].MessageId)
	assert.Equal(t, "Reset", sent[0].Action)
	assert.JSONEq(t, `{"type":"Soft"}`, string(sent[0].RequestPayload))

	// the answer is the command as it is after the attempt to send it; the jti is not
	// shown
	assert.JSONEq(t, `{
		"id": "`+id+`",
		"chargeStationId": "CP001",
		"ocppVersion": "ocpp1.6",
		"action": "Reset",
		"payload": {"type": "Soft"},
		"status": "sent",
		"result": null,
		"errorCode": null,
		"errorDescription": null,
		"progress": null,
		"progressAt": null,
		"actor": "user:test",
		"createdAt": "`+now.Format(time.RFC3339)+`",
		"sentAt": "`+now.Format(time.RFC3339)+`",
		"completedAt": null,
		"expiresAt": "`+now.Add(5*time.Minute).Format(time.RFC3339)+`"
	}`, rr.Body.String())

	// who sent it is stored with it
	stored := f.stored(t, id)
	assert.Equal(t, orgA, stored.OrgId)
	assert.Equal(t, stationOfA, stored.StationKey)
	require.NotNil(t, stored.Actor)
	assert.Equal(t, "user:test", *stored.Actor)
	require.NotNil(t, stored.Jti)
	assert.Equal(t, "jti-a", *stored.Jti)
}

func TestEnqueueCommandNamesWhoActsForTheCaller(t *testing.T) {
	f := newCommandsFixture(t)
	p := &tenancy.Principal{Subject: "svc:support-tool", Actor: "user:staff-123", OrgId: orgA, OrgScope: tenancy.OrgScopeSelf,
		Perms: []string{tenancy.PermConnectorCommand}}

	rr := f.enqueue(p, stationOfA, "", resetSoft)
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	got := decodeCommand(t, rr)
	require.NotNil(t, got.Actor)
	assert.Equal(t, "svc:support-tool;act=user:staff-123", *got.Actor)

	stored := f.stored(t, got.Id.String())
	require.NotNil(t, stored.Actor)
	assert.Equal(t, "svc:support-tool;act=user:staff-123", *stored.Actor)
	// a token without a jti
	assert.Nil(t, stored.Jti)
}

func TestEnqueueCommandForADisconnectedChargeStationQueuesIt(t *testing.T) {
	f := newCommandsFixture(t)
	f.connected.set(stationOfA, false)

	got := f.mustEnqueue(t, stationOfA, resetSoft)
	assert.Equal(t, api.CommandStatusQueued, got.Status)
	assert.Nil(t, got.SentAt)
	assert.Empty(t, f.calls.sent())
}

func TestEnqueueCommandRefusesWhatCannotBeSent(t *testing.T) {
	f := newCommandsFixture(t)
	now := f.clock.Now()
	readOnly := withPerms(commandCaller(), tenancy.PermStationRead)

	tests := map[string]struct {
		caller     *tenancy.Principal
		csId       string
		query      string
		body       string
		wantStatus int
		wantError  string // the start of the error
	}{
		"a payload that does not match the schema": {commandCaller(), stationOfA, "", `{"action":"Reset","payload":{"type":"Warm"}}`, http.StatusBadRequest, "invalid_payload: payload does not match OCPP schema: "},
		"a payload without a required field":       {commandCaller(), stationOfA, "", `{"action":"UnlockConnector","payload":{}}`, http.StatusBadRequest, "invalid_payload: "},
		"an unknown action":                        {commandCaller(), stationOfA, "", `{"action":"Explode","payload":{}}`, http.StatusBadRequest, "unknown_action: "},
		"an action of another OCPP version":        {commandCaller(), stationOfA, "", `{"action":"SetVariables","payload":{}}`, http.StatusBadRequest, "unknown_action: "},
		"no action":                                {commandCaller(), stationOfA, "", `{"payload":{}}`, http.StatusBadRequest, "action is required"},
		"no payload":                               {commandCaller(), stationOfA, "", `{"action":"ClearCache"}`, http.StatusBadRequest, "payload must be a JSON object"},
		"a payload that is null":                   {commandCaller(), stationOfA, "", `{"action":"ClearCache","payload":null}`, http.StatusBadRequest, "payload must be a JSON object"},
		"a payload that is not an object":          {commandCaller(), stationOfA, "", `{"action":"ClearCache","payload":["x"]}`, http.StatusBadRequest, "payload must be a JSON object"},
		"no body":                                  {commandCaller(), stationOfA, "", ``, http.StatusBadRequest, ""},
		"a body that is not JSON":                  {commandCaller(), stationOfA, "", `Reset`, http.StatusBadRequest, ""},
		"an expiry and a time to live":             {commandCaller(), stationOfA, "", `{"action":"ClearCache","payload":{},"ttlSeconds":60,"expiresAt":"` + now.Add(time.Hour).Format(time.RFC3339) + `"}`, http.StatusBadRequest, "expiresAt and ttlSeconds cannot be given together"},
		"a time to live of 0":                      {commandCaller(), stationOfA, "", `{"action":"ClearCache","payload":{},"ttlSeconds":0}`, http.StatusBadRequest, "ttlSeconds must be at least 1"},
		"a time to live beyond the maximum":        {commandCaller(), stationOfA, "", `{"action":"ClearCache","payload":{},"ttlSeconds":604801}`, http.StatusBadRequest, "invalid_expiry: "},
		"a time to live beyond a duration":         {commandCaller(), stationOfA, "", `{"action":"ClearCache","payload":{},"ttlSeconds":9223372036854775807}`, http.StatusBadRequest, "invalid_expiry: "},
		"an expiry in the past":                    {commandCaller(), stationOfA, "", `{"action":"ClearCache","payload":{},"expiresAt":"` + now.Add(-time.Second).Format(time.RFC3339) + `"}`, http.StatusBadRequest, "invalid_expiry: "},
		"a negative wait":                          {commandCaller(), stationOfA, "?waitSeconds=-1", resetSoft, http.StatusBadRequest, "waitSeconds must be between 0 and 30"},
		"a wait beyond 30 seconds":                 {commandCaller(), stationOfA, "?waitSeconds=31", resetSoft, http.StatusBadRequest, "waitSeconds must be between 0 and 30"},
		"a charge station that never booted":       {commandCaller(), stationOfAWithoutBoot, "", resetSoft, http.StatusConflict, "station_version_unknown: "},
		"a charge station of another tenant":       {commandCaller(), stationOfB, "", resetSoft, http.StatusNotFound, ""},
		"a charge station that does not exist":     {commandCaller(), "aaaa.NONE", "", resetSoft, http.StatusNotFound, ""},
		"a caller without the permission":          {readOnly, stationOfA, "", resetSoft, http.StatusForbidden, "missing permission cpms.connector.command"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// the request does not wait for anything, whatever it asks for
			req := as(test.caller, http.MethodPost, "/cs/"+test.csId+"/commands"+test.query, test.body)
			ctx, cancel := context.WithTimeout(req.Context(), 200*time.Millisecond)
			defer cancel()
			rr := serve(f.router, req.WithContext(ctx))
			require.Equal(t, test.wantStatus, rr.Code, rr.Body.String())

			var status api.Status
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &status), rr.Body.String())
			if test.wantStatus == http.StatusNotFound {
				assert.JSONEq(t, notFoundBody, rr.Body.String())
			} else {
				require.NotNil(t, status.Error, rr.Body.String())
				assert.True(t, strings.HasPrefix(*status.Error, test.wantError), *status.Error)
			}
		})
	}

	t.Run("the schema error is in the answer", func(t *testing.T) {
		rr := f.enqueue(commandCaller(), stationOfA, "", `{"action":"Reset","payload":{"type":"Warm"}}`)
		require.Equal(t, http.StatusBadRequest, rr.Code)
		assert.Contains(t, rr.Body.String(), "/type")
	})

	// nothing was stored and nothing reached a charge station
	assert.Zero(t, f.storedCommands(t))
	assert.Empty(t, f.calls.sent())
}

func TestEnqueueCommandExpiry(t *testing.T) {
	f := newCommandsFixture(t)
	f.connected.set(stationOfA, false)
	now := f.clock.Now()
	expiresAt := now.Add(2 * time.Hour)

	tests := map[string]struct {
		body string
		want time.Time
	}{
		"the default of the action": {resetSoft, now.Add(5 * time.Minute)},
		"the default of the queue":  {`{"action":"ClearCache","payload":{}}`, now.Add(time.Hour)},
		"a time to live":            {`{"action":"Reset","payload":{"type":"Hard"},"ttlSeconds":90}`, now.Add(90 * time.Second)},
		"an expiry":                 {`{"action":"Reset","payload":{"type":"Hard"},"expiresAt":"` + expiresAt.Format(time.RFC3339) + `"}`, expiresAt},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got := f.mustEnqueue(t, stationOfA, test.body)
			assert.True(t, test.want.Equal(got.ExpiresAt), "%s, want %s", got.ExpiresAt, test.want)
			assert.True(t, test.want.Equal(f.stored(t, got.Id.String()).ExpiresAt))
		})
	}
}

// observeResult answers the call of a command as the charge station would.
func (f *commandsFixture) observeResult(t *testing.T, call *transport.Message, response string) {
	t.Helper()
	tracked := f.dispatcher.ObserveCallResult(context.Background(), stationOfA, &transport.Message{
		MessageType:     transport.MessageTypeCallResult,
		Action:          call.Action,
		MessageId:       call.MessageId,
		RequestPayload:  call.RequestPayload,
		ResponsePayload: json.RawMessage(response),
	})
	require.True(t, tracked)
}

func TestEnqueueCommandWaitsForTheAnswer(t *testing.T) {
	f := newCommandsFixture(t)

	answered := make(chan *httptest.ResponseRecorder, 1)
	started := time.Now()
	go func() { answered <- f.enqueue(commandCaller(), stationOfA, "?waitSeconds=2", resetSoft) }()

	var calls []*transport.Message
	require.Eventually(t, func() bool { calls = f.calls.sent(); return len(calls) == 1 }, 2*time.Second, 5*time.Millisecond)
	f.observeResult(t, calls[0], `{"status":"Accepted"}`)

	var rr *httptest.ResponseRecorder
	select {
	case rr = <-answered:
	case <-time.After(5 * time.Second):
		t.Fatal("no answer")
	}
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Less(t, time.Since(started), 2*time.Second, "answered as soon as the command completed")
	got := decodeCommand(t, rr)
	assert.Equal(t, calls[0].MessageId, got.Id.String())
	assert.Equal(t, api.CommandStatusAccepted, got.Status)
	require.NotNil(t, got.Result)
	assert.JSONEq(t, `{"status":"Accepted"}`, string(*got.Result))
	assert.NotNil(t, got.CompletedAt)
	assert.Equal(t, "/api/v0/commands/"+got.Id.String(), rr.Header().Get("Location"))
}

func TestEnqueueCommandWaitsNoLongerThanAsked(t *testing.T) {
	f := newCommandsFixture(t)

	t.Run("no answer from the charge station", func(t *testing.T) {
		started := time.Now()
		rr := f.enqueue(commandCaller(), stationOfA, "?waitSeconds=2", resetSoft)
		elapsed := time.Since(started)

		require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
		assert.GreaterOrEqual(t, elapsed, 2*time.Second)
		assert.Less(t, elapsed, 3*time.Second)
		got := decodeCommand(t, rr)
		assert.Equal(t, api.CommandStatusSent, got.Status)
		assert.Equal(t, "/api/v0/commands/"+got.Id.String(), rr.Header().Get("Location"))
	})

	t.Run("a caller that goes away", func(t *testing.T) {
		f.connected.set(stationOfA, false)
		req := as(commandCaller(), http.MethodPost, "/cs/"+stationOfA+"/commands?waitSeconds=30", resetSoft)
		ctx, cancel := context.WithTimeout(req.Context(), 300*time.Millisecond)
		defer cancel()
		started := time.Now()
		rr := serve(f.router, req.WithContext(ctx))

		require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
		assert.Less(t, time.Since(started), 2*time.Second)
		assert.Equal(t, api.CommandStatusQueued, decodeCommand(t, rr).Status)
	})

}

// failingResolver fails to build the payload of every command that is sent.
type failingResolver struct{}

func (failingResolver) Resolve(context.Context, *store.Command) (json.RawMessage, error) {
	return nil, errors.New("no payload")
}

func TestEnqueueCommandThatIsFinalAtOnce(t *testing.T) {
	f := newCommandsFixture(t)
	// the command fails when it is sent
	f.dispatcher.Resolvers = []commands.PayloadResolver{failingResolver{}}

	// without a wait the answer is 202, whatever the status
	rr := f.enqueue(commandCaller(), stationOfA, "", resetSoft)
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	assert.Equal(t, api.CommandStatusFailed, decodeCommand(t, rr).Status)

	// with a wait it is 200 at once
	started := time.Now()
	rr = f.enqueue(commandCaller(), stationOfA, "?waitSeconds=10", resetSoft)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Less(t, time.Since(started), time.Second)
	got := decodeCommand(t, rr)
	assert.Equal(t, api.CommandStatusFailed, got.Status)
	require.NotNil(t, got.ErrorCode)
	assert.Equal(t, commands.ErrorCodePayloadResolution, *got.ErrorCode)
	assert.Equal(t, "/api/v0/commands/"+got.Id.String(), rr.Header().Get("Location"))
}

func TestGetCommand(t *testing.T) {
	f := newCommandsFixture(t)
	cmd := f.mustEnqueue(t, stationOfA, resetSoft)
	path := "/commands/" + cmd.Id.String()

	t.Run("of the caller", func(t *testing.T) {
		rr := serve(f.router, as(commandCaller(), http.MethodGet, path, ""))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, cmd, decodeCommand(t, rr))
	})

	t.Run("with nothing but the permission to read", func(t *testing.T) {
		rr := serve(f.router, as(withPerms(commandCaller(), tenancy.PermStationRead), http.MethodGet, path, ""))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	})

	t.Run("of a tenant below the caller", func(t *testing.T) {
		rr := serve(f.router, as(callerOf(orgPartner, tenancy.OrgScopeSubtree), http.MethodGet, path, ""))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	})

	t.Run("of another tenant", func(t *testing.T) {
		other := serve(f.router, as(callerOf(orgB, tenancy.OrgScopeSelf), http.MethodGet, path, ""))
		unknown := serve(f.router, as(callerOf(orgB, tenancy.OrgScopeSelf), http.MethodGet, "/commands/0193b2c4-0000-7000-8000-000000000000", ""))
		require.Equal(t, http.StatusNotFound, other.Code, other.Body.String())
		assert.JSONEq(t, notFoundBody, other.Body.String())
		// answered exactly like a command that does not exist
		assert.Equal(t, unknown.Code, other.Code)
		assert.Equal(t, unknown.Body.String(), other.Body.String())
		assert.Equal(t, unknown.Header(), other.Header())
	})

	t.Run("without the permission", func(t *testing.T) {
		rr := serve(f.router, as(withPerms(commandCaller(), tenancy.PermConnectorCommand), http.MethodGet, path, ""))
		require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Forbidden","error":"missing permission cpms.station.read"}`, rr.Body.String())
	})

	t.Run("an id that is not a UUID", func(t *testing.T) {
		rr := serve(f.router, as(commandCaller(), http.MethodGet, "/commands/not-a-uuid", ""))
		assert.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
	})
}

func TestListChargeStationCommands(t *testing.T) {
	f := newCommandsFixture(t)
	f.connected.set(stationOfA, false)
	start := f.clock.Now()

	// one command a second, the oldest first
	var ids []string
	for _, body := range []string{
		resetSoft,
		`{"action":"ClearCache","payload":{}}`,
		`{"action":"Reset","payload":{"type":"Hard"}}`,
		`{"action":"UnlockConnector","payload":{"connectorId":1}}`,
		resetSoft,
	} {
		ids = append(ids, f.mustEnqueue(t, stationOfA, body).Id.String())
		f.clock.SetTime(f.clock.Now().Add(time.Second))
	}
	_, err := f.service.Cancel(context.Background(), ids[1])
	require.NoError(t, err)
	// a command of another charge station
	rr := f.enqueue(callerOf(orgB, tenancy.OrgScopeSelf), stationOfB, "", resetSoft)
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())

	list := func(t *testing.T, query string) (ids []string, total int) {
		t.Helper()
		rr := serve(f.router, as(commandCaller(), http.MethodGet, "/cs/"+stationOfA+"/commands"+query, ""))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var got api.CommandList
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
		ids = []string{}
		for _, cmd := range got.Commands {
			assert.Equal(t, "CP001", cmd.ChargeStationId)
			ids = append(ids, cmd.Id.String())
		}
		return ids, got.Total
	}

	tests := map[string]struct {
		query     string
		wantIds   []string
		wantTotal int
	}{
		"all, the newest first": {"", []string{ids[4], ids[3], ids[2], ids[1], ids[0]}, 5},
		"a status":              {"?status=cancelled", []string{ids[1]}, 1},
		"two statuses":          {"?status=cancelled&status=queued", []string{ids[4], ids[3], ids[2], ids[1], ids[0]}, 5},
		"an action":             {"?action=Reset", []string{ids[4], ids[2], ids[0]}, 3},
		"two actions":           {"?action=Reset&action=ClearCache", []string{ids[4], ids[2], ids[1], ids[0]}, 4},
		"a status and actions":  {"?status=queued&action=Reset&action=ClearCache", []string{ids[4], ids[2], ids[0]}, 3},
		"a page":                {"?limit=2&offset=1", []string{ids[3], ids[2]}, 5},
		"the last page":         {"?limit=2&offset=4", []string{ids[0]}, 5},
		"beyond the last page":  {"?offset=10", []string{}, 5},
		"created after":         {"?createdAfter=" + start.Add(2*time.Second).Format(time.RFC3339), []string{ids[4], ids[3]}, 2},
		"created before":        {"?createdBefore=" + start.Add(2*time.Second).Format(time.RFC3339), []string{ids[1], ids[0]}, 2},
		"nothing matches":       {"?status=accepted", []string{}, 0},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			gotIds, total := list(t, test.query)
			assert.Equal(t, test.wantIds, gotIds)
			assert.Equal(t, test.wantTotal, total)
		})
	}

	t.Run("filters that are not valid", func(t *testing.T) {
		for _, query := range []string{"?limit=0", "?limit=501", "?offset=-1", "?status=done", "?limit=x", "?createdAfter=yesterday"} {
			rr := serve(f.router, as(commandCaller(), http.MethodGet, "/cs/"+stationOfA+"/commands"+query, ""))
			assert.Equal(t, http.StatusBadRequest, rr.Code, "%s: %s", query, rr.Body.String())
		}
	})

	t.Run("the charge station of another tenant", func(t *testing.T) {
		rr := serve(f.router, as(callerOf(orgB, tenancy.OrgScopeSelf), http.MethodGet, "/cs/"+stationOfA+"/commands", ""))
		require.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())
		assert.JSONEq(t, notFoundBody, rr.Body.String())
	})

	t.Run("without the permission", func(t *testing.T) {
		rr := serve(f.router, as(withPerms(commandCaller(), tenancy.PermConnectorCommand), http.MethodGet, "/cs/"+stationOfA+"/commands", ""))
		require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	})
}

func TestCancelCommand(t *testing.T) {
	f := newCommandsFixture(t)
	f.connected.set(stationOfA, false)
	cancel := func(p *tenancy.Principal, id string) *httptest.ResponseRecorder {
		return serve(f.router, as(p, http.MethodPost, "/commands/"+id+":cancel", ""))
	}

	t.Run("a queued command", func(t *testing.T) {
		cmd := f.mustEnqueue(t, stationOfA, resetSoft)
		rr := cancel(commandCaller(), cmd.Id.String())
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		got := decodeCommand(t, rr)
		assert.Equal(t, api.CommandStatusCancelled, got.Status)
		require.NotNil(t, got.CompletedAt)
		assert.Equal(t, store.CommandStatusCancelled, f.stored(t, cmd.Id.String()).Status)

		// a second time: the command as it is
		rr = cancel(commandCaller(), cmd.Id.String())
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.Equal(t, got, decodeCommand(t, rr))
	})

	t.Run("a sent command", func(t *testing.T) {
		f.connected.set(stationOfA, true)
		defer f.connected.set(stationOfA, false)
		cmd := f.mustEnqueue(t, stationOfA, resetSoft)
		require.Equal(t, api.CommandStatusSent, cmd.Status)

		rr := cancel(commandCaller(), cmd.Id.String())
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		got := decodeCommand(t, rr)
		assert.Equal(t, cmd, got)
		assert.Equal(t, store.CommandStatusSent, f.stored(t, cmd.Id.String()).Status)
	})

	t.Run("a command of another tenant", func(t *testing.T) {
		cmd := f.mustEnqueue(t, stationOfA, resetSoft)
		other := cancel(callerOf(orgB, tenancy.OrgScopeSelf), cmd.Id.String())
		unknown := cancel(callerOf(orgB, tenancy.OrgScopeSelf), "0193b2c4-0000-7000-8000-000000000000")
		require.Equal(t, http.StatusNotFound, other.Code, other.Body.String())
		assert.JSONEq(t, notFoundBody, other.Body.String())
		assert.Equal(t, unknown.Body.String(), other.Body.String())
		assert.Equal(t, store.CommandStatusQueued, f.stored(t, cmd.Id.String()).Status)
	})

	t.Run("without the permission", func(t *testing.T) {
		cmd := f.mustEnqueue(t, stationOfA, resetSoft)
		rr := cancel(withPerms(commandCaller(), tenancy.PermStationRead), cmd.Id.String())
		require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Forbidden","error":"missing permission cpms.connector.command"}`, rr.Body.String())
		assert.Equal(t, store.CommandStatusQueued, f.stored(t, cmd.Id.String()).Status)
	})

	t.Run("the action is not part of the id of another operation", func(t *testing.T) {
		cmd := f.mustEnqueue(t, stationOfA, resetSoft)
		for _, tc := range []struct{ method, path string }{
			{http.MethodGet, "/commands/" + cmd.Id.String() + ":cancel"},
			{http.MethodPost, "/commands/" + cmd.Id.String()},
			{http.MethodPost, "/commands/" + cmd.Id.String() + ":cancel/"},
			{http.MethodPost, "/commands/" + cmd.Id.String() + ":archive"},
		} {
			rr := serve(f.router, as(commandCaller(), tc.method, tc.path, ""))
			assert.Contains(t, []int{http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed}, rr.Code, "%s %s", tc.method, tc.path)
		}
		assert.Equal(t, store.CommandStatusQueued, f.stored(t, cmd.Id.String()).Status)
	})
}

func TestCommandsOfASuspendedTenant(t *testing.T) {
	// Commands to a charge station are not writes: a suspended tenant can still send
	// and cancel them (never block charging), and read them.
	f := newCommandsFixture(t)
	f.connected.set(stationOfA, false)
	queued := f.mustEnqueue(t, stationOfA, resetSoft)
	f.setTenantStatus(t, orgA, store.TenantStatusSuspended)

	rr := f.enqueue(commandCaller(), stationOfA, "", `{"action":"UnlockConnector","payload":{"connectorId":1}}`)
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	rr = serve(f.router, as(commandCaller(), http.MethodPost, "/commands/"+queued.Id.String()+":cancel", ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	rr = serve(f.router, as(commandCaller(), http.MethodGet, "/commands/"+queued.Id.String(), ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	rr = serve(f.router, as(commandCaller(), http.MethodGet, "/cs/"+stationOfA+"/commands", ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
}

func TestCommandOperationsWithoutACommandQueue(t *testing.T) {
	// a storage engine that does not keep commands (Firestore)
	m := newMultiTenant(t)
	r := m.router(t)
	const notImplementedBody = `{"status":"Not Implemented","error":"the storage engine of the manager does not keep commands"}`

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/cs/" + stationOfA + "/commands", resetSoft},
		{http.MethodPost, "/cs/" + stationOfA + "/commands?waitSeconds=5", resetSoft},
		{http.MethodGet, "/cs/" + stationOfA + "/commands", ""},
		{http.MethodGet, "/commands/0193b2c4-0000-7000-8000-000000000000", ""},
		{http.MethodPost, "/commands/0193b2c4-0000-7000-8000-000000000000:cancel", ""},
	} {
		rr := serve(r, as(callerOf(orgA, tenancy.OrgScopeSelf), tc.method, tc.path, tc.body))
		require.Equal(t, http.StatusNotImplemented, rr.Code, "%s %s: %s", tc.method, tc.path, rr.Body.String())
		assert.JSONEq(t, notImplementedBody, rr.Body.String())
	}

	// the charge station of another tenant is still not found
	rr := serve(r, as(callerOf(orgB, tenancy.OrgScopeSelf), http.MethodPost, "/cs/"+stationOfA+"/commands", resetSoft))
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// notKeepingCommands is a store that does not keep commands, like the Firestore store.
type notKeepingCommands struct {
	*inmemory.Store
}

func (notKeepingCommands) CreateCommand(context.Context, *store.Command) error {
	return store.ErrNotImplemented
}

func (notKeepingCommands) GetCommand(context.Context, string) (*store.Command, error) {
	return nil, store.ErrNotImplemented
}

func (notKeepingCommands) ListCommands(context.Context, store.CommandFilter) ([]*store.Command, int, error) {
	return nil, 0, store.ErrNotImplemented
}

func TestCommandOperationsOnAStoreThatDoesNotKeepCommands(t *testing.T) {
	f := newCommandsFixture(t)
	notKeeping := notKeepingCommands{f.engine}
	f.service.Store = notKeeping
	f.dispatcher.Store = notKeeping

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/cs/" + stationOfA + "/commands", resetSoft},
		{http.MethodGet, "/cs/" + stationOfA + "/commands", ""},
		{http.MethodGet, "/commands/0193b2c4-0000-7000-8000-000000000000", ""},
		{http.MethodPost, "/commands/0193b2c4-0000-7000-8000-000000000000:cancel", ""},
	} {
		rr := serve(f.router, as(commandCaller(), tc.method, tc.path, tc.body))
		assert.Equal(t, http.StatusNotImplemented, rr.Code, "%s %s: %s", tc.method, tc.path, rr.Body.String())
	}
}

func TestCommandsInSingleMode(t *testing.T) {
	// a station key in single mode is the id of the charge station, also with a "."
	const csId = "CS.001"
	f := newCommandsFixture(t)
	ctx := context.Background()
	scoper := &tenancy.Scoper{
		Mode:         tenancy.ModeSingle,
		Resolver:     &tenancy.Resolver{Stations: f.engine, DefaultOrgId: testOrgId},
		DefaultOrgId: testOrgId,
	}
	require.NoError(t, f.engine.SetChargeStationAuth(ctx, csId, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: testOrgId}))
	require.NoError(t, f.engine.SetChargeStationRuntimeDetails(ctx, csId, &store.ChargeStationRuntimeDetails{OcppVersion: "1.6"}))
	srv, err := api.NewServer(f.engine, f.clock, nil, &events.OutboxEmitter{Store: f.engine, Orgs: scoper.Resolver, Clock: f.clock, Product: events.Product},
		api.WithScoper(scoper), api.WithCommands(f.service))
	require.NoError(t, err)
	caller := callerOf(testOrgId, tenancy.OrgScopeSelf)
	// requests are checked against the API spec, as on a single-tenant manager
	r := routerOf(t, srv, scoper, func(*http.Request) *tenancy.Principal { return caller }, api.ValidationMiddleware)

	rr := serve(r, as(caller, http.MethodPost, "/cs/"+csId+"/commands", resetSoft))
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	cmd := decodeCommand(t, rr)
	assert.Equal(t, csId, cmd.ChargeStationId)
	assert.Equal(t, testOrgId, f.stored(t, cmd.Id.String()).OrgId)

	rr = serve(r, as(caller, http.MethodPost, "/commands/"+cmd.Id.String()+":cancel", ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, api.CommandStatusCancelled, decodeCommand(t, rr).Status)

	rr = serve(r, as(caller, http.MethodGet, "/commands/"+cmd.Id.String(), ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, csId, decodeCommand(t, rr).ChargeStationId)
}

func TestEveryOcpp16ActionCanBeSentAndReadBack(t *testing.T) {
	f := newCommandsFixture(t)
	f.connected.set(stationOfA, false)

	// a payload that the OCPP 1.6 schema of each action accepts
	payloads := map[string]string{
		"CancelReservation":          `{"reservationId":1}`,
		"CertificateSigned":          `{"certificateChain":"-----BEGIN CERTIFICATE-----"}`,
		"ChangeAvailability":         `{"connectorId":0,"type":"Inoperative"}`,
		"ChangeConfiguration":        `{"key":"HeartbeatInterval","value":"60"}`,
		"ClearCache":                 `{}`,
		"ClearChargingProfile":       `{}`,
		"DataTransfer":               `{"vendorId":"vendor"}`,
		"DeleteCertificate":          `{"certificateHashData":{"hashAlgorithm":"SHA256","issuerNameHash":"a","issuerKeyHash":"b","serialNumber":"c"}}`,
		"ExtendedTriggerMessage":     `{"requestedMessage":"Heartbeat"}`,
		"GetCompositeSchedule":       `{"connectorId":1,"duration":60}`,
		"GetConfiguration":           `{}`,
		"GetDiagnostics":             `{"location":"ftp://example.com/"}`,
		"GetInstalledCertificateIds": `{"certificateType":"CentralSystemRootCertificate"}`,
		"GetLocalListVersion":        `{}`,
		"GetLog":                     `{"logType":"DiagnosticsLog","requestId":1,"log":{"remoteLocation":"https://example.com/"}}`,
		"InstallCertificate":         `{"certificateType":"CentralSystemRootCertificate","certificate":"-----BEGIN CERTIFICATE-----"}`,
		"RemoteStartTransaction":     `{"idTag":"TAG1"}`,
		"RemoteStopTransaction":      `{"transactionId":1}`,
		"ReserveNow":                 `{"connectorId":1,"expiryDate":"2026-01-02T04:00:00Z","idTag":"TAG1","reservationId":1}`,
		"Reset":                      `{"type":"Soft"}`,
		"SendLocalList":              `{"listVersion":1,"updateType":"Full"}`,
		"SetChargingProfile":         `{"connectorId":1,"csChargingProfiles":{"chargingProfileId":1,"stackLevel":0,"chargingProfilePurpose":"TxDefaultProfile","chargingProfileKind":"Absolute","chargingSchedule":{"chargingRateUnit":"A","chargingSchedulePeriod":[{"startPeriod":0,"limit":16}]}}}`,
		"SignedUpdateFirmware":       `{"requestId":1,"firmware":{"location":"https://example.com/fw","retrieveDateTime":"2026-01-02T04:00:00Z","signingCertificate":"-----BEGIN CERTIFICATE-----","signature":"c2lnbmF0dXJl"}}`,
		"TriggerMessage":             `{"requestedMessage":"Heartbeat"}`,
		"UnlockConnector":            `{"connectorId":1}`,
		"UpdateFirmware":             `{"location":"https://example.com/fw","retrieveDate":"2026-01-02T04:00:00Z"}`,
	}
	var actions []string
	for action := range payloads {
		actions = append(actions, action)
	}
	assert.ElementsMatch(t, commands.Actions(transport.OcppVersion16), actions, "a payload for every action of the catalogue")

	for action, payload := range payloads {
		t.Run(action, func(t *testing.T) {
			sent := f.mustEnqueue(t, stationOfA, `{"action":"`+action+`","payload":`+payload+`}`)
			rr := serve(f.router, as(commandCaller(), http.MethodGet, "/commands/"+sent.Id.String(), ""))
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			got := decodeCommand(t, rr)
			assert.Equal(t, action, got.Action)
			assert.JSONEq(t, payload, string(got.Payload))
			assert.Equal(t, api.CommandStatusQueued, got.Status)
		})
	}
}
