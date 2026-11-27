// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/ocpi"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	clockTest "k8s.io/utils/clock/testing"
)

// The orgs of the multi-tenant tests: the tenants A (org key aaaa) and B (org key
// bbbb) are below a partner that is not a tenant itself, the tenant C (org key cccc)
// is on its own.
const (
	orgA       = "0193b2c4-0000-7000-8000-0000000000a1"
	orgB       = "0193b2c4-0000-7000-8000-0000000000b2"
	orgC       = "0193b2c4-0000-7000-8000-0000000000c3"
	orgPartner = "0193b2c4-0000-7000-8000-0000000000f0"

	stationOfA = "aaaa.CP001"
	stationOfB = "bbbb.CP001"
)

// notFoundBody is the one answer to a request for something that does not exist or
// that the caller may not reach.
const notFoundBody = `{"status":"Not Found"}`

// multiTenant is a multi mode manager on an in-memory store with the tenants A, B and
// C and one charge station of A and one of B.
type multiTenant struct {
	engine *inmemory.Store
	clock  *clockTest.FakePassiveClock
	scoper *tenancy.Scoper
}

func newMultiTenant(t *testing.T) *multiTenant {
	t.Helper()
	ctx := context.Background()
	c := clockTest.NewFakePassiveClock(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	engine := inmemory.NewStore(c)

	partner := orgPartner
	for _, tenant := range []*store.Tenant{
		{OrgId: orgA, ParentId: &partner, Path: store.OrgPathLabel(orgPartner) + "." + store.OrgPathLabel(orgA), Slug: "tenant-a", OrgKey: "aaaa"},
		{OrgId: orgB, ParentId: &partner, Path: store.OrgPathLabel(orgPartner) + "." + store.OrgPathLabel(orgB), Slug: "tenant-b", OrgKey: "bbbb"},
		{OrgId: orgC, Path: store.OrgPathLabel(orgC), Slug: "tenant-c", OrgKey: "cccc"},
	} {
		require.NoError(t, engine.PutTenant(ctx, tenant))
	}
	require.NoError(t, engine.SetChargeStationAuth(ctx, stationOfA, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgA}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, stationOfB, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgB}))

	return &multiTenant{
		engine: engine,
		clock:  c,
		scoper: &tenancy.Scoper{
			Mode:         tenancy.ModeMulti,
			Tenants:      engine,
			Resolver:     &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine, DefaultOrgId: testOrgId},
			DefaultOrgId: testOrgId,
		},
	}
}

// router creates the router of the API server of the manager. The caller of a request
// is the principal that the test put in its context with as. Requests are not
// validated against the API spec: ValidationMiddleware takes a query string for a part
// of the last path parameter.
func (m *multiTenant) router(t *testing.T) *chi.Mux {
	t.Helper()
	ocpiApi := ocpi.NewOCPI(m.engine, nil, "GB", "TWK", testOrgId)
	return newRouter(t, m.engine, m.clock, ocpiApi, m.scoper, func(r *http.Request) *tenancy.Principal {
		p, _ := tenancy.FromContext(r.Context())
		return p
	})
}

// callerOf returns a caller of the org with every permission.
func callerOf(orgId string, scope tenancy.OrgScope) *tenancy.Principal {
	return &tenancy.Principal{Subject: "user:test", OrgId: orgId, OrgScope: scope, Perms: tenancy.AllPermissions}
}

// as makes the request a request of the caller; nil is a request without a caller.
func as(p *tenancy.Principal, method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if p != nil {
		req = req.WithContext(tenancy.WithPrincipal(req.Context(), p))
	}
	return req
}

func serve(handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

// spy is what the authorisation lets through.
type spy struct {
	calls      int
	stationOrg string
}

func (s *spy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.calls++
	s.stationOrg, _ = tenancy.StationOrgFromContext(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func permissionIndex(t *testing.T) map[string]string {
	t.Helper()
	swagger, err := api.GetSwagger()
	require.NoError(t, err)
	index, err := api.BuildPermissionIndex(swagger)
	require.NoError(t, err)
	return index
}

// newSpyRouter routes every operation of the index, below basePath, through the
// authorisation to the spy.
func newSpyRouter(scoper *tenancy.Scoper, index map[string]string, basePath string) (*chi.Mux, *spy) {
	next := &spy{}
	authz := api.AuthzMiddleware(scoper, index, basePath)
	operations := chi.NewRouter()
	for operation := range index {
		method, path, _ := strings.Cut(operation, " ")
		operations.Method(method, path, authz(next))
	}
	if basePath == "" {
		return operations, next
	}
	r := chi.NewRouter()
	r.Mount(basePath, operations)
	return r, next
}

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// target returns the path of an operation ("METHOD /spec/path") for a charge station:
// every other path parameter is 1.
func target(operation, csId string) (method, path string) {
	method, path, _ = strings.Cut(operation, " ")
	path = strings.ReplaceAll(path, "{csId}", csId)
	return method, pathParam.ReplaceAllString(path, "1")
}

// stationOperations returns the operations on a charge station that exists.
func stationOperations(index map[string]string) []string {
	var operations []string
	for operation := range index {
		if strings.Contains(operation, "{csId}") && operation != "POST /cs/{csId}" {
			operations = append(operations, operation)
		}
	}
	sort.Strings(operations)
	return operations
}

// A new operation needs a line here: the permission of an operation is a decision,
// not a default.
var wantPermissions = map[string]string{
	"POST /certificate":                              tenancy.PermCertificateWrite,
	"DELETE /certificate/{certificateHash}":          tenancy.PermCertificateWrite,
	"GET /certificate/{certificateHash}":             tenancy.PermStationRead,
	"POST /cs/{csId}":                                tenancy.PermStationWrite,
	"GET /cs/{csId}/auth":                            tenancy.PermStationWrite,
	"POST /cs/{csId}/availability":                   tenancy.PermConnectorCommand,
	"POST /cs/{csId}/cache/clear":                    tenancy.PermConnectorCommand,
	"DELETE /cs/{csId}/certificate":                  tenancy.PermStationWrite,
	"GET /cs/{csId}/certificates":                    tenancy.PermStationRead,
	"POST /cs/{csId}/certificates":                   tenancy.PermStationWrite,
	"GET /cs/{csId}/charging-profile":                tenancy.PermStationRead,
	"POST /cs/{csId}/charging-profile":               tenancy.PermConnectorCommand,
	"DELETE /cs/{csId}/charging-profile/{profileId}": tenancy.PermConnectorCommand,
	"GET /cs/{csId}/composite-schedule":              tenancy.PermStationRead,
	"GET /cs/{csId}/configuration":                   tenancy.PermStationRead,
	"PATCH /cs/{csId}/configuration":                 tenancy.PermStationWrite,
	"GET /cs/{csId}/connectors":                      tenancy.PermStationRead,
	"POST /cs/{csId}/data-transfer":                  tenancy.PermConnectorCommand,
	"POST /cs/{csId}/diagnostics":                    tenancy.PermConnectorCommand,
	"GET /cs/{csId}/diagnostics/status":              tenancy.PermStationRead,
	"POST /cs/{csId}/display-message":                tenancy.PermStationWrite,
	"DELETE /cs/{csId}/display-message/{messageId}":  tenancy.PermStationWrite,
	"GET /cs/{csId}/display-messages":                tenancy.PermStationRead,
	"GET /cs/{csId}/events":                          tenancy.PermStationRead,
	"GET /cs/{csId}/firmware/status":                 tenancy.PermStationRead,
	"POST /cs/{csId}/firmware/update":                tenancy.PermConnectorCommand,
	"GET /cs/{csId}/local-list":                      tenancy.PermStationRead,
	"POST /cs/{csId}/local-list":                     tenancy.PermTokenWrite,
	"GET /cs/{csId}/local-list/version":              tenancy.PermStationRead,
	"POST /cs/{csId}/logs":                           tenancy.PermConnectorCommand,
	"GET /cs/{csId}/logs/status":                     tenancy.PermStationRead,
	"GET /cs/{csId}/meter-values":                    tenancy.PermSessionRead,
	"POST /cs/{csId}/monitoring":                     tenancy.PermStationWrite,
	"POST /cs/{csId}/monitoring/base":                tenancy.PermStationWrite,
	"POST /cs/{csId}/monitoring/level":               tenancy.PermStationWrite,
	"POST /cs/{csId}/monitoring/report":              tenancy.PermConnectorCommand,
	"DELETE /cs/{csId}/monitoring/{monitorId}":       tenancy.PermStationWrite,
	"POST /cs/{csId}/reconfigure":                    tenancy.PermStationWrite,
	"GET /cs/{csId}/reports":                         tenancy.PermStationRead,
	"POST /cs/{csId}/reservation":                    tenancy.PermConnectorCommand,
	"DELETE /cs/{csId}/reservation/{reservationId}":  tenancy.PermConnectorCommand,
	"GET /cs/{csId}/reservations":                    tenancy.PermStationRead,
	"POST /cs/{csId}/reset":                          tenancy.PermConnectorCommand,
	"POST /cs/{csId}/start-transaction":              tenancy.PermConnectorCommand,
	"GET /cs/{csId}/status":                          tenancy.PermStationRead,
	"POST /cs/{csId}/stop-transaction":               tenancy.PermConnectorCommand,
	"GET /cs/{csId}/transaction/{transactionId}":     tenancy.PermSessionRead,
	"GET /cs/{csId}/transactions":                    tenancy.PermSessionRead,
	"POST /cs/{csId}/trigger":                        tenancy.PermConnectorCommand,
	"POST /cs/{csId}/unlock-connector":               tenancy.PermConnectorCommand,
	"GET /cs/{csId}/variables":                       tenancy.PermStationRead,
	"PATCH /cs/{csId}/variables":                     tenancy.PermStationWrite,
	"POST /location/{locationId}":                    tenancy.PermStationWrite,
	"POST /register":                                 tenancy.PermOcpiConnection,
	"GET /token":                                     tenancy.PermTokenRead,
	"POST /token":                                    tenancy.PermTokenWrite,
	"GET /token/{tokenUid}":                          tenancy.PermTokenRead,
}

func TestEveryOperationHasPermission(t *testing.T) {
	swagger, err := api.GetSwagger()
	require.NoError(t, err)
	index, err := api.BuildPermissionIndex(swagger)
	require.NoError(t, err)

	operations := 0
	for path, item := range swagger.Paths.Map() {
		operations += len(item.Operations())

		// the charge station of an operation is always the path parameter csId, which
		// is what the authorisation looks at
		if path == "/cs" || strings.HasPrefix(path, "/cs/") {
			assert.True(t, path == "/cs/{csId}" || strings.HasPrefix(path, "/cs/{csId}/"), "path %s", path)
			assert.Equal(t, 1, strings.Count(path, "{csId}"), "path %s", path)
		} else {
			assert.NotContains(t, path, "{csId}", "path %s", path)
		}
	}
	assert.Len(t, index, operations, "one permission for each operation of the spec")
	assert.Equal(t, wantPermissions, index)
	for operation, permission := range index {
		assert.Contains(t, tenancy.AllPermissions, permission, operation)
	}
}

func TestBuildPermissionIndexRefusesAnOperationWithoutAKnownPermission(t *testing.T) {
	spec := func(extension string) *openapi3.T {
		doc := `
openapi: 3.0.0
info: {title: test, version: 0.0.0}
paths:
  /thing:
    get:
      operationId: getThing
      x-permission: cpms.station.read
      responses: {'200': {description: OK}}
    post:
      operationId: setThing
` + extension + `
      responses: {'201': {description: Created}}
`
		swagger, err := openapi3.NewLoader().LoadFromData([]byte(doc))
		require.NoError(t, err)
		return swagger
	}

	index, err := api.BuildPermissionIndex(spec("      x-permission: cpms.station.write"))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"GET /thing": "cpms.station.read", "POST /thing": "cpms.station.write"}, index)

	for name, extension := range map[string]string{
		"no permission":         "",
		"an empty permission":   "      x-permission: ''",
		"a list of permissions": "      x-permission: [cpms.station.write]",
		"an unknown permission": "      x-permission: cpms.station.wirte",
		"another extension":     "      x-permissions: cpms.station.write",
	} {
		t.Run(name, func(t *testing.T) {
			index, err := api.BuildPermissionIndex(spec(extension))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "POST /thing")
			assert.Nil(t, index)
		})
	}

	_, err = api.BuildPermissionIndex(nil)
	require.Error(t, err)
}

func TestCrossTenantStationAccessIs404(t *testing.T) {
	m := newMultiTenant(t)
	index := permissionIndex(t)
	operations := stationOperations(index)
	require.NotEmpty(t, operations)

	for _, basePath := range []string{"", "/api/v0"} {
		r, next := newSpyRouter(m.scoper, index, basePath)
		callerOfB := callerOf(orgB, tenancy.OrgScopeSelf)

		for _, operation := range operations {
			t.Run(basePath+" "+operation, func(t *testing.T) {
				next.calls = 0

				// the charge station of another tenant
				method, path := target(operation, stationOfA)
				other := serve(r, as(callerOfB, method, basePath+path+"?limit=1", ""))
				require.Equal(t, http.StatusNotFound, other.Code)
				assert.JSONEq(t, notFoundBody, other.Body.String())
				require.Zero(t, next.calls, "the operation must not run")

				// it is answered exactly like a charge station that does not exist
				method, path = target(operation, "bbbb.UNKNOWN")
				unknown := serve(r, as(callerOfB, method, basePath+path+"?limit=1", ""))
				require.Equal(t, http.StatusNotFound, unknown.Code)
				assert.Equal(t, unknown.Body.String(), other.Body.String())
				assert.Equal(t, unknown.Header(), other.Header())
				require.Zero(t, next.calls)

				// the own charge station
				method, path = target(operation, stationOfB)
				own := serve(r, as(callerOfB, method, basePath+path+"?limit=1", ""))
				require.Equal(t, http.StatusNoContent, own.Code)
				require.Equal(t, 1, next.calls)
				assert.Equal(t, orgB, next.stationOrg)
			})
		}
	}
}

func TestRegisterChargeStationIsLimitedToTheTenantOfTheStationKey(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	// the key says tenant A, the charge station was moved to tenant B
	require.NoError(t, m.engine.SetChargeStationAuth(ctx, "aaaa.MOVED", &store.ChargeStationAuth{OrgId: orgB}))
	r, next := newSpyRouter(m.scoper, permissionIndex(t), "")
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	callerOfB := callerOf(orgB, tenancy.OrgScopeSelf)

	tests := map[string]struct {
		caller     *tenancy.Principal
		csId       string
		wantStatus int
		wantOrg    string // of the charge station, when the operation runs
	}{
		"a new charge station of the own tenant":          {callerOfA, "aaaa.NEW", http.StatusNoContent, orgA},
		"a new charge station of another tenant":          {callerOfB, "aaaa.NEW", http.StatusNotFound, ""},
		"a new charge station of no tenant":               {callerOfA, "zzzz.NEW", http.StatusNotFound, ""},
		"a new charge station without an org key":         {callerOfA, "NEW", http.StatusNotFound, ""},
		"an own charge station that exists":               {callerOfA, stationOfA, http.StatusNoContent, orgA},
		"a charge station of another tenant that exists":  {callerOfB, stationOfA, http.StatusNotFound, ""},
		"a moved charge station by its owner":             {callerOfB, "aaaa.MOVED", http.StatusNoContent, orgB},
		"a moved charge station by the tenant of its key": {callerOfA, "aaaa.MOVED", http.StatusNotFound, ""},
		"a station id that is not valid":                  {callerOfA, "aaaa.bad%23id", http.StatusBadRequest, ""},
		"a station id that ends with a dot":               {callerOfA, "aaaa.CP.", http.StatusBadRequest, ""},
		"a station id that is not valid, another tenant":  {callerOfB, "aaaa.bad%23id", http.StatusNotFound, ""},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			next.calls, next.stationOrg = 0, ""

			rr := serve(r, as(tt.caller, http.MethodPost, "/cs/"+tt.csId, `{"securityProfile":0}`))
			require.Equal(t, tt.wantStatus, rr.Code, rr.Body.String())
			if tt.wantStatus == http.StatusNoContent {
				assert.Equal(t, 1, next.calls)
				assert.Equal(t, tt.wantOrg, next.stationOrg)
				return
			}
			assert.Zero(t, next.calls, "the operation must not run")
			if tt.wantStatus == http.StatusNotFound {
				assert.JSONEq(t, notFoundBody, rr.Body.String())
			}
		})
	}
}

// The org of a charge station comes from the store and from its key, never from the
// caller: through the whole API server.
func TestRegisterChargeStationStoresTheOrg(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	require.NoError(t, m.engine.SetChargeStationAuth(ctx, "aaaa.MOVED", &store.ChargeStationAuth{OrgId: orgB}))
	r := m.router(t)
	const body = `{"securityProfile":1,"base64SHA256Password":"DEADBEEF"}`

	t.Run("a new charge station belongs to the tenant of its key", func(t *testing.T) {
		rr := serve(r, as(callerOf(orgPartner, tenancy.OrgScopeSubtree), http.MethodPost, "/cs/aaaa.NEW", body))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

		got, err := m.engine.LookupChargeStationAuth(ctx, "aaaa.NEW")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, orgA, got.OrgId)
	})

	t.Run("registering again keeps the org", func(t *testing.T) {
		rr := serve(r, as(callerOf(orgB, tenancy.OrgScopeSelf), http.MethodPost, "/cs/aaaa.MOVED", body))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

		got, err := m.engine.LookupChargeStationAuth(ctx, "aaaa.MOVED")
		require.NoError(t, err)
		assert.Equal(t, orgB, got.OrgId)
		assert.Equal(t, "DEADBEEF", got.Base64SHA256Password)
	})

	t.Run("another tenant changes nothing", func(t *testing.T) {
		before, err := m.engine.LookupChargeStationAuth(ctx, stationOfA)
		require.NoError(t, err)

		for _, csId := range []string{stationOfA, "aaaa.OTHER"} {
			rr := serve(r, as(callerOf(orgB, tenancy.OrgScopeSelf), http.MethodPost, "/cs/"+csId, body))
			require.Equal(t, http.StatusNotFound, rr.Code)
			assert.JSONEq(t, notFoundBody, rr.Body.String())
		}

		after, err := m.engine.LookupChargeStationAuth(ctx, stationOfA)
		require.NoError(t, err)
		assert.Equal(t, before, after)
		created, err := m.engine.LookupChargeStationAuth(ctx, "aaaa.OTHER")
		require.NoError(t, err)
		assert.Nil(t, created)
	})
}

func TestSubtreeScopeReachesTheTenantsBelow(t *testing.T) {
	m := newMultiTenant(t)
	index := permissionIndex(t)
	r, next := newSpyRouter(m.scoper, index, "")
	require.NoError(t, m.engine.SetChargeStationAuth(context.Background(), "cccc.CP001", &store.ChargeStationAuth{OrgId: orgC}))

	reaches := func(p *tenancy.Principal, csId string) bool {
		t.Helper()
		reached := 0
		for _, operation := range stationOperations(index) {
			next.calls = 0
			method, path := target(operation, csId)
			rr := serve(r, as(p, method, path, ""))
			if next.calls == 1 {
				reached++
				continue
			}
			require.Equal(t, http.StatusNotFound, rr.Code, operation)
		}
		require.Contains(t, []int{0, len(stationOperations(index))}, reached, "all operations or none")
		return reached > 0
	}

	// the partner is not a tenant here: its scope is what is below it
	partner := callerOf(orgPartner, tenancy.OrgScopeSubtree)
	assert.True(t, reaches(partner, stationOfA))
	assert.True(t, reaches(partner, stationOfB))
	assert.False(t, reaches(partner, "cccc.CP001"))

	partnerSelf := callerOf(orgPartner, tenancy.OrgScopeSelf)
	assert.False(t, reaches(partnerSelf, stationOfA))
	assert.False(t, reaches(partnerSelf, stationOfB))

	// the subtree of a tenant does not go up or sideways
	tenantA := callerOf(orgA, tenancy.OrgScopeSubtree)
	assert.True(t, reaches(tenantA, stationOfA))
	assert.False(t, reaches(tenantA, stationOfB))

	// a scope that is not known reaches nothing
	assert.False(t, reaches(callerOf(orgA, tenancy.OrgScope("all")), stationOfA))
}

func TestMissingPermissionIs403(t *testing.T) {
	m := newMultiTenant(t)
	index := permissionIndex(t)
	r, next := newSpyRouter(m.scoper, index, "")

	for operation, permission := range index {
		t.Run(operation, func(t *testing.T) {
			next.calls = 0
			// every permission but the one of the operation
			p := callerOf(orgB, tenancy.OrgScopeSelf)
			p.Perms = nil
			for _, perm := range tenancy.AllPermissions {
				if perm != permission {
					p.Perms = append(p.Perms, perm)
				}
			}

			// the answer does not depend on whose charge station it is
			for _, csId := range []string{stationOfB, stationOfA, "bbbb.UNKNOWN"} {
				method, path := target(operation, csId)
				rr := serve(r, as(p, method, path, ""))
				require.Equal(t, http.StatusForbidden, rr.Code)
				assert.JSONEq(t, `{"status":"Forbidden","error":"missing permission `+permission+`"}`, rr.Body.String())
			}
			assert.Zero(t, next.calls, "the operation must not run")

			// with the permission alone the operation runs
			p.Perms = []string{permission}
			method, path := target(operation, stationOfB)
			rr := serve(r, as(p, method, path, ""))
			require.Equal(t, http.StatusNoContent, rr.Code)
			assert.Equal(t, 1, next.calls)
		})
	}
}

func TestAuthorisationDeniesByDefault(t *testing.T) {
	m := newMultiTenant(t)
	index := permissionIndex(t)
	callerOfB := callerOf(orgB, tenancy.OrgScopeSelf)

	t.Run("a request without a caller", func(t *testing.T) {
		r, next := newSpyRouter(m.scoper, index, "")
		for _, path := range []string{"/token", "/cs/" + stationOfB + "/status"} {
			rr := serve(r, as(nil, http.MethodGet, path, ""))
			require.Equal(t, http.StatusUnauthorized, rr.Code)
			assert.JSONEq(t, `{"status":"Unauthorized"}`, rr.Body.String())
		}
		assert.Zero(t, next.calls)
	})

	t.Run("an operation that is not in the index", func(t *testing.T) {
		next := &spy{}
		authz := api.AuthzMiddleware(m.scoper, index, "")
		r := chi.NewRouter()
		r.Method(http.MethodGet, "/cs/{csId}/not-in-the-spec", authz(next))
		r.Method(http.MethodGet, "/not-in-the-spec", authz(next))
		// the path is in the spec, the method is not
		r.Method(http.MethodDelete, "/token", authz(next))
		r.Method(http.MethodPut, "/cs/{csId}/status", authz(next))

		for _, tc := range []struct{ method, path string }{
			{http.MethodGet, "/cs/" + stationOfB + "/not-in-the-spec"},
			{http.MethodGet, "/not-in-the-spec"},
			{http.MethodDelete, "/token"},
			{http.MethodPut, "/cs/" + stationOfB + "/status"},
		} {
			rr := serve(r, as(callerOfB, tc.method, tc.path, ""))
			require.Equal(t, http.StatusForbidden, rr.Code, "%s %s", tc.method, tc.path)
			assert.JSONEq(t, `{"status":"Forbidden","error":"operation not permitted"}`, rr.Body.String())
		}
		assert.Zero(t, next.calls)
	})

	t.Run("an index without the operation", func(t *testing.T) {
		r, next := newSpyRouter(m.scoper, map[string]string{"GET /token": tenancy.PermTokenRead, "GET /cs/{csId}/status": ""}, "")
		assert.Equal(t, http.StatusNoContent, serve(r, as(callerOfB, http.MethodGet, "/token", "")).Code)
		require.Equal(t, 1, next.calls)

		// an operation without a permission is not an operation without a check
		assert.Equal(t, http.StatusForbidden, serve(r, as(callerOfB, http.MethodGet, "/cs/"+stationOfB+"/status", "")).Code)
		assert.Equal(t, 1, next.calls)

		// no index at all
		none, unreached := newSpyRouter(m.scoper, nil, "")
		none.Method(http.MethodGet, "/token", api.AuthzMiddleware(m.scoper, nil, "")(unreached))
		assert.Equal(t, http.StatusForbidden, serve(none, as(callerOfB, http.MethodGet, "/token", "")).Code)
		assert.Zero(t, unreached.calls)
	})

	t.Run("a base path that is not the one of the routes", func(t *testing.T) {
		// mounted at the root, told that it is mounted at /api/v0
		r, next := newSpyRouter(m.scoper, index, "")
		wrong := api.AuthzMiddleware(m.scoper, index, "/api/v0")
		r.Method(http.MethodGet, "/x/token", wrong(next))
		r.Method(http.MethodGet, "/api/v0token", wrong(next))
		for _, path := range []string{"/x/token", "/api/v0token"} {
			assert.Equal(t, http.StatusForbidden, serve(r, as(callerOfB, http.MethodGet, path, "")).Code, path)
		}
		assert.Zero(t, next.calls)
	})

	t.Run("without the route of the request", func(t *testing.T) {
		next := &spy{}
		handler := api.AuthzMiddleware(m.scoper, index, "")(next)
		rr := serve(handler, as(callerOfB, http.MethodGet, "/token", ""))
		assert.Equal(t, http.StatusForbidden, rr.Code)
		assert.Zero(t, next.calls)
	})

	t.Run("a lookup that fails", func(t *testing.T) {
		failing := &tenancy.Scoper{
			Mode:     tenancy.ModeMulti,
			Tenants:  m.engine,
			Resolver: &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: m.engine, Stations: failingStations{}},
		}
		r, next := newSpyRouter(failing, index, "")
		rr := serve(r, as(callerOfB, http.MethodGet, "/cs/"+stationOfB+"/status", ""))
		require.Equal(t, http.StatusInternalServerError, rr.Code)
		// the reason is not for the caller
		assert.JSONEq(t, `{"status":"Internal Server Error"}`, rr.Body.String())
		assert.Zero(t, next.calls)
	})

	t.Run("without a scoper", func(t *testing.T) {
		assert.Panics(t, func() { api.AuthzMiddleware(nil, index, "") })
	})
}

type failingStations struct {
	store.ChargeStationAuthStore
}

func (failingStations) LookupChargeStationAuth(context.Context, string) (*store.ChargeStationAuth, error) {
	return nil, errors.New("database is down: host db-1.internal")
}

func TestSingleModeScope(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clockTest.NewFakePassiveClock(time.Now()))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "CP001", &store.ChargeStationAuth{OrgId: testOrgId}))
	// from before orgs: it belongs to the default org
	require.NoError(t, engine.SetChargeStationAuth(ctx, "CP002", &store.ChargeStationAuth{}))
	// of an org that the manager does not serve in single mode
	require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.CP003", &store.ChargeStationAuth{OrgId: orgA}))
	scoper := &tenancy.Scoper{
		Mode:         tenancy.ModeSingle,
		Resolver:     &tenancy.Resolver{Stations: engine, DefaultOrgId: testOrgId},
		DefaultOrgId: testOrgId,
	}
	index := permissionIndex(t)
	r, next := newSpyRouter(scoper, index, "")

	status := func(p *tenancy.Principal, method, path string) int {
		next.calls = 0
		rr := serve(r, as(p, method, path, ""))
		if next.calls == 1 {
			require.Equal(t, testOrgId, next.stationOrg)
		}
		return rr.Code
	}

	for _, scope := range []tenancy.OrgScope{tenancy.OrgScopeSelf, tenancy.OrgScopeSubtree} {
		defaultCaller := callerOf(testOrgId, scope)
		otherCaller := callerOf(orgA, scope)

		for _, operation := range stationOperations(index) {
			method, path := target(operation, "CP001")
			require.Equal(t, http.StatusNoContent, status(defaultCaller, method, path), operation)
			require.Equal(t, http.StatusNotFound, status(otherCaller, method, path), operation)
		}

		assert.Equal(t, http.StatusNoContent, status(defaultCaller, http.MethodGet, "/cs/CP002/status"))
		assert.Equal(t, http.StatusNotFound, status(defaultCaller, http.MethodGet, "/cs/UNKNOWN/status"))
		assert.Equal(t, http.StatusNotFound, status(defaultCaller, http.MethodGet, "/cs/aaaa.CP003/status"))
		assert.Equal(t, http.StatusNotFound, status(otherCaller, http.MethodGet, "/cs/aaaa.CP003/status"))

		// a new charge station belongs to the default org, whatever its id looks like
		assert.Equal(t, http.StatusNoContent, status(defaultCaller, http.MethodPost, "/cs/NEW"))
		assert.Equal(t, http.StatusNoContent, status(defaultCaller, http.MethodPost, "/cs/aaaa.NEW"))
		assert.Equal(t, http.StatusNotFound, status(otherCaller, http.MethodPost, "/cs/NEW"))
		assert.Equal(t, http.StatusNotFound, status(otherCaller, http.MethodPost, "/cs/aaaa.NEW"))
	}
}

// statusSpy is an API server that records the charge station of a status request.
type statusSpy struct {
	api.Unimplemented
	csIds []string
}

func (s *statusSpy) GetChargeStationStatus(w http.ResponseWriter, _ *http.Request, csId string) {
	s.csIds = append(s.csIds, csId)
	w.WriteHeader(http.StatusNoContent)
}

// The authorisation checks the station key that the operation gets. The generated
// router unescapes the path parameter, also when the router already got it unescaped
// (a path whose escapes are all needed, like %25, is routed in its decoded form), so
// a key that is read as it is in the URL is not the key that the operation works on.
func TestStationKeyIsCheckedAsTheOperationGetsIt(t *testing.T) {
	m := newMultiTenant(t)
	require.NoError(t, m.engine.SetChargeStationAuth(context.Background(), "bbbb.a/b", &store.ChargeStationAuth{OrgId: orgB}))
	// a charge station of B whose key, once unescaped, is the key of the station of A:
	// reading the key in another way than the operation does would let B, who may
	// reach this one, through to the station of A
	require.NoError(t, m.engine.SetChargeStationAuth(context.Background(), "aaaa%2ECP001", &store.ChargeStationAuth{OrgId: orgB}))

	server := &statusSpy{}
	r := chi.NewRouter()
	r.Mount("/api/v0", api.HandlerWithOptions(server, api.ChiServerOptions{
		Middlewares: []api.MiddlewareFunc{api.AuthzMiddleware(m.scoper, permissionIndex(t), "/api/v0")},
	}))

	tests := []struct {
		name     string
		caller   *tenancy.Principal
		rawCsId  string
		wantCsId string // "" when the operation must not run
	}{
		{"the own charge station", callerOf(orgB, tenancy.OrgScopeSelf), "bbbb.CP001", "bbbb.CP001"},
		{"the own charge station, escaped", callerOf(orgB, tenancy.OrgScopeSelf), "bbbb%2ECP001", "bbbb.CP001"},
		{"the own charge station with an escaped slash", callerOf(orgB, tenancy.OrgScopeSelf), "bbbb.a%2Fb", "bbbb.a/b"},
		{"another charge station, escaped", callerOf(orgB, tenancy.OrgScopeSelf), "aaaa%2ECP001", ""},
		{"another charge station, escaped twice", callerOf(orgB, tenancy.OrgScopeSelf), "aaaa%252ECP001", ""},
		{"the look-alike of B by B", callerOf(orgB, tenancy.OrgScopeSelf), "aaaa%25252ECP001", "aaaa%2ECP001"},
		{"another charge station, first letter escaped", callerOf(orgB, tenancy.OrgScopeSelf), "%61aaa.CP001", ""},
		{"the charge station of A by A, escaped", callerOf(orgA, tenancy.OrgScopeSelf), "aaaa%2ECP001", "aaaa.CP001"},
		{"the charge station of A by A, escaped twice", callerOf(orgA, tenancy.OrgScopeSelf), "aaaa%252ECP001", "aaaa.CP001"},
		{"the look-alike of B by A", callerOf(orgA, tenancy.OrgScopeSelf), "aaaa%25252ECP001", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server.csIds = nil
			rr := serve(r, as(tt.caller, http.MethodGet, "/api/v0/cs/"+tt.rawCsId+"/status?x=1", ""))
			if tt.wantCsId == "" {
				require.Equal(t, http.StatusNotFound, rr.Code)
				assert.Empty(t, server.csIds, "the operation must not run")
				return
			}
			require.Equal(t, http.StatusNoContent, rr.Code)
			assert.Equal(t, []string{tt.wantCsId}, server.csIds)
		})
	}
}
