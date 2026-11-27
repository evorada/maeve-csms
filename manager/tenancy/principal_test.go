// SPDX-License-Identifier: Apache-2.0

package tenancy_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

func TestPrincipalHas(t *testing.T) {
	p := &tenancy.Principal{Perms: []string{tenancy.PermStationRead, tenancy.PermTokenWrite}}

	assert.True(t, p.Has(tenancy.PermStationRead))
	assert.True(t, p.Has(tenancy.PermTokenWrite))
	assert.False(t, p.Has(tenancy.PermStationWrite))
	assert.False(t, p.Has(""))
	assert.False(t, (&tenancy.Principal{}).Has(tenancy.PermStationRead))

	var nobody *tenancy.Principal
	assert.False(t, nobody.Has(tenancy.PermStationRead))
}

func TestPrincipalIsService(t *testing.T) {
	assert.True(t, (&tenancy.Principal{Subject: "svc:provisioner"}).IsService())
	assert.False(t, (&tenancy.Principal{Subject: "user:alice"}).IsService())
	assert.False(t, (&tenancy.Principal{Subject: "machine:svc:x"}).IsService())
	assert.False(t, (&tenancy.Principal{Subject: "svc"}).IsService())
	assert.False(t, (&tenancy.Principal{}).IsService())

	var nobody *tenancy.Principal
	assert.False(t, nobody.IsService())
}

func TestPrincipalContextRoundTrip(t *testing.T) {
	_, ok := tenancy.FromContext(context.Background())
	assert.False(t, ok, "a context without a caller")

	want := &tenancy.Principal{Subject: "user:alice", OrgId: "0193b2c4-0000-7000-8000-0000000000c1"}
	got, ok := tenancy.FromContext(tenancy.WithPrincipal(context.Background(), want))
	require.True(t, ok)
	assert.Same(t, want, got)

	_, ok = tenancy.FromContext(tenancy.WithPrincipal(context.Background(), nil))
	assert.False(t, ok, "a nil caller is no caller")
}

func TestAllPermissionsAreDistinct(t *testing.T) {
	seen := make(map[string]bool)
	for _, perm := range tenancy.AllPermissions {
		assert.NotEmpty(t, perm)
		assert.Falsef(t, seen[perm], "%s is listed twice", perm)
		seen[perm] = true
	}
	assert.Len(t, tenancy.AllPermissions, 11)
}

func TestManifestPermissionsAreAllPermissionsButTheOneOfTheControlPlane(t *testing.T) {
	assert.NotContains(t, tenancy.ManifestPermissions, tenancy.PermTenantManage)
	assert.Len(t, tenancy.ManifestPermissions, len(tenancy.AllPermissions)-1)
	// in the order of the catalogue
	withoutTenantManage := []string{}
	for _, perm := range tenancy.AllPermissions {
		if perm != tenancy.PermTenantManage {
			withoutTenantManage = append(withoutTenantManage, perm)
		}
	}
	assert.Equal(t, withoutTenantManage, tenancy.ManifestPermissions)
	assert.Contains(t, tenancy.AllPermissions, tenancy.PermTenantManage)
}
