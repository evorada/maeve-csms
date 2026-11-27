// SPDX-License-Identifier: Apache-2.0

package internalapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/internalapi"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
)

const (
	orgA       = "0193b2c4-0000-7000-8000-0000000000a1"
	orgB       = "0193b2c4-0000-7000-8000-0000000000b2"
	orgC       = "0193b2c4-0000-7000-8000-0000000000c3"
	defaultOrg = "0193b2c4-0000-7000-8000-0000000000dd"

	passwordA     = "cGFzc3dvcmQgb2YgdGhlIHN0YXRpb24gb2YgdGVuYW50IEE="
	passwordB     = "cGFzc3dvcmQgb2YgdGhlIHN0YXRpb24gb2YgdGVuYW50IEI="
	passwordPlain = "cGFzc3dvcmQgb2YgdGhlIHN0YXRpb24gd2l0aG91dCBvcmc="
)

// multiTenantHandler returns the handler in multi mode on an in-memory store with the
// tenants A (org key aaaa, host a.example) and B (org key bbbb, host b.example), which
// both have a station CP001.
func multiTenantHandler(t *testing.T) (http.Handler, *inmemory.Store) {
	t.Helper()
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: orgA, Path: store.OrgPathLabel(orgA), Slug: "tenant-a", OrgKey: "aaaa", Hostnames: []string{"a.example"},
	}))
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: orgB, Path: store.OrgPathLabel(orgB), Slug: "tenant-b", OrgKey: "bbbb", Hostnames: []string{"b.example"},
	}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.CP001", &store.ChargeStationAuth{
		SecurityProfile:      store.TLSWithBasicAuth,
		Base64SHA256Password: passwordA,
		OrgId:                orgA,
	}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "bbbb.CP001", &store.ChargeStationAuth{
		SecurityProfile:        store.TLSWithClientSideCertificates,
		Base64SHA256Password:   passwordB,
		InvalidUsernameAllowed: true,
		OrgId:                  orgB,
	}))

	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine, DefaultOrgId: defaultOrg}
	return internalapi.NewHandler(resolver, engine), engine
}

func stationAuthUrl(host, stationId string) string {
	return "/station-auth?" + url.Values{"host": {host}, "stationId": {stationId}}.Encode()
}

func get(t *testing.T, handler http.Handler, target string) (int, string) {
	t.Helper()
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, target, nil))
	if rr.Body.Len() > 0 {
		assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	}
	return rr.Code, rr.Body.String()
}

func TestStationAuthOfTwoTenantsWithTheSameStationId(t *testing.T) {
	handler, _ := multiTenantHandler(t)

	status, body := get(t, handler, stationAuthUrl("a.example", "CP001"))
	require.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `{
		"orgId": "`+orgA+`",
		"orgKey": "aaaa",
		"stationKey": "aaaa.CP001",
		"tenantStatus": "active",
		"securityProfile": 1,
		"base64SHA256Password": "`+passwordA+`",
		"invalidUsernameAllowed": false,
		"acceptedCertOrgNames": []
	}`, body)

	status, body = get(t, handler, stationAuthUrl("b.example", "CP001"))
	require.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `{
		"orgId": "`+orgB+`",
		"orgKey": "bbbb",
		"stationKey": "bbbb.CP001",
		"tenantStatus": "active",
		"securityProfile": 2,
		"base64SHA256Password": "`+passwordB+`",
		"invalidUsernameAllowed": true,
		"acceptedCertOrgNames": []
	}`, body)

	t.Run("the host is taken as the gateway received it", func(t *testing.T) {
		status, body := get(t, handler, stationAuthUrl("B.Example:443", "CP001"))
		require.Equal(t, http.StatusOK, status)
		var resp internalapi.StationAuthResponse
		require.NoError(t, json.Unmarshal([]byte(body), &resp))
		assert.Equal(t, "bbbb.CP001", resp.StationKey)
		assert.Equal(t, orgB, resp.OrgId)
	})

	t.Run("the answer must not be stored", func(t *testing.T) {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, stationAuthUrl("a.example", "CP001"), nil))
		assert.Equal(t, "no-store", rr.Header().Get("Cache-Control"))
	})
}

func TestStationAuthUnknownHost(t *testing.T) {
	handler, _ := multiTenantHandler(t)

	for name, host := range map[string]string{
		"host of no tenant":     "c.example",
		"subdomain of a tenant": "x.a.example",
		"org key as host":       "aaaa",
		"only a port":           ":443",
	} {
		t.Run(name, func(t *testing.T) {
			status, body := get(t, handler, stationAuthUrl(host, "CP001"))
			assert.Equal(t, http.StatusNotFound, status)
			assert.JSONEq(t, `{"status":"Not Found","error":"unknown_host"}`, body)
		})
	}
}

func TestStationAuthUnknownStation(t *testing.T) {
	ctx := context.Background()
	handler, engine := multiTenantHandler(t)
	// a station that only tenant B has, and one from before tenants (plain key)
	require.NoError(t, engine.SetChargeStationAuth(ctx, "bbbb.CP002", &store.ChargeStationAuth{Base64SHA256Password: passwordB, OrgId: orgB}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "CP003", &store.ChargeStationAuth{Base64SHA256Password: passwordPlain}))

	tests := map[string]struct {
		host      string
		stationId string
	}{
		"station that no tenant has":                     {host: "a.example", stationId: "CP999"},
		"station that only the other tenant has":         {host: "a.example", stationId: "CP002"},
		"station key of the other tenant as station id":  {host: "a.example", stationId: "bbbb.CP001"},
		"own station key as station id":                  {host: "a.example", stationId: "aaaa.CP001"},
		"station with a plain key":                       {host: "a.example", stationId: "CP003"},
		"station id in another case":                     {host: "a.example", stationId: "cp001"},
		"station of the other tenant on the other host":  {host: "b.example", stationId: "CP999"},
		"plain key through the host of the other tenant": {host: "b.example", stationId: "CP003"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			status, body := get(t, handler, stationAuthUrl(tt.host, tt.stationId))
			assert.Equal(t, http.StatusNotFound, status)
			assert.JSONEq(t, `{"status":"Not Found","error":"unknown_station"}`, body)
		})
	}

	t.Run("the station of the other tenant is there for its own host", func(t *testing.T) {
		status, _ := get(t, handler, stationAuthUrl("b.example", "CP002"))
		assert.Equal(t, http.StatusOK, status)
	})
}

func TestStationAuthBadRequest(t *testing.T) {
	handler, _ := multiTenantHandler(t)

	targets := map[string]string{
		"no host":                        "/station-auth?stationId=CP001",
		"empty host":                     "/station-auth?host=&stationId=CP001",
		"no station id":                  "/station-auth?host=a.example",
		"empty station id":               "/station-auth?host=a.example&stationId=",
		"no parameters":                  "/station-auth",
		"two hosts":                      "/station-auth?host=a.example&host=b.example&stationId=CP001",
		"two station ids":                "/station-auth?host=a.example&stationId=CP001&stationId=CP002",
		"station id with a wildcard":     stationAuthUrl("a.example", "CP#"),
		"station id with a plus":         stationAuthUrl("a.example", "CP+001"),
		"station id with a slash":        stationAuthUrl("a.example", "CP/001"),
		"station id with a space":        stationAuthUrl("a.example", "CP 001"),
		"station id with a leading dot":  stationAuthUrl("a.example", ".CP001"),
		"station id with a trailing dot": stationAuthUrl("a.example", "CP001."),
		"station id of 49 characters":    stationAuthUrl("a.example", strings.Repeat("a", 49)),
		"station id with a control char": stationAuthUrl("a.example", "CP\x00001"),
	}
	for name, target := range targets {
		t.Run(name, func(t *testing.T) {
			status, body := get(t, handler, target)
			assert.Equal(t, http.StatusBadRequest, status)
			var resp internalapi.StatusResponse
			require.NoError(t, json.Unmarshal([]byte(body), &resp))
			assert.Equal(t, "Bad Request", resp.Status)
			assert.NotEmpty(t, resp.Error)
		})
	}

	t.Run("an invalid station id is refused before the host is looked at", func(t *testing.T) {
		status, _ := get(t, handler, stationAuthUrl("c.example", "CP#"))
		assert.Equal(t, http.StatusBadRequest, status)
	})
}

func TestStationAuthInSingleMode(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, engine.SetChargeStationAuth(ctx, "CP001", &store.ChargeStationAuth{
		SecurityProfile:      store.UnsecuredTransportWithBasicAuth,
		Base64SHA256Password: passwordPlain,
	}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.CP001", &store.ChargeStationAuth{
		SecurityProfile: store.TLSWithClientSideCertificates,
		OrgId:           orgC,
	}))
	// a tenant in the store changes nothing in single mode, whatever its config: the
	// gateway is not given a list of organisation names, so it checks against its own
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: orgA, Path: store.OrgPathLabel(orgA), Slug: "tenant-a", OrgKey: "aaaa", Hostnames: []string{"a.example"},
		ConfigVersion: 1, Config: json.RawMessage(`{"version":1,"accepted_cert_org_names":[]}`),
	}))
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: orgC, Path: store.OrgPathLabel(orgC), Slug: "tenant-c", OrgKey: "cccc", Hostnames: []string{"c.example"},
		ConfigVersion: 1, Config: json.RawMessage(`{"version":1,"accepted_cert_org_names":["Charge Co"]}`),
	}))

	for name, mode := range map[string]tenancy.Mode{"single": tenancy.ModeSingle, "not set": ""} {
		t.Run(name, func(t *testing.T) {
			resolver := &tenancy.Resolver{Mode: mode, Tenants: engine, Stations: engine, DefaultOrgId: defaultOrg}
			handler := internalapi.NewHandler(resolver, engine)

			// whatever the host, the key is the plain station id and the org is the default org
			for _, host := range []string{"a.example", "anything.example:9310", "localhost"} {
				status, body := get(t, handler, stationAuthUrl(host, "CP001"))
				require.Equal(t, http.StatusOK, status)
				assert.JSONEq(t, `{
					"orgId": "`+defaultOrg+`",
					"stationKey": "CP001",
					"securityProfile": 0,
					"base64SHA256Password": "`+passwordPlain+`",
					"invalidUsernameAllowed": false
				}`, body)
			}

			// a station id with a dot is a plain id too, and the station keeps its own org
			status, body := get(t, handler, stationAuthUrl("a.example", "aaaa.CP001"))
			require.Equal(t, http.StatusOK, status)
			assert.JSONEq(t, `{
				"orgId": "`+orgC+`",
				"stationKey": "aaaa.CP001",
				"securityProfile": 2,
				"invalidUsernameAllowed": false
			}`, body)

			status, body = get(t, handler, stationAuthUrl("a.example", "CP999"))
			assert.Equal(t, http.StatusNotFound, status)
			assert.JSONEq(t, `{"status":"Not Found","error":"unknown_station"}`, body)
		})
	}
}

func TestStationAuthOrgIsTheOrgThatOwnsTheStation(t *testing.T) {
	ctx := context.Background()
	handler, engine := multiTenantHandler(t)
	require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.no-org", &store.ChargeStationAuth{}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.legacy-org", &store.ChargeStationAuth{OrgId: store.LegacyOrgId}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.moved", &store.ChargeStationAuth{OrgId: orgC}))

	for stationId, wantOrgId := range map[string]string{
		"no-org":     orgA,
		"legacy-org": orgA,
		"moved":      orgC,
	} {
		t.Run(stationId, func(t *testing.T) {
			status, body := get(t, handler, stationAuthUrl("a.example", stationId))
			require.Equal(t, http.StatusOK, status)
			var resp internalapi.StationAuthResponse
			require.NoError(t, json.Unmarshal([]byte(body), &resp))
			assert.Equal(t, wantOrgId, resp.OrgId)
			// the key and the org key are those of the tenant of the host
			assert.Equal(t, "aaaa", resp.OrgKey)
			assert.Equal(t, "aaaa."+stationId, resp.StationKey)
		})
	}
}

func TestStationAuthAcceptedCertOrgNames(t *testing.T) {
	ctx := context.Background()
	handler, engine := multiTenantHandler(t)

	putConfig := func(orgId, config string) {
		tenant, err := engine.GetTenant(ctx, orgId)
		require.NoError(t, err)
		tenant.ConfigVersion++
		tenant.Config = json.RawMessage(config)
		require.NoError(t, engine.PutTenant(ctx, tenant))
		// a new handler: the resolver keeps a tenant and its config for a while
		handler = internalapi.NewHandler(&tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine, DefaultOrgId: defaultOrg}, engine)
	}
	// names returns the member of the answer as it was sent, nil when it was left out
	names := func(host, stationId string) *[]string {
		status, body := get(t, handler, stationAuthUrl(host, stationId))
		require.Equal(t, http.StatusOK, status, body)
		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(body), &raw))
		member, sent := raw["acceptedCertOrgNames"]
		if !sent {
			return nil
		}
		// never null: the gateway would take that for no list
		require.NotEqual(t, "null", string(member))
		var list []string
		require.NoError(t, json.Unmarshal(member, &list))
		return &list
	}
	list := func(names ...string) *[]string {
		l := append([]string{}, names...)
		return &l
	}

	// a tenant that was never given a config
	assert.Equal(t, list(), names("b.example", "CP001"))

	putConfig(orgA, `{"version":1,"accepted_cert_org_names":["Charge Co","Charge Co Services"]}`)
	assert.Equal(t, list("Charge Co", "Charge Co Services"), names("a.example", "CP001"))
	// the names of the other tenant are its own
	assert.Equal(t, list(), names("b.example", "CP001"))

	t.Run("a config that sets no names", func(t *testing.T) {
		putConfig(orgB, `{"version":1,"heartbeat_interval_s":120}`)
		assert.Equal(t, list(), names("b.example", "CP001"))
		putConfig(orgB, `{"version":2,"accepted_cert_org_names":[]}`)
		assert.Equal(t, list(), names("b.example", "CP001"))
	})

	t.Run("the names are those of the tenant that owns the station", func(t *testing.T) {
		putConfig(orgB, `{"version":3,"accepted_cert_org_names":["Other"]}`)
		require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.moved", &store.ChargeStationAuth{
			SecurityProfile: store.TLSWithClientSideCertificates, OrgId: orgB,
		}))
		assert.Equal(t, list("Other"), names("a.example", "moved"))
	})

	t.Run("no list for a station whose org is not a tenant", func(t *testing.T) {
		require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.no-tenant", &store.ChargeStationAuth{
			SecurityProfile: store.TLSWithClientSideCertificates, OrgId: orgC,
		}))
		assert.Nil(t, names("a.example", "no-tenant"))
	})

	t.Run("no list when the stored config cannot be read", func(t *testing.T) {
		// not an empty list, which would switch the check of the organisation off
		putConfig(orgA, `{"version":2,"accepted_cert_org_names":["Charge Co"],"session_rules":{}}`)
		assert.Nil(t, names("a.example", "CP001"))
	})
}

// failingTenantByOrg is an engine that cannot read a tenant by its org.
type failingTenantByOrg struct {
	store.Engine
}

func (failingTenantByOrg) GetTenant(context.Context, string) (*store.Tenant, error) {
	return nil, errors.New("pq: connection refused to db.internal")
}

func TestStationAuthFailsWhenTheConfigOfTheTenantCannotBeLookedUp(t *testing.T) {
	_, engine := multiTenantHandler(t)
	failing := failingTenantByOrg{Engine: engine}
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: failing, Stations: failing}
	handler := internalapi.NewHandler(resolver, failing)

	// not an answer without a list or with an empty one: the gateway must not let a
	// station in on a guess
	status, body := get(t, handler, stationAuthUrl("b.example", "CP001"))
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.JSONEq(t, `{"status":"Internal Server Error"}`, body)
}

func TestStationAuthOfASuspendedTenantIsStillReturned(t *testing.T) {
	ctx := context.Background()
	_, engine := multiTenantHandler(t)

	for _, tenantStatus := range []store.TenantStatus{store.TenantStatusSuspended, store.TenantStatusCancelled} {
		found, err := engine.SetTenantStatus(ctx, orgB, tenantStatus)
		require.NoError(t, err)
		require.True(t, found)
		// a new handler: the resolver keeps a tenant for a while
		resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine}
		handler := internalapi.NewHandler(resolver, engine)

		status, body := get(t, handler, stationAuthUrl("b.example", "CP001"))
		require.Equal(t, http.StatusOK, status)
		var resp internalapi.StationAuthResponse
		require.NoError(t, json.Unmarshal([]byte(body), &resp))
		assert.Equal(t, string(tenantStatus), resp.TenantStatus)
		assert.Equal(t, "bbbb.CP001", resp.StationKey)
		assert.Equal(t, passwordB, resp.Base64SHA256Password)
	}
}

// failingEngine fails the lookups of the internal API.
type failingEngine struct {
	store.Engine
	err error
}

func (e failingEngine) LookupChargeStationAuth(context.Context, string) (*store.ChargeStationAuth, error) {
	return nil, e.err
}

func (e failingEngine) GetTenantByHost(context.Context, string) (*store.Tenant, error) {
	return nil, e.err
}

func (e failingEngine) LookupCertificate(context.Context, string) (string, error) {
	return "", e.err
}

func TestInternalErrorsDoNotLeakDetails(t *testing.T) {
	engine := failingEngine{Engine: inmemory.NewStore(clock.RealClock{}), err: errors.New("pq: password authentication failed for user maeve")}

	handlers := map[string]http.Handler{
		"single mode: the station lookup fails": internalapi.NewHandler(&tenancy.Resolver{Stations: engine}, engine),
		"multi mode: the tenant lookup fails":   internalapi.NewHandler(&tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine}, engine),
	}
	for name, handler := range handlers {
		t.Run(name, func(t *testing.T) {
			status, body := get(t, handler, stationAuthUrl("a.example", "CP001"))
			assert.Equal(t, http.StatusInternalServerError, status)
			assert.JSONEq(t, `{"status":"Internal Server Error"}`, body)
		})
	}

	t.Run("certificate lookup fails", func(t *testing.T) {
		handler := internalapi.NewHandler(&tenancy.Resolver{Stations: engine}, engine)
		status, body := get(t, handler, "/certificates/abc")
		assert.Equal(t, http.StatusInternalServerError, status)
		assert.JSONEq(t, `{"status":"Internal Server Error"}`, body)
	})
}

// certificateEngine has the certificates of a map, by hash.
type certificateEngine struct {
	store.Engine
	certificates map[string]string
}

func (e certificateEngine) LookupCertificate(_ context.Context, certificateHash string) (string, error) {
	return e.certificates[certificateHash], nil
}

func TestLookupCertificate(t *testing.T) {
	const (
		certificateHash = "x4yWb_sBMLlXHk-2V7KBZ6cGlfsLBhD7-GIIPIfSPiA"
		pemCertificate  = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
	)
	engine := certificateEngine{
		Engine:       inmemory.NewStore(clock.RealClock{}),
		certificates: map[string]string{certificateHash: pemCertificate},
	}
	handler := internalapi.NewHandler(&tenancy.Resolver{Stations: engine}, engine)

	t.Run("known hash", func(t *testing.T) {
		status, body := get(t, handler, "/certificates/"+certificateHash)
		require.Equal(t, http.StatusOK, status)
		var resp internalapi.CertificateResponse
		require.NoError(t, json.Unmarshal([]byte(body), &resp))
		assert.Equal(t, pemCertificate, resp.Certificate)
	})

	t.Run("unknown hash", func(t *testing.T) {
		status, body := get(t, handler, "/certificates/unknown")
		assert.Equal(t, http.StatusNotFound, status)
		assert.JSONEq(t, `{"status":"Not Found"}`, body)
	})

	t.Run("hash that is too long", func(t *testing.T) {
		status, _ := get(t, handler, "/certificates/"+strings.Repeat("a", 65))
		assert.Equal(t, http.StatusBadRequest, status)
	})
}

func TestOnlyLookupsAreServed(t *testing.T) {
	handler, _ := multiTenantHandler(t)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(method, stationAuthUrl("a.example", "CP001"), nil))
		assert.Equal(t, http.StatusMethodNotAllowed, rr.Code, method)
	}

	for _, target := range []string{"/", "/station-auth/aaaa.CP001", "/tenants", "/certificates"} {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, target, nil))
		assert.Equal(t, http.StatusNotFound, rr.Code, target)
	}
}
