// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
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
		{OrgId: orgA, ParentId: &partner, Path: store.OrgPathLabel(orgPartner) + "." + store.OrgPathLabel(orgA), Slug: "tenant-a", OrgKey: "aaaa",
			Parties: []store.TenantParty{{Id: partyOfA, Role: "CPO", CountryCode: "BE", PartyId: "AAA"}}},
		{OrgId: orgB, ParentId: &partner, Path: store.OrgPathLabel(orgPartner) + "." + store.OrgPathLabel(orgB), Slug: "tenant-b", OrgKey: "bbbb",
			Parties: []store.TenantParty{{Id: partyOfB, Role: "CPO", CountryCode: "NL", PartyId: "BBB"}}},
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
	"POST /certificate":                                 tenancy.PermCertificateWrite,
	"GET /commands/{commandId}":                         tenancy.PermStationRead,
	"POST /commands/{commandId}:cancel":                 tenancy.PermConnectorCommand,
	"DELETE /certificate/{certificateHash}":             tenancy.PermCertificateWrite,
	"GET /certificate/{certificateHash}":                tenancy.PermStationRead,
	"POST /cs/{csId}":                                   tenancy.PermStationWrite,
	"GET /cs/{csId}/auth":                               tenancy.PermStationWrite,
	"POST /cs/{csId}/availability":                      tenancy.PermConnectorCommand,
	"POST /cs/{csId}/cache/clear":                       tenancy.PermConnectorCommand,
	"DELETE /cs/{csId}/certificate":                     tenancy.PermStationWrite,
	"GET /cs/{csId}/certificates":                       tenancy.PermStationRead,
	"POST /cs/{csId}/certificates":                      tenancy.PermStationWrite,
	"GET /cs/{csId}/charging-profile":                   tenancy.PermStationRead,
	"GET /cs/{csId}/commands":                           tenancy.PermStationRead,
	"POST /cs/{csId}/commands":                          tenancy.PermConnectorCommand,
	"POST /cs/{csId}/charging-profile":                  tenancy.PermConnectorCommand,
	"DELETE /cs/{csId}/charging-profile/{profileId}":    tenancy.PermConnectorCommand,
	"GET /cs/{csId}/composite-schedule":                 tenancy.PermStationRead,
	"GET /cs/{csId}/configuration":                      tenancy.PermStationRead,
	"PATCH /cs/{csId}/configuration":                    tenancy.PermStationWrite,
	"GET /cs/{csId}/connectors":                         tenancy.PermStationRead,
	"POST /cs/{csId}/data-transfer":                     tenancy.PermConnectorCommand,
	"POST /cs/{csId}/diagnostics":                       tenancy.PermConnectorCommand,
	"GET /cs/{csId}/diagnostics/status":                 tenancy.PermStationRead,
	"POST /cs/{csId}/display-message":                   tenancy.PermStationWrite,
	"DELETE /cs/{csId}/display-message/{messageId}":     tenancy.PermStationWrite,
	"GET /cs/{csId}/display-messages":                   tenancy.PermStationRead,
	"GET /cs/{csId}/events":                             tenancy.PermStationRead,
	"GET /cs/{csId}/firmware/status":                    tenancy.PermStationRead,
	"POST /cs/{csId}/firmware/update":                   tenancy.PermConnectorCommand,
	"GET /cs/{csId}/local-list":                         tenancy.PermStationRead,
	"POST /cs/{csId}/local-list":                        tenancy.PermTokenWrite,
	"GET /cs/{csId}/local-list/version":                 tenancy.PermStationRead,
	"POST /cs/{csId}/logs":                              tenancy.PermConnectorCommand,
	"GET /cs/{csId}/logs/status":                        tenancy.PermStationRead,
	"GET /cs/{csId}/meter-values":                       tenancy.PermSessionRead,
	"POST /cs/{csId}/monitoring":                        tenancy.PermStationWrite,
	"POST /cs/{csId}/monitoring/base":                   tenancy.PermStationWrite,
	"POST /cs/{csId}/monitoring/level":                  tenancy.PermStationWrite,
	"POST /cs/{csId}/monitoring/report":                 tenancy.PermConnectorCommand,
	"DELETE /cs/{csId}/monitoring/{monitorId}":          tenancy.PermStationWrite,
	"POST /cs/{csId}/reconfigure":                       tenancy.PermStationWrite,
	"GET /cs/{csId}/reports":                            tenancy.PermStationRead,
	"POST /cs/{csId}/reservation":                       tenancy.PermConnectorCommand,
	"DELETE /cs/{csId}/reservation/{reservationId}":     tenancy.PermConnectorCommand,
	"GET /cs/{csId}/reservations":                       tenancy.PermStationRead,
	"POST /cs/{csId}/reset":                             tenancy.PermConnectorCommand,
	"POST /cs/{csId}/start-transaction":                 tenancy.PermConnectorCommand,
	"GET /cs/{csId}/status":                             tenancy.PermStationRead,
	"POST /cs/{csId}/stop-transaction":                  tenancy.PermConnectorCommand,
	"GET /cs/{csId}/transaction/{transactionId}":        tenancy.PermSessionRead,
	"GET /cs/{csId}/transactions":                       tenancy.PermSessionRead,
	"POST /cs/{csId}/trigger":                           tenancy.PermConnectorCommand,
	"POST /cs/{csId}/unlock-connector":                  tenancy.PermConnectorCommand,
	"GET /cs/{csId}/variables":                          tenancy.PermStationRead,
	"PATCH /cs/{csId}/variables":                        tenancy.PermStationWrite,
	"POST /location/{locationId}":                       tenancy.PermStationWrite,
	"GET /locations":                                    tenancy.PermStationRead,
	"POST /locations":                                   tenancy.PermStationWrite,
	"GET /locations/{locationId}":                       tenancy.PermStationRead,
	"PUT /locations/{locationId}":                       tenancy.PermStationWrite,
	"DELETE /locations/{locationId}":                    tenancy.PermStationWrite,
	"GET /locations/{locationId}/ocpi":                  tenancy.PermStationRead,
	"GET /locations/{locationId}/evses":                 tenancy.PermStationRead,
	"POST /locations/{locationId}/evses":                tenancy.PermStationWrite,
	"GET /evses/{evseUid}":                              tenancy.PermStationRead,
	"PUT /evses/{evseUid}":                              tenancy.PermStationWrite,
	"DELETE /evses/{evseUid}":                           tenancy.PermStationWrite,
	"POST /evses/{evseUid}/connectors":                  tenancy.PermStationWrite,
	"PUT /evses/{evseUid}/connectors/{connectorUid}":    tenancy.PermStationWrite,
	"DELETE /evses/{evseUid}/connectors/{connectorUid}": tenancy.PermStationWrite,
	"POST /register":                                    tenancy.PermOcpiConnection,
	"GET /token":                                        tenancy.PermTokenRead,
	"POST /token":                                       tenancy.PermTokenWrite,
	"GET /token/{tokenUid}":                             tenancy.PermTokenRead,
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

// suspendedBody is the one answer to a write for a tenant that is suspended.
const suspendedBody = `{"status":"Forbidden","error":"tenant_suspended"}`

// refusedForSuspendedTenant lists the operations that are refused while the tenant
// that they write for is suspended: the ones that create or change something that the
// tenant owns. Everything else stays possible: reading, commands to a charge station
// and what does not belong to a tenant. A new operation that writes needs a line here:
// what a suspended tenant cannot do is a decision, not a default.
var refusedForSuspendedTenant = []string{
	"DELETE /cs/{csId}/certificate",
	"DELETE /cs/{csId}/display-message/{messageId}",
	"DELETE /cs/{csId}/monitoring/{monitorId}",
	"PATCH /cs/{csId}/configuration",
	"PATCH /cs/{csId}/variables",
	"POST /cs/{csId}",
	"POST /cs/{csId}/certificates",
	"POST /cs/{csId}/display-message",
	"POST /cs/{csId}/local-list",
	"POST /cs/{csId}/monitoring",
	"POST /cs/{csId}/monitoring/base",
	"POST /cs/{csId}/monitoring/level",
	"POST /cs/{csId}/reconfigure",
	"POST /location/{locationId}",
	"POST /locations",
	"PUT /locations/{locationId}",
	"DELETE /locations/{locationId}",
	"POST /locations/{locationId}/evses",
	"PUT /evses/{evseUid}",
	"DELETE /evses/{evseUid}",
	"POST /evses/{evseUid}/connectors",
	"PUT /evses/{evseUid}/connectors/{connectorUid}",
	"DELETE /evses/{evseUid}/connectors/{connectorUid}",
	"POST /token",
}

// setTenantStatus changes the status of a tenant like the control plane does: the
// resolver is told to forget the tenant.
func (m *multiTenant) setTenantStatus(t *testing.T, orgId string, status store.TenantStatus) {
	t.Helper()
	found, err := m.engine.SetTenantStatus(context.Background(), orgId, status)
	require.NoError(t, err)
	require.True(t, found)
	m.scoper.Resolver.Invalidate(orgId)
}

func TestSuspendedTenantCannotWrite(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	index := permissionIndex(t)
	// the operations of the manager, and a spy in their place
	r := m.router(t)
	spied, next := newSpyRouter(m.scoper, index, "")
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	// what the writes that pick their org themselves are sent
	bodies := map[string]string{
		"POST /token":                 tokenBody("NEWTOKEN", ""),
		"POST /location/{locationId}": locationBody("New location", ""),
	}
	// the assets of A that the asset operations write to, with what they are sent
	asset := m.assetsOf(t, orgA, stationOfA)
	assetPaths := asset.paths()
	for operation, body := range asset.bodies() {
		bodies[operation] = body
	}

	m.setTenantStatus(t, orgA, store.TenantStatusSuspended)

	t.Run("every operation with a write permission that is not a read is refused", func(t *testing.T) {
		var writes []string
		for operation, permission := range index {
			if tenancy.WritePermissions[permission] && !strings.HasPrefix(operation, http.MethodGet+" ") {
				writes = append(writes, operation)
			}
		}
		assert.ElementsMatch(t, refusedForSuspendedTenant, writes)
	})

	for operation := range index {
		t.Run(operation, func(t *testing.T) {
			method, path := target(operation, stationOfA)
			next.calls = 0

			if !slices.Contains(refusedForSuspendedTenant, operation) {
				rr := serve(spied, as(callerOfA, method, path, ""))
				require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
				assert.Equal(t, 1, next.calls, "the operation must run")
				return
			}

			if assetPath, ok := assetPaths[operation]; ok {
				path = assetPath
			}
			rr := serve(r, as(callerOfA, method, path, bodies[operation]))
			require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
			assert.JSONEq(t, suspendedBody, rr.Body.String())
			if strings.Contains(operation, "{csId}") {
				// refused before the operation is reached
				rr = serve(spied, as(callerOfA, method, path, ""))
				require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
				assert.JSONEq(t, suspendedBody, rr.Body.String())
				assert.Zero(t, next.calls, "the operation must not run")
			}
		})
	}

	t.Run("a charge station cannot be registered", func(t *testing.T) {
		rr := serve(r, as(callerOfA, http.MethodPost, "/cs/aaaa.NEW", `{"securityProfile":2}`))
		require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
		assert.JSONEq(t, suspendedBody, rr.Body.String())
		// not even with an id that would be refused
		rr = serve(r, as(callerOfA, http.MethodPost, "/cs/aaaa.NOT%20VALID", `{"securityProfile":2}`))
		require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	})

	t.Run("nothing was written", func(t *testing.T) {
		token, err := m.engine.LookupTokenForOrg(ctx, orgA, "NEWTOKEN")
		require.NoError(t, err)
		assert.Nil(t, token)
		location, err := m.engine.LookupLocation(ctx, "1")
		require.NoError(t, err)
		assert.Nil(t, location)
		auth, err := m.engine.LookupChargeStationAuth(ctx, "aaaa.NEW")
		require.NoError(t, err)
		assert.Nil(t, auth)
		auth, err = m.engine.LookupChargeStationAuth(ctx, stationOfA)
		require.NoError(t, err)
		assert.Equal(t, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgA}, auth)
		asset.assertUnchanged(t)
		locations, _, err := m.engine.ListAssetLocations(ctx, store.AssetLocationFilter{OrgIds: []string{orgA}})
		require.NoError(t, err)
		assert.Len(t, locations, 1, "no location was created")
	})

	t.Run("reading through the operations of the manager", func(t *testing.T) {
		require.NoError(t, m.engine.SetToken(ctx, storedToken(orgA, "ONLYA", "of A")))
		for _, path := range []string{"/token", "/token/ONLYA", "/cs/" + stationOfA + "/auth"} {
			rr := serve(r, as(callerOfA, http.MethodGet, path, ""))
			assert.Equal(t, http.StatusOK, rr.Code, "%s: %s", path, rr.Body.String())
		}
	})
}

func TestSuspensionStopsTheWritesForTheSuspendedTenantAndNoOther(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	index := permissionIndex(t)
	r := m.router(t)
	spied, next := newSpyRouter(m.scoper, index, "")
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	callerOfB := callerOf(orgB, tenancy.OrgScopeSelf)
	callerOfC := callerOf(orgC, tenancy.OrgScopeSubtree)
	partner := callerOf(orgPartner, tenancy.OrgScopeSubtree)
	const configuration = "/configuration"

	// the tenants are known to the resolver before one of them is suspended
	rr := serve(r, as(callerOfA, http.MethodPost, "/token", tokenBody("BEFORE", "")))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	rr = serve(spied, as(callerOfA, http.MethodPatch, "/cs/"+stationOfA+configuration, ""))
	require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())

	m.setTenantStatus(t, orgA, store.TenantStatusSuspended)

	tests := map[string]struct {
		req        *http.Request
		wantStatus int
		wantBody   string
	}{
		"the suspended tenant":                             {as(callerOfA, http.MethodPost, "/token", tokenBody("T1", "")), http.StatusForbidden, suspendedBody},
		"the suspended tenant names itself":                {as(callerOfA, http.MethodPost, "/token", tokenBody("T2", orgA)), http.StatusForbidden, suspendedBody},
		"a partner for the suspended tenant":               {as(partner, http.MethodPost, "/token", tokenBody("T3", orgA)), http.StatusForbidden, suspendedBody},
		"a partner for the suspended tenant, a location":   {as(partner, http.MethodPost, "/location/loc-a", locationBody("Of A", orgA)), http.StatusForbidden, suspendedBody},
		"a partner for another tenant":                     {as(partner, http.MethodPost, "/token", tokenBody("T4", orgB)), http.StatusCreated, ""},
		"a partner for another tenant, a location":         {as(partner, http.MethodPost, "/location/loc-b", locationBody("Of B", orgB)), http.StatusCreated, ""},
		"another tenant":                                   {as(callerOfB, http.MethodPost, "/token", tokenBody("T5", "")), http.StatusCreated, ""},
		"a caller that may not reach the suspended tenant": {as(callerOfC, http.MethodPost, "/token", tokenBody("T6", orgA)), http.StatusNotFound, notFoundBody},
		"the suspended tenant for another tenant":          {as(callerOfA, http.MethodPost, "/token", tokenBody("T7", orgB)), http.StatusNotFound, notFoundBody},
		"the suspended tenant without the permission": {
			as(withPerms(callerOf(orgA, tenancy.OrgScopeSelf), tenancy.PermTokenRead), http.MethodPost, "/token", tokenBody("T8", "")),
			http.StatusForbidden, `{"status":"Forbidden","error":"missing permission cpms.token.write"}`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			rr := serve(r, test.req)
			require.Equal(t, test.wantStatus, rr.Code, rr.Body.String())
			if test.wantBody != "" {
				assert.JSONEq(t, test.wantBody, rr.Body.String())
			}
		})
	}

	t.Run("only the tokens and locations of the other tenant were written", func(t *testing.T) {
		tokens, err := m.engine.ListTokens(ctx, 0, 100)
		require.NoError(t, err)
		var got []string
		for _, token := range tokens {
			got = append(got, token.OrgId+"/"+token.Uid)
		}
		assert.ElementsMatch(t, []string{orgA + "/BEFORE", orgB + "/T4", orgB + "/T5"}, got)
		location, err := m.engine.LookupLocation(ctx, "loc-a")
		require.NoError(t, err)
		assert.Nil(t, location)
		location, err = m.engine.LookupLocation(ctx, "loc-b")
		require.NoError(t, err)
		require.NotNil(t, location)
		assert.Equal(t, orgB, location.OrgId)
	})

	// a write to a charge station: 0 is that the operation was not reached
	stationTests := map[string]struct {
		caller     *tenancy.Principal
		csId       string
		wantStatus int
		wantBody   string
	}{
		"the suspended tenant":                             {callerOfA, stationOfA, http.StatusForbidden, suspendedBody},
		"a partner for the suspended tenant":               {partner, stationOfA, http.StatusForbidden, suspendedBody},
		"a partner for another tenant":                     {partner, stationOfB, http.StatusNoContent, ""},
		"another tenant":                                   {callerOfB, stationOfB, http.StatusNoContent, ""},
		"a caller that may not reach the suspended tenant": {callerOfC, stationOfA, http.StatusNotFound, notFoundBody},
		"the suspended tenant for another tenant":          {callerOfA, stationOfB, http.StatusNotFound, notFoundBody},
	}
	for name, test := range stationTests {
		t.Run("a charge station: "+name, func(t *testing.T) {
			next.calls = 0
			rr := serve(spied, as(test.caller, http.MethodPatch, "/cs/"+test.csId+configuration, ""))
			require.Equal(t, test.wantStatus, rr.Code, rr.Body.String())
			if test.wantBody != "" {
				assert.JSONEq(t, test.wantBody, rr.Body.String())
			}
			assert.Equal(t, test.wantStatus == http.StatusNoContent, next.calls == 1)
		})
	}

	t.Run("a cancelled tenant cannot write either", func(t *testing.T) {
		m.setTenantStatus(t, orgC, store.TenantStatusCancelled)
		rr := serve(r, as(callerOfC, http.MethodPost, "/token", tokenBody("T9", "")))
		require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
		assert.JSONEq(t, suspendedBody, rr.Body.String())
	})

	t.Run("a resumed tenant writes again", func(t *testing.T) {
		m.setTenantStatus(t, orgA, store.TenantStatusActive)

		rr := serve(r, as(callerOfA, http.MethodPost, "/token", tokenBody("AFTER", "")))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		rr = serve(r, as(callerOfA, http.MethodPost, "/location/loc-a", locationBody("Of A", "")))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		rr = serve(r, as(callerOfA, http.MethodPost, "/cs/aaaa.NEW", `{"securityProfile":2}`))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		next.calls = 0
		rr = serve(spied, as(callerOfA, http.MethodPatch, "/cs/"+stationOfA+configuration, ""))
		require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
		assert.Equal(t, 1, next.calls)

		token, err := m.engine.LookupTokenForOrg(ctx, orgA, "AFTER")
		require.NoError(t, err)
		assert.NotNil(t, token)
	})
}

// failingTenants cannot read a tenant by its org id.
type failingTenants struct {
	store.TenantStore
}

func (failingTenants) GetTenant(context.Context, string) (*store.Tenant, error) {
	return nil, errors.New("database is down: host db-1.internal")
}

func TestWriteIsRefusedWhenTheStatusOfTheTenantCannotBeRead(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	m.scoper.Resolver.Tenants = failingTenants{m.engine}
	index := permissionIndex(t)
	r := m.router(t)
	spied, next := newSpyRouter(m.scoper, index, "")
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	const internalErrorBody = `{"status":"Internal Server Error"}`

	for name, rr := range map[string]*httptest.ResponseRecorder{
		"a charge station": serve(spied, as(callerOfA, http.MethodPatch, "/cs/"+stationOfA+"/configuration", "")),
		"a token":          serve(r, as(callerOfA, http.MethodPost, "/token", tokenBody("NEWTOKEN", ""))),
		"a location":       serve(r, as(callerOfA, http.MethodPost, "/location/loc-a", locationBody("Of A", ""))),
	} {
		require.Equal(t, http.StatusInternalServerError, rr.Code, name)
		// neither written nor reported as suspended, and the reason is not for the caller
		assert.JSONEq(t, internalErrorBody, rr.Body.String(), name)
	}
	assert.Zero(t, next.calls)
	token, err := m.engine.LookupTokenForOrg(ctx, orgA, "NEWTOKEN")
	require.NoError(t, err)
	assert.Nil(t, token)
	location, err := m.engine.LookupLocation(ctx, "loc-a")
	require.NoError(t, err)
	assert.Nil(t, location)

	// reading and commands do not need the status
	for _, operation := range []string{"GET /cs/{csId}/status", "POST /cs/{csId}/reset", "POST /cs/{csId}/stop-transaction", "GET /token"} {
		next.calls = 0
		method, path := target(operation, stationOfA)
		rr := serve(spied, as(callerOfA, method, path, ""))
		require.Equal(t, http.StatusNoContent, rr.Code, operation)
		assert.Equal(t, 1, next.calls, operation)
	}
}

func TestSingleModeDoesNotLookAtTheStatusOfATenant(t *testing.T) {
	ctx := context.Background()
	c := clockTest.NewFakePassiveClock(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	engine := inmemory.NewStore(c)
	// the control plane stores tenants in single mode too
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{OrgId: testOrgId, Path: store.OrgPathLabel(testOrgId), Slug: "tenant-t", OrgKey: "tttt"}))
	found, err := engine.SetTenantStatus(ctx, testOrgId, store.TenantStatusSuspended)
	require.NoError(t, err)
	require.True(t, found)
	require.NoError(t, engine.SetChargeStationAuth(ctx, "CP001", &store.ChargeStationAuth{OrgId: testOrgId}))
	scoper := &tenancy.Scoper{
		Mode:         tenancy.ModeSingle,
		Tenants:      engine,
		Resolver:     &tenancy.Resolver{Tenants: engine, Stations: engine, DefaultOrgId: testOrgId},
		DefaultOrgId: testOrgId,
	}
	r := newRouter(t, engine, c, nil, scoper, func(r *http.Request) *tenancy.Principal {
		p, _ := tenancy.FromContext(r.Context())
		return p
	})
	spied, next := newSpyRouter(scoper, permissionIndex(t), "")
	p := callerOf(testOrgId, tenancy.OrgScopeSubtree)

	rr := serve(r, as(p, http.MethodPost, "/token", tokenBody("NEWTOKEN", "")))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	rr = serve(spied, as(p, http.MethodPatch, "/cs/CP001/configuration", ""))
	require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
	assert.Equal(t, 1, next.calls)
}

func TestSpecSaysWhichOperationsASuspendedTenantCannotCall(t *testing.T) {
	swagger, err := api.GetSwagger()
	require.NoError(t, err)

	for path, item := range swagger.Paths.Map() {
		for method, operation := range item.Operations() {
			forbidden := operation.Responses.Value("403")
			require.NotNil(t, forbidden, "%s %s", method, path)
			require.NotNil(t, forbidden.Value.Description, "%s %s", method, path)
			assert.Equal(t, slices.Contains(refusedForSuspendedTenant, method+" "+path),
				strings.Contains(*forbidden.Value.Description, "tenant_suspended"), "%s %s", method, path)
		}
	}
}
