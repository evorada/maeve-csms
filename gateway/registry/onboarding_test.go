// SPDX-License-Identifier: Apache-2.0

package registry_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/gateway/registry"
)

func TestLookupChargeStationForItsOnboarding(t *testing.T) {
	t.Run("a station that is not known", func(t *testing.T) {
		stub := newManagerStub(t, http.StatusOK, `{"orgId":"0193b2c4-0000-7000-8000-000000000001","orgKey":"aaaa",`+
			`"stationKey":"aaaa.cs001","tenantStatus":"active","invalidUsernameAllowed":false,`+
			`"onboarding":{"openSp1":true,"base64SHA256Password":"ONBOARDING"},"acceptedCertOrgNames":[]}`)
		reg := registry.RemoteRegistry{ManagerApiAddr: stub.URL}

		got, err := reg.LookupChargeStation(context.Background(), "a.example", "cs001")
		require.NoError(t, err)
		assert.Equal(t, &registry.ChargeStation{
			ClientId:             "cs001",
			StationKey:           "aaaa.cs001",
			OrgId:                "0193b2c4-0000-7000-8000-000000000001",
			OrgKey:               "aaaa",
			Onboarding:           &registry.Onboarding{OpenSP1: true, Base64SHA256Password: "ONBOARDING"},
			HasTenantOrgNames:    true,
			AcceptedCertOrgNames: []string{},
		}, got)
	})

	t.Run("a pending station whose approval is in flight", func(t *testing.T) {
		stub := newManagerStub(t, http.StatusOK, `{"orgId":"0193b2c4-0000-7000-8000-000000000001","orgKey":"aaaa",`+
			`"stationKey":"aaaa.cs001","securityProfile":0,"pendingBase64SHA256Password":"PENDING","invalidUsernameAllowed":false,`+
			`"onboarding":{"openSp1":false}}`)
		reg := registry.RemoteRegistry{ManagerApiAddr: stub.URL}

		got, err := reg.LookupChargeStation(context.Background(), "a.example", "cs001")
		require.NoError(t, err)
		assert.Equal(t, registry.UnsecuredTransportWithBasicAuth, got.SecurityProfile)
		assert.Equal(t, "PENDING", got.PendingBase64SHA256Password)
		assert.Equal(t, &registry.Onboarding{OpenSP1: false}, got.Onboarding)
	})

	t.Run("a registered station with a pending password", func(t *testing.T) {
		stub := newManagerStub(t, http.StatusOK, `{"orgKey":"aaaa","stationKey":"aaaa.cs001","securityProfile":1,`+
			`"base64SHA256Password":"DEADBEEF","pendingBase64SHA256Password":"PENDING","invalidUsernameAllowed":false}`)
		reg := registry.RemoteRegistry{ManagerApiAddr: stub.URL}

		got, err := reg.LookupChargeStation(context.Background(), "a.example", "cs001")
		require.NoError(t, err)
		assert.Equal(t, "DEADBEEF", got.Base64SHA256Password)
		assert.Equal(t, "PENDING", got.PendingBase64SHA256Password)
		assert.Nil(t, got.Onboarding)
	})
}

// onboardingStub is the internal API of the manager for onboarding connections: it
// answers with status and body and records the requests.
type onboardingStub struct {
	*httptest.Server
	mu       sync.Mutex
	requests []recordedRequest
}

type recordedRequest struct {
	method, path, contentType, body string
	query                           url.Values
}

func newOnboardingStub(t *testing.T, status int, body string) *onboardingStub {
	stub := &onboardingStub{}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestBody, _ := io.ReadAll(r.Body)
		stub.mu.Lock()
		stub.requests = append(stub.requests, recordedRequest{
			method: r.Method, path: r.URL.Path, contentType: r.Header.Get("Content-Type"), body: string(requestBody), query: r.URL.Query(),
		})
		stub.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(stub.Close)
	return stub
}

func (s *onboardingStub) recorded() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedRequest{}, s.requests...)
}

func TestRecordOnboardingConnection(t *testing.T) {
	stub := newOnboardingStub(t, http.StatusOK, `{"created":true}`)
	reg := registry.RemoteRegistry{ManagerApiAddr: stub.URL + "/"}

	require.NoError(t, reg.RecordOnboardingConnection(context.Background(), "a.example", "cs 001&x", "wss", "onboarding_password"))

	requests := stub.recorded()
	require.Len(t, requests, 1)
	assert.Equal(t, http.MethodPut, requests[0].method)
	assert.Equal(t, "/_internal/v1/station-onboarding", requests[0].path)
	assert.Equal(t, url.Values{"host": {"a.example"}, "stationId": {"cs 001&x"}}, requests[0].query)
	assert.Equal(t, "application/json", requests[0].contentType)
	assert.JSONEq(t, `{"transport":"wss","credential":"onboarding_password"}`, requests[0].body)
}

func TestRecordOnboardingConnectionThatTheManagerRefuses(t *testing.T) {
	t.Run("the limit of pending stations", func(t *testing.T) {
		stub := newOnboardingStub(t, http.StatusTooManyRequests, `{"status":"Too Many Requests","error":"pending_limit_reached"}`)
		reg := registry.RemoteRegistry{ManagerApiAddr: stub.URL}
		err := reg.RecordOnboardingConnection(context.Background(), "a.example", "cs001", "ws", "open")
		assert.ErrorIs(t, err, registry.ErrPendingLimitReached)
	})

	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"onboarding disabled":    {http.StatusNotFound, `{"status":"Not Found","error":"onboarding_disabled"}`},
		"not the internal api":   {http.StatusNotFound, `404 page not found`},
		"credential not allowed": {http.StatusForbidden, `{"status":"Forbidden","error":"credential_not_allowed"}`},
		"station not pending":    {http.StatusConflict, `{"status":"Conflict","error":"station_not_pending"}`},
		"manager fails":          {http.StatusInternalServerError, `{"status":"Internal Server Error"}`},
		"a success that is not":  {http.StatusCreated, `{"created":true}`},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newOnboardingStub(t, tc.status, tc.body)
			reg := registry.RemoteRegistry{ManagerApiAddr: stub.URL}
			err := reg.RecordOnboardingConnection(context.Background(), "a.example", "cs001", "ws", "open")
			require.Error(t, err)
			assert.NotErrorIs(t, err, registry.ErrPendingLimitReached)
		})
	}

	t.Run("a manager that cannot be reached", func(t *testing.T) {
		reg := registry.RemoteRegistry{ManagerApiAddr: "http://127.0.0.1:1", HTTPClient: &http.Client{Timeout: time.Second}}
		err := reg.RecordOnboardingConnection(context.Background(), "a.example", "cs001", "ws", "open")
		require.Error(t, err)
		assert.NotErrorIs(t, err, registry.ErrPendingLimitReached)
	})
}

func TestCachingRegistryPassesOnboardingConnectionsThrough(t *testing.T) {
	ctx := context.Background()
	inner := registry.NewMockRegistry()
	inner.Add("a.example", &registry.ChargeStation{
		ClientId: "cs001", OrgKey: "aaaa", Onboarding: &registry.Onboarding{OpenSP1: true, Base64SHA256Password: "ONBOARDING"},
	})
	cache := &registry.CachingRegistry{Inner: inner, TTL: time.Minute, NegativeTTL: time.Second}

	for i := 0; i < 2; i++ {
		require.NoError(t, cache.RecordOnboardingConnection(ctx, "a.example", "cs001", "ws", "open"))
	}
	assert.Len(t, inner.OnboardingConnections(), 2, "a connection is recorded every time")

	inner.SetOnboardingError(registry.ErrPendingLimitReached)
	assert.ErrorIs(t, cache.RecordOnboardingConnection(ctx, "a.example", "cs001", "ws", "open"), registry.ErrPendingLimitReached)

	// a caller cannot change the onboarding rules that the cache keeps
	first, err := cache.LookupChargeStation(ctx, "a.example", "cs001")
	require.NoError(t, err)
	first.Onboarding.OpenSP1 = false
	first.Onboarding.Base64SHA256Password = "CHANGED"
	second, err := cache.LookupChargeStation(ctx, "a.example", "cs001")
	require.NoError(t, err)
	assert.Equal(t, &registry.Onboarding{OpenSP1: true, Base64SHA256Password: "ONBOARDING"}, second.Onboarding)
}
