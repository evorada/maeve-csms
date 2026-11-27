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
