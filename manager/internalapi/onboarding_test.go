// SPDX-License-Identifier: Apache-2.0

package internalapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/internalapi"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
)

// onboardingPassword is the hash of the onboarding password "password".
const onboardingPassword = "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg="

func stationOnboardingUrl(host, stationId string) string {
	return "/station-onboarding?" + url.Values{"host": {host}, "stationId": {stationId}}.Encode()
}

func putOnboarding(t *testing.T, handler http.Handler, host, stationId, body string) (int, string) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, stationOnboardingUrl(host, stationId), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rr, req)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", rr.Header().Get("Cache-Control"))
	return rr.Code, rr.Body.String()
}

func onboardingBody(transport, credential string) string {
	return fmt.Sprintf(`{"transport":%q,"credential":%q}`, transport, credential)
}

func TestStationAuthOfAStationThatIsNotRegistered(t *testing.T) {
	handler, engine := multiTenantHandler(t)
	// tenant A has no config (the defaults), tenant B wants the onboarding password
	putTenantConfig(t, engine, orgB, `{"version":1,"onboarding_open_sp1":false,"onboarding_password_sha256":"`+onboardingPassword+`"}`)

	status, body := get(t, handler, stationAuthUrl("a.example", "CP999"))
	require.Equal(t, http.StatusOK, status)
	// no security profile and no password: the gateway admits it with the onboarding rules
	assert.JSONEq(t, `{
		"orgId": "`+orgA+`",
		"orgKey": "aaaa",
		"stationKey": "aaaa.CP999",
		"tenantStatus": "active",
		"invalidUsernameAllowed": false,
		"onboarding": {"openSp1": true},
		"acceptedCertOrgNames": []
	}`, body)

	status, body = get(t, handler, stationAuthUrl("b.example", "CP999"))
	require.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `{
		"orgId": "`+orgB+`",
		"orgKey": "bbbb",
		"stationKey": "bbbb.CP999",
		"tenantStatus": "active",
		"invalidUsernameAllowed": false,
		"onboarding": {"openSp1": false, "base64SHA256Password": "`+onboardingPassword+`"},
		"acceptedCertOrgNames": []
	}`, body)

	// a lookup writes nothing
	for _, key := range []string{"aaaa.CP999", "bbbb.CP999"} {
		auth, err := engine.LookupChargeStationAuth(context.Background(), key)
		require.NoError(t, err)
		assert.Nil(t, auth, key)
	}

	t.Run("a registered station is answered as before", func(t *testing.T) {
		status, body := get(t, handler, stationAuthUrl("a.example", "CP001"))
		require.Equal(t, http.StatusOK, status)
		assert.NotContains(t, body, "onboarding")
		assert.NotContains(t, body, "pendingBase64SHA256Password")
	})
}

func TestStationAuthOfAStationThatIsNotRegisteredWithoutOnboarding(t *testing.T) {
	ctx := context.Background()

	t.Run("a tenant that onboards no stations", func(t *testing.T) {
		handler, engine := multiTenantHandler(t)
		putTenantConfig(t, engine, orgA, preregisteredOnly)
		status, body := get(t, handler, stationAuthUrl("a.example", "CP999"))
		assert.Equal(t, http.StatusNotFound, status)
		assert.JSONEq(t, `{"status":"Not Found","error":"unknown_station"}`, body)
	})

	t.Run("a tenant whose config cannot be read", func(t *testing.T) {
		handler, engine := multiTenantHandler(t)
		putTenantConfig(t, engine, orgA, `{"version":1,"onboarding_mode":"open"}`)
		status, body := get(t, handler, stationAuthUrl("a.example", "CP999"))
		assert.Equal(t, http.StatusNotFound, status)
		assert.JSONEq(t, `{"status":"Not Found","error":"unknown_station"}`, body)
	})

	t.Run("single mode", func(t *testing.T) {
		engine := inmemory.NewStore(clock.RealClock{})
		require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
			OrgId: orgA, Path: store.OrgPathLabel(orgA), Slug: "tenant-a", OrgKey: "aaaa", Hostnames: []string{"a.example"},
		}))
		handler := internalapi.NewHandler(&tenancy.Resolver{Mode: tenancy.ModeSingle, Tenants: engine, Stations: engine, DefaultOrgId: orgA}, engine)

		status, body := get(t, handler, stationAuthUrl("a.example", "CP999"))
		assert.Equal(t, http.StatusNotFound, status)
		assert.JSONEq(t, `{"status":"Not Found","error":"unknown_station"}`, body)

		status, body = putOnboarding(t, handler, "a.example", "CP999", onboardingBody("ws", "open"))
		assert.Equal(t, http.StatusNotFound, status)
		assert.JSONEq(t, `{"status":"Not Found","error":"onboarding_disabled"}`, body)
		auth, err := engine.LookupChargeStationAuth(ctx, "CP999")
		require.NoError(t, err)
		assert.Nil(t, auth)
	})
}

func TestStationAuthOfAPendingStation(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: orgA, Path: store.OrgPathLabel(orgA), Slug: "tenant-a", OrgKey: "aaaa", Hostnames: []string{"a.example"},
		ConfigVersion: 1, Config: json.RawMessage(`{"version":1,"onboarding_password_sha256":"` + onboardingPassword + `"}`),
	}))
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine, DefaultOrgId: defaultOrg}
	handler := internalapi.NewHandler(resolver, engine)

	status, _ := putOnboarding(t, handler, "a.example", "CP010", onboardingBody("wss", "onboarding_password"))
	require.Equal(t, http.StatusOK, status)

	status, body := get(t, handler, stationAuthUrl("a.example", "CP010"))
	require.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `{
		"orgId": "`+orgA+`",
		"orgKey": "aaaa",
		"stationKey": "aaaa.CP010",
		"tenantStatus": "active",
		"securityProfile": 1,
		"invalidUsernameAllowed": false,
		"onboarding": {"openSp1": true, "base64SHA256Password": "`+onboardingPassword+`"},
		"acceptedCertOrgNames": []
	}`, body)

	t.Run("a new onboarding password of the tenant applies to the stations that wait", func(t *testing.T) {
		const newPassword = "+/+/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
		putTenantConfig(t, engine, orgA, `{"version":2,"onboarding_open_sp1":false,"onboarding_password_sha256":"`+newPassword+`"}`)
		resolver.Invalidate(orgA)

		status, body := get(t, handler, stationAuthUrl("a.example", "CP010"))
		require.Equal(t, http.StatusOK, status)
		var resp internalapi.StationAuthResponse
		require.NoError(t, json.Unmarshal([]byte(body), &resp))
		assert.Equal(t, &internalapi.OnboardingResponse{OpenSP1: false, Base64SHA256Password: newPassword}, resp.Onboarding)
	})

	t.Run("the password of an approval in flight", func(t *testing.T) {
		auth, err := engine.LookupChargeStationAuth(ctx, "aaaa.CP010")
		require.NoError(t, err)
		withPending := *auth
		withPending.PendingBase64SHA256Password = passwordA
		require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.CP010", &withPending))

		status, body := get(t, handler, stationAuthUrl("a.example", "CP010"))
		require.Equal(t, http.StatusOK, status)
		var resp internalapi.StationAuthResponse
		require.NoError(t, json.Unmarshal([]byte(body), &resp))
		assert.Equal(t, passwordA, resp.PendingBase64SHA256Password)
		assert.NotNil(t, resp.Onboarding)
	})

	t.Run("a tenant that stopped onboarding refuses its pending stations", func(t *testing.T) {
		putTenantConfig(t, engine, orgA, `{"version":3,"onboarding_mode":"preregistered_only"}`)
		resolver.Invalidate(orgA)

		status, body := get(t, handler, stationAuthUrl("a.example", "CP010"))
		assert.Equal(t, http.StatusNotFound, status)
		assert.JSONEq(t, `{"status":"Not Found","error":"unknown_station"}`, body)
	})
}

func TestStationAuthOfAnActiveStationWithAPendingPassword(t *testing.T) {
	ctx := context.Background()
	handler, engine := multiTenantHandler(t)
	require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.CP001", &store.ChargeStationAuth{
		SecurityProfile:             store.TLSWithBasicAuth,
		Base64SHA256Password:        passwordA,
		PendingBase64SHA256Password: passwordB,
		OrgId:                       orgA,
	}))

	status, body := get(t, handler, stationAuthUrl("a.example", "CP001"))
	require.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `{
		"orgId": "`+orgA+`",
		"orgKey": "aaaa",
		"stationKey": "aaaa.CP001",
		"tenantStatus": "active",
		"securityProfile": 1,
		"base64SHA256Password": "`+passwordA+`",
		"pendingBase64SHA256Password": "`+passwordB+`",
		"invalidUsernameAllowed": false,
		"acceptedCertOrgNames": []
	}`, body)
}

func TestStationOnboardingCreatesAPendingStation(t *testing.T) {
	ctx := context.Background()
	handler, engine := multiTenantHandler(t)
	before := time.Now()

	status, body := putOnboarding(t, handler, "a.example", "CP020", onboardingBody("ws", "open"))
	require.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `{"created":true}`, body)

	record, err := engine.LookupChargeStation(ctx, "aaaa.CP020")
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.Equal(t, store.StationStatePending, record.State)
	assert.Equal(t, orgA, record.OrgId)
	require.NotNil(t, record.Onboarding)
	assert.Equal(t, "ws", record.Onboarding.Transport)
	assert.Equal(t, "open", record.Onboarding.Credential)
	assert.Equal(t, store.OnboardingAwaitingApproval, record.Onboarding.Status)
	assert.WithinDuration(t, before, record.Onboarding.FirstSeenAt, time.Minute)
	auth, err := engine.LookupChargeStationAuth(ctx, "aaaa.CP020")
	require.NoError(t, err)
	assert.Equal(t, &store.ChargeStationAuth{SecurityProfile: store.UnsecuredTransportWithBasicAuth, OrgId: orgA}, auth)

	// the next connection of the station is its last one
	status, body = putOnboarding(t, handler, "a.example", "CP020", onboardingBody("ws", "open"))
	require.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `{"created":false}`, body)

	// the lookup gives the pending station with the onboarding rules
	status, body = get(t, handler, stationAuthUrl("a.example", "CP020"))
	require.Equal(t, http.StatusOK, status)
	var resp internalapi.StationAuthResponse
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	assert.Equal(t, &internalapi.OnboardingResponse{OpenSP1: true}, resp.Onboarding)
	require.NotNil(t, resp.SecurityProfile)
	assert.Equal(t, 0, *resp.SecurityProfile)
}

func TestStationOnboardingOfAStationOfAnotherTenant(t *testing.T) {
	ctx := context.Background()
	handler, engine := multiTenantHandler(t)
	// a station that only tenant B has connects to the host of tenant A
	require.NoError(t, engine.SetChargeStationAuth(ctx, "bbbb.CP002", &store.ChargeStationAuth{
		SecurityProfile: store.TLSWithBasicAuth, Base64SHA256Password: passwordB, OrgId: orgB,
	}))

	status, body := get(t, handler, stationAuthUrl("a.example", "CP002"))
	require.Equal(t, http.StatusOK, status)
	var resp internalapi.StationAuthResponse
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	// it is a station of tenant A that A does not know, never the station of B
	assert.Equal(t, "aaaa.CP002", resp.StationKey)
	assert.Equal(t, orgA, resp.OrgId)
	assert.Empty(t, resp.Base64SHA256Password)
	assert.Nil(t, resp.SecurityProfile)

	status, _ = putOnboarding(t, handler, "a.example", "CP002", onboardingBody("ws", "open"))
	require.Equal(t, http.StatusOK, status)

	pending, err := engine.LookupChargeStation(ctx, "aaaa.CP002")
	require.NoError(t, err)
	assert.Equal(t, store.StationStatePending, pending.State)
	assert.Equal(t, orgA, pending.OrgId)
	other, err := engine.LookupChargeStation(ctx, "bbbb.CP002")
	require.NoError(t, err)
	assert.Equal(t, store.StationStateActive, other.State)
	assert.Equal(t, orgB, other.OrgId)
	assert.Nil(t, other.Onboarding)
	otherAuth, err := engine.LookupChargeStationAuth(ctx, "bbbb.CP002")
	require.NoError(t, err)
	assert.Equal(t, passwordB, otherAuth.Base64SHA256Password)
}

func TestStationOnboardingOfAStationThatIsNotPending(t *testing.T) {
	ctx := context.Background()
	handler, engine := multiTenantHandler(t)
	require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.CP003", &store.ChargeStationAuth{
		SecurityProfile: store.TLSWithBasicAuth, Base64SHA256Password: passwordA, OrgId: orgA,
	}))
	require.NoError(t, engine.DecommissionChargeStation(ctx, "aaaa.CP003", nil))

	for stationId, state := range map[string]store.StationState{
		"CP001": store.StationStateActive,
		"CP003": store.StationStateDecommissioned,
	} {
		status, body := putOnboarding(t, handler, "a.example", stationId, onboardingBody("ws", "open"))
		assert.Equal(t, http.StatusConflict, status, stationId)
		assert.JSONEq(t, `{"status":"Conflict","error":"station_not_pending"}`, body, stationId)

		record, err := engine.LookupChargeStation(ctx, "aaaa."+stationId)
		require.NoError(t, err)
		assert.Equal(t, state, record.State, stationId)
		assert.Nil(t, record.Onboarding, stationId)
	}

	// a decommissioned station stays refused by the lookup too
	status, body := get(t, handler, stationAuthUrl("a.example", "CP003"))
	assert.Equal(t, http.StatusNotFound, status)
	assert.JSONEq(t, `{"status":"Not Found","error":"unknown_station"}`, body)
}

func TestStationOnboardingIsLimited(t *testing.T) {
	ctx := context.Background()

	t.Run("the 51st station that is not registered is refused", func(t *testing.T) {
		handler, engine := multiTenantHandler(t)
		for i := 1; i <= 50; i++ {
			status, body := putOnboarding(t, handler, "a.example", fmt.Sprintf("NEW%03d", i), onboardingBody("ws", "open"))
			require.Equal(t, http.StatusOK, status, body)
		}

		status, body := putOnboarding(t, handler, "a.example", "NEW051", onboardingBody("ws", "open"))
		assert.Equal(t, http.StatusTooManyRequests, status)
		assert.JSONEq(t, `{"status":"Too Many Requests","error":"pending_limit_reached"}`, body)
		auth, err := engine.LookupChargeStationAuth(ctx, "aaaa.NEW051")
		require.NoError(t, err)
		assert.Nil(t, auth, "nothing is written")

		// the pending stations still connect, and the other tenant has a limit of its own
		status, _ = putOnboarding(t, handler, "a.example", "NEW050", onboardingBody("ws", "open"))
		assert.Equal(t, http.StatusOK, status)
		status, _ = putOnboarding(t, handler, "b.example", "NEW051", onboardingBody("ws", "open"))
		assert.Equal(t, http.StatusOK, status)

		pending, err := engine.CountChargeStations(ctx, orgA, store.StationStatePending)
		require.NoError(t, err)
		assert.Equal(t, 50, pending)
	})

	t.Run("the limit of the tenant", func(t *testing.T) {
		handler, engine := multiTenantHandler(t)
		putTenantConfig(t, engine, orgA, `{"version":1,"onboarding_max_pending":1}`)
		status, _ := putOnboarding(t, handler, "a.example", "NEW001", onboardingBody("ws", "open"))
		require.Equal(t, http.StatusOK, status)
		status, _ = putOnboarding(t, handler, "a.example", "NEW002", onboardingBody("ws", "open"))
		assert.Equal(t, http.StatusTooManyRequests, status)
	})
}

func TestStationOnboardingFollowsTheRulesOfTheTenant(t *testing.T) {
	cases := []struct {
		name       string
		config     string
		body       string
		wantStatus int
		wantError  string
	}{
		{name: "open SP1", config: `{"version":1}`, body: onboardingBody("ws", "open"), wantStatus: http.StatusOK},
		{name: "open SP1 turned off", config: `{"version":1,"onboarding_open_sp1":false}`, body: onboardingBody("ws", "open"),
			wantStatus: http.StatusForbidden, wantError: "credential_not_allowed"},
		{name: "the onboarding password", config: `{"version":1,"onboarding_password_sha256":"` + onboardingPassword + `"}`,
			body: onboardingBody("wss", "onboarding_password"), wantStatus: http.StatusOK},
		{name: "the onboarding password without TLS", config: `{"version":1,"onboarding_open_sp1":false,"onboarding_password_sha256":"` + onboardingPassword + `"}`,
			body: onboardingBody("ws", "onboarding_password"), wantStatus: http.StatusOK},
		{name: "a tenant without an onboarding password", config: `{"version":1}`, body: onboardingBody("wss", "onboarding_password"),
			wantStatus: http.StatusForbidden, wantError: "credential_not_allowed"},
		{name: "a tenant that onboards no stations", config: preregisteredOnly, body: onboardingBody("ws", "open"),
			wantStatus: http.StatusNotFound, wantError: "onboarding_disabled"},
		{name: "a tenant whose config cannot be read", config: `{"version":1,"onboarding_mode":"open"}`, body: onboardingBody("ws", "open"),
			wantStatus: http.StatusNotFound, wantError: "onboarding_disabled"},
		{name: "open with TLS", config: `{"version":1}`, body: onboardingBody("wss", "open"),
			wantStatus: http.StatusBadRequest, wantError: `credential: "open" is only for the transport "ws"`},
		{name: "an unknown transport", config: `{"version":1}`, body: onboardingBody("http", "open"),
			wantStatus: http.StatusBadRequest, wantError: `transport: "ws" or "wss" is required`},
		{name: "an unknown credential", config: `{"version":1}`, body: onboardingBody("wss", "certificate"),
			wantStatus: http.StatusBadRequest, wantError: `credential: "open", "onboarding_password" or "pending_password" is required`},
		{name: "a member that is not known", config: `{"version":1}`, body: `{"transport":"ws","credential":"open","securityProfile":2}`,
			wantStatus: http.StatusBadRequest},
		{name: "two objects", config: `{"version":1}`, body: onboardingBody("ws", "open") + onboardingBody("ws", "open"),
			wantStatus: http.StatusBadRequest},
		{name: "no body", config: `{"version":1}`, body: ``, wantStatus: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler, engine := multiTenantHandler(t)
			putTenantConfig(t, engine, orgA, tc.config)

			status, body := putOnboarding(t, handler, "a.example", "CP030", tc.body)
			assert.Equal(t, tc.wantStatus, status, body)
			if tc.wantError != "" {
				var resp internalapi.StatusResponse
				require.NoError(t, json.Unmarshal([]byte(body), &resp))
				assert.Equal(t, tc.wantError, resp.Error)
			}
			record, err := engine.LookupChargeStation(context.Background(), "aaaa.CP030")
			require.NoError(t, err)
			if tc.wantStatus == http.StatusOK {
				assert.NotNil(t, record)
			} else {
				assert.Nil(t, record, "nothing is written")
			}
		})
	}

	t.Run("an unknown host", func(t *testing.T) {
		handler, _ := multiTenantHandler(t)
		status, body := putOnboarding(t, handler, "c.example", "CP030", onboardingBody("ws", "open"))
		assert.Equal(t, http.StatusNotFound, status)
		assert.JSONEq(t, `{"status":"Not Found","error":"unknown_host"}`, body)
	})

	t.Run("a station id that is not valid", func(t *testing.T) {
		handler, _ := multiTenantHandler(t)
		status, _ := putOnboarding(t, handler, "a.example", "CP#1", onboardingBody("ws", "open"))
		assert.Equal(t, http.StatusBadRequest, status)
	})
}
