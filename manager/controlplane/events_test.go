// SPDX-License-Identifier: Apache-2.0

package controlplane_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/controlplane"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
)

// orgOther is a root tenant next to the tree of orgTop.
const orgOther = "0193b2c4-0000-7000-8000-0000000000c3"

// addTenants provisions orgTop with the tenants orgA and orgB below it, and orgOther as
// a tenant of its own.
func (f *fixture) addTenants(t *testing.T) {
	t.Helper()
	top := orgTop
	for _, tenant := range []*store.Tenant{
		{OrgId: orgTop, Path: store.OrgPathLabel(orgTop), Slug: "top", OrgKey: "topp"},
		{OrgId: orgA, ParentId: &top, Path: store.OrgPathLabel(orgTop) + "." + store.OrgPathLabel(orgA), Slug: "tenant-a", OrgKey: "aaaa"},
		{OrgId: orgB, ParentId: &top, Path: store.OrgPathLabel(orgTop) + "." + store.OrgPathLabel(orgB), Slug: "tenant-b", OrgKey: "bbbb"},
		{OrgId: orgOther, Path: store.OrgPathLabel(orgOther), Slug: "other", OrgKey: "othr"},
	} {
		require.NoError(t, f.engine.PutTenant(context.Background(), tenant))
	}
}

// appendEvent stores an event of the org and type in the outbox, not yet published,
// and returns its id.
func (f *fixture) appendEvent(t *testing.T, orgId, eventType string) string {
	t.Helper()
	f.eventCount++
	n := f.eventCount
	id := fmt.Sprintf("0193b2c4-0000-7000-8000-%012d", n)
	require.NoError(t, f.engine.AppendOutboxEvents(context.Background(), &store.OutboxEvent{
		Id:      id,
		OrgId:   orgId,
		Type:    eventType,
		Subject: events.URN(events.Product, orgId, events.ResourceStation, "k"+strconv.Itoa(n)),
		Actor:   "station:k" + strconv.Itoa(n),
		Time:    time.Date(2026, 11, 27, 10, 0, n, 0, time.UTC),
		Data:    json.RawMessage(fmt.Sprintf(`{"n":%d}`, n)),
	}))
	return id
}

// publishEvents publishes every event of the outbox, as the relay does.
func (f *fixture) publishEvents(t *testing.T) {
	t.Helper()
	_, err := f.engine.RelayOutboxEvents(context.Background(), store.MaxOutboxEventListLimit,
		func(_ context.Context, evs []*store.OutboxEvent) (int, error) { return len(evs), nil })
	require.NoError(t, err)
}

// publish stores the events of the orgs and types (org, type, org, type, ...) and
// publishes them; it returns their ids in that order.
func (f *fixture) publish(t *testing.T, orgsAndTypes ...string) []string {
	t.Helper()
	var ids []string
	for i := 0; i+1 < len(orgsAndTypes); i += 2 {
		ids = append(ids, f.appendEvent(t, orgsAndTypes[i], orgsAndTypes[i+1]))
	}
	f.publishEvents(t)
	return ids
}

// reader returns the token of a platform service that may manage tenants, of the org
// and with the org scope.
func (f *fixture) reader(orgId string, scope tenancy.OrgScope) string {
	claims := f.platform.Claims(time.Now())
	claims["sub"] = "svc:events"
	claims["org"] = orgId
	claims["org_scope"] = string(scope)
	claims["perms"] = []string{tenancy.PermTenantManage}
	return f.platform.Sign(claims)
}

type eventPage struct {
	Events     []json.RawMessage `json:"events"`
	NextCursor string            `json:"next_cursor"`
}

// listEvents asks for the events with the query and returns the page and the ids of its
// events.
func (f *fixture) listEvents(t *testing.T, token string, query url.Values) (eventPage, []string) {
	t.Helper()
	target := "/events"
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	rr := f.do(http.MethodGet, target, token, "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var page eventPage
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &page))
	require.NotNil(t, page.Events, "an empty list, not null: %s", rr.Body.String())
	ids := make([]string, len(page.Events))
	for i, raw := range page.Events {
		var ev struct {
			Id string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(raw, &ev))
		ids[i] = ev.Id
	}
	return page, ids
}

// published returns the published events of the store by id.
func (f *fixture) published(t *testing.T) map[string]*store.OutboxEvent {
	t.Helper()
	evs, err := f.engine.ListOutboxEvents(context.Background(), store.OutboxEventFilter{Limit: store.MaxOutboxEventListLimit})
	require.NoError(t, err)
	byId := map[string]*store.OutboxEvent{}
	for _, ev := range evs {
		byId[ev.Id] = ev
	}
	return byId
}

func cursorOf(ev *store.OutboxEvent) string {
	return strconv.FormatInt(ev.PublishedSeq, 10)
}

func TestEventsArePagedAfterACursor(t *testing.T) {
	f := newFixture(t)
	f.addTenants(t)
	token := f.reader(orgA, tenancy.OrgScopeSelf)
	ids := f.publish(t,
		orgA, events.TypeStationConnected,
		orgA, events.TypeConnectorStatusChanged,
		orgA, events.TypeSessionStarted,
		orgA, events.TypeSessionUpdated,
		orgA, events.TypeSessionCompleted,
	)
	// in the outbox, and not published yet
	pending := f.appendEvent(t, orgA, events.TypeStationDisconnected)
	stored := f.published(t)

	// the first page starts with the oldest event
	page, got := f.listEvents(t, token, url.Values{"limit": {"2"}})
	assert.Equal(t, ids[:2], got)
	assert.Equal(t, cursorOf(stored[ids[1]]), page.NextCursor)
	// each event is the CloudEvent that was published on the stream
	for i, raw := range page.Events {
		published, err := json.Marshal(events.ToCloudEvent(stored[ids[i]], events.Product))
		require.NoError(t, err)
		assert.JSONEq(t, string(published), string(raw))
	}
	assert.JSONEq(t, `{
		"specversion": "1.0",
		"id": "`+ids[0]+`",
		"source": "urn:cpms",
		"type": "cpms.station.connected",
		"subject": "urn:cpms:`+orgA+`:station/k1",
		"time": "2026-11-27T10:00:01Z",
		"datacontenttype": "application/json",
		"orgid": "`+orgA+`",
		"actor": "station:k1",
		"data": {"n": 1}
	}`, string(page.Events[0]))

	page, got = f.listEvents(t, token, url.Values{"after": {page.NextCursor}, "limit": {"2"}})
	assert.Equal(t, ids[2:4], got)
	assert.Equal(t, cursorOf(stored[ids[3]]), page.NextCursor)

	page, got = f.listEvents(t, token, url.Values{"after": {page.NextCursor}, "limit": {"2"}})
	assert.Equal(t, ids[4:], got)
	last := page.NextCursor
	assert.Equal(t, cursorOf(stored[ids[4]]), last)

	// at the end the cursor stays where it is
	page, got = f.listEvents(t, token, url.Values{"after": {last}})
	assert.Empty(t, got)
	assert.Equal(t, last, page.NextCursor)

	// without a cursor and a limit: everything that is published, from the start
	page, got = f.listEvents(t, token, nil)
	assert.Equal(t, ids, got)
	assert.Equal(t, last, page.NextCursor)

	// an event is returned once it is published
	f.publishEvents(t)
	page, got = f.listEvents(t, token, url.Values{"after": {last}})
	assert.Equal(t, []string{pending}, got)
	assert.Equal(t, cursorOf(f.published(t)[pending]), page.NextCursor)

	// an empty page from the start says 0
	page, got = f.listEvents(t, f.reader(orgB, tenancy.OrgScopeSelf), nil)
	assert.Empty(t, got)
	assert.Equal(t, "0", page.NextCursor)
}

func TestEventsComeInPagesOf100UnlessALimitIsGiven(t *testing.T) {
	f := newFixture(t)
	f.addTenants(t)
	var orgsAndTypes []string
	for range 1001 {
		orgsAndTypes = append(orgsAndTypes, orgA, events.TypeSessionUpdated)
	}
	ids := f.publish(t, orgsAndTypes...)
	token := f.reader(orgA, tenancy.OrgScopeSelf)

	page, got := f.listEvents(t, token, nil)
	assert.Equal(t, ids[:100], got)
	assert.Equal(t, cursorOf(f.published(t)[ids[99]]), page.NextCursor)

	_, got = f.listEvents(t, token, url.Values{"limit": {"1"}})
	assert.Equal(t, ids[:1], got)
	_, got = f.listEvents(t, token, url.Values{"limit": {"1000"}})
	assert.Equal(t, ids[:1000], got)
}

func TestEventsCanBeSelectedByType(t *testing.T) {
	f := newFixture(t)
	f.addTenants(t)
	ids := f.publish(t,
		orgA, events.TypeSessionStarted,
		orgA, events.TypeSessionCompleted,
		orgA, events.TypeStationConnected,
		orgA, events.TypeSessionCompleted,
		orgA, events.TypeStationDisconnected,
	)
	token := f.reader(orgA, tenancy.OrgScopeSelf)

	for _, name := range []string{"session.completed", "cpms.session.completed"} {
		t.Run(name, func(t *testing.T) {
			page, got := f.listEvents(t, token, url.Values{"type": {name}})
			assert.Equal(t, []string{ids[1], ids[3]}, got)
			assert.Equal(t, cursorOf(f.published(t)[ids[3]]), page.NextCursor)

			// a page ends with the last event of the type
			page, got = f.listEvents(t, token, url.Values{"type": {name}, "limit": {"1"}})
			assert.Equal(t, []string{ids[1]}, got)
			assert.Equal(t, cursorOf(f.published(t)[ids[1]]), page.NextCursor)
			_, got = f.listEvents(t, token, url.Values{"type": {name}, "after": {page.NextCursor}})
			assert.Equal(t, []string{ids[3]}, got)
		})
	}

	t.Run("several types, short and full names mixed", func(t *testing.T) {
		_, got := f.listEvents(t, token, url.Values{"type": {"session.completed", "cpms.station.connected", "cpms.session.started"}})
		assert.Equal(t, ids[:4], got)
	})

	t.Run("a type without events", func(t *testing.T) {
		page, got := f.listEvents(t, token, url.Values{"type": {"cpms.command.completed"}})
		assert.Empty(t, got)
		assert.Equal(t, "0", page.NextCursor)
	})
}

func TestEventsAreOnlyThoseOfTheOrgsOfTheCaller(t *testing.T) {
	f := newFixture(t)
	f.addTenants(t)
	ids := f.publish(t,
		orgTop, events.TypeStationConnected,
		orgA, events.TypeStationConnected,
		orgOther, events.TypeStationConnected,
		orgB, events.TypeStationConnected,
		orgA, events.TypeSessionCompleted,
		testutil.PlatformOrgId, events.TypeStationConnected,
		orgOther, events.TypeSessionCompleted,
	)
	ofTop, ofA, ofOther1, ofB, ofA2, ofPlatform, ofOther2 := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], ids[6]

	cases := []struct {
		name  string
		org   string
		scope tenancy.OrgScope
		want  []string
	}{
		{"a tenant", orgA, tenancy.OrgScopeSelf, []string{ofA, ofA2}},
		{"a tenant and the tenants below it (none)", orgA, tenancy.OrgScopeSubtree, []string{ofA, ofA2}},
		{"a partner and the tenants below it", orgTop, tenancy.OrgScopeSubtree, []string{ofTop, ofA, ofB, ofA2}},
		{"a partner alone", orgTop, tenancy.OrgScopeSelf, []string{ofTop}},
		{"another tree", orgOther, tenancy.OrgScopeSubtree, []string{ofOther1, ofOther2}},
		{"an org that is not a tenant, alone", testutil.PlatformOrgId, tenancy.OrgScopeSelf, []string{ofPlatform}},
		// the orgs below an org are those that are tenants here: none
		{"an org that is not a tenant, with the orgs below it", testutil.PlatformOrgId, tenancy.OrgScopeSubtree, []string{}},
		{"an org without events", "0193b2c4-0000-7000-8000-0000000000d4", tenancy.OrgScopeSelf, []string{}},
		{"an org without events, with the orgs below it", "0193b2c4-0000-7000-8000-0000000000d4", tenancy.OrgScopeSubtree, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := f.reader(tc.org, tc.scope)
			_, got := f.listEvents(t, token, nil)
			assert.Equal(t, tc.want, got)
			// also page by page, and with a type
			_, got = f.listEvents(t, token, url.Values{"limit": {"1"}, "type": {"station.connected"}})
			if want := onlyType(t, f, tc.want, events.TypeStationConnected); len(want) > 0 {
				assert.Equal(t, want[:1], got)
			} else {
				assert.Empty(t, got)
			}
		})
	}

	t.Run("the org of the token is compared in any case", func(t *testing.T) {
		claims := f.platform.Claims(time.Now())
		claims["sub"] = "svc:events"
		claims["org"] = "0193B2C4-0000-7000-8000-0000000000A1"
		claims["org_scope"] = "self"
		claims["perms"] = []string{tenancy.PermTenantManage}
		_, got := f.listEvents(t, f.platform.Sign(claims), nil)
		assert.Equal(t, []string{ofA, ofA2}, got)
	})
}

// onlyType returns the ids of the events of the type, in their order.
func onlyType(t *testing.T, f *fixture, ids []string, eventType string) []string {
	t.Helper()
	stored := f.published(t)
	var selected []string
	for _, id := range ids {
		if stored[id].Type == eventType {
			selected = append(selected, id)
		}
	}
	return selected
}

func TestEventsInSingleMode(t *testing.T) {
	f := newFixture(t)
	resolver := &tenancy.Resolver{Stations: f.engine, DefaultOrgId: store.LegacyOrgId}
	f.server.Scoper = &tenancy.Scoper{Resolver: resolver, DefaultOrgId: store.LegacyOrgId}
	ids := f.publish(t,
		store.LegacyOrgId, events.TypeStationConnected,
		orgA, events.TypeStationConnected,
	)

	// the only org of single mode is the default org
	_, got := f.listEvents(t, f.reader(store.LegacyOrgId, tenancy.OrgScopeSubtree), nil)
	assert.Equal(t, ids[:1], got)
	_, got = f.listEvents(t, f.reader(orgA, tenancy.OrgScopeSubtree), nil)
	assert.Empty(t, got)
}

func TestEventsRefuseParametersThatAreNotValid(t *testing.T) {
	f := newFixture(t)
	f.addTenants(t)
	f.publish(t, orgA, events.TypeStationConnected)
	token := f.reader(orgA, tenancy.OrgScopeSelf)

	cases := []struct {
		query   string
		field   string
		message string
	}{
		{"after=abc", "after", "must be a cursor that a page of events returned"},
		{"after=-1", "after", "must be a cursor that a page of events returned"},
		{"after=1.5", "after", "must be a cursor that a page of events returned"},
		{"after=", "after", "must be a cursor that a page of events returned"},
		{"after=99999999999999999999", "after", "must be a cursor that a page of events returned"},
		{"after=1&after=2", "after", "cannot be read"},
		{"limit=0", "limit", "must be between 1 and 1000"},
		{"limit=1001", "limit", "must be between 1 and 1000"},
		{"limit=-5", "limit", "must be between 1 and 1000"},
		{"limit=ten", "limit", "cannot be read"},
		{"type=session", "type", "must be a type of the manifest"},
		{"type=", "type", "must be a type of the manifest"},
		{"type=cpms.", "type", "must be a type of the manifest"},
		{"type=cpms.cpms.session.completed", "type", "must be a type of the manifest"},
		{"type=other.session.completed", "type", "must be a type of the manifest"},
		{"type=Session.Completed", "type", "must be a type of the manifest"},
		{"type=station.renamed", "type", "must be a type of the manifest"},
		{"type=session.completed&type=nothing", "type", "must be a type of the manifest"},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			rr := f.do(http.MethodGet, "/events?"+tc.query, token, "")
			require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
			assert.JSONEq(t, fmt.Sprintf(`{"status":"Bad Request","error":"invalid_field","field":%q,"message":%q}`, tc.field, tc.message), rr.Body.String())
		})
	}
}

// noOutbox is a storage engine without an outbox, like the Firestore one.
type noOutbox struct {
	*inmemory.Store
}

func (noOutbox) ListOutboxEvents(context.Context, store.OutboxEventFilter) ([]*store.OutboxEvent, error) {
	return nil, fmt.Errorf("ListOutboxEvents: %w", store.ErrNotImplemented)
}

// noSubtree is a tenant store that cannot list the orgs of a subtree, like the
// Firestore one.
type noSubtree struct {
	*inmemory.Store
}

func (noSubtree) ListSubtreeOrgIds(context.Context, string) ([]string, error) {
	return nil, fmt.Errorf("ListSubtreeOrgIds: %w", store.ErrNotImplemented)
}

// brokenOutbox is an outbox whose database fails.
type brokenOutbox struct {
	*inmemory.Store
}

func (brokenOutbox) ListOutboxEvents(context.Context, store.OutboxEventFilter) ([]*store.OutboxEvent, error) {
	return nil, errors.New("database is down: host db-1.internal")
}

func TestEventsOfAStorageThatKeepsNone(t *testing.T) {
	const notImplemented = `{"status":"Not Implemented","error":"not_implemented"}`
	const internalError = `{"status":"Internal Server Error","error":"internal_error"}`

	cases := []struct {
		name   string
		change func(f *fixture)
		scope  tenancy.OrgScope
		status int
		body   string
	}{
		{"no outbox", func(f *fixture) { f.server.Outbox = noOutbox{f.engine} }, tenancy.OrgScopeSelf, http.StatusNotImplemented, notImplemented},
		{"no outbox at all", func(f *fixture) { f.server.Outbox = nil }, tenancy.OrgScopeSelf, http.StatusNotImplemented, notImplemented},
		{"no subtrees", func(f *fixture) { f.server.Scoper.Tenants = noSubtree{f.engine} }, tenancy.OrgScopeSubtree, http.StatusNotImplemented, notImplemented},
		{"the outbox fails", func(f *fixture) { f.server.Outbox = brokenOutbox{f.engine} }, tenancy.OrgScopeSelf, http.StatusInternalServerError, internalError},
		{"no scoper", func(f *fixture) { f.server.Scoper = nil }, tenancy.OrgScopeSelf, http.StatusInternalServerError, internalError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.addTenants(t)
			f.publish(t, orgA, events.TypeStationConnected)
			tc.change(f)

			rr := f.do(http.MethodGet, "/events", f.reader(orgA, tc.scope), "")
			require.Equal(t, tc.status, rr.Code, rr.Body.String())
			assert.JSONEq(t, tc.body, rr.Body.String())
		})
	}

	t.Run("the parameters are checked first", func(t *testing.T) {
		f := newFixture(t)
		f.server.Outbox = noOutbox{f.engine}
		rr := f.do(http.MethodGet, "/events?after=x", f.reader(orgA, tenancy.OrgScopeSelf), "")
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})
}

func TestEventsNeedAPlatformServiceThatMayManageTenants(t *testing.T) {
	f := newFixture(t)
	f.addTenants(t)
	f.publish(t, orgA, events.TypeStationConnected)

	person := f.platform.Claims(time.Now())
	person["org"] = orgA
	person["perms"] = append([]string{tenancy.PermTenantManage}, tenancy.ManifestPermissions...)
	rr := f.do(http.MethodGet, "/events", f.platform.Sign(person), "")
	assert.Equal(t, http.StatusForbidden, rr.Code)

	rr = f.do(http.MethodGet, "/events", f.token("svc:events", tenancy.ManifestPermissions...), "")
	assert.Equal(t, http.StatusForbidden, rr.Code)

	rr = f.do(http.MethodGet, "/events", "", "")
	assert.Equal(t, http.StatusUnauthorized, rr.Code)

	// the operation itself refuses a request that was not authenticated
	rr = httptest.NewRecorder()
	f.server.ListEvents(rr, httptest.NewRequest(http.MethodGet, "/events", nil), controlplane.ListEventsParams{})
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}
