// SPDX-License-Identifier: Apache-2.0

package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/config"
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

// newApiHandler creates the handler of the API listener.
func newApiHandler(engine store.Engine, verifier tenancy.Verifier) http.Handler {
	return server.NewApiHandler(server.ApiDeps{
		Settings:     config.ApiSettings{},
		Engine:       engine,
		Verifier:     verifier,
		DefaultOrgId: store.LegacyOrgId,
	})
}

// newConsoleVerifier creates a verifier for the tokens of a console that the test
// controls.
func newConsoleVerifier(t *testing.T) (*testutil.Console, tenancy.Verifier) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	console := testutil.NewConsole(t)
	verifier, err := tenancy.NewJWKSVerifier(ctx, tenancy.JWKSVerifierConfig{
		JWKSURL:   console.JWKSURL(),
		Issuer:    testutil.ConsoleIssuer,
		Audience:  testutil.ConsoleAudience,
		ClockSkew: 5 * time.Second,
	})
	require.NoError(t, err)
	return console, verifier
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

func TestApiNeedsAConsoleToken(t *testing.T) {
	console, verifier := newConsoleVerifier(t)
	engine := inmemory.NewStore(clock.RealClock{})
	handler := newApiHandler(engine, verifier)

	t.Run("without a token", func(t *testing.T) {
		rr := get(handler, "/api/v0/token", "")
		require.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.Equal(t, `Bearer error="invalid_token"`, rr.Header().Get("WWW-Authenticate"))
		assert.JSONEq(t, `{"status":"Unauthorized","error":"missing bearer token"}`, rr.Body.String())
	})

	t.Run("with a valid token", func(t *testing.T) {
		rr := get(handler, "/api/v0/token", console.Token(time.Now()))
		require.Equal(t, http.StatusOK, rr.Code)
		assert.JSONEq(t, `[]`, rr.Body.String())
	})

	t.Run("with a token that is not valid", func(t *testing.T) {
		expired := console.Token(time.Now().Add(-time.Hour))
		rr := get(handler, "/api/v0/token", expired)
		require.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.JSONEq(t, `{"status":"Unauthorized","error":"invalid token"}`, rr.Body.String())

		claims := console.Claims(time.Now())
		claims["aud"] = "billing"
		rr = get(handler, "/api/v0/token", console.Sign(claims))
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

	t.Run("the other routes of the listener are served without a token", func(t *testing.T) {
		for _, target := range []string{"/health", "/metrics", "/api/openapi.json", "/transactions", "/adminui"} {
			rr := get(handler, target, "")
			assert.Equal(t, http.StatusOK, rr.Code, target)
		}
	})
}

func TestApiAnswersServiceUnavailableWithoutKeys(t *testing.T) {
	console, _ := newConsoleVerifier(t)
	token := console.Token(time.Now())
	console.SetDown(true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	verifier, err := tenancy.NewJWKSVerifier(ctx, tenancy.JWKSVerifierConfig{
		JWKSURL:  console.JWKSURL(),
		Issuer:   testutil.ConsoleIssuer,
		Audience: testutil.ConsoleAudience,
	})
	require.NoError(t, err)
	handler := newApiHandler(inmemory.NewStore(clock.RealClock{}), verifier)

	rr := get(handler, "/api/v0/token", token)
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)
	assert.JSONEq(t, `{"status":"Service Unavailable","error":"auth keys unavailable"}`, rr.Body.String())

	assert.Equal(t, http.StatusOK, get(handler, "/health", "").Code)
}

func TestApiHandlerNeedsAVerifier(t *testing.T) {
	assert.Panics(t, func() { newApiHandler(inmemory.NewStore(clock.RealClock{}), nil) })
}

func TestRequestLogNamesTheCaller(t *testing.T) {
	console, verifier := newConsoleVerifier(t)
	handler := newApiHandler(inmemory.NewStore(clock.RealClock{}), verifier)

	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logged, nil)))
	defer slog.SetDefault(previous)

	claims := console.Claims(time.Now())
	claims["sub"] = "svc:support-tool"
	claims["act"] = map[string]any{"sub": "user:staff-123"}
	claims["jti"] = "jti-42"
	token := console.Sign(claims)
	require.Equal(t, http.StatusOK, get(handler, "/api/v0/token", token).Code)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logged.Bytes(), &entry), logged.String())
	assert.Equal(t, "GET /api/v0/token", entry["msg"])
	assert.Equal(t, float64(http.StatusOK), entry["status"])
	assert.Equal(t, "svc:support-tool", entry["sub"])
	assert.Equal(t, "user:staff-123", entry["act"])
	assert.Equal(t, testutil.ConsoleOrgId, entry["org"])
	assert.Equal(t, "jti-42", entry["jti"])
	assert.NotContains(t, logged.String(), token, "the token must not be logged")

	// a request that is refused has no caller, and its token is not logged either
	logged.Reset()
	claims["aud"] = "billing"
	refused := console.Sign(claims)
	require.Equal(t, http.StatusUnauthorized, get(handler, "/api/v0/token", refused).Code)
	assert.Contains(t, logged.String(), `"status":401`)
	assert.Contains(t, logged.String(), `"reason":"invalid token: wrong audience"`)
	assert.NotContains(t, logged.String(), `"sub"`)
	assert.NotContains(t, logged.String(), refused)
	assert.NotContains(t, logged.String(), strings.Split(refused, ".")[2])
}

func TestHealthHandler(t *testing.T) {
	handler := newApiHandler(inmemory.NewStore(clock.RealClock{}), devVerifier)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	res := w.Result()
	defer func() {
		err := res.Body.Close()
		if err != nil {
			t.Errorf("closing body: %v", err)
		}
	}()

	if res.StatusCode != http.StatusOK {
		t.Errorf("status code: want %d, got %d", http.StatusOK, res.StatusCode)
	}
}

func TestMetricsHandler(t *testing.T) {
	handler := newApiHandler(inmemory.NewStore(clock.RealClock{}), devVerifier)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	res := w.Result()
	defer func() {
		err := res.Body.Close()
		if err != nil {
			t.Errorf("closing body: %v", err)
		}
	}()

	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	if res.StatusCode != http.StatusOK {
		t.Errorf("status code: want %d, got %d", http.StatusOK, res.StatusCode)
	}

	assert.Contains(t, string(b), "go_goroutines", "metrics should contain go_goroutines")
}

func TestSwaggerHandler(t *testing.T) {
	handler := newApiHandler(inmemory.NewStore(clock.RealClock{}), devVerifier)

	req := httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	res := w.Result()
	defer func() {
		err := res.Body.Close()
		if err != nil {
			t.Errorf("closing body: %v", err)
		}
	}()

	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	if res.StatusCode != http.StatusOK {
		t.Errorf("status code: want %d, got %d", http.StatusOK, res.StatusCode)
	}

	var jsonData map[string]interface{}
	err = json.Unmarshal(b, &jsonData)
	require.NoError(t, err)
	require.Equal(t, jsonData["info"].(map[string]any)["title"], "MaEVe CSMS")
}
