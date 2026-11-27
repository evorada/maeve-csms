// SPDX-License-Identifier: Apache-2.0

package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/config"
	"github.com/thoughtworks/maeve-csms/manager/server"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
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

	t.Run("nothing else is served on the internal listener", func(t *testing.T) {
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
	handler := server.NewApiHandler(config.ApiSettings{}, engine, nil, nil, store.LegacyOrgId)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/_internal/v1/station-auth?host=localhost&stationId=CP001", nil))
	assert.Equal(t, http.StatusNotFound, rr.Code)
}
