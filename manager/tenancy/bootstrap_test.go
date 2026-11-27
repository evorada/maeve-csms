// SPDX-License-Identifier: Apache-2.0

package tenancy_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
)

func TestBootstrapUpsertsTheTenants(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	cfg := []tenancy.BootstrapTenant{
		{OrgId: orgA, Slug: "dev1", OrgKey: "dev1", Hostnames: []string{"localhost"}},
		{OrgId: strings.ToUpper(orgB), Slug: "dev2", OrgKey: "dev2", Hostnames: []string{"127.0.0.1", "dev2.example"}},
	}

	require.NoError(t, tenancy.Bootstrap(ctx, engine, cfg))

	tenantA, err := engine.GetTenant(ctx, orgA)
	require.NoError(t, err)
	require.NotNil(t, tenantA)
	assert.Equal(t, "dev1", tenantA.Slug)
	assert.Equal(t, "dev1", tenantA.OrgKey)
	assert.Equal(t, []string{"localhost"}, tenantA.Hostnames)
	assert.Equal(t, store.OrgPathLabel(orgA), tenantA.Path)
	assert.Nil(t, tenantA.ParentId)
	assert.Equal(t, store.TenantStatusActive, tenantA.Status)
	assert.Equal(t, int64(0), tenantA.EntitlementsVersion)
	assert.JSONEq(t, "{}", string(tenantA.Entitlements))
	assert.Equal(t, int64(0), tenantA.ConfigVersion)
	assert.JSONEq(t, "{}", string(tenantA.Config))

	// the org id is stored in its canonical form, and so is the path
	tenantB, err := engine.GetTenantByHost(ctx, "127.0.0.1")
	require.NoError(t, err)
	require.NotNil(t, tenantB)
	assert.Equal(t, orgB, tenantB.OrgId)
	assert.Equal(t, store.OrgPathLabel(orgB), tenantB.Path)
	assert.Equal(t, []string{"127.0.0.1", "dev2.example"}, tenantB.Hostnames)

	t.Run("again: the tenants are updated, a status that was set is kept", func(t *testing.T) {
		found, err := engine.SetTenantStatus(ctx, orgA, store.TenantStatusSuspended)
		require.NoError(t, err)
		require.True(t, found)
		cfg[0].Hostnames = []string{"dev1.example"}

		require.NoError(t, tenancy.Bootstrap(ctx, engine, cfg))

		tenantA, err := engine.GetTenant(ctx, orgA)
		require.NoError(t, err)
		assert.Equal(t, []string{"dev1.example"}, tenantA.Hostnames)
		assert.Equal(t, store.TenantStatusSuspended, tenantA.Status)
		tenants, err := engine.ListTenants(ctx, "", 10)
		require.NoError(t, err)
		assert.Len(t, tenants, 2)
	})

	t.Run("nothing to do without tenants", func(t *testing.T) {
		require.NoError(t, tenancy.Bootstrap(ctx, nil, nil))
	})
}

func TestBootstrapFailsForAnInvalidTenant(t *testing.T) {
	valid := tenancy.BootstrapTenant{OrgId: orgA, Slug: "dev1", OrgKey: "dev1", Hostnames: []string{"localhost"}}

	tests := map[string]struct {
		tenant  tenancy.BootstrapTenant
		wantErr string
	}{
		"invalid org key":       {tenant: tenancy.BootstrapTenant{OrgId: orgB, Slug: "dev2", OrgKey: "DEV2"}, wantErr: "invalid org key"},
		"org key with a dot":    {tenant: tenancy.BootstrapTenant{OrgId: orgB, Slug: "dev2", OrgKey: "de.v2"}, wantErr: "invalid org key"},
		"invalid slug":          {tenant: tenancy.BootstrapTenant{OrgId: orgB, Slug: "d", OrgKey: "dev2"}, wantErr: "invalid slug"},
		"invalid hostname":      {tenant: tenancy.BootstrapTenant{OrgId: orgB, Slug: "dev2", OrgKey: "dev2", Hostnames: []string{"dev2.example:443"}}, wantErr: "invalid hostname"},
		"org id is not a uuid":  {tenant: tenancy.BootstrapTenant{OrgId: "dev2", Slug: "dev2", OrgKey: "dev2"}, wantErr: "invalid org id"},
		"org id that is listed": {tenant: tenancy.BootstrapTenant{OrgId: orgA, Slug: "dev2", OrgKey: "dev2"}, wantErr: "more than once"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			engine := inmemory.NewStore(clock.RealClock{})

			err := tenancy.Bootstrap(ctx, engine, []tenancy.BootstrapTenant{valid, tt.tenant})
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)

			// the valid tenant before it is not written either
			tenants, err := engine.ListTenants(ctx, "", 10)
			require.NoError(t, err)
			assert.Empty(t, tenants)
		})
	}

	t.Run("a tenant that clashes with another one", func(t *testing.T) {
		engine := inmemory.NewStore(clock.RealClock{})
		clash := tenancy.BootstrapTenant{OrgId: orgB, Slug: "dev2", OrgKey: "dev2", Hostnames: []string{"localhost"}}

		err := tenancy.Bootstrap(context.Background(), engine, []tenancy.BootstrapTenant{valid, clash})
		assert.ErrorIs(t, err, store.ErrTenantConflict)
	})

	t.Run("without a tenant store", func(t *testing.T) {
		assert.Error(t, tenancy.Bootstrap(context.Background(), nil, []tenancy.BootstrapTenant{valid}))
	})
}

func TestValidateTenantIdentity(t *testing.T) {
	valid := map[string]struct {
		slug, orgKey string
		hostnames    []string
	}{
		"shortest names":             {slug: "abc", orgKey: "ab12"},
		"longest names":              {slug: strings.Repeat("a", 32), orgKey: "abcd1234"},
		"slug with a hyphen":         {slug: "acme-charging", orgKey: "acme"},
		"hostnames":                  {slug: "acme", orgKey: "acme", hostnames: []string{"acme.ocpp.evorada.io", "localhost", "127.0.0.1", "xn--mnchen-3ya.example"}},
		"hostname of 253 chars":      {slug: "acme", orgKey: "acme", hostnames: []string{strings.Repeat(strings.Repeat("a", 62)+".", 4) + "a"}},
		"hostname label of 63":       {slug: "acme", orgKey: "acme", hostnames: []string{strings.Repeat("a", 63) + ".example"}},
		"no hostname":                {slug: "acme", orgKey: "acme", hostnames: []string{}},
		"digits only in the org key": {slug: "acme", orgKey: "1234"},
	}
	for name, tt := range valid {
		t.Run(name, func(t *testing.T) {
			assert.NoError(t, tenancy.ValidateTenantIdentity(tt.slug, tt.orgKey, tt.hostnames))
		})
	}

	invalid := map[string]struct {
		slug, orgKey string
		hostnames    []string
	}{
		"empty slug":                 {slug: "", orgKey: "acme"},
		"slug too short":             {slug: "ab", orgKey: "acme"},
		"slug too long":              {slug: strings.Repeat("a", 33), orgKey: "acme"},
		"slug in upper case":         {slug: "Acme", orgKey: "acme"},
		"slug with a dot":            {slug: "acme.io", orgKey: "acme"},
		"slug with a newline":        {slug: "acme\n", orgKey: "acme"},
		"empty org key":              {slug: "acme", orgKey: ""},
		"org key too short":          {slug: "acme", orgKey: "abc"},
		"org key too long":           {slug: "acme", orgKey: "abcd12345"},
		"org key in upper case":      {slug: "acme", orgKey: "ACME"},
		"org key with a dot":         {slug: "acme", orgKey: "ac.me"},
		"org key with a hyphen":      {slug: "acme", orgKey: "ac-me"},
		"org key with a wildcard":    {slug: "acme", orgKey: "acm*"},
		"org key with a newline":     {slug: "acme", orgKey: "acme\n"},
		"empty hostname":             {slug: "acme", orgKey: "acme", hostnames: []string{""}},
		"hostname in upper case":     {slug: "acme", orgKey: "acme", hostnames: []string{"Acme.example"}},
		"hostname with a port":       {slug: "acme", orgKey: "acme", hostnames: []string{"acme.example:443"}},
		"hostname with trailing dot": {slug: "acme", orgKey: "acme", hostnames: []string{"acme.example."}},
		"wildcard hostname":          {slug: "acme", orgKey: "acme", hostnames: []string{"*.example"}},
		"hostname with a scheme":     {slug: "acme", orgKey: "acme", hostnames: []string{"wss://acme.example"}},
		"hostname with a path":       {slug: "acme", orgKey: "acme", hostnames: []string{"acme.example/ws"}},
		"hostname with a space":      {slug: "acme", orgKey: "acme", hostnames: []string{"acme .example"}},
		"hostname with underscore":   {slug: "acme", orgKey: "acme", hostnames: []string{"acme_1.example"}},
		"label starts with hyphen":   {slug: "acme", orgKey: "acme", hostnames: []string{"-acme.example"}},
		"empty label":                {slug: "acme", orgKey: "acme", hostnames: []string{"acme..example"}},
		"label of 64 chars":          {slug: "acme", orgKey: "acme", hostnames: []string{strings.Repeat("a", 64) + ".example"}},
		"hostname of 254 chars":      {slug: "acme", orgKey: "acme", hostnames: []string{strings.Repeat(strings.Repeat("a", 62)+".", 4) + "ab"}},
		"ipv6 literal":               {slug: "acme", orgKey: "acme", hostnames: []string{"[::1]"}},
		"second hostname is invalid": {slug: "acme", orgKey: "acme", hostnames: []string{"acme.example", "ACME.example"}},
	}
	for name, tt := range invalid {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, tenancy.ValidateTenantIdentity(tt.slug, tt.orgKey, tt.hostnames))
		})
	}
}

func TestValidateTenantIdentityNamesTheField(t *testing.T) {
	tests := map[string]struct {
		slug, orgKey string
		hostnames    []string
		field        string
	}{
		"slug":      {"A", "acme", []string{"acme.example.com"}, tenancy.IdentityFieldSlug},
		"org key":   {"acme", "K7F2", []string{"acme.example.com"}, tenancy.IdentityFieldOrgKey},
		"hostname":  {"acme", "acme", []string{"acme.example.com", "acme.example.com:443"}, tenancy.IdentityFieldHostnames},
		"the first": {"A", "K7F2", []string{"*"}, tenancy.IdentityFieldSlug},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tenancy.ValidateTenantIdentity(tt.slug, tt.orgKey, tt.hostnames)
			var identityError *tenancy.IdentityError
			require.ErrorAs(t, err, &identityError)
			assert.Equal(t, tt.field, identityError.Field)
			assert.NotEmpty(t, identityError.Error())
		})
	}
}
