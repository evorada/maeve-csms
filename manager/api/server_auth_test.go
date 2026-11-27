// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/entitlements"
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

// setEntitlements stores an entitlement document for a tenant of newMultiTenant.
func (m *multiTenant) setEntitlements(t *testing.T, orgId, document string) {
	t.Helper()
	ctx := context.Background()
	tenant, err := m.engine.GetTenant(ctx, orgId)
	require.NoError(t, err)
	require.NotNil(t, tenant)
	tenant.EntitlementsVersion++
	tenant.Entitlements = json.RawMessage(document)
	require.NoError(t, m.engine.PutTenant(ctx, tenant))
}

// reportConnectors stores a status for connectors of a charge station, like the
// StatusNotifications of the charge station do.
func (m *multiTenant) reportConnectors(t *testing.T, csId string, connectorIds ...int) {
	t.Helper()
	for _, connectorId := range connectorIds {
		require.NoError(t, m.engine.SetConnectorStatus(context.Background(), csId, connectorId, &store.ConnectorStatus{
			ChargeStationId: csId,
			ConnectorId:     connectorId,
			Status:          store.ConnectorStatusAvailable,
			ErrorCode:       store.ConnectorErrorCodeNoError,
		}))
	}
}

// connectorLimit is an entitlement document with a limit on the connectors of the org.
func connectorLimit(value int, enforcement string) string {
	return fmt.Sprintf(`{"version":1,"status":"active","limits":{"cpms.connectors.max":{"value":%d,"enforcement":%q,"scope":"org"}}}`, value, enforcement)
}

// limitExceededBody is the answer to a registration that a hard limit on the connectors
// does not allow.
func limitExceededBody(limit, current int) string {
	return fmt.Sprintf(`{"status":"Forbidden","error":"entitlement_limit_exceeded: cpms.connectors.max (limit %d, current %d, requested 1)"}`, limit, current)
}

func TestRegisterChargeStationIsRefusedWhenAHardConnectorLimitIsUsedUp(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	m.setEntitlements(t, orgA, connectorLimit(2, "hard"))
	// connector 0 is the charge station as a whole: two connectors are counted
	m.reportConnectors(t, stationOfA, 0, 1, 2)
	r := m.router(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)

	t.Run("a new charge station is not registered", func(t *testing.T) {
		for name, caller := range map[string]*tenancy.Principal{
			"the tenant":  callerOfA,
			"its partner": callerOf(orgPartner, tenancy.OrgScopeSubtree),
		} {
			rr := serve(r, as(caller, http.MethodPost, "/cs/aaaa.NEW", `{"securityProfile":2}`))
			require.Equal(t, http.StatusForbidden, rr.Code, "%s: %s", name, rr.Body.String())
			assert.JSONEq(t, limitExceededBody(2, 2), rr.Body.String(), name)
		}
		got, err := m.engine.LookupChargeStationAuth(ctx, "aaaa.NEW")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("a charge station that is registered is registered again", func(t *testing.T) {
		rr := serve(r, as(callerOfA, http.MethodPost, "/cs/"+stationOfA, `{"securityProfile":2,"base64SHA256Password":"DEADBEEF"}`))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		got, err := m.engine.LookupChargeStationAuth(ctx, stationOfA)
		require.NoError(t, err)
		assert.Equal(t, &store.ChargeStationAuth{
			SecurityProfile:      store.TLSWithClientSideCertificates,
			Base64SHA256Password: "DEADBEEF",
			OrgId:                orgA,
		}, got)
	})

	t.Run("a caller that may not reach the tenant does not learn about the limit", func(t *testing.T) {
		rr := serve(r, as(callerOf(orgB, tenancy.OrgScopeSelf), http.MethodPost, "/cs/aaaa.NEW", `{"securityProfile":2}`))
		require.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())
		assert.JSONEq(t, notFoundBody, rr.Body.String())
	})

	t.Run("a request that is not valid is answered as such", func(t *testing.T) {
		rr := serve(r, as(callerOfA, http.MethodPost, "/cs/aaaa.NEW", `{"securityProfile":128}`))
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
	})

	t.Run("a suspended tenant is told that it is suspended", func(t *testing.T) {
		m.setTenantStatus(t, orgA, store.TenantStatusSuspended)
		defer m.setTenantStatus(t, orgA, store.TenantStatusActive)
		rr := serve(r, as(callerOfA, http.MethodPost, "/cs/aaaa.NEW", `{"securityProfile":2}`))
		require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
		assert.JSONEq(t, suspendedBody, rr.Body.String())
	})

	t.Run("the other tenants are not limited", func(t *testing.T) {
		for caller, csId := range map[string]string{orgB: "bbbb.NEW", orgC: "cccc.NEW"} {
			rr := serve(r, as(callerOf(caller, tenancy.OrgScopeSelf), http.MethodPost, "/cs/"+csId, `{"securityProfile":2}`))
			require.Equal(t, http.StatusCreated, rr.Code, "%s: %s", csId, rr.Body.String())
		}
	})

	t.Run("a higher limit applies to the next request", func(t *testing.T) {
		m.setEntitlements(t, orgA, connectorLimit(3, "hard"))
		rr := serve(r, as(callerOfA, http.MethodPost, "/cs/aaaa.NEW", `{"securityProfile":2}`))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		got, err := m.engine.LookupChargeStationAuth(ctx, "aaaa.NEW")
		require.NoError(t, err)
		assert.Equal(t, &store.ChargeStationAuth{SecurityProfile: store.TLSWithClientSideCertificates, OrgId: orgA}, got)
	})
}

func TestRegisterChargeStationAndTheConnectorLimitOfTheTenant(t *testing.T) {
	cases := []struct {
		name         string
		entitlements string
		// the connectors that the charge station of the tenant has reported
		connectors []int
		wantStatus int
		wantBody   string
	}{
		{name: "a hard limit with room for one", entitlements: connectorLimit(3, "hard"), connectors: []int{0, 1, 2}, wantStatus: http.StatusCreated},
		{name: "a hard limit that is used up", entitlements: connectorLimit(2, "hard"), connectors: []int{0, 1, 2}, wantStatus: http.StatusForbidden, wantBody: limitExceededBody(2, 2)},
		{name: "a hard limit that is exceeded", entitlements: connectorLimit(1, "hard"), connectors: []int{1, 2, 3}, wantStatus: http.StatusForbidden, wantBody: limitExceededBody(1, 3)},
		{name: "a hard limit of zero", entitlements: connectorLimit(0, "hard"), wantStatus: http.StatusForbidden, wantBody: limitExceededBody(0, 0)},
		{name: "only the charge station as a whole has reported", entitlements: connectorLimit(1, "hard"), connectors: []int{0}, wantStatus: http.StatusCreated},
		{name: "a soft limit that is used up", entitlements: connectorLimit(2, "soft"), connectors: []int{0, 1, 2}, wantStatus: http.StatusCreated},
		{name: "a soft limit that is exceeded", entitlements: connectorLimit(0, "soft"), connectors: []int{1, 2, 3}, wantStatus: http.StatusCreated},
		{name: "entitlements without limits", entitlements: `{"version":1,"status":"active","features":{"cpms":true}}`, connectors: []int{1, 2, 3}, wantStatus: http.StatusCreated},
		{name: "entitlements that were never sent", entitlements: `{}`, connectors: []int{1, 2, 3}, wantStatus: http.StatusCreated},
		// a mistake of the console does not stop the tenant
		{name: "entitlements that cannot be read", entitlements: `{"version":1,"limits":{"cpms.connectors.max":0}}`, connectors: []int{1, 2, 3}, wantStatus: http.StatusCreated},
		{name: "a negative limit", entitlements: connectorLimit(-1, "hard"), connectors: []int{1, 2, 3}, wantStatus: http.StatusCreated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMultiTenant(t)
			m.setEntitlements(t, orgA, tc.entitlements)
			m.reportConnectors(t, stationOfA, tc.connectors...)

			rr := serve(m.router(t), as(callerOf(orgA, tenancy.OrgScopeSelf), http.MethodPost, "/cs/aaaa.NEW", `{"securityProfile":2}`))

			require.Equal(t, tc.wantStatus, rr.Code, rr.Body.String())
			got, err := m.engine.LookupChargeStationAuth(context.Background(), "aaaa.NEW")
			require.NoError(t, err)
			if tc.wantStatus == http.StatusCreated {
				assert.NotNil(t, got)
			} else {
				assert.JSONEq(t, tc.wantBody, rr.Body.String())
				assert.Nil(t, got)
			}
		})
	}
}

func TestRegisterChargeStationCountsThePoolOfALimitOfTheSubtree(t *testing.T) {
	m := newMultiTenant(t)
	// the tenants A and B share the two connectors of their partner
	pooled := `{"version":1,"limits":{"cpms.connectors.max":{"value":2,"enforcement":"hard","scope":"subtree","pool_org_id":"` + orgPartner + `"}}}`
	m.setEntitlements(t, orgA, pooled)
	m.setEntitlements(t, orgB, pooled)
	m.reportConnectors(t, stationOfA, 0, 1)
	r := m.router(t)
	register := func(orgId, csId string) *httptest.ResponseRecorder {
		return serve(r, as(callerOf(orgId, tenancy.OrgScopeSelf), http.MethodPost, "/cs/"+csId, `{"securityProfile":2}`))
	}

	rr := register(orgB, "bbbb.NEW1")
	require.Equal(t, http.StatusCreated, rr.Code, "one of two connectors is used: %s", rr.Body.String())

	// a connector of B uses up the pool for A as well
	m.reportConnectors(t, stationOfB, 1)
	for orgId, csId := range map[string]string{orgA: "aaaa.NEW", orgB: "bbbb.NEW2"} {
		rr = register(orgId, csId)
		require.Equal(t, http.StatusForbidden, rr.Code, "%s: %s", csId, rr.Body.String())
		assert.JSONEq(t, limitExceededBody(2, 2), rr.Body.String(), csId)
	}

	// a tenant outside the pool is not limited by it
	rr = register(orgC, "cccc.NEW")
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
}

func TestRegisterChargeStationIsNotLimitedInSingleMode(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()
	ctx := context.Background()
	// a tenant in the store changes nothing in single mode
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: testOrgId, Path: store.OrgPathLabel(testOrgId), Slug: "tenant", OrgKey: "aaaa",
		EntitlementsVersion: 1, Entitlements: json.RawMessage(connectorLimit(0, "hard")),
	}))

	req := httptest.NewRequest(http.MethodPost, "/cs/cs001", strings.NewReader(`{"securityProfile":2}`))
	req.Header.Set("content-type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	got, err := engine.LookupChargeStationAuth(ctx, "cs001")
	require.NoError(t, err)
	assert.NotNil(t, got)
}

// failingCounter cannot count connectors.
type failingCounter struct {
	err error
}

func (f failingCounter) CountConnectorsForOrgs(context.Context, []string) (int, error) {
	return 0, f.err
}

// failingStationLookup is a store that cannot read the auth details of a charge station.
type failingStationLookup struct {
	store.Engine
}

func (failingStationLookup) LookupChargeStationAuth(context.Context, string) (*store.ChargeStationAuth, error) {
	return nil, errors.New("database is down: host db-1.internal")
}

func TestRegisterChargeStationWhenTheConnectorLimitCannotBeChecked(t *testing.T) {
	ctx := context.Background()
	const internalErrorBody = `{"status":"Internal Server Error"}`
	down := errors.New("database is down: host db-1.internal")

	// the operation itself, as the middleware in front of it hands a request over
	register := func(t *testing.T, m *multiTenant, engine store.Engine, enforcer *entitlements.Enforcer, csId string) *httptest.ResponseRecorder {
		t.Helper()
		srv, err := api.NewServer(engine, m.clock, ocpi.NewOCPI(m.engine, nil, "GB", "TWK", testOrgId),
			api.WithScoper(m.scoper), api.WithEntitlements(enforcer))
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/cs/"+csId, strings.NewReader(`{"securityProfile":2}`))
		req.Header.Set("content-type", "application/json")
		rr := httptest.NewRecorder()
		srv.RegisterChargeStation(rr, req.WithContext(tenancy.WithStationOrg(req.Context(), orgA)), csId)
		return rr
	}
	// tenant A has room for one more connector
	newTenants := func(t *testing.T) *multiTenant {
		m := newMultiTenant(t)
		m.setEntitlements(t, orgA, connectorLimit(2, "hard"))
		m.reportConnectors(t, stationOfA, 1)
		return m
	}

	failures := map[string]func(m *multiTenant) *entitlements.Enforcer{
		"the tenant cannot be read": func(m *multiTenant) *entitlements.Enforcer {
			return &entitlements.Enforcer{Tenants: failingTenants{m.engine}, Counter: m.engine, Mode: tenancy.ModeMulti}
		},
		"the connectors cannot be counted": func(m *multiTenant) *entitlements.Enforcer {
			return &entitlements.Enforcer{Tenants: m.engine, Counter: failingCounter{err: down}, Mode: tenancy.ModeMulti}
		},
	}
	for name, enforcer := range failures {
		t.Run(name, func(t *testing.T) {
			m := newTenants(t)

			rr := register(t, m, m.engine, enforcer(m), "aaaa.NEW")
			require.Equal(t, http.StatusInternalServerError, rr.Code)
			// the reason is not for the caller
			assert.JSONEq(t, internalErrorBody, rr.Body.String())
			got, err := m.engine.LookupChargeStationAuth(ctx, "aaaa.NEW")
			require.NoError(t, err)
			assert.Nil(t, got, "a charge station must not be registered without the check of the limit")

			// a charge station that is registered needs no check
			rr = register(t, m, m.engine, enforcer(m), stationOfA)
			require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		})
	}

	t.Run("it is not known whether the charge station is new", func(t *testing.T) {
		m := newTenants(t)
		enforcer := &entitlements.Enforcer{Tenants: m.engine, Counter: m.engine, Mode: tenancy.ModeMulti}

		rr := register(t, m, failingStationLookup{m.engine}, enforcer, "aaaa.NEW")
		require.Equal(t, http.StatusInternalServerError, rr.Code)
		assert.JSONEq(t, internalErrorBody, rr.Body.String())
		got, err := m.engine.LookupChargeStationAuth(ctx, "aaaa.NEW")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("a storage backend that cannot count connectors limits nothing", func(t *testing.T) {
		m := newTenants(t)
		m.reportConnectors(t, stationOfA, 2, 3)
		enforcer := &entitlements.Enforcer{
			Tenants: m.engine,
			Counter: failingCounter{err: fmt.Errorf("CountConnectorsForOrgs: %w", store.ErrNotImplemented)},
			Mode:    tenancy.ModeMulti,
		}

		rr := register(t, m, m.engine, enforcer, "aaaa.NEW")
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	})

	t.Run("a server without an enforcer limits nothing", func(t *testing.T) {
		m := newTenants(t)
		m.reportConnectors(t, stationOfA, 2, 3)

		rr := register(t, m, m.engine, nil, "aaaa.NEW")
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	})
}

func TestSpecNamesTheLimitWhereItIsEnforced(t *testing.T) {
	swagger, err := api.GetSwagger()
	require.NoError(t, err)

	for path, item := range swagger.Paths.Map() {
		for method, operation := range item.Operations() {
			forbidden := operation.Responses.Value("403")
			require.NotNil(t, forbidden, "%s %s", method, path)
			require.NotNil(t, forbidden.Value.Description, "%s %s", method, path)
			assert.Equal(t, method == http.MethodPost && path == "/cs/{csId}",
				strings.Contains(*forbidden.Value.Description, "entitlement_limit_exceeded"), "%s %s", method, path)
		}
	}
}
