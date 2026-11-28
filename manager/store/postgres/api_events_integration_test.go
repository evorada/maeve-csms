// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/controlplane"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	clockTest "k8s.io/utils/clock/testing"
)

// The tests in this file check that the API stores station.registered in the
// PostgreSQL transaction of the registration, and that the control plane replays it.

var apiEventsTime = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

// apiEventsServer returns a single-tenant API server whose events the fixture records.
func apiEventsServer(t *testing.T, fixture *eventstest.Fixture) *api.Server {
	t.Helper()
	srv, err := api.NewServer(fixture.Engine(testStore), clockTest.NewFakePassiveClock(apiEventsTime), nil, fixture.Events)
	require.NoError(t, err)
	return srv
}

// registerThroughApi calls the operation as the middlewares in front of it hand a
// request of user:test over.
func registerThroughApi(srv *api.Server, csId string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/cs/"+csId, strings.NewReader(`{"securityProfile":1}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := tenancy.WithStationOrg(req.Context(), store.LegacyOrgId)
	ctx = events.WithActor(ctx, "user:test")
	rr := httptest.NewRecorder()
	srv.RegisterChargeStation(rr, req.WithContext(ctx), csId)
	return rr
}

func TestApiEvents_StationRegisteredIsReplayed(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()
	fixture := eventstest.New(testStore, "", clockTest.NewFakePassiveClock(apiEventsTime))
	srv := apiEventsServer(t, fixture)

	rr := registerThroughApi(srv, "API001")
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	// registering again emits nothing
	rr = registerThroughApi(srv, "API001")
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

	evs := fixture.Take(t)
	require.Equal(t, []string{events.TypeStationRegistered}, eventstest.Types(evs))
	assert.Equal(t, store.LegacyOrgId, evs[0].OrgId)
	assert.Equal(t, "user:test", evs[0].Actor)
	assert.Equal(t, "urn:cpms:"+store.LegacyOrgId+":station/API001", evs[0].Subject)
	auth, err := testStore.LookupChargeStationAuth(ctx, "API001")
	require.NoError(t, err)
	require.NotNil(t, auth)
	requireNoOpenTransaction(t)

	// the replay of the control plane, for a caller of the only org of single mode
	cp := &controlplane.Server{
		Outbox: testStore,
		Scoper: &tenancy.Scoper{Resolver: &tenancy.Resolver{Stations: testStore}},
	}
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req = req.WithContext(tenancy.WithPrincipal(req.Context(), &tenancy.Principal{
		Subject: "svc:events", OrgId: store.LegacyOrgId, OrgScope: tenancy.OrgScopeSelf, Perms: []string{tenancy.PermTenantManage},
	}))
	types := []string{"cpms.station.registered"}
	replay := httptest.NewRecorder()
	cp.ListEvents(replay, req, controlplane.ListEventsParams{Type: &types})
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	var page struct {
		Events     []events.CloudEvent `json:"events"`
		NextCursor string              `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(replay.Body.Bytes(), &page))
	require.Len(t, page.Events, 1)
	assert.Equal(t, evs[0].Id, page.Events[0].Id)
	assert.Equal(t, "cpms.station.registered", page.Events[0].Type)
	assert.Equal(t, apiEventsTime, page.Events[0].Time)
	assert.JSONEq(t, `{"station_key":"API001","station_id":"API001","security_profile":1}`, string(page.Events[0].Data))
	assert.NotEqual(t, "0", page.NextCursor)
}

func TestApiEvents_RegistrationIsUndoneWhenItsEventCannotBeStored(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()
	fixture := eventstest.New(testStore, "", clockTest.NewFakePassiveClock(apiEventsTime))
	// the event is written in the transaction, and then the append fails
	fixture.Events.Emitter = &events.OutboxEmitter{
		Store:   failingOutbox{testStore},
		Clock:   clockTest.NewFakePassiveClock(apiEventsTime),
		Product: events.Product,
	}
	srv := apiEventsServer(t, fixture)

	rr := registerThroughApi(srv, "API002")
	require.Equal(t, http.StatusInternalServerError, rr.Code, rr.Body.String())

	auth, err := testStore.LookupChargeStationAuth(ctx, "API002")
	require.NoError(t, err)
	assert.Nil(t, auth, "no charge station without its event")
	assert.Zero(t, outboxRows(t), "no event without its charge station")
	requireNoOpenTransaction(t)
}
