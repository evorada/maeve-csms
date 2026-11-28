// SPDX-License-Identifier: Apache-2.0

package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"

	"github.com/thoughtworks/maeve-csms/manager/server"
)

// devVerifier accepts every request, like the manager does in dev mode.
var devVerifier = tenancy.DevVerifier{Principal: tenancy.Principal{
	Subject:  "user:dev",
	OrgId:    store.LegacyOrgId,
	OrgScope: tenancy.OrgScopeSubtree,
	Perms:    tenancy.AllPermissions,
}}

// scoperOf returns the scoper that the manager builds next to its resolver.
func scoperOf(resolver *tenancy.Resolver) *tenancy.Scoper {
	return &tenancy.Scoper{Mode: resolver.Mode, Tenants: resolver.Tenants, Resolver: resolver, DefaultOrgId: resolver.DefaultOrgId}
}

// eventsOf returns the emitter that the manager builds: the domain events go to the
// outbox of the store, with the org that the resolver gives a charge station.
func eventsOf(engine store.Engine, resolver *tenancy.Resolver) events.Emitter {
	return &events.OutboxEmitter{Store: engine, Orgs: resolver, Product: events.Product}
}

// newApiHandler creates the handler of the API listener of a single-tenant manager
// whose only org is the legacy org.
func newApiHandler(engine store.Engine, verifier tenancy.Verifier) http.Handler {
	resolver := &tenancy.Resolver{Stations: engine, DefaultOrgId: store.LegacyOrgId}
	return server.NewApiHandler(server.ApiDeps{
		Engine:   engine,
		Verifier: verifier,
		Scoper:   scoperOf(resolver),
		Events:   eventsOf(engine, resolver),
	})
}

// newPlatformVerifier creates a verifier for the tokens of a platform that the test
// controls.
func newPlatformVerifier(t *testing.T) (*testutil.Platform, tenancy.Verifier) {
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
	return platform, verifier
}

func get(handler http.Handler, target, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestApiNeedsAPlatformToken(t *testing.T) {
	platform, verifier := newPlatformVerifier(t)
	engine := inmemory.NewStore(clock.RealClock{})
	handler := newApiHandler(engine, verifier)

	t.Run("without a token", func(t *testing.T) {
		rr := get(handler, "/api/v0/token", "")
		require.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.Equal(t, `Bearer error="invalid_token"`, rr.Header().Get("WWW-Authenticate"))
		assert.JSONEq(t, `{"status":"Unauthorized","error":"missing bearer token"}`, rr.Body.String())
	})

	t.Run("with a valid token", func(t *testing.T) {
		rr := get(handler, "/api/v0/token", platform.Token(time.Now()))
		require.Equal(t, http.StatusOK, rr.Code)
		assert.JSONEq(t, `[]`, rr.Body.String())
	})

	t.Run("with a token that is not valid", func(t *testing.T) {
		expired := platform.Token(time.Now().Add(-time.Hour))
		rr := get(handler, "/api/v0/token", expired)
		require.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.JSONEq(t, `{"status":"Unauthorized","error":"invalid token"}`, rr.Body.String())

		claims := platform.Claims(time.Now())
		claims["aud"] = "billing"
		rr = get(handler, "/api/v0/token", platform.Sign(claims))
		require.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("nothing below /api/v0 is served without a token", func(t *testing.T) {
		for _, tc := range []struct{ method, target, body string }{
			{http.MethodGet, "/api/v0", ""},
			{http.MethodGet, "/api/v0/", ""},
			{http.MethodGet, "/api/v0/cs/cs001/auth", ""},
			{http.MethodGet, "/api/v0/cs/cs001/transactions", ""},
			{http.MethodGet, "/api/v0/token/ABCDEF", ""},
			{http.MethodGet, "/api/v0/no-such-route", ""},
			{http.MethodPost, "/api/v0/cs/cs001", `{"securityProfile":0}`},
			{http.MethodPost, "/api/v0/token", `{"countryCode":"GB","partyId":"TWK","type":"RFID","uid":"ABCDEF","contractId":"GBTWK012345678V","issuer":"TWK","valid":true,"cacheMode":"ALWAYS"}`},
			// refused before the body is looked at
			{http.MethodPost, "/api/v0/token", `{"not":"a token"}`},
			{http.MethodDelete, "/api/v0/cs/cs001/reservation/1", ""},
			{http.MethodOptions, "/api/v0/token", ""},
			{http.MethodPut, "/api/v0/token", ""},
		} {
			req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			assert.Equal(t, http.StatusUnauthorized, rr.Code, "%s %s", tc.method, tc.target)
		}

		// nothing was written
		tokens, err := engine.ListTokens(context.Background(), 0, 10)
		require.NoError(t, err)
		assert.Empty(t, tokens)
		auth, err := engine.LookupChargeStationAuth(context.Background(), "cs001")
		require.NoError(t, err)
		assert.Nil(t, auth)
	})
}

// The API listener is the one that callers from outside reach. It serves the API to
// callers with a token and nothing else: not the health check, the metrics or the
// OpenAPI document, and not the legacy pages, whatever the configuration says.
func TestApiListenerServesNothingButTheApi(t *testing.T) {
	_, verifier := newPlatformVerifier(t)
	engine := inmemory.NewStore(clock.RealClock{})
	handler := newApiHandler(engine, verifier)

	// what the forms of the admin pages send
	form := url.Values{"csid": {"cs001"}, "auth": {"unsecured"}, "uid": {"ABC123"}}.Encode()

	for _, target := range []string{
		"/health", "/metrics", "/api/openapi.json", "/transactions",
		"/adminui", "/adminui/", "/adminui/connect", "/adminui/token", "/adminui/no-such-page",
		"/", "/api", "/api/", "/openapi.json",
	} {
		for _, method := range []string{
			http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
			http.MethodPatch, http.MethodDelete, http.MethodOptions,
		} {
			req := httptest.NewRequest(method, target, strings.NewReader(form))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			assert.Equal(t, http.StatusNotFound, rr.Code, "%s %s", method, target)
		}
	}

	// nothing was registered
	auth, err := engine.LookupChargeStationAuth(context.Background(), "cs001")
	require.NoError(t, err)
	assert.Nil(t, auth)
	tokens, err := engine.ListTokens(context.Background(), 0, 10)
	require.NoError(t, err)
	assert.Empty(t, tokens)

	// and the API is still there, for callers with a token
	assert.Equal(t, http.StatusUnauthorized, get(handler, "/api/v0/token", "").Code)
}

func TestApiAnswersServiceUnavailableWithoutKeys(t *testing.T) {
	platform, _ := newPlatformVerifier(t)
	token := platform.Token(time.Now())
	platform.SetDown(true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	verifier, err := tenancy.NewJWKSVerifier(ctx, tenancy.JWKSVerifierConfig{
		JWKSURL:  platform.JWKSURL(),
		Issuer:   testutil.PlatformIssuer,
		Audience: testutil.PlatformAudience,
	})
	require.NoError(t, err)
	handler := newApiHandler(inmemory.NewStore(clock.RealClock{}), verifier)

	rr := get(handler, "/api/v0/token", token)
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)
	assert.JSONEq(t, `{"status":"Service Unavailable","error":"auth keys unavailable"}`, rr.Body.String())
}

func TestApiHandlerNeedsAVerifier(t *testing.T) {
	assert.Panics(t, func() { newApiHandler(inmemory.NewStore(clock.RealClock{}), nil) })
}

func TestApiHandlerNeedsAScoper(t *testing.T) {
	assert.Panics(t, func() {
		server.NewApiHandler(server.ApiDeps{Engine: inmemory.NewStore(clock.RealClock{}), Verifier: devVerifier})
	})
}

// The tenants of the multi-tenant tests and their charge stations.
const (
	orgA       = "0193b2c4-0000-7000-8000-0000000000a1"
	orgB       = "0193b2c4-0000-7000-8000-0000000000b2"
	stationOfA = "aaaa.CP001"
	stationOfB = "bbbb.CP001"
)

// newMultiTenantApiHandler creates the handler of the API listener of a multi-tenant
// manager with the tenants A (org key aaaa) and B (org key bbbb), one connected charge
// station each, and the platform that mints the tokens of its callers.
func newMultiTenantApiHandler(t *testing.T) (http.Handler, *testutil.Platform, *inmemory.Store) {
	t.Helper()
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	for orgId, orgKey := range map[string]string{orgA: "aaaa", orgB: "bbbb"} {
		require.NoError(t, engine.PutTenant(ctx, &store.Tenant{OrgId: orgId, Path: store.OrgPathLabel(orgId), Slug: "tenant-" + orgKey, OrgKey: orgKey}))
		require.NoError(t, engine.SetChargeStationAuth(ctx, orgKey+".CP001", &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgId}))
		require.NoError(t, engine.SetChargeStationStatus(ctx, orgKey+".CP001", &store.ChargeStationStatus{Connected: true}))
	}

	platform, verifier := newPlatformVerifier(t)
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine, DefaultOrgId: store.LegacyOrgId}
	handler := server.NewApiHandler(server.ApiDeps{
		Engine:   engine,
		Verifier: verifier,
		Scoper:   scoperOf(resolver),
		Events:   eventsOf(engine, resolver),
	})
	return handler, platform, engine
}

// tokenOf returns a token of the platform for a person of the org with the scope self
// and the given permissions.
func tokenOf(platform *testutil.Platform, orgId string, perms ...string) string {
	claims := platform.Claims(time.Now())
	claims["org"] = orgId
	claims["org_scope"] = "self"
	claims["perms"] = append([]string{}, perms...)
	return platform.Sign(claims)
}

func request(handler http.Handler, method, target, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestApiAnswersNotFoundForTheChargeStationOfAnotherTenant(t *testing.T) {
	handler, platform, _ := newMultiTenantApiHandler(t)
	tokenOfA := tokenOf(platform, orgA, tenancy.AllPermissions...)
	tokenOfB := tokenOf(platform, orgB, tenancy.AllPermissions...)

	own := get(handler, "/api/v0/cs/"+stationOfA+"/status", tokenOfA)
	require.Equal(t, http.StatusOK, own.Code, own.Body.String())
	assert.JSONEq(t, `{"id":"aaaa.CP001","connected":true}`, own.Body.String())

	other := get(handler, "/api/v0/cs/"+stationOfA+"/status", tokenOfB)
	require.Equal(t, http.StatusNotFound, other.Code)
	assert.JSONEq(t, `{"status":"Not Found"}`, other.Body.String())

	// every operation on a charge station, with the prefix of the API and a query
	// string: the charge station of another tenant is answered exactly like one that
	// does not exist
	swagger, err := api.GetSwagger()
	require.NoError(t, err)
	permissions, err := api.BuildPermissionIndex(swagger)
	require.NoError(t, err)
	walked := 0
	for operation := range permissions {
		method, path, _ := strings.Cut(operation, " ")
		if !strings.Contains(path, "{csId}") || operation == "POST /cs/{csId}" {
			continue
		}
		walked++
		// every other path parameter is 1; the query has what the operations require
		target := func(csId string) string {
			path := strings.ReplaceAll(path, "{csId}", csId)
			path = regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(path, "1")
			return "/api/v0" + path + "?connectorId=1&duration=60&limit=1"
		}

		other := request(handler, method, target(stationOfA), tokenOfB, "")
		unknown := request(handler, method, target("bbbb.UNKNOWN"), tokenOfB, "")
		require.Equal(t, http.StatusNotFound, other.Code, "%s: %s", operation, other.Body.String())
		assert.JSONEq(t, `{"status":"Not Found"}`, other.Body.String(), operation)
		assert.Equal(t, unknown.Code, other.Code, operation)
		assert.Equal(t, unknown.Body.String(), other.Body.String(), operation)
		assert.Equal(t, unknown.Header(), other.Header(), operation)
	}
	assert.Greater(t, walked, 40)
}

func TestApiChecksThePermissionOfTheOperation(t *testing.T) {
	handler, platform, engine := newMultiTenantApiHandler(t)
	const tokenBody = `{"countryCode":"GB","partyId":"TWK","type":"RFID","uid":"ABCDEF","contractId":"GBTWK012345678V","issuer":"TWK","valid":true,"cacheMode":"ALWAYS"}`

	operations := []struct {
		method, target, body string
		permission           string
		wantStatus           int // with the permission
	}{
		{http.MethodGet, "/api/v0/token", "", tenancy.PermTokenRead, http.StatusOK},
		{http.MethodGet, "/api/v0/token?limit=5&offset=0", "", tenancy.PermTokenRead, http.StatusOK},
		{http.MethodGet, "/api/v0/token/NOSUCHTOKEN?orgId=" + orgA, "", tenancy.PermTokenRead, http.StatusNotFound},
		{http.MethodGet, "/api/v0/cs/" + stationOfA + "/status?x=1", "", tenancy.PermStationRead, http.StatusOK},
		{http.MethodGet, "/api/v0/cs/" + stationOfA + "/auth", "", tenancy.PermStationWrite, http.StatusOK},
		{http.MethodPost, "/api/v0/cs/aaaa.NEW?x=1", `{"securityProfile":0}`, tenancy.PermStationWrite, http.StatusCreated},
		{http.MethodPost, "/api/v0/token?x=1", tokenBody, tenancy.PermTokenWrite, http.StatusCreated},
	}
	for _, tc := range operations {
		name := tc.method + " " + tc.target

		// every permission but the one of the operation
		var others []string
		for _, perm := range tenancy.AllPermissions {
			if perm != tc.permission {
				others = append(others, perm)
			}
		}
		rr := request(handler, tc.method, tc.target, tokenOf(platform, orgA, others...), tc.body)
		require.Equal(t, http.StatusForbidden, rr.Code, name)
		assert.JSONEq(t, `{"status":"Forbidden","error":"missing permission `+tc.permission+`"}`, rr.Body.String(), name)

		// a token without permissions
		rr = request(handler, tc.method, tc.target, tokenOf(platform, orgA), tc.body)
		require.Equal(t, http.StatusForbidden, rr.Code, name)
	}

	// nothing was written
	tokens, err := engine.ListTokens(context.Background(), 0, 10)
	require.NoError(t, err)
	require.Empty(t, tokens)
	created, err := engine.LookupChargeStationAuth(context.Background(), "aaaa.NEW")
	require.NoError(t, err)
	require.Nil(t, created)

	// with the permission of the operation, and no other
	for _, tc := range operations {
		rr := request(handler, tc.method, tc.target, tokenOf(platform, orgA, tc.permission), tc.body)
		require.Equal(t, tc.wantStatus, rr.Code, "%s %s: %s", tc.method, tc.target, rr.Body.String())
	}
}

func TestRequestLogNamesTheCaller(t *testing.T) {
	platform, verifier := newPlatformVerifier(t)
	handler := newApiHandler(inmemory.NewStore(clock.RealClock{}), verifier)

	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logged, nil)))
	defer slog.SetDefault(previous)

	claims := platform.Claims(time.Now())
	claims["sub"] = "svc:support-tool"
	claims["act"] = map[string]any{"sub": "user:staff-123"}
	claims["jti"] = "jti-42"
	token := platform.Sign(claims)
	require.Equal(t, http.StatusOK, get(handler, "/api/v0/token", token).Code)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logged.Bytes(), &entry), logged.String())
	assert.Equal(t, "GET /api/v0/token", entry["msg"])
	assert.Equal(t, float64(http.StatusOK), entry["status"])
	assert.Equal(t, "svc:support-tool", entry["sub"])
	assert.Equal(t, "user:staff-123", entry["act"])
	assert.Equal(t, testutil.PlatformOrgId, entry["org"])
	assert.Equal(t, "jti-42", entry["jti"])
	assert.NotContains(t, logged.String(), token, "the token must not be logged")

	// a request that is refused has no caller, and its token is not logged either
	logged.Reset()
	claims["aud"] = "billing"
	refused := platform.Sign(claims)
	require.Equal(t, http.StatusUnauthorized, get(handler, "/api/v0/token", refused).Code)
	assert.Contains(t, logged.String(), `"status":401`)
	assert.Contains(t, logged.String(), `"reason":"invalid token: wrong audience"`)
	assert.NotContains(t, logged.String(), `"sub"`)
	assert.NotContains(t, logged.String(), refused)
	assert.NotContains(t, logged.String(), strings.Split(refused, ".")[2])
}

// A manager without tenants (single mode, here with the caller of dev mode) has one
// org: the history of a charge station is all that the charge station stored.
func TestDevModeReadsTheWholeHistoryOfAChargeStation(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	handler := newApiHandler(engine, devVerifier)
	storeMeterValue := func(evseId int, transactionId, timestamp string) {
		t.Helper()
		require.NoError(t, engine.StoreMeterValues(ctx, "cs001", evseId, transactionId, []store.MeterValue{{
			Timestamp: timestamp, SampledValues: []store.SampledValue{{Value: 1500}},
		}}))
	}

	// what a charge station stores before and after it is registered
	require.NoError(t, engine.CreateTransaction(ctx, "cs001", "tx-1", "TAG", "ISO14443", nil, 0, false))
	storeMeterValue(1, "tx-1", "2026-11-28T09:00:00Z")
	require.NoError(t, engine.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth}))
	require.NoError(t, engine.CreateTransaction(ctx, "cs001", "tx-2", "TAG", "ISO14443", nil, 0, false))
	storeMeterValue(1, "tx-2", "2026-11-28T10:00:00Z")
	storeMeterValue(0, "", "2026-11-28T10:10:00Z")

	var list struct {
		Total int `json:"total"`
	}
	rr := get(handler, "/api/v0/cs/cs001/transactions", "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &list))
	assert.Equal(t, 2, list.Total)
	for _, transactionId := range []string{"tx-1", "tx-2"} {
		rr = get(handler, "/api/v0/cs/cs001/transaction/"+transactionId, "")
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	}
	rr = get(handler, "/api/v0/cs/cs001/meter-values", "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &list))
	assert.Equal(t, 3, list.Total)
}
