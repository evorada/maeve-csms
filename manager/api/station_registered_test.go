// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/entitlements"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	"github.com/thoughtworks/maeve-csms/manager/ocpi"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// withEvents returns the router of the API server of m whose domain events the
// returned fixture records. The fixture resolves no org: the API names the org of its
// events itself.
func (m *multiTenant) withEvents(t *testing.T) (*chi.Mux, *eventstest.Fixture) {
	t.Helper()
	fixture := eventstest.New(m.engine, "", m.clock)
	enforcer := &entitlements.Enforcer{Tenants: m.engine, Counter: m.engine, Mode: m.scoper.Mode}
	srv, err := api.NewServer(fixture.Engine(m.engine), m.clock, ocpi.NewOCPI(m.engine, nil, "GB", "TWK", testOrgId), fixture.Events,
		api.WithScoper(m.scoper), api.WithEntitlements(enforcer))
	require.NoError(t, err)
	return routerOf(t, srv, m.scoper, func(r *http.Request) *tenancy.Principal {
		p, _ := tenancy.FromContext(r.Context())
		return p
	}), fixture
}

func TestRegisteringANewChargeStationEmitsStationRegistered(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	m.setTenantConfig(t, orgA, `{"version":1,"default_security_profile":"SP3"}`)
	r, fixture := m.withEvents(t)
	// a partner registers for a tenant below it
	partner := &tenancy.Principal{Subject: "user:partner-admin", OrgId: orgPartner, OrgScope: tenancy.OrgScopeSubtree, Perms: tenancy.AllPermissions}

	rr := serve(r, as(partner, http.MethodPost, "/cs/aaaa.NEW", `{"securityProfile":1,"base64SHA256Password":"DEADBEEF"}`))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

	evs := fixture.Take(t)
	require.Len(t, evs, 1)
	ev := evs[0]
	assert.Equal(t, events.TypeStationRegistered, ev.Type)
	// the org that owns the charge station, not the org of the caller
	assert.Equal(t, orgA, ev.OrgId)
	assert.Equal(t, "urn:cpms:"+orgA+":station/aaaa.NEW", ev.Subject)
	assert.Equal(t, "user:partner-admin", ev.Actor)
	assert.Equal(t, m.clock.Now(), ev.Time)
	assert.JSONEq(t, `{"station_key":"aaaa.NEW","station_id":"NEW","security_profile":1}`, string(ev.Data))

	t.Run("registering it again emits nothing", func(t *testing.T) {
		rr := serve(r, as(callerOf(orgA, tenancy.OrgScopeSelf), http.MethodPost, "/cs/aaaa.NEW", `{"securityProfile":2}`))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		auth, err := m.engine.LookupChargeStationAuth(ctx, "aaaa.NEW")
		require.NoError(t, err)
		require.Equal(t, store.TLSWithClientSideCertificates, auth.SecurityProfile, "registered again")
		assert.Empty(t, fixture.Take(t))

		rr = serve(r, as(callerOf(orgA, tenancy.OrgScopeSelf), http.MethodPost, "/cs/"+stationOfA, `{}`))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		assert.Empty(t, fixture.Take(t))
	})

	t.Run("the security profile that the charge station got", func(t *testing.T) {
		rr := serve(r, as(callerOf(orgA, tenancy.OrgScopeSelf), http.MethodPost, "/cs/aaaa.DEFAULT", `{}`))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		evs := fixture.Take(t)
		require.Len(t, evs, 1)
		assert.Equal(t, "user:test", evs[0].Actor)
		assert.JSONEq(t, `{"station_key":"aaaa.DEFAULT","station_id":"DEFAULT","security_profile":2}`, string(evs[0].Data))
	})
}

func TestARegistrationThatIsRefusedEmitsNothing(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	// tenant A has room for no more connectors
	m.setEntitlements(t, orgA, connectorLimit(1, "hard"))
	m.reportConnectors(t, stationOfA, 1)
	r, fixture := m.withEvents(t)

	cases := []struct {
		name   string
		caller *tenancy.Principal
		csId   string
		body   string
		status int
	}{
		{"the connector limit of the tenant is used up", callerOf(orgA, tenancy.OrgScopeSelf), "aaaa.NEW", `{"securityProfile":1}`, http.StatusForbidden},
		{"outside the scope of the caller", callerOf(orgB, tenancy.OrgScopeSelf), "aaaa.NEW", `{"securityProfile":1}`, http.StatusNotFound},
		{"a body that is not valid", callerOf(orgB, tenancy.OrgScopeSelf), "bbbb.NEW", `{"securityProfile":200}`, http.StatusBadRequest},
		{"a station id that is not valid", callerOf(orgB, tenancy.OrgScopeSelf), "bbbb.NEW%20ONE", `{"securityProfile":1}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := serve(r, as(tc.caller, http.MethodPost, "/cs/"+tc.csId, tc.body))
			require.Equal(t, tc.status, rr.Code, rr.Body.String())
			assert.Empty(t, fixture.Take(t))
		})
	}
	auth, err := m.engine.LookupChargeStationAuth(ctx, "aaaa.NEW")
	require.NoError(t, err)
	assert.Nil(t, auth)

	// the charge station that exists is registered again, and emits nothing either
	rr := serve(r, as(callerOf(orgA, tenancy.OrgScopeSelf), http.MethodPost, "/cs/"+stationOfA, `{"securityProfile":1}`))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	assert.Empty(t, fixture.Take(t))
}

func TestARegistrationWhoseEventCannotBeStoredFails(t *testing.T) {
	m := newMultiTenant(t)
	r, fixture := m.withEvents(t)

	fixture.Events.Err = errors.New("outbox full")
	rr := serve(r, as(callerOf(orgA, tenancy.OrgScopeSelf), http.MethodPost, "/cs/aaaa.NEW", `{"securityProfile":1}`))
	require.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.JSONEq(t, `{"status":"Internal Server Error"}`, rr.Body.String())
	assert.Empty(t, fixture.Take(t))

	fixture.Events.Err = nil
	fixture.Tx.CommitErr = errors.New("commit failed")
	rr = serve(r, as(callerOf(orgA, tenancy.OrgScopeSelf), http.MethodPost, "/cs/aaaa.OTHER", `{"securityProfile":1}`))
	require.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.JSONEq(t, `{"status":"Internal Server Error"}`, rr.Body.String())
	assert.Empty(t, fixture.Take(t))
}

func TestRegisteringInSingleModeEmitsAnEventOfTheDefaultOrg(t *testing.T) {
	server, r, engine, c := setupServer(t)
	defer server.Close()

	rr := serve(r, as(nil, http.MethodPost, "/cs/cs001", `{"securityProfile":0}`))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

	var published []*store.OutboxEvent
	_, err := engine.RelayOutboxEvents(context.Background(), 10, func(_ context.Context, evs []*store.OutboxEvent) (int, error) {
		published = append(published, evs...)
		return len(evs), nil
	})
	require.NoError(t, err)
	require.Len(t, published, 1)
	assert.Equal(t, events.TypeStationRegistered, published[0].Type)
	assert.Equal(t, testOrgId, published[0].OrgId)
	assert.Equal(t, "urn:cpms:"+testOrgId+":station/cs001", published[0].Subject)
	// the caller of every request of the single-tenant router
	assert.Equal(t, "user:test", published[0].Actor)
	assert.Equal(t, c.Now().UTC(), published[0].Time)
	assert.JSONEq(t, `{"station_key":"cs001","station_id":"cs001","security_profile":0}`, string(published[0].Data))
}
