// SPDX-License-Identifier: Apache-2.0

package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/controlplane"
	"github.com/thoughtworks/maeve-csms/manager/entitlements"
	"github.com/thoughtworks/maeve-csms/manager/internalapi"
	"github.com/thoughtworks/maeve-csms/manager/server"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"k8s.io/utils/clock"
)

// controlPlaneRoute is an operation of the control plane.
type controlPlaneRoute struct {
	method string
	target string
}

// controlPlaneRoutes returns every route that the handler of the control plane
// registers, as a request for the given org, after checking that they are the
// operations of the spec of the control plane, no more and no less.
func controlPlaneRoutes(t *testing.T, orgId string) []controlPlaneRoute {
	t.Helper()

	var registered []string
	routes, ok := controlplane.NewHandler(&controlplane.Server{}).(chi.Routes)
	require.True(t, ok)
	require.NoError(t, chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		registered = append(registered, method+" "+route)
		return nil
	}))
	sort.Strings(registered)

	swagger, err := controlplane.GetSwagger()
	require.NoError(t, err)
	var specified []string
	for path, item := range swagger.Paths.Map() {
		for method := range item.Operations() {
			specified = append(specified, method+" "+path)
		}
	}
	sort.Strings(specified)
	require.Equal(t, specified, registered)
	require.Len(t, registered, 6, "a new operation: check that the tests of the control plane cover it")

	result := make([]controlPlaneRoute, 0, len(registered))
	for _, route := range registered {
		method, path, _ := strings.Cut(route, " ")
		target := controlplane.BasePath + strings.ReplaceAll(path, "{orgId}", orgId)
		require.NotContains(t, target, "{", "a parameter that this test does not know")
		result = append(result, controlPlaneRoute{method: method, target: target})
	}
	return result
}

// serviceToken returns a token of the console for one of its services.
func serviceToken(console *testutil.Console, subject string, perms ...string) string {
	claims := console.Claims(time.Now())
	claims["sub"] = subject
	claims["perms"] = append([]string{}, perms...)
	return console.Sign(claims)
}

// tenantDocumentOfA is a valid document for the tenant A of the multi-tenant tests,
// with another hostname than the one that the tests give it.
var tenantDocumentOfA = `{"path":"` + store.OrgPathLabel(orgA) + `","slug":"tenant-aaaa","org_key":"aaaa","hostnames":["changed.example.com"]}`

// newMultiTenantInternalHandler creates the handler of the internal listener of a
// multi-tenant manager with the tenant A (org key aaaa, host a.example.com), and the
// console that mints the tokens of its callers.
func newMultiTenantInternalHandler(t *testing.T) (http.Handler, *testutil.Console, *inmemory.Store) {
	t.Helper()
	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, engine.PutTenant(context.Background(), &store.Tenant{
		OrgId: orgA, Path: store.OrgPathLabel(orgA), Slug: "tenant-aaaa", OrgKey: "aaaa", Hostnames: []string{"a.example.com"},
	}))

	console, verifier := newConsoleVerifier(t)
	handler := server.NewInternalHandler(server.InternalDeps{
		Resolver: &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine, DefaultOrgId: store.LegacyOrgId},
		Engine:   engine,
		Verifier: verifier,
	})
	return handler, console, engine
}

func send(handler http.Handler, route controlPlaneRoute, token string) *httptest.ResponseRecorder {
	body := ""
	if route.method == http.MethodPut {
		body = tenantDocumentOfA
	}
	req := httptest.NewRequest(route.method, route.target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestControlPlaneRefusesEveryCallerButAConsoleServiceThatMayManageTenants(t *testing.T) {
	handler, console, engine := newMultiTenantInternalHandler(t)
	routes := controlPlaneRoutes(t, orgA)

	everyPermission := append([]string{}, tenancy.AllPermissions...)
	expired := console.Claims(time.Now().Add(-time.Hour))
	expired["sub"] = "svc:provisioner"
	expired["perms"] = everyPermission
	otherConsole := testutil.NewConsole(t)

	callers := []struct {
		name   string
		token  string
		status int
	}{
		{"no token", "", http.StatusUnauthorized},
		{"an expired token of a service with the permission", console.Sign(expired), http.StatusUnauthorized},
		{"a token of another console", serviceToken(otherConsole, "svc:provisioner", everyPermission...), http.StatusUnauthorized},
		{"a person with every permission", serviceToken(console, "user:alice", everyPermission...), http.StatusForbidden},
		{"a machine with every permission", serviceToken(console, "machine:ci", everyPermission...), http.StatusForbidden},
		{"a service with every permission but the one to manage tenants", serviceToken(console, "svc:provisioner", tenancy.ManifestPermissions...), http.StatusForbidden},
		{"a service without permissions", serviceToken(console, "svc:provisioner"), http.StatusForbidden},
	}
	for _, caller := range callers {
		t.Run(caller.name, func(t *testing.T) {
			for _, route := range routes {
				rr := send(handler, route, caller.token)
				assert.Equal(t, caller.status, rr.Code, "%s %s", route.method, route.target)
			}

			// also where the control plane serves nothing
			for _, target := range []string{"", "/", "/no-such-path", "/tenants", "/tenants/" + orgA + ":archive", "/manifest/"} {
				for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
					rr := send(handler, controlPlaneRoute{method: method, target: controlplane.BasePath + target}, caller.token)
					assert.Equal(t, caller.status, rr.Code, "%s %s", method, target)
				}
			}
		})
	}

	// none of these requests changed the tenant
	tenant, err := engine.GetTenant(context.Background(), orgA)
	require.NoError(t, err)
	assert.Equal(t, store.TenantStatusActive, tenant.Status)
	assert.Equal(t, []string{"a.example.com"}, tenant.Hostnames)

	t.Run("a service that may manage tenants", func(t *testing.T) {
		token := serviceToken(console, "svc:provisioner", tenancy.PermTenantManage)
		for _, route := range routes {
			rr := send(handler, route, token)
			assert.Equal(t, http.StatusOK, rr.Code, "%s %s: %s", route.method, route.target, rr.Body.String())
		}

		tenant, err := engine.GetTenant(context.Background(), orgA)
		require.NoError(t, err)
		assert.Equal(t, []string{"changed.example.com"}, tenant.Hostnames)

		rr := send(handler, controlPlaneRoute{http.MethodGet, controlplane.BasePath + "/health"}, token)
		assert.JSONEq(t, `{"status":"ok","mode":"multi","checks":{}}`, rr.Body.String())
		rr = send(handler, controlPlaneRoute{http.MethodGet, controlplane.BasePath + "/no-such-path"}, token)
		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestControlPlaneChangesWhatTheGatewayLookupsAnswer(t *testing.T) {
	handler, console, _ := newMultiTenantInternalHandler(t)
	token := serviceToken(console, "svc:provisioner", tenancy.PermTenantManage)
	// the answer for a charge station that is not registered says whether its host
	// belongs to a tenant
	lookup := func(host string) string {
		rr := get(handler, "/_internal/v1/station-auth?host="+host+"&stationId=CP001", "")
		require.Equal(t, http.StatusNotFound, rr.Code)
		var answer internalapi.StatusResponse
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &answer))
		return answer.Error
	}

	// the lookup keeps the tenant of the host from now on
	require.Equal(t, internalapi.ErrorUnknownStation, lookup("a.example.com"))
	require.Equal(t, internalapi.ErrorUnknownHost, lookup("changed.example.com"))

	rr := send(handler, controlPlaneRoute{http.MethodPut, controlplane.BasePath + "/tenants/" + orgA}, token)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	assert.Equal(t, internalapi.ErrorUnknownStation, lookup("changed.example.com"))
	assert.Equal(t, internalapi.ErrorUnknownHost, lookup("a.example.com"), "the host that the tenant gave up must not be answered from the cache")
}

func TestTenantConfigFromTheControlPlaneReachesTheGatewayLookupAtOnce(t *testing.T) {
	handler, console, engine := newMultiTenantInternalHandler(t)
	token := serviceToken(console, "svc:provisioner", tenancy.PermTenantManage)
	require.NoError(t, engine.SetChargeStationAuth(context.Background(), "aaaa.CP001", &store.ChargeStationAuth{
		SecurityProfile: store.TLSWithClientSideCertificates,
		OrgId:           orgA,
	}))

	putConfig := func(config string) *httptest.ResponseRecorder {
		body := `{"path":"` + store.OrgPathLabel(orgA) + `","slug":"tenant-aaaa","org_key":"aaaa","hostnames":["a.example.com"],"config":` + config + `}`
		req := httptest.NewRequest(http.MethodPut, controlplane.BasePath+"/tenants/"+orgA, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}
	// the organisation names that the gateway is told to accept in a client certificate
	names := func() []string {
		rr := get(handler, "/_internal/v1/station-auth?host=a.example.com&stationId=CP001", "")
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var answer internalapi.StationAuthResponse
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &answer))
		require.NotNil(t, answer.AcceptedCertOrgNames, rr.Body.String())
		return *answer.AcceptedCertOrgNames
	}

	// the lookup keeps the tenant and its config from now on
	require.Empty(t, names())

	rr := putConfig(`{"version":1,"accepted_cert_org_names":["Charge Co"]}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, []string{"Charge Co"}, names())

	// a config that is refused changes nothing: not the stored names, and never to no check
	for _, config := range []string{
		`{"version":2,"accepted_cert_org_names":[""]}`,
		`{"version":2,"accepted_cert_org_names":[],"session_rules":{}}`,
		`{"version":2,"accepted_cert_org_names":null}`,
	} {
		rr = putConfig(config)
		require.Equal(t, http.StatusBadRequest, rr.Code, config)
		assert.Contains(t, rr.Body.String(), `"error":"invalid_config"`, config)
		assert.Equal(t, []string{"Charge Co"}, names(), config)
	}
}

func TestInternalListenerOnlyAuthenticatesTheControlPlane(t *testing.T) {
	handler, console, engine := newMultiTenantInternalHandler(t)
	require.NoError(t, engine.SetChargeStationAuth(context.Background(), "aaaa.CP001", &store.ChargeStationAuth{
		SecurityProfile: store.TLSWithBasicAuth,
		OrgId:           orgA,
	}))

	open := map[string]int{
		"/_internal/v1/station-auth?host=a.example.com&stationId=CP001": http.StatusOK,
		"/_internal/v1/certificates/unknown":                            http.StatusNotFound,
		"/health":                                                       http.StatusOK,
		"/metrics":                                                      http.StatusOK,
		"/api/openapi.json":                                             http.StatusOK,
	}
	check := func(t *testing.T, handler http.Handler, controlPlaneStatus int) {
		t.Helper()
		for target, status := range open {
			rr := get(handler, target, "")
			assert.Equal(t, status, rr.Code, target)
			assert.Empty(t, rr.Header().Get("WWW-Authenticate"), target)
		}
		for _, route := range controlPlaneRoutes(t, orgA) {
			assert.Equal(t, controlPlaneStatus, send(handler, route, "").Code, "%s %s", route.method, route.target)
		}
	}

	t.Run("the gateway, the probes and the scraper need no token", func(t *testing.T) {
		check(t, handler, http.StatusUnauthorized)
	})

	t.Run("and do not need the console to be there", func(t *testing.T) {
		console.SetDown(true)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		verifier, err := tenancy.NewJWKSVerifier(ctx, tenancy.JWKSVerifierConfig{
			JWKSURL:  console.JWKSURL(),
			Issuer:   testutil.ConsoleIssuer,
			Audience: testutil.ConsoleAudience,
		})
		require.NoError(t, err)
		withoutKeys := server.NewInternalHandler(server.InternalDeps{
			Resolver: &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine},
			Engine:   engine,
			Verifier: verifier,
		})
		check(t, withoutKeys, http.StatusUnauthorized)

		// a token cannot be verified without the keys of the console
		token := serviceToken(console, "svc:provisioner", tenancy.PermTenantManage)
		rr := send(withoutKeys, controlPlaneRoute{http.MethodGet, controlplane.BasePath + "/manifest"}, token)
		assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
	})
}

func TestControlPlaneIsClosedInDevMode(t *testing.T) {
	// dev mode accepts every request, as a person with every permission
	engine := inmemory.NewStore(clock.RealClock{})
	handler := server.NewInternalHandler(server.InternalDeps{
		Resolver: &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine},
		Engine:   engine,
		Verifier: devVerifier,
	})

	for _, route := range controlPlaneRoutes(t, orgA) {
		for _, token := range []string{"", "anything"} {
			rr := send(handler, route, token)
			assert.Equal(t, http.StatusForbidden, rr.Code, "%s %s", route.method, route.target)
		}
	}
	tenant, err := engine.GetTenant(context.Background(), orgA)
	require.NoError(t, err)
	assert.Nil(t, tenant)
}

func TestApiHandlerDoesNotServeTheControlPlane(t *testing.T) {
	console, verifier := newConsoleVerifier(t)
	engine := inmemory.NewStore(clock.RealClock{})
	handler := newApiHandler(engine, verifier)
	token := serviceToken(console, "svc:provisioner", tenancy.AllPermissions...)

	routes := append(controlPlaneRoutes(t, orgA),
		controlPlaneRoute{http.MethodGet, controlplane.BasePath},
		controlPlaneRoute{http.MethodGet, controlplane.BasePath + "/"},
		controlPlaneRoute{http.MethodGet, "/api/v0" + controlplane.BasePath + "/manifest"},
	)
	for _, route := range routes {
		for _, token := range []string{"", token} {
			rr := send(handler, route, token)
			if strings.HasPrefix(route.target, "/api/v0") && token == "" {
				assert.Equal(t, http.StatusUnauthorized, rr.Code, "%s %s", route.method, route.target)
				continue
			}
			assert.Equal(t, http.StatusNotFound, rr.Code, "%s %s", route.method, route.target)
		}
	}
	tenant, err := engine.GetTenant(context.Background(), orgA)
	require.NoError(t, err)
	assert.Nil(t, tenant)
}

func TestInternalHandlerNeedsAVerifier(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	assert.PanicsWithValue(t, "server: the internal handler needs a verifier", func() {
		server.NewInternalHandler(server.InternalDeps{Resolver: &tenancy.Resolver{Stations: engine}, Engine: engine})
	})
}

// engineWithHealth is a storage engine that can say whether it works, like the
// PostgreSQL one.
type engineWithHealth struct {
	store.Engine
	err error
}

func (e engineWithHealth) Health(context.Context) error {
	return e.err
}

func TestControlPlaneHealthChecksAStorageEngineThatHasAHealthCheck(t *testing.T) {
	console, verifier := newConsoleVerifier(t)
	token := serviceToken(console, "svc:provisioner", tenancy.PermTenantManage)

	for _, tc := range []struct {
		name   string
		err    error
		status int
		health string
	}{
		{"up", nil, http.StatusOK, "ok"},
		{"down", context.DeadlineExceeded, http.StatusServiceUnavailable, "down"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := engineWithHealth{Engine: inmemory.NewStore(clock.RealClock{}), err: tc.err}
			handler := server.NewInternalHandler(server.InternalDeps{
				Resolver: &tenancy.Resolver{Stations: engine},
				Engine:   engine,
				Verifier: verifier,
			})

			rr := send(handler, controlPlaneRoute{http.MethodGet, controlplane.BasePath + "/health"}, token)
			require.Equal(t, tc.status, rr.Code)
			var health struct {
				Status string `json:"status"`
				Mode   string `json:"mode"`
				Checks map[string]struct {
					Status string `json:"status"`
				} `json:"checks"`
			}
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &health))
			assert.Equal(t, tc.health, health.Status)
			assert.Equal(t, "single", health.Mode)
			assert.Equal(t, tc.health, health.Checks["store"].Status)

			// the health check of the probes does not look at the storage engine
			assert.Equal(t, http.StatusOK, get(handler, "/health", "").Code)
		})
	}
}

// A tenant that the console suspends cannot create or change anything through the API
// from that moment on, and can again from the moment it is resumed: the API does not
// wait until the resolver forgets what it knows about the tenant.
func TestSuspendingATenantStopsItsWritesThroughTheApiAtOnce(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: orgA, Path: store.OrgPathLabel(orgA), Slug: "tenant-aaaa", OrgKey: "aaaa", Hostnames: []string{"a.example.com"},
	}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, stationOfA, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgA}))

	// one resolver behind both listeners, as in the manager. It keeps a tenant for an
	// hour: only the control plane makes it read the tenant again.
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine, DefaultOrgId: store.LegacyOrgId, CacheTTL: time.Hour}
	console, verifier := newConsoleVerifier(t)
	internal := server.NewInternalHandler(server.InternalDeps{Resolver: resolver, Engine: engine, Verifier: verifier})
	apiHandler := server.NewApiHandler(server.ApiDeps{
		Engine:   engine,
		Verifier: verifier,
		Scoper:   &tenancy.Scoper{Mode: tenancy.ModeMulti, Tenants: engine, Resolver: resolver, DefaultOrgId: store.LegacyOrgId},
	})

	provisioner := serviceToken(console, "svc:provisioner", tenancy.PermTenantManage)
	claims := console.Claims(time.Now())
	claims["org"] = orgA
	claims["org_scope"] = "self"
	claims["perms"] = tenancy.ManifestPermissions
	operator := console.Sign(claims)

	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v0"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+operator)
		rr := httptest.NewRecorder()
		apiHandler.ServeHTTP(rr, req)
		return rr
	}
	setToken := func(uid string) *httptest.ResponseRecorder {
		return call(http.MethodPost, "/token", `{"countryCode":"GB","partyId":"TWK","type":"RFID","uid":"`+uid+
			`","contractId":"GBTWK012345678V","issuer":"TWK","valid":true,"cacheMode":"ALWAYS"}`)
	}
	register := func() *httptest.ResponseRecorder {
		return call(http.MethodPost, "/cs/"+stationOfA, `{"securityProfile":2}`)
	}
	setStatus := func(action string, want controlplane.TenantStateStatus) {
		t.Helper()
		rr := send(internal, controlPlaneRoute{http.MethodPost, controlplane.BasePath + "/tenants/" + orgA + ":" + action}, provisioner)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var state controlplane.TenantState
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &state))
		require.Equal(t, want, state.Status)
	}
	const suspended = `{"status":"Forbidden","error":"tenant_suspended"}`

	// the resolver knows the tenant as active from now on
	require.Equal(t, http.StatusCreated, setToken("BEFORE").Code)
	require.Equal(t, http.StatusCreated, register().Code)

	setStatus("suspend", controlplane.TenantStateStatusSuspended)

	for name, rr := range map[string]*httptest.ResponseRecorder{"a token": setToken("WHILE"), "a charge station": register()} {
		require.Equal(t, http.StatusForbidden, rr.Code, name)
		assert.JSONEq(t, suspended, rr.Body.String(), name)
	}
	token, err := engine.LookupTokenForOrg(ctx, orgA, "WHILE")
	require.NoError(t, err)
	assert.Nil(t, token)

	// reading and commands go on, and the gateway still lets the charge station in
	assert.Equal(t, http.StatusOK, call(http.MethodGet, "/token", "").Code)
	assert.Equal(t, http.StatusOK, call(http.MethodGet, "/cs/"+stationOfA+"/auth", "").Code)
	assert.Equal(t, http.StatusAccepted, call(http.MethodPost, "/cs/"+stationOfA+"/reset", `{"type":"Soft"}`).Code)
	assert.Equal(t, http.StatusOK, get(internal, "/_internal/v1/station-auth?host=a.example.com&stationId=CP001", "").Code)

	// suspending again changes nothing
	setStatus("suspend", controlplane.TenantStateStatusSuspended)
	require.Equal(t, http.StatusForbidden, setToken("WHILE").Code)

	setStatus("resume", controlplane.TenantStateStatusActive)

	require.Equal(t, http.StatusCreated, setToken("AFTER").Code)
	require.Equal(t, http.StatusCreated, register().Code)
}

// A hard limit on the connectors that the console sends with the entitlements of a
// tenant stops new charge stations of that tenant from the next request on, and a
// higher limit lets them in again: the API reads the entitlements that are stored, not
// what the resolver keeps of the tenant.
func TestAConnectorLimitFromTheConsoleAppliesToTheApiAtOnce(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	// one resolver behind both listeners, as in the manager. It keeps a tenant for an
	// hour.
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine, DefaultOrgId: store.LegacyOrgId, CacheTTL: time.Hour}
	console, verifier := newConsoleVerifier(t)
	internal := server.NewInternalHandler(server.InternalDeps{Resolver: resolver, Engine: engine, Verifier: verifier})
	apiHandler := server.NewApiHandler(server.ApiDeps{
		Engine:       engine,
		Verifier:     verifier,
		Scoper:       &tenancy.Scoper{Mode: tenancy.ModeMulti, Tenants: engine, Resolver: resolver, DefaultOrgId: store.LegacyOrgId},
		Entitlements: &entitlements.Enforcer{Tenants: engine, Counter: engine, Mode: tenancy.ModeMulti},
	})

	provisioner := serviceToken(console, "svc:provisioner", tenancy.PermTenantManage)
	claims := console.Claims(time.Now())
	claims["org"] = orgA
	claims["org_scope"] = "self"
	claims["perms"] = tenancy.ManifestPermissions
	operator := console.Sign(claims)

	putEntitlements := func(document string) {
		t.Helper()
		body := `{"path":"` + store.OrgPathLabel(orgA) + `","slug":"tenant-aaaa","org_key":"aaaa","hostnames":["a.example.com"],"entitlements":` + document + `}`
		req := httptest.NewRequest(http.MethodPut, controlplane.BasePath+"/tenants/"+orgA, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+provisioner)
		rr := httptest.NewRecorder()
		internal.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	}
	register := func(csId string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v0/cs/"+csId, strings.NewReader(`{"securityProfile":2}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+operator)
		rr := httptest.NewRecorder()
		apiHandler.ServeHTTP(rr, req)
		return rr
	}

	putEntitlements(`{"version":1,"status":"active","features":{"cpms":true},"limits":{"cpms.connectors.max":{"value":2,"enforcement":"hard","scope":"org"}}}`)
	require.Equal(t, http.StatusCreated, register(stationOfA).Code)
	// the charge station reports its two connectors
	for connectorId := 0; connectorId <= 2; connectorId++ {
		require.NoError(t, engine.SetConnectorStatus(ctx, stationOfA, connectorId, &store.ConnectorStatus{
			ChargeStationId: stationOfA, ConnectorId: connectorId, Status: store.ConnectorStatusAvailable,
		}))
	}

	rr := register("aaaa.CP002")
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	assert.JSONEq(t, `{"status":"Forbidden","error":"entitlement_limit_exceeded: cpms.connectors.max (limit 2, current 2, requested 1)"}`, rr.Body.String())
	auth, err := engine.LookupChargeStationAuth(ctx, "aaaa.CP002")
	require.NoError(t, err)
	assert.Nil(t, auth)
	// the charge station that is registered can be registered again, and the gateway
	// still lets it in
	assert.Equal(t, http.StatusCreated, register(stationOfA).Code)
	assert.Equal(t, http.StatusOK, get(internal, "/_internal/v1/station-auth?host=a.example.com&stationId=CP001", "").Code)

	// the same limit, only billed
	putEntitlements(`{"version":2,"limits":{"cpms.connectors.max":{"value":2,"enforcement":"soft","scope":"org"}}}`)
	require.Equal(t, http.StatusCreated, register("aaaa.CP002").Code)

	putEntitlements(`{"version":3,"limits":{"cpms.connectors.max":{"value":2,"enforcement":"hard","scope":"org"}}}`)
	require.Equal(t, http.StatusForbidden, register("aaaa.CP003").Code)

	putEntitlements(`{"version":4,"limits":{"cpms.connectors.max":{"value":3,"enforcement":"hard","scope":"org"}}}`)
	require.Equal(t, http.StatusCreated, register("aaaa.CP003").Code)
}
