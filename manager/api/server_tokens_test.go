// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

func storedToken(orgId, uid, visualNumber string) *store.Token {
	return &store.Token{
		OrgId:        orgId,
		CountryCode:  "GB",
		PartyId:      "TWK",
		Type:         "RFID",
		Uid:          uid,
		ContractId:   "GBTWK012345678V",
		VisualNumber: &visualNumber,
		Issuer:       "TWK",
		Valid:        true,
		CacheMode:    store.CacheModeAlways,
		LastUpdated:  "2026-01-02T03:04:05Z",
	}
}

func tokenBody(uid, orgId string) string {
	body := map[string]any{
		"countryCode": "GB", "partyId": "TWK", "type": "RFID", "uid": uid,
		"contractId": "GBTWK012345678V", "issuer": "TWK", "valid": true, "cacheMode": "ALWAYS",
	}
	if orgId != "" {
		body["orgId"] = orgId
	}
	b, _ := json.Marshal(body)
	return string(b)
}

// listedTokens returns "org/uid" of every token of a token list response, sorted.
func listedTokens(t *testing.T, body []byte) []string {
	t.Helper()
	var tokens []api.Token
	require.NoError(t, json.Unmarshal(body, &tokens))
	got := make([]string, 0, len(tokens))
	for _, token := range tokens {
		require.NotNil(t, token.OrgId)
		got = append(got, fmt.Sprintf("%s/%s", token.OrgId, token.Uid))
	}
	sort.Strings(got)
	return got
}

// newTokenTenants returns the tenants A, B and C with a token "SHARED" each (the same
// UID in every org) and a token of their own.
func newTokenTenants(t *testing.T) *multiTenant {
	t.Helper()
	m := newMultiTenant(t)
	ctx := context.Background()
	for _, token := range []*store.Token{
		storedToken(orgA, "SHARED", "of A"),
		storedToken(orgB, "SHARED", "of B"),
		storedToken(orgC, "SHARED", "of C"),
		storedToken(orgA, "ONLYA", "of A"),
		storedToken(orgB, "ONLYB", "of B"),
		storedToken(orgC, "ONLYC", "of C"),
	} {
		require.NoError(t, m.engine.SetToken(ctx, token))
	}
	return m
}

func TestListTokensOnlyListsTheOrgsOfTheCaller(t *testing.T) {
	m := newTokenTenants(t)
	r := m.router(t)

	tests := map[string]struct {
		caller *tenancy.Principal
		query  string
		want   []string
	}{
		"a tenant lists its own tokens":                 {callerOf(orgA, tenancy.OrgScopeSelf), "", []string{orgA + "/ONLYA", orgA + "/SHARED"}},
		"a tenant asks for its own org":                 {callerOf(orgA, tenancy.OrgScopeSelf), "?orgId=" + orgA, []string{orgA + "/ONLYA", orgA + "/SHARED"}},
		"a tenant asks for another org":                 {callerOf(orgA, tenancy.OrgScopeSelf), "?orgId=" + orgB, []string{}},
		"a partner lists the tokens of the orgs below":  {callerOf(orgPartner, tenancy.OrgScopeSubtree), "", []string{orgA + "/ONLYA", orgA + "/SHARED", orgB + "/ONLYB", orgB + "/SHARED"}},
		"a partner narrows the list to one org":         {callerOf(orgPartner, tenancy.OrgScopeSubtree), "?orgId=" + orgB, []string{orgB + "/ONLYB", orgB + "/SHARED"}},
		"a partner asks for an org that is not below":   {callerOf(orgPartner, tenancy.OrgScopeSubtree), "?orgId=" + orgC, []string{}},
		"a partner with the scope self reaches nothing": {callerOf(orgPartner, tenancy.OrgScopeSelf), "", []string{}},
		"a scope that is not known reaches nothing":     {callerOf(orgA, tenancy.OrgScope("all")), "", []string{}},
		"paging stays inside the orgs of the caller":    {callerOf(orgA, tenancy.OrgScopeSelf), "?offset=0&limit=100", []string{orgA + "/ONLYA", orgA + "/SHARED"}},
		"an offset past the own tokens gives no others": {callerOf(orgA, tenancy.OrgScopeSelf), "?offset=2&limit=100", []string{}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rr := serve(r, as(tt.caller, http.MethodGet, "/token"+tt.query, ""))
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			assert.Equal(t, tt.want, listedTokens(t, rr.Body.Bytes()))
		})
	}

	t.Run("an org id that is not a UUID is refused", func(t *testing.T) {
		rr := serve(r, as(callerOf(orgA, tenancy.OrgScopeSelf), http.MethodGet, "/token?orgId=all", ""))
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("without the permission", func(t *testing.T) {
		p := callerOf(orgA, tenancy.OrgScopeSelf)
		p.Perms = []string{tenancy.PermTokenWrite, tenancy.PermStationRead}
		rr := serve(r, as(p, http.MethodGet, "/token", ""))
		require.Equal(t, http.StatusForbidden, rr.Code)
		assert.JSONEq(t, `{"status":"Forbidden","error":"missing permission cpms.token.read"}`, rr.Body.String())
	})
}

func TestLookupTokenOnlyFindsTheTokensOfTheCaller(t *testing.T) {
	m := newTokenTenants(t)
	r := m.router(t)

	tests := map[string]struct {
		caller  *tenancy.Principal
		target  string
		wantOrg string // "" when the token must not be found
	}{
		"the own token":                               {callerOf(orgA, tenancy.OrgScopeSelf), "/token/SHARED", orgA},
		"the same UID for another tenant":             {callerOf(orgB, tenancy.OrgScopeSelf), "/token/SHARED", orgB},
		"the own org is named":                        {callerOf(orgA, tenancy.OrgScopeSelf), "/token/SHARED?orgId=" + orgA, orgA},
		"a token that only another tenant has":        {callerOf(orgA, tenancy.OrgScopeSelf), "/token/ONLYB", ""},
		"a token of another tenant, org named":        {callerOf(orgA, tenancy.OrgScopeSelf), "/token/ONLYB?orgId=" + orgB, ""},
		"the same UID in another org, org named":      {callerOf(orgA, tenancy.OrgScopeSelf), "/token/SHARED?orgId=" + orgB, ""},
		"a partner names an org below":                {callerOf(orgPartner, tenancy.OrgScopeSubtree), "/token/SHARED?orgId=" + orgB, orgB},
		"a partner names the other org below":         {callerOf(orgPartner, tenancy.OrgScopeSubtree), "/token/SHARED?orgId=" + orgA, orgA},
		"a partner names an org that is not below":    {callerOf(orgPartner, tenancy.OrgScopeSubtree), "/token/SHARED?orgId=" + orgC, ""},
		"a partner that is not a tenant names no org": {callerOf(orgPartner, tenancy.OrgScopeSubtree), "/token/SHARED", ""},
		"a partner with the scope self":               {callerOf(orgPartner, tenancy.OrgScopeSelf), "/token/SHARED?orgId=" + orgA, ""},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rr := serve(r, as(tt.caller, http.MethodGet, tt.target, ""))
			if tt.wantOrg == "" {
				// exactly the answer for a token that does not exist
				unknown := serve(r, as(tt.caller, http.MethodGet, "/token/NOSUCHTOKEN", ""))
				require.Equal(t, http.StatusNotFound, unknown.Code)
				require.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())
				assert.JSONEq(t, notFoundBody, rr.Body.String())
				assert.Equal(t, unknown.Body.String(), rr.Body.String())
				return
			}

			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			var got api.Token
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
			require.NotNil(t, got.OrgId)
			assert.Equal(t, tt.wantOrg, got.OrgId.String())
			assert.Equal(t, "SHARED", got.Uid)
		})
	}
}

func TestSetTokenOnlyWritesTheOrgsOfTheCaller(t *testing.T) {
	ctx := context.Background()

	tests := map[string]struct {
		caller     *tenancy.Principal
		orgId      string // of the request body
		wantStatus int
		wantBody   string
		wantOrg    string // the org that gets the token
	}{
		"the org of the caller":                          {callerOf(orgA, tenancy.OrgScopeSelf), "", http.StatusCreated, "", orgA},
		"the org of the caller is named":                 {callerOf(orgA, tenancy.OrgScopeSelf), orgA, http.StatusCreated, "", orgA},
		"another org":                                    {callerOf(orgA, tenancy.OrgScopeSelf), orgB, http.StatusNotFound, notFoundBody, ""},
		"an org that does not exist":                     {callerOf(orgA, tenancy.OrgScopeSelf), "0193b2c4-0000-7000-8000-00000000dead", http.StatusNotFound, notFoundBody, ""},
		"a partner names an org below":                   {callerOf(orgPartner, tenancy.OrgScopeSubtree), orgB, http.StatusCreated, "", orgB},
		"a partner names an org that is not below":       {callerOf(orgPartner, tenancy.OrgScopeSubtree), orgC, http.StatusNotFound, notFoundBody, ""},
		"a partner that is not a tenant names no org":    {callerOf(orgPartner, tenancy.OrgScopeSubtree), "", http.StatusBadRequest, `{"status":"Bad Request","error":"orgId is required"}`, ""},
		"a partner that is not a tenant names itself":    {callerOf(orgPartner, tenancy.OrgScopeSubtree), orgPartner, http.StatusNotFound, notFoundBody, ""},
		"a tenant with the scope subtree names no org":   {callerOf(orgA, tenancy.OrgScopeSubtree), "", http.StatusCreated, "", orgA},
		"a partner with the scope self names an org":     {callerOf(orgPartner, tenancy.OrgScopeSelf), orgA, http.StatusNotFound, notFoundBody, ""},
		"a caller without the permission, org below":     {withPerms(callerOf(orgPartner, tenancy.OrgScopeSubtree), tenancy.PermTokenRead), orgB, http.StatusForbidden, `{"status":"Forbidden","error":"missing permission cpms.token.write"}`, ""},
		"a caller without the permission, org not below": {withPerms(callerOf(orgPartner, tenancy.OrgScopeSubtree), tenancy.PermTokenRead), orgC, http.StatusForbidden, `{"status":"Forbidden","error":"missing permission cpms.token.write"}`, ""},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			m := newTokenTenants(t)
			r := m.router(t)
			before, err := m.engine.ListTokens(ctx, 0, 100)
			require.NoError(t, err)

			rr := serve(r, as(tt.caller, http.MethodPost, "/token", tokenBody("NEWTOKEN", tt.orgId)))
			require.Equal(t, tt.wantStatus, rr.Code, rr.Body.String())
			if tt.wantBody != "" {
				assert.JSONEq(t, tt.wantBody, rr.Body.String())
			}

			after, err := m.engine.ListTokens(ctx, 0, 100)
			require.NoError(t, err)
			if tt.wantOrg == "" {
				assert.Len(t, after, len(before), "nothing is stored")
				return
			}
			require.Len(t, after, len(before)+1)
			for _, orgId := range []string{orgA, orgB, orgC, orgPartner} {
				got, err := m.engine.LookupTokenForOrg(ctx, orgId, "NEWTOKEN")
				require.NoError(t, err)
				assert.Equal(t, orgId == tt.wantOrg, got != nil, "org %s", orgId)
			}
		})
	}

	t.Run("a token of another org with the same UID is not replaced", func(t *testing.T) {
		m := newTokenTenants(t)
		r := m.router(t)

		rr := serve(r, as(callerOf(orgA, tenancy.OrgScopeSelf), http.MethodPost, "/token", tokenBody("SHARED", "")))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

		ofB, err := m.engine.LookupTokenForOrg(ctx, orgB, "SHARED")
		require.NoError(t, err)
		assert.Equal(t, storedToken(orgB, "SHARED", "of B"), ofB)
		ofA, err := m.engine.LookupTokenForOrg(ctx, orgA, "SHARED")
		require.NoError(t, err)
		assert.Nil(t, ofA.VisualNumber, "the token of A is the new one")
	})
}

func withPerms(p *tenancy.Principal, perms ...string) *tenancy.Principal {
	p.Perms = perms
	return p
}

func TestTokensInSingleMode(t *testing.T) {
	ctx := context.Background()
	server, _, engine, c := setupServer(t)
	defer server.Close()
	require.NoError(t, engine.SetToken(ctx, storedToken(testOrgId, "SHARED", "of the default org")))
	// of an org that the manager does not serve in single mode
	require.NoError(t, engine.SetToken(ctx, storedToken(orgA, "SHARED", "of A")))

	scoper := &tenancy.Scoper{
		Mode:         tenancy.ModeSingle,
		Resolver:     &tenancy.Resolver{Stations: engine, DefaultOrgId: testOrgId},
		DefaultOrgId: testOrgId,
	}
	r := newRouter(t, engine, c, nil, scoper, func(r *http.Request) *tenancy.Principal {
		p, _ := tenancy.FromContext(r.Context())
		return p
	})

	for _, scope := range []tenancy.OrgScope{tenancy.OrgScopeSelf, tenancy.OrgScopeSubtree} {
		defaultCaller := callerOf(testOrgId, scope)
		rr := serve(r, as(defaultCaller, http.MethodGet, "/token", ""))
		require.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, []string{testOrgId + "/SHARED"}, listedTokens(t, rr.Body.Bytes()))
		assert.Equal(t, http.StatusOK, serve(r, as(defaultCaller, http.MethodGet, "/token/SHARED", "")).Code)
		assert.Equal(t, http.StatusNotFound, serve(r, as(defaultCaller, http.MethodGet, "/token/SHARED?orgId="+orgA, "")).Code)
		assert.Equal(t, http.StatusNotFound, serve(r, as(defaultCaller, http.MethodPost, "/token", tokenBody("NEW1", orgA))).Code)

		// a caller of another org reaches nothing, not even its own org
		otherCaller := callerOf(orgA, scope)
		rr = serve(r, as(otherCaller, http.MethodGet, "/token", ""))
		require.Equal(t, http.StatusOK, rr.Code)
		assert.Empty(t, listedTokens(t, rr.Body.Bytes()))
		assert.Equal(t, http.StatusNotFound, serve(r, as(otherCaller, http.MethodGet, "/token/SHARED", "")).Code)
		assert.Equal(t, http.StatusNotFound, serve(r, as(otherCaller, http.MethodPost, "/token", tokenBody("NEW2", ""))).Code)
		assert.Equal(t, http.StatusNotFound, serve(r, as(otherCaller, http.MethodPost, "/token", tokenBody("NEW3", testOrgId))).Code)
	}

	tokens, err := engine.ListTokens(ctx, 0, 100)
	require.NoError(t, err)
	assert.Len(t, tokens, 2, "nothing was stored")
}
