// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/ocpi"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// setTenantConfig stores a config document for a tenant of newMultiTenant.
func (m *multiTenant) setTenantConfig(t *testing.T, orgId, config string) {
	t.Helper()
	ctx := context.Background()
	tenant, err := m.engine.GetTenant(ctx, orgId)
	require.NoError(t, err)
	require.NotNil(t, tenant)
	tenant.ConfigVersion++
	tenant.Config = json.RawMessage(config)
	require.NoError(t, m.engine.PutTenant(ctx, tenant))
	m.scoper.Resolver.Invalidate(orgId)
}

func TestRegisterChargeStationWithoutASecurityProfile(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	// tenant A wants client certificates, tenant C the profile without TLS; tenant B was
	// never given a config
	m.setTenantConfig(t, orgA, `{"version":1,"default_security_profile":"SP3"}`)
	m.setTenantConfig(t, orgC, `{"version":1,"default_security_profile":"SP1"}`)
	require.NoError(t, m.engine.SetChargeStationAuth(ctx, "aaaa.MOVED", &store.ChargeStationAuth{
		SecurityProfile: store.TLSWithClientSideCertificates, OrgId: orgC,
	}))
	r := m.router(t)

	cases := []struct {
		name   string
		caller string
		csId   string
		body   string
		want   store.SecurityProfile
	}{
		{name: "the default of the tenant", caller: orgA, csId: "aaaa.NEW", body: `{}`, want: store.TLSWithClientSideCertificates},
		{name: "null is no security profile", caller: orgA, csId: "aaaa.NULL", body: `{"securityProfile":null}`, want: store.TLSWithClientSideCertificates},
		{name: "a tenant without a config gets TLS with basic auth", caller: orgB, csId: "bbbb.NEW", body: `{"base64SHA256Password":"DEADBEEF"}`, want: store.TLSWithBasicAuth},
		{name: "a tenant that chose the profile without TLS", caller: orgC, csId: "cccc.NEW", body: `{}`, want: store.UnsecuredTransportWithBasicAuth},
		{name: "the default of the tenant that owns the charge station", caller: orgC, csId: "aaaa.MOVED", body: `{}`, want: store.UnsecuredTransportWithBasicAuth},
		// a security profile in the request is used as it is
		{name: "profile 0 in the request", caller: orgA, csId: "aaaa.SP1", body: `{"securityProfile":0}`, want: store.UnsecuredTransportWithBasicAuth},
		{name: "profile 1 in the request", caller: orgA, csId: "aaaa.SP2", body: `{"securityProfile":1}`, want: store.TLSWithBasicAuth},
		{name: "profile 2 in the request", caller: orgB, csId: "bbbb.SP3", body: `{"securityProfile":2}`, want: store.TLSWithClientSideCertificates},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := serve(r, as(callerOf(tc.caller, tenancy.OrgScopeSelf), http.MethodPost, "/cs/"+tc.csId, tc.body))
			require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

			got, err := m.engine.LookupChargeStationAuth(ctx, tc.csId)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tc.want, got.SecurityProfile)
			assert.Equal(t, tc.caller, got.OrgId)

			// the security profile is always in the answer of the lookup, also profile 0
			rr = serve(r, as(callerOf(tc.caller, tenancy.OrgScopeSelf), http.MethodGet, "/cs/"+tc.csId+"/auth", ""))
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			var answer map[string]any
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &answer))
			assert.EqualValues(t, tc.want, answer["securityProfile"], rr.Body.String())
		})
	}

	t.Run("a new config of the tenant applies to the next charge station", func(t *testing.T) {
		m.setTenantConfig(t, orgA, `{"version":2,"default_security_profile":"SP2"}`)
		rr := serve(r, as(callerOf(orgA, tenancy.OrgScopeSelf), http.MethodPost, "/cs/aaaa.LATER", `{}`))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		got, err := m.engine.LookupChargeStationAuth(ctx, "aaaa.LATER")
		require.NoError(t, err)
		assert.Equal(t, store.TLSWithBasicAuth, got.SecurityProfile)
		// the charge stations that are registered keep their profile
		got, err = m.engine.LookupChargeStationAuth(ctx, "aaaa.NEW")
		require.NoError(t, err)
		assert.Equal(t, store.TLSWithClientSideCertificates, got.SecurityProfile)
	})

	t.Run("a stored config that cannot be read gives TLS with basic auth", func(t *testing.T) {
		// never a part of the document: this one asks for the profile without TLS
		m.setTenantConfig(t, orgC, `{"version":2,"default_security_profile":"SP1","session_rules":{}}`)
		rr := serve(r, as(callerOf(orgC, tenancy.OrgScopeSelf), http.MethodPost, "/cs/cccc.UNREADABLE", `{}`))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		got, err := m.engine.LookupChargeStationAuth(ctx, "cccc.UNREADABLE")
		require.NoError(t, err)
		assert.Equal(t, store.TLSWithBasicAuth, got.SecurityProfile)
	})
}

func TestRegisterChargeStationWithoutASecurityProfileInSingleMode(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()
	ctx := context.Background()
	// a tenant in the store changes nothing in single mode
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: testOrgId, Path: store.OrgPathLabel(testOrgId), Slug: "tenant", OrgKey: "aaaa",
		ConfigVersion: 1, Config: json.RawMessage(`{"version":1,"default_security_profile":"SP1"}`),
	}))

	for csId, tc := range map[string]struct {
		body string
		want store.SecurityProfile
	}{
		"cs001": {body: `{}`, want: store.TLSWithBasicAuth},
		"cs002": {body: `{"base64SHA256Password":"DEADBEEF","invalidUsernameAllowed":true}`, want: store.TLSWithBasicAuth},
		// as before
		"cs003": {body: `{"securityProfile":0}`, want: store.UnsecuredTransportWithBasicAuth},
		"cs004": {body: `{"securityProfile":2}`, want: store.TLSWithClientSideCertificates},
	} {
		req := httptest.NewRequest(http.MethodPost, "/cs/"+csId, strings.NewReader(tc.body))
		req.Header.Set("content-type", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		require.Equal(t, http.StatusCreated, rr.Code, csId)

		got, err := engine.LookupChargeStationAuth(ctx, csId)
		require.NoError(t, err)
		require.NotNil(t, got, csId)
		assert.Equal(t, tc.want, got.SecurityProfile, csId)
		assert.Equal(t, testOrgId, got.OrgId, csId)
	}
}

func TestRegisterChargeStationRegistersNothingWhenTheTenantConfigCannotBeLookedUp(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	m.scoper.Resolver.Tenants = failingTenants{m.engine}
	srv, err := api.NewServer(m.engine, m.clock, ocpi.NewOCPI(m.engine, nil, "GB", "TWK", testOrgId), api.WithScoper(m.scoper))
	require.NoError(t, err)

	// the operation itself, as the middleware in front of it hands a request over
	register := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/cs/aaaa.NEW", strings.NewReader(body))
		req.Header.Set("content-type", "application/json")
		rr := httptest.NewRecorder()
		srv.RegisterChargeStation(rr, req.WithContext(tenancy.WithStationOrg(req.Context(), orgA)), "aaaa.NEW")
		return rr
	}

	rr := register(`{"base64SHA256Password":"DEADBEEF"}`)
	require.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.JSONEq(t, `{"status":"Internal Server Error"}`, rr.Body.String())
	got, err := m.engine.LookupChargeStationAuth(ctx, "aaaa.NEW")
	require.NoError(t, err)
	assert.Nil(t, got, "a charge station must not be registered with a guessed security profile")

	// a request that names its profile does not need the config
	rr = register(`{"securityProfile":2}`)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
}
