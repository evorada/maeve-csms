// SPDX-License-Identifier: Apache-2.0

package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/config"
	"github.com/thoughtworks/maeve-csms/manager/server"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

func TestInternalHandlerServesTheGatewayLookups(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, engine.SetChargeStationAuth(context.Background(), "CP001", &store.ChargeStationAuth{
		SecurityProfile:      store.TLSWithBasicAuth,
		Base64SHA256Password: "c2VjcmV0",
	}))
	handler := server.NewInternalHandler(server.InternalDeps{
		Resolver: &tenancy.Resolver{Stations: engine},
		Engine:   engine,
		Verifier: devVerifier,
	})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/_internal/v1/station-auth?host=localhost&stationId=CP001", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.JSONEq(t, `{
		"orgId": "`+store.LegacyOrgId+`",
		"stationKey": "CP001",
		"securityProfile": 1,
		"base64SHA256Password": "c2VjcmV0",
		"invalidUsernameAllowed": false
	}`, rr.Body.String())

	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/_internal/v1/certificates/unknown", nil))
	assert.Equal(t, http.StatusNotFound, rr.Code)

	t.Run("the api and the legacy pages are not served on the internal listener", func(t *testing.T) {
		for _, target := range []string{"/", "/station-auth?host=localhost&stationId=CP001", "/api/v0/cs/CP001/auth", "/adminui", "/transactions"} {
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, target, nil))
			assert.Equal(t, http.StatusNotFound, rr.Code, target)
		}
	})
}

func TestApiHandlerDoesNotServeTheInternalApi(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, engine.SetChargeStationAuth(context.Background(), "CP001", &store.ChargeStationAuth{}))
	handler := newApiHandler(engine, devVerifier)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/_internal/v1/station-auth?host=localhost&stationId=CP001", nil))
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestInternalHandlerServesHealthMetricsAndTheApiDocument(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	handler := server.NewInternalHandler(server.InternalDeps{Resolver: &tenancy.Resolver{Stations: engine}, Engine: engine, Verifier: devVerifier})

	rr := get(handler, "/health", "")
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, `{"status":"OK"}`, rr.Body.String())

	rr = get(handler, "/metrics", "")
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Type"), "text/plain")
	assert.Contains(t, rr.Body.String(), "# TYPE go_goroutines gauge")

	rr = get(handler, "/api/openapi.json", "")
	require.Equal(t, http.StatusOK, rr.Code)
	var document map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &document))
	assert.Equal(t, "MaEVe CSMS", document["info"].(map[string]any)["title"])
	assert.Contains(t, document["paths"], "/cs/{csId}")

	// a page of another site must not be able to read what this listener serves
	for _, target := range []string{"/health", "/metrics", "/api/openapi.json", "/_internal/v1/station-auth?host=localhost&stationId=CP001"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Origin", "https://example.com")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Empty(t, rr.Header().Get("Access-Control-Allow-Origin"), target)
	}
}

// The legacy pages have no authentication, show the data of every org and register
// charge stations and tokens: each of them is only served when its own setting says so.
func TestInternalHandlerServesTheLegacyPagesOnlyWhenEnabled(t *testing.T) {
	adminPages := []string{"/adminui", "/adminui/", "/adminui/connect", "/adminui/token"}

	for name, tc := range map[string]struct {
		settings         config.ApiSettings
		wantTransactions bool
		wantAdminUI      bool
	}{
		"default":           {settings: config.ApiSettings{}},
		"transactions page": {settings: config.ApiSettings{EnableTransactionsPage: true}, wantTransactions: true},
		"admin pages":       {settings: config.ApiSettings{EnableAdminUI: true}, wantAdminUI: true},
		"both":              {settings: config.ApiSettings{EnableTransactionsPage: true, EnableAdminUI: true}, wantTransactions: true, wantAdminUI: true},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			engine := inmemory.NewStore(clock.RealClock{})
			require.NoError(t, engine.CreateTransaction(ctx, "CP001", "tx-4711", "ABCDEF", "ISO14443", nil, 0, false))

			var logged bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logged, nil)))
			defer slog.SetDefault(previous)

			handler := server.NewInternalHandler(server.InternalDeps{
				Resolver:     &tenancy.Resolver{Stations: engine},
				Engine:       engine,
				Verifier:     devVerifier,
				Settings:     tc.settings,
				DefaultOrgId: orgA,
			})
			startupLog := logged.String()

			status := func(enabled bool) int {
				if enabled {
					return http.StatusOK
				}
				return http.StatusNotFound
			}

			rr := get(handler, "/transactions", "")
			require.Equal(t, status(tc.wantTransactions), rr.Code)
			if tc.wantTransactions {
				assert.Contains(t, rr.Body.String(), "tx-4711")
			} else {
				assert.NotContains(t, rr.Body.String(), "tx-4711")
			}
			assert.Equal(t, http.StatusNotFound, get(handler, "/transactions/", "").Code)

			for _, target := range adminPages {
				assert.Equal(t, status(tc.wantAdminUI), get(handler, target, "").Code, target)
			}
			for target, form := range map[string]url.Values{
				"/adminui/connect": {"csid": {"cs001"}, "auth": {"unsecured"}},
				"/adminui/token":   {"uid": {"ABC123"}},
			} {
				req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				rr := httptest.NewRecorder()
				handler.ServeHTTP(rr, req)
				assert.Equal(t, status(tc.wantAdminUI), rr.Code, "POST %s", target)
			}

			auth, err := engine.LookupChargeStationAuth(ctx, "cs001")
			require.NoError(t, err)
			tokens, err := engine.ListTokens(ctx, 0, 10)
			require.NoError(t, err)
			if tc.wantAdminUI {
				// what the admin pages register belongs to the default org
				require.NotNil(t, auth)
				assert.Equal(t, orgA, auth.OrgId)
				require.Len(t, tokens, 1)
				assert.Equal(t, orgA, tokens[0].OrgId)
			} else {
				assert.Nil(t, auth)
				assert.Empty(t, tokens)
			}

			// an enabled legacy page is named in the log when the manager starts
			for route, enabled := range map[string]bool{"/transactions": tc.wantTransactions, "/adminui": tc.wantAdminUI} {
				line := `"msg":"legacy route enabled on internal listener","route":"` + route + `"`
				if enabled {
					assert.Contains(t, startupLog, line)
				} else {
					assert.NotContains(t, startupLog, line)
				}
			}
		})
	}
}
