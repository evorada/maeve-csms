// SPDX-License-Identifier: Apache-2.0

package controlplane_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/controlplane"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"k8s.io/utils/clock"
)

const (
	orgA   = "0193b2c4-0000-7000-8000-0000000000a1"
	orgB   = "0193b2c4-0000-7000-8000-0000000000b2"
	orgTop = "0193b2c4-0000-7000-8000-0000000000f0"
)

// fixture is a control plane on an in-memory store, behind the middleware that verifies
// the tokens of a platform that the test controls.
type fixture struct {
	platform *testutil.Platform
	engine   *inmemory.Store
	resolver *tenancy.Resolver
	// server can be changed by a test: the handler uses it for every request
	server  *controlplane.Server
	handler http.Handler
	// eventCount numbers the events that a test stores
	eventCount int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	platform := testutil.NewPlatform(t)
	verifier, err := tenancy.NewJWKSVerifier(ctx, tenancy.JWKSVerifierConfig{
		JWKSURL:   platform.JWKSURL(),
		Issuer:    testutil.PlatformIssuer,
		Audience:  testutil.PlatformAudience,
		ClockSkew: 5 * time.Second,
	})
	require.NoError(t, err)

	engine := inmemory.NewStore(clock.RealClock{})
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine}
	srv := &controlplane.Server{
		Tenants:  engine,
		Resolver: resolver,
		Mode:     tenancy.ModeMulti,
		Outbox:   engine,
		Scoper:   &tenancy.Scoper{Mode: tenancy.ModeMulti, Tenants: engine, Resolver: resolver},
	}
	return &fixture{
		platform: platform,
		engine:   engine,
		resolver: resolver,
		server:   srv,
		handler:  tenancy.Middleware(verifier)(controlplane.NewHandler(srv)),
	}
}

// token returns a token of the platform for the subject with the permissions.
func (f *fixture) token(subject string, perms ...string) string {
	claims := f.platform.Claims(time.Now())
	claims["sub"] = subject
	claims["perms"] = append([]string{}, perms...)
	return f.platform.Sign(claims)
}

// provisioner returns the token of a platform service that may manage tenants.
func (f *fixture) provisioner() string {
	return f.token("svc:provisioner", tenancy.PermTenantManage)
}

func (f *fixture) do(method, target, token, body string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rr := httptest.NewRecorder()
	f.handler.ServeHTTP(rr, req)
	return rr
}

// put sends a tenant document as the provisioner.
func (f *fixture) put(orgId, body string) *httptest.ResponseRecorder {
	return f.do(http.MethodPut, "/tenants/"+orgId, f.provisioner(), body)
}

func (f *fixture) tenant(t *testing.T, orgId string) *store.Tenant {
	t.Helper()
	tenant, err := f.engine.GetTenant(context.Background(), orgId)
	require.NoError(t, err)
	return tenant
}

// absent removes a field from the document of tenantDocument.
type absent struct{}

// tenantDocument returns the document of a root tenant with the slug and org key
// "acme" and one hostname, with the given fields changed.
func tenantDocument(t *testing.T, orgId string, changes map[string]any) string {
	t.Helper()
	doc := map[string]any{
		"path":      store.OrgPathLabel(orgId),
		"slug":      "acme",
		"org_key":   "acme",
		"hostnames": []string{"acme.example.com"},
	}
	for field, value := range changes {
		if _, remove := value.(absent); remove {
			delete(doc, field)
			continue
		}
		doc[field] = value
	}
	body, err := json.Marshal(doc)
	require.NoError(t, err)
	return string(body)
}

func decode(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body), rr.Body.String())
	return body
}

func TestOnlyAPlatformServiceThatMayManageTenantsIsLetThrough(t *testing.T) {
	f := newFixture(t)
	document := tenantDocument(t, orgA, nil)

	refused := map[string]struct {
		token  string
		status int
	}{
		"no token":                              {"", http.StatusUnauthorized},
		"not a token":                           {"garbage", http.StatusUnauthorized},
		"a person with the permission":          {f.token("user:alice", tenancy.PermTenantManage), http.StatusForbidden},
		"a person with every permission":        {f.token("user:alice", tenancy.AllPermissions...), http.StatusForbidden},
		"a machine with the permission":         {f.token("machine:ci", tenancy.PermTenantManage), http.StatusForbidden},
		"a service without the permission":      {f.token("svc:provisioner"), http.StatusForbidden},
		"a service with every other permission": {f.token("svc:provisioner", tenancy.ManifestPermissions...), http.StatusForbidden},
		"a subject that only looks like one":    {f.token("user:svc:provisioner", tenancy.PermTenantManage), http.StatusForbidden},
	}
	for name, tc := range refused {
		t.Run(name, func(t *testing.T) {
			rr := f.do(http.MethodGet, "/manifest", tc.token, "")
			require.Equal(t, tc.status, rr.Code)
			if tc.status == http.StatusForbidden {
				assert.JSONEq(t, `{"status":"Forbidden","error":"forbidden"}`, rr.Body.String())
			}

			// nothing is done for such a caller, and it does not learn which paths exist
			assert.Equal(t, tc.status, f.do(http.MethodPut, "/tenants/"+orgA, tc.token, document).Code)
			assert.Equal(t, tc.status, f.do(http.MethodGet, "/no-such-path", tc.token, "").Code)
			assert.Nil(t, f.tenant(t, orgA))
		})
	}

	t.Run("a service with the permission", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, f.do(http.MethodGet, "/manifest", f.provisioner(), "").Code)

		// the org of the token does not matter
		claims := f.platform.Claims(time.Now())
		claims["sub"] = "svc:provisioner"
		claims["org"] = orgB
		claims["org_scope"] = "self"
		claims["perms"] = []string{tenancy.PermTenantManage}
		assert.Equal(t, http.StatusOK, f.do(http.MethodPut, "/tenants/"+orgA, f.platform.Sign(claims), document).Code)
		assert.NotNil(t, f.tenant(t, orgA))

		rr := f.do(http.MethodGet, "/no-such-path", f.provisioner(), "")
		assert.Equal(t, http.StatusNotFound, rr.Code)
		assert.JSONEq(t, `{"status":"Not Found","error":"not_found"}`, rr.Body.String())
		rr = f.do(http.MethodDelete, "/tenants/"+orgA, f.provisioner(), "")
		assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
		assert.JSONEq(t, `{"status":"Method Not Allowed","error":"method_not_allowed"}`, rr.Body.String())
	})
}

func TestARequestThatWasNotAuthenticatedIsRefused(t *testing.T) {
	// without the middleware that verifies tokens in front of it
	handler := controlplane.NewHandler(&controlplane.Server{Tenants: inmemory.NewStore(clock.RealClock{})})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/manifest", nil))
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.JSONEq(t, `{"status":"Unauthorized","error":"unauthenticated"}`, rr.Body.String())
}

func TestManifest(t *testing.T) {
	f := newFixture(t)

	rr := f.do(http.MethodGet, "/manifest", f.provisioner(), "")
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var manifest struct {
		Product      string          `json:"product"`
		Service      string          `json:"service"`
		Version      string          `json:"version"`
		Resources    []string        `json:"resources"`
		Permissions  []string        `json:"permissions"`
		Events       json.RawMessage `json:"events"`
		Meters       json.RawMessage `json:"meters"`
		ConfigSchema map[string]any  `json:"config_schema"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &manifest))
	assert.Equal(t, "cpms", manifest.Product)
	assert.Equal(t, "maeve", manifest.Service)
	assert.NotEmpty(t, manifest.Version)
	assert.Equal(t, []string{"station", "connector", "location", "token", "session"}, manifest.Resources)
	assert.Equal(t, tenancy.ManifestPermissions, manifest.Permissions)
	assert.NotContains(t, manifest.Permissions, tenancy.PermTenantManage)
	assert.Contains(t, manifest.Permissions, tenancy.PermStationWrite)
	// the types of the domain events with the product, as the type of their CloudEvents
	assert.JSONEq(t, `[
		"cpms.station.registered",
		"cpms.station.connected",
		"cpms.station.disconnected",
		"cpms.connector.status_changed",
		"cpms.session.started",
		"cpms.session.updated",
		"cpms.session.completed",
		"cpms.command.completed"
	]`, string(manifest.Events))
	// an empty list, not null
	assert.JSONEq(t, `[]`, string(manifest.Meters))
	// the schema of the tenant config, as the manager itself applies it
	var schema map[string]any
	require.NoError(t, json.Unmarshal(tenancy.ConfigSchema(), &schema))
	assert.Equal(t, schema, manifest.ConfigSchema)
	assert.Equal(t, "http://json-schema.org/draft-07/schema#", manifest.ConfigSchema["$schema"])
	assert.Contains(t, manifest.ConfigSchema["properties"], "default_security_profile")

	t.Run("the version of the build", func(t *testing.T) {
		previous := controlplane.Version
		controlplane.Version = "1.2.3"
		defer func() { controlplane.Version = previous }()
		assert.Equal(t, "1.2.3", controlplane.BuildManifest().Version)
	})

	t.Run("the manifest does not share its permissions with the catalogue", func(t *testing.T) {
		controlplane.BuildManifest().Permissions[0] = "changed"
		assert.NotEqual(t, "changed", tenancy.ManifestPermissions[0])
	})
}

type healthChecker struct {
	err error
}

func (h healthChecker) Health(context.Context) error {
	return h.err
}

func TestHealth(t *testing.T) {
	f := newFixture(t)

	t.Run("a store without a health check", func(t *testing.T) {
		rr := f.do(http.MethodGet, "/health", f.provisioner(), "")
		require.Equal(t, http.StatusOK, rr.Code)
		assert.JSONEq(t, `{"status":"ok","mode":"multi","checks":{}}`, rr.Body.String())
	})

	t.Run("a store that works", func(t *testing.T) {
		f.server.Store = healthChecker{}
		rr := f.do(http.MethodGet, "/health", f.provisioner(), "")
		require.Equal(t, http.StatusOK, rr.Code)
		body := decode(t, rr)
		assert.Equal(t, "ok", body["status"])
		check := body["checks"].(map[string]any)["store"].(map[string]any)
		assert.Equal(t, "ok", check["status"])
		assert.Contains(t, check, "latency_ms")
		assert.NotContains(t, check, "error")
	})

	t.Run("a store that is down", func(t *testing.T) {
		f.server.Store = healthChecker{err: errors.New("failed to connect to host=db.internal user=maeve")}
		rr := f.do(http.MethodGet, "/health", f.provisioner(), "")
		require.Equal(t, http.StatusServiceUnavailable, rr.Code)
		body := decode(t, rr)
		assert.Equal(t, "down", body["status"])
		check := body["checks"].(map[string]any)["store"].(map[string]any)
		assert.Equal(t, "down", check["status"])
		assert.Equal(t, "unavailable", check["error"])
		// why the store is down is not for the caller
		assert.NotContains(t, rr.Body.String(), "db.internal")
	})

	t.Run("a store that does not answer in time", func(t *testing.T) {
		f.server.Store = healthChecker{err: context.DeadlineExceeded}
		rr := f.do(http.MethodGet, "/health", f.provisioner(), "")
		require.Equal(t, http.StatusServiceUnavailable, rr.Code)
		assert.Equal(t, "timeout", decode(t, rr)["checks"].(map[string]any)["store"].(map[string]any)["error"])
	})

	t.Run("single mode", func(t *testing.T) {
		f.server.Store = nil
		f.server.Mode = ""
		rr := f.do(http.MethodGet, "/health", f.provisioner(), "")
		require.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "single", decode(t, rr)["mode"])
	})
}

func TestPutTenantCreatesAndUpdates(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	document := tenantDocument(t, orgA, map[string]any{
		"path":      store.OrgPathLabel(orgTop) + "." + store.OrgPathLabel(orgA),
		"parent_id": orgTop,
		"hostnames": []string{"b.acme.example.com", "a.acme.example.com"},
		"parties": []map[string]any{
			{"id": "party-1", "role": "CPO", "country_code": "DE", "party_id": "ACM"},
			{"id": "party-2", "role": "EMSP", "country_code": "XX", "party_id": "A1B"},
		},
		"entitlements": map[string]any{"org_id": orgA, "version": 3, "status": "active", "features": map[string]bool{"ocpi": true}},
		"config":       map[string]any{"version": 7, "heartbeat_interval_s": 60},
		"added_later":  "a field that this version does not know",
	})

	rr := f.put(orgA, document)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	created := decode(t, rr)
	assert.Equal(t, orgA, created["org_id"])
	assert.Equal(t, "active", created["status"])
	assert.EqualValues(t, 3, created["entitlements_version"])
	assert.EqualValues(t, 7, created["config_version"])
	assert.NotEmpty(t, created["updated_at"])

	tenant := f.tenant(t, orgA)
	require.NotNil(t, tenant)
	assert.Equal(t, store.OrgPathLabel(orgTop)+"."+store.OrgPathLabel(orgA), tenant.Path)
	require.NotNil(t, tenant.ParentId)
	assert.Equal(t, orgTop, *tenant.ParentId)
	assert.Equal(t, "acme", tenant.Slug)
	assert.Equal(t, "acme", tenant.OrgKey)
	assert.Equal(t, []string{"a.acme.example.com", "b.acme.example.com"}, tenant.Hostnames)
	assert.Equal(t, []store.TenantParty{
		{Id: "party-1", Role: "CPO", CountryCode: "DE", PartyId: "ACM"},
		{Id: "party-2", Role: "EMSP", CountryCode: "XX", PartyId: "A1B"},
	}, tenant.Parties)
	assert.JSONEq(t, `{"org_id":"`+orgA+`","version":3,"status":"active","features":{"ocpi":true}}`, string(tenant.Entitlements))
	assert.JSONEq(t, `{"version":7,"heartbeat_interval_s":60}`, string(tenant.Config))

	t.Run("the same document again changes nothing", func(t *testing.T) {
		rr := f.put(orgA, document)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		again := decode(t, rr)
		delete(created, "updated_at")
		delete(again, "updated_at")
		assert.Equal(t, created, again)

		after := f.tenant(t, orgA)
		after.UpdatedAt = tenant.UpdatedAt
		assert.Equal(t, tenant, after)
	})

	t.Run("the org id in any case is the same tenant", func(t *testing.T) {
		rr := f.put(strings.ToUpper(orgA), document)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, orgA, decode(t, rr)["org_id"])
		tenants, err := f.engine.ListTenants(ctx, "", 10)
		require.NoError(t, err)
		assert.Len(t, tenants, 1)
	})

	t.Run("an older entitlement document and an older config are ignored", func(t *testing.T) {
		rr := f.put(orgA, tenantDocument(t, orgA, map[string]any{
			"path":         store.OrgPathLabel(orgTop) + "." + store.OrgPathLabel(orgA),
			"parent_id":    orgTop,
			"entitlements": map[string]any{"version": 2, "status": "old"},
			"config":       map[string]any{"version": 6},
		}))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		state := decode(t, rr)
		assert.EqualValues(t, 3, state["entitlements_version"])
		assert.EqualValues(t, 7, state["config_version"])

		after := f.tenant(t, orgA)
		assert.EqualValues(t, 3, after.EntitlementsVersion)
		assert.JSONEq(t, string(tenant.Entitlements), string(after.Entitlements))
		assert.EqualValues(t, 7, after.ConfigVersion)
		assert.JSONEq(t, string(tenant.Config), string(after.Config))
		// what has no version is replaced
		assert.Equal(t, []string{"acme.example.com"}, after.Hostnames)
		assert.Empty(t, after.Parties)
	})

	t.Run("a document without entitlements and config keeps the stored ones", func(t *testing.T) {
		rr := f.put(orgA, tenantDocument(t, orgA, nil))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		after := f.tenant(t, orgA)
		assert.EqualValues(t, 3, after.EntitlementsVersion)
		assert.EqualValues(t, 7, after.ConfigVersion)
		// the tenant moved to the root of the tree
		assert.Nil(t, after.ParentId)
		assert.Equal(t, store.OrgPathLabel(orgA), after.Path)
	})

	t.Run("a newer entitlement document replaces the stored one", func(t *testing.T) {
		rr := f.put(orgA, tenantDocument(t, orgA, map[string]any{"entitlements": map[string]any{"version": 4}}))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.EqualValues(t, 4, decode(t, rr)["entitlements_version"])
		assert.JSONEq(t, `{"version":4}`, string(f.tenant(t, orgA).Entitlements))
	})

	t.Run("the slug and the org key cannot change", func(t *testing.T) {
		for _, field := range []string{"slug", "org_key"} {
			rr := f.put(orgA, tenantDocument(t, orgA, map[string]any{field: "other"}))
			require.Equal(t, http.StatusConflict, rr.Code, field)
			assert.JSONEq(t, `{"status":"Conflict","error":"immutable_field"}`, rr.Body.String())
		}
		after := f.tenant(t, orgA)
		assert.Equal(t, "acme", after.Slug)
		assert.Equal(t, "acme", after.OrgKey)
	})

	t.Run("what belongs to another tenant cannot be taken", func(t *testing.T) {
		other := map[string]any{"slug": "other", "org_key": "other", "hostnames": []string{"other.example.com"}}
		require.Equal(t, http.StatusOK, f.put(orgB, tenantDocument(t, orgB, other)).Code)

		for field, value := range map[string]any{
			"slug":      "acme",
			"org_key":   "acme",
			"hostnames": []string{"other.example.com", "acme.example.com"},
		} {
			changed := map[string]any{"slug": "third", "org_key": "third", "hostnames": []string{"third.example.com"}}
			changed[field] = value
			rr := f.put(orgTop, tenantDocument(t, orgTop, changed))
			require.Equal(t, http.StatusConflict, rr.Code, field)
			assert.JSONEq(t, `{"status":"Conflict","error":"conflict"}`, rr.Body.String())
			assert.Nil(t, f.tenant(t, orgTop), field)
		}

		// and a hostname of another tenant is not taken from it by an update
		rr := f.put(orgB, tenantDocument(t, orgB, map[string]any{"slug": "other", "org_key": "other", "hostnames": []string{"acme.example.com"}}))
		require.Equal(t, http.StatusConflict, rr.Code)
		assert.Equal(t, []string{"other.example.com"}, f.tenant(t, orgB).Hostnames)
	})
}

func TestPutTenantRefusesWhatIsNotValid(t *testing.T) {
	f := newFixture(t)
	labelA, labelB, labelTop := store.OrgPathLabel(orgA), store.OrgPathLabel(orgB), store.OrgPathLabel(orgTop)

	manyHostnames := make([]string, 21)
	for i := range manyHostnames {
		manyHostnames[i] = "h" + string(rune('a'+i)) + ".example.com"
	}
	// 64 orgs above the tenant
	deepPath := ""
	for i := 0; i < 64; i++ {
		deepPath += "0123456789abcdef0123456789abcd" + string("0123456789abcdef"[i/16]) + string("0123456789abcdef"[i%16]) + "."
	}
	deepPath += labelA
	party := func(changes map[string]any) []map[string]any {
		p := map[string]any{"id": "party-1", "role": "CPO", "country_code": "DE", "party_id": "ACM"}
		for field, value := range changes {
			p[field] = value
		}
		return []map[string]any{{"id": "party-0", "role": "EMSP", "country_code": "NL", "party_id": "AAA"}, p}
	}

	cases := []struct {
		name    string
		orgId   string         // orgA when empty
		body    string         // sent as it is when not empty
		changes map[string]any // otherwise the document of orgA with these changes
		field   string
	}{
		{name: "org id is not a UUID", orgId: "acme", field: "org_id"},
		{name: "org id without hyphens", orgId: labelA, field: "org_id"},
		{name: "org id in braces", orgId: "{" + orgA + "}", field: "org_id"},
		{name: "org id with a suffix", orgId: orgA + ":archive", field: "org_id"},
		{name: "no body", body: " ", field: "body"},
		{name: "body is not JSON", body: `{"path":`, field: "body"},
		{name: "body is not an object", body: `["` + labelA + `"]`, field: "body"},
		{name: "two documents", body: `{} {}`, field: "body"},
		{name: "empty document", body: `{}`, field: "path"},
		{name: "path missing", changes: map[string]any{"path": absent{}}, field: "path"},
		{name: "path in uppercase", changes: map[string]any{"path": strings.ToUpper(labelA)}, field: "path"},
		{name: "path with hyphens", changes: map[string]any{"path": orgA}, field: "path"},
		{name: "path with a wildcard", changes: map[string]any{"path": "*." + labelA}, field: "path"},
		{name: "path of another org", changes: map[string]any{"path": labelB}, field: "path"},
		{name: "path that ends with another org", changes: map[string]any{"path": labelA + "." + labelB, "parent_id": orgA}, field: "path"},
		{name: "path that names the org twice", changes: map[string]any{"path": labelA + "." + labelA, "parent_id": orgA}, field: "path"},
		{name: "path that is too deep", changes: map[string]any{"path": deepPath, "parent_id": orgTop}, field: "path"},
		{name: "path has the wrong type", changes: map[string]any{"path": []string{labelA}}, field: "path"},
		{name: "parent is not a UUID", changes: map[string]any{"path": labelTop + "." + labelA, "parent_id": labelTop}, field: "parent_id"},
		{name: "parent is not the org above", changes: map[string]any{"path": labelTop + "." + labelA, "parent_id": orgB}, field: "parent_id"},
		{name: "parent of a root org", changes: map[string]any{"parent_id": orgTop}, field: "parent_id"},
		{name: "parent missing below another org", changes: map[string]any{"path": labelTop + "." + labelA}, field: "parent_id"},
		{name: "slug missing", changes: map[string]any{"slug": absent{}}, field: "slug"},
		{name: "slug in uppercase", changes: map[string]any{"slug": "Acme"}, field: "slug"},
		{name: "org key missing", changes: map[string]any{"org_key": absent{}}, field: "org_key"},
		{name: "org key in uppercase", changes: map[string]any{"org_key": "K7F2"}, field: "org_key"},
		{name: "org key with a dot", changes: map[string]any{"org_key": "ac.me"}, field: "org_key"},
		{name: "hostnames missing", changes: map[string]any{"hostnames": absent{}}, field: "hostnames"},
		{name: "hostnames null", changes: map[string]any{"hostnames": nil}, field: "hostnames"},
		{name: "hostnames is not a list", changes: map[string]any{"hostnames": "acme.example.com"}, field: "hostnames"},
		{name: "hostname with a port", changes: map[string]any{"hostnames": []string{"acme.example.com:443"}}, field: "hostnames"},
		{name: "hostname in uppercase", changes: map[string]any{"hostnames": []string{"ok.example.com", "Acme.example.com"}}, field: "hostnames"},
		{name: "hostname with a wildcard", changes: map[string]any{"hostnames": []string{"*.example.com"}}, field: "hostnames"},
		{name: "too many hostnames", changes: map[string]any{"hostnames": manyHostnames}, field: "hostnames"},
		{name: "parties is not a list", changes: map[string]any{"parties": map[string]any{}}, field: "parties"},
		{name: "party without an id", changes: map[string]any{"parties": party(map[string]any{"id": ""})}, field: "parties[1].id"},
		{name: "party with a control character in its id", changes: map[string]any{"parties": party(map[string]any{"id": "a\u0000b"})}, field: "parties[1].id"},
		{name: "party with an unknown role", changes: map[string]any{"parties": party(map[string]any{"role": "NSP"})}, field: "parties[1].role"},
		{name: "party with a role in lowercase", changes: map[string]any{"parties": party(map[string]any{"role": "cpo"})}, field: "parties[1].role"},
		{name: "party with a country code in lowercase", changes: map[string]any{"parties": party(map[string]any{"country_code": "de"})}, field: "parties[1].country_code"},
		{name: "party with a long country code", changes: map[string]any{"parties": party(map[string]any{"country_code": "DEU"})}, field: "parties[1].country_code"},
		{name: "party with a short party id", changes: map[string]any{"parties": party(map[string]any{"party_id": "AC"})}, field: "parties[1].party_id"},
		{name: "party with a party id in lowercase", changes: map[string]any{"parties": party(map[string]any{"party_id": "acm"})}, field: "parties[1].party_id"},
		{name: "entitlements is not an object", changes: map[string]any{"entitlements": []int{3}}, field: "entitlements"},
		{name: "entitlements without a version", changes: map[string]any{"entitlements": map[string]any{"status": "active"}}, field: "entitlements.version"},
		{name: "entitlements with a null version", changes: map[string]any{"entitlements": map[string]any{"version": nil}}, field: "entitlements.version"},
		{name: "entitlements with a negative version", changes: map[string]any{"entitlements": map[string]any{"version": -1}}, field: "entitlements.version"},
		{name: "entitlements with a fraction as version", changes: map[string]any{"entitlements": map[string]any{"version": 1.5}}, field: "entitlements.version"},
		{name: "entitlements with a text as version", changes: map[string]any{"entitlements": map[string]any{"version": "3"}}, field: "entitlements.version"},
		{name: "entitlements with a version that is too large", changes: map[string]any{"entitlements": json.RawMessage(`{"version":9223372036854775808}`)}, field: "entitlements.version"},
		{name: "entitlements of another org", changes: map[string]any{"entitlements": map[string]any{"version": 1, "org_id": orgB}}, field: "entitlements.org_id"},
		{name: "entitlements of something that is not an org", changes: map[string]any{"entitlements": map[string]any{"version": 1, "org_id": "acme"}}, field: "entitlements.org_id"},
		{name: "entitlements with an org of the wrong type", changes: map[string]any{"entitlements": map[string]any{"version": 1, "org_id": 7}}, field: "entitlements.org_id"},
		{name: "entitlements with the character U+0000", changes: map[string]any{"entitlements": map[string]any{"version": 1, "status": "a\u0000b"}}, field: "entitlements"},
		{name: "config is not an object", changes: map[string]any{"config": "{}"}, field: "config"},
		{name: "config without a version", changes: map[string]any{"config": map[string]any{}}, field: "config.version"},
		{name: "config with a negative version", changes: map[string]any{"config": map[string]any{"version": -7}}, field: "config.version"},
		{name: "config with the character U+0000 in a key", changes: map[string]any{"config": map[string]any{"version": 1, "a\u0000b": true}}, field: "config"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orgId := tc.orgId
			if orgId == "" {
				orgId = orgA
			}
			body := tc.body
			if body == "" {
				body = tenantDocument(t, orgA, tc.changes)
			}

			rr := f.put(orgId, body)
			require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
			answer := decode(t, rr)
			assert.Equal(t, "Bad Request", answer["status"])
			assert.Equal(t, "invalid_field", answer["error"])
			assert.Equal(t, tc.field, answer["field"], rr.Body.String())
			assert.NotEmpty(t, answer["message"])
		})
	}

	// nothing of all this reached the store
	tenants, err := f.engine.ListTenants(context.Background(), "", 10)
	require.NoError(t, err)
	assert.Empty(t, tenants)

	t.Run("a text with an escaped backslash before u0000 is not the character U+0000", func(t *testing.T) {
		rr := f.put(orgA, tenantDocument(t, orgA, map[string]any{"config": map[string]any{"version": 1, "default_tariff_ref": `\u0000`}}))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	})

	t.Run("a document that is too large", func(t *testing.T) {
		rr := f.put(orgB, tenantDocument(t, orgB, map[string]any{"config": map[string]any{"version": 1, "padding": strings.Repeat("x", 1<<20)}}))
		require.Equal(t, http.StatusRequestEntityTooLarge, rr.Code)
		assert.JSONEq(t, `{"status":"Request Entity Too Large","error":"too_large"}`, rr.Body.String())
		assert.Nil(t, f.tenant(t, orgB))
	})
}

func TestPutTenantRefusesAConfigThatDoesNotFitTheSchema(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	cases := []struct {
		name    string
		config  map[string]any
		field   string
		details []string // the fields of the details; only field when empty
	}{
		{name: "a security profile that does not exist", config: map[string]any{"version": 1, "default_security_profile": "SP4"}, field: "config.default_security_profile"},
		{name: "a security profile as a number", config: map[string]any{"version": 1, "default_security_profile": 0}, field: "config.default_security_profile"},
		{name: "a heartbeat interval that is too short", config: map[string]any{"version": 1, "heartbeat_interval_s": 30}, field: "config.heartbeat_interval_s"},
		{name: "a setting that does not exist", config: map[string]any{"version": 1, "heartbeat_interval": 120}, field: "config.heartbeat_interval"},
		{name: "session rules", config: map[string]any{"version": 1, "session_rules": map[string]any{}}, field: "config.session_rules"},
		{name: "an onboarding mode that does not exist", config: map[string]any{"version": 1, "onboarding_mode": "open"}, field: "config.onboarding_mode"},
		{name: "an organisation name that is empty", config: map[string]any{"version": 1, "accepted_cert_org_names": []string{"Charge Co", ""}}, field: "config.accepted_cert_org_names[1]"},
		{name: "organisation names that are not a list", config: map[string]any{"version": 1, "accepted_cert_org_names": "Charge Co"}, field: "config.accepted_cert_org_names"},
		{
			name:    "more than one setting",
			config:  map[string]any{"version": 1, "onboarding_mode": "open", "heartbeat_interval_s": 30},
			field:   "config.heartbeat_interval_s",
			details: []string{"config.heartbeat_interval_s", "config.onboarding_mode"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := f.put(orgA, tenantDocument(t, orgA, map[string]any{"config": tc.config}))
			require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())

			var answer struct {
				Status  string `json:"status"`
				Error   string `json:"error"`
				Field   string `json:"field"`
				Message string `json:"message"`
				Details []struct {
					Field   string `json:"field"`
					Message string `json:"message"`
				} `json:"details"`
			}
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &answer))
			assert.Equal(t, "Bad Request", answer.Status)
			assert.Equal(t, "invalid_config", answer.Error)
			assert.Equal(t, tc.field, answer.Field, rr.Body.String())
			assert.NotEmpty(t, answer.Message)

			want := tc.details
			if want == nil {
				want = []string{tc.field}
			}
			var got []string
			for _, detail := range answer.Details {
				got = append(got, detail.Field)
				assert.NotEmpty(t, detail.Message)
			}
			assert.Equal(t, want, got, rr.Body.String())

			// nothing of the request was stored: there is no tenant
			assert.Nil(t, f.tenant(t, orgA))
		})
	}

	t.Run("the stored tenant is not touched", func(t *testing.T) {
		valid := map[string]any{"version": 5, "default_security_profile": "SP3", "accepted_cert_org_names": []string{"Charge Co"}}
		require.Equal(t, http.StatusOK, f.put(orgA, tenantDocument(t, orgA, map[string]any{"config": valid})).Code)
		before := f.tenant(t, orgA)
		// the resolver keeps the tenant and its config from now on
		provider := &tenancy.ConfigProvider{Resolver: f.resolver}
		cfg, err := provider.ConfigForOrg(ctx, orgA)
		require.NoError(t, err)
		require.Equal(t, store.TLSWithClientSideCertificates, cfg.SecurityProfile())

		for name, config := range map[string]map[string]any{
			"a newer config":                 {"version": 6, "default_security_profile": "SP0", "accepted_cert_org_names": []string{}},
			"a config that would be ignored": {"version": 4, "default_security_profile": "SP0"},
		} {
			rr := f.put(orgA, tenantDocument(t, orgA, map[string]any{
				"hostnames": []string{"other.example.com"},
				"config":    config,
			}))
			require.Equal(t, http.StatusBadRequest, rr.Code, name)
			assert.Equal(t, "invalid_config", decode(t, rr)["error"], name)
		}

		after := f.tenant(t, orgA)
		assert.EqualValues(t, 5, after.ConfigVersion)
		assert.JSONEq(t, string(before.Config), string(after.Config))
		assert.Equal(t, []string{"acme.example.com"}, after.Hostnames)

		cfg, err = provider.ConfigForOrg(ctx, orgA)
		require.NoError(t, err)
		assert.Equal(t, store.TLSWithClientSideCertificates, cfg.SecurityProfile())
		names, known, err := provider.AcceptedCertOrgNames(ctx, orgA)
		require.NoError(t, err)
		assert.True(t, known)
		assert.Equal(t, []string{"Charge Co"}, names)
	})

	t.Run("a valid config takes effect at once", func(t *testing.T) {
		provider := &tenancy.ConfigProvider{Resolver: f.resolver}
		_, err := provider.ConfigForOrg(ctx, orgA)
		require.NoError(t, err)

		rr := f.put(orgA, tenantDocument(t, orgA, map[string]any{
			"config": map[string]any{"version": 6, "default_security_profile": "SP2", "heartbeat_interval_s": 120},
		}))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		cfg, err := provider.ConfigForOrg(ctx, orgA)
		require.NoError(t, err)
		assert.EqualValues(t, 6, cfg.Version)
		assert.Equal(t, store.TLSWithBasicAuth, cfg.SecurityProfile())
		assert.Equal(t, 2*time.Minute, cfg.HeartbeatInterval(5*time.Minute))
		assert.Empty(t, cfg.AcceptedCertOrgNames)
	})
}

// failingTenants is a tenant store whose every method fails.
type failingTenants struct {
	store.TenantStore
	err error
}

func (f failingTenants) PutTenant(context.Context, *store.Tenant) error {
	return f.err
}

func (f failingTenants) GetTenant(context.Context, string) (*store.Tenant, error) {
	return nil, f.err
}

func (f failingTenants) SetTenantStatus(context.Context, string, store.TenantStatus) (bool, error) {
	return false, f.err
}

func TestStoreFailuresAreNotShownToTheCaller(t *testing.T) {
	f := newFixture(t)
	requests := []struct{ method, target, body string }{
		{http.MethodPut, "/tenants/" + orgA, tenantDocument(t, orgA, nil)},
		{http.MethodPost, "/tenants/" + orgA + ":suspend", ""},
		{http.MethodPost, "/tenants/" + orgA + ":resume", ""},
		{http.MethodGet, "/tenants/" + orgA + "/entitlements", ""},
	}

	t.Run("a storage engine without tenants", func(t *testing.T) {
		f.server.Tenants = failingTenants{err: store.ErrNotImplemented}
		for _, req := range requests {
			rr := f.do(req.method, req.target, f.provisioner(), req.body)
			require.Equal(t, http.StatusNotImplemented, rr.Code, req.target)
			assert.JSONEq(t, `{"status":"Not Implemented","error":"not_implemented"}`, rr.Body.String())
		}
	})

	t.Run("a storage engine that fails", func(t *testing.T) {
		f.server.Tenants = failingTenants{err: errors.New(`pq: relation "tenants" does not exist on db.internal`)}
		for _, req := range requests {
			rr := f.do(req.method, req.target, f.provisioner(), req.body)
			require.Equal(t, http.StatusInternalServerError, rr.Code, req.target)
			assert.JSONEq(t, `{"status":"Internal Server Error","error":"internal_error"}`, rr.Body.String())
		}
	})
}

func TestPutTenantTakesEffectForTheResolver(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	require.Equal(t, http.StatusOK, f.put(orgA, tenantDocument(t, orgA, map[string]any{"hostnames": []string{"old.example.com"}})).Code)
	// the resolver keeps the tenant of the host from now on
	resolution, err := f.resolver.ResolveStation(ctx, "old.example.com", "CP001")
	require.NoError(t, err)
	assert.Equal(t, "acme.CP001", resolution.StationKey)
	_, err = f.resolver.ResolveStation(ctx, "new.example.com", "CP001")
	require.ErrorIs(t, err, tenancy.ErrUnknownHost)

	require.Equal(t, http.StatusOK, f.put(orgA, tenantDocument(t, orgA, map[string]any{"hostnames": []string{"new.example.com"}})).Code)

	resolution, err = f.resolver.ResolveStation(ctx, "new.example.com", "CP001")
	require.NoError(t, err)
	assert.Equal(t, orgA, resolution.OrgId)
	_, err = f.resolver.ResolveStation(ctx, "old.example.com", "CP001")
	assert.ErrorIs(t, err, tenancy.ErrUnknownHost, "the host that the tenant gave up must not resolve from the cache")
}

func TestSuspendAndResume(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token := f.provisioner()

	t.Run("a tenant that does not exist", func(t *testing.T) {
		for _, action := range []string{":suspend", ":resume"} {
			rr := f.do(http.MethodPost, "/tenants/"+orgA+action, token, "")
			require.Equal(t, http.StatusNotFound, rr.Code, action)
			assert.JSONEq(t, `{"status":"Not Found","error":"tenant_not_found"}`, rr.Body.String())
		}
		assert.Nil(t, f.tenant(t, orgA))
	})

	t.Run("an org id that is not a UUID", func(t *testing.T) {
		for _, action := range []string{":suspend", ":resume"} {
			rr := f.do(http.MethodPost, "/tenants/acme"+action, token, "")
			require.Equal(t, http.StatusBadRequest, rr.Code, action)
			assert.Equal(t, "org_id", decode(t, rr)["field"])
		}
	})

	document := tenantDocument(t, orgA, map[string]any{"entitlements": map[string]any{"version": 3}})
	require.Equal(t, http.StatusOK, f.put(orgA, document).Code)
	require.Equal(t, http.StatusOK, f.put(orgB, tenantDocument(t, orgB, map[string]any{"slug": "other", "org_key": "other", "hostnames": []string{}})).Code)
	// the resolver keeps the tenant of the host from now on
	resolution, err := f.resolver.ResolveStation(ctx, "acme.example.com", "CP001")
	require.NoError(t, err)
	require.Equal(t, store.TenantStatusActive, resolution.Tenant.Status)

	for i := 0; i < 2; i++ {
		rr := f.do(http.MethodPost, "/tenants/"+orgA+":suspend", token, "")
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		state := decode(t, rr)
		assert.Equal(t, orgA, state["org_id"])
		assert.Equal(t, "suspended", state["status"])
		assert.EqualValues(t, 3, state["entitlements_version"])
	}
	assert.Equal(t, store.TenantStatusSuspended, f.tenant(t, orgA).Status)
	assert.Equal(t, store.TenantStatusActive, f.tenant(t, orgB).Status, "another tenant is not touched")

	// the resolver does not answer from what it kept before
	resolution, err = f.resolver.ResolveStation(ctx, "acme.example.com", "CP001")
	require.NoError(t, err, "a suspended tenant still resolves: its charge stations stay connected")
	assert.Equal(t, store.TenantStatusSuspended, resolution.Tenant.Status)

	t.Run("an update does not resume the tenant", func(t *testing.T) {
		rr := f.put(orgA, document)
		require.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "suspended", decode(t, rr)["status"])
		assert.Equal(t, store.TenantStatusSuspended, f.tenant(t, orgA).Status)
	})

	for i := 0; i < 2; i++ {
		rr := f.do(http.MethodPost, "/tenants/"+strings.ToUpper(orgA)+":resume", token, "")
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, "active", decode(t, rr)["status"])
	}
	assert.Equal(t, store.TenantStatusActive, f.tenant(t, orgA).Status)
	resolution, err = f.resolver.ResolveStation(ctx, "acme.example.com", "CP001")
	require.NoError(t, err)
	assert.Equal(t, store.TenantStatusActive, resolution.Tenant.Status)
}

func TestTenantEntitlements(t *testing.T) {
	f := newFixture(t)
	token := f.provisioner()
	target := "/tenants/" + orgA + "/entitlements"

	t.Run("a tenant that does not exist", func(t *testing.T) {
		rr := f.do(http.MethodGet, target, token, "")
		require.Equal(t, http.StatusNotFound, rr.Code)
		assert.JSONEq(t, `{"status":"Not Found","error":"tenant_not_found"}`, rr.Body.String())
		assert.Empty(t, rr.Header().Get("ETag"))
	})

	t.Run("an org id that is not a UUID", func(t *testing.T) {
		rr := f.do(http.MethodGet, "/tenants/acme/entitlements", token, "")
		require.Equal(t, http.StatusBadRequest, rr.Code)
		assert.Equal(t, "org_id", decode(t, rr)["field"])
	})

	require.Equal(t, http.StatusOK, f.put(orgA, tenantDocument(t, orgA, nil)).Code)
	t.Run("a tenant without entitlements", func(t *testing.T) {
		rr := f.do(http.MethodGet, target, token, "")
		require.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, `"0"`, rr.Header().Get("ETag"))
		assert.JSONEq(t, `{}`, rr.Body.String())
	})

	entitlements := `{"org_id":"` + orgA + `","version":3,"status":"active","features":{"ocpi":true},"limits":{"cpms.connectors.max":{"value":9007199254740993,"enforcement":"hard","scope":"subtree"}},"added_later":[1,2]}`
	require.Equal(t, http.StatusOK, f.put(orgA, tenantDocument(t, orgA, map[string]any{"entitlements": json.RawMessage(entitlements)})).Code)

	rr := f.do(http.MethodGet, target, token, "")
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, `"3"`, rr.Header().Get("ETag"))
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	assert.JSONEq(t, entitlements, rr.Body.String())
	// a number that a float cannot hold comes back as it was sent
	assert.Contains(t, rr.Body.String(), "9007199254740993")

	for _, known := range []string{`"3"`, `W/"3"`, `"2", "3"`, `"2",W/"3"`, `*`} {
		rr := f.do(http.MethodGet, target, token, "", "If-None-Match", known)
		require.Equal(t, http.StatusNotModified, rr.Code, known)
		assert.Equal(t, `"3"`, rr.Header().Get("ETag"), known)
		assert.Empty(t, rr.Body.String(), known)
	}
	for _, other := range []string{`"2"`, `3`, `"33"`, `""`} {
		rr := f.do(http.MethodGet, target, token, "", "If-None-Match", other)
		require.Equal(t, http.StatusOK, rr.Code, other)
		assert.JSONEq(t, entitlements, rr.Body.String(), other)
	}
}

// recorder notes which operation the generated router called, and with which org.
type recorder struct {
	controlplane.Unimplemented
	calls []string
}

func (r *recorder) PutTenant(_ http.ResponseWriter, _ *http.Request, orgId controlplane.OrgId) {
	r.calls = append(r.calls, "put "+orgId)
}

func (r *recorder) SuspendTenant(_ http.ResponseWriter, _ *http.Request, orgId controlplane.OrgId) {
	r.calls = append(r.calls, "suspend "+orgId)
}

func (r *recorder) ResumeTenant(_ http.ResponseWriter, _ *http.Request, orgId controlplane.OrgId) {
	r.calls = append(r.calls, "resume "+orgId)
}

func (r *recorder) GetTenantEntitlements(_ http.ResponseWriter, _ *http.Request, orgId controlplane.OrgId, _ controlplane.GetTenantEntitlementsParams) {
	r.calls = append(r.calls, "entitlements "+orgId)
}

func TestRouterDispatchesTheActionsAfterTheOrgId(t *testing.T) {
	cases := []struct {
		method, target string
		call           string // "" when no operation is called
	}{
		{http.MethodPost, "/tenants/" + orgA + ":suspend", "suspend " + orgA},
		{http.MethodPost, "/tenants/" + orgA + ":resume", "resume " + orgA},
		{http.MethodPut, "/tenants/" + orgA, "put " + orgA},
		{http.MethodGet, "/tenants/" + orgA + "/entitlements", "entitlements " + orgA},
		// an action is not part of the org id of another operation, unless the operation
		// gets it as the org id and refuses it
		{http.MethodPut, "/tenants/" + orgA + ":suspend", "put " + orgA + ":suspend"},
		{http.MethodGet, "/tenants/" + orgA + ":suspend/entitlements", "entitlements " + orgA + ":suspend"},
		{http.MethodGet, "/tenants/" + orgA + ":suspend", ""},
		{http.MethodPost, "/tenants/" + orgA, ""},
		{http.MethodPost, "/tenants/" + orgA + ":archive", ""},
		{http.MethodPost, "/tenants/" + orgA + ":suspend/", ""},
		{http.MethodPost, "/tenants/" + orgA + "/:suspend", ""},
		{http.MethodPost, "/tenants/:suspend", ""},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.target, func(t *testing.T) {
			rec := &recorder{}
			rr := httptest.NewRecorder()
			controlplane.Handler(rec).ServeHTTP(rr, httptest.NewRequest(tc.method, tc.target, nil))
			if tc.call == "" {
				assert.Empty(t, rec.calls)
				assert.Contains(t, []int{http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed}, rr.Code)
				return
			}
			assert.Equal(t, []string{tc.call}, rec.calls)
		})
	}
}

func TestSpecIsServedWhereTheHandlerIsMounted(t *testing.T) {
	swagger, err := controlplane.GetSwagger()
	require.NoError(t, err)
	require.Len(t, swagger.Servers, 1)
	assert.Equal(t, controlplane.BasePath, swagger.Servers[0].URL)
	// every operation needs the token of the platform
	require.Len(t, swagger.Security, 1)
	assert.Contains(t, swagger.Security[0], "bearerAuth")
	for path, item := range swagger.Paths.Map() {
		for method, operation := range item.Operations() {
			assert.Nil(t, operation.Security, "%s %s must not change the security of the API", method, path)
		}
	}
}
