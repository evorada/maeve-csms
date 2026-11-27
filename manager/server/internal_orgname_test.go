// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
)

// failingTenantByOrg is a tenant store that cannot read a tenant by its org.
type failingTenantByOrg struct {
	store.TenantStore
}

func (failingTenantByOrg) GetTenant(context.Context, string) (*store.Tenant, error) {
	return nil, errors.New("database is down")
}

func TestCertOrgNameOfTheAdminPagesIsTheOneOfTheTenant(t *testing.T) {
	ctx := context.Background()
	const (
		orgA = "0193b2c4-0000-7000-8000-0000000000a1"
		orgB = "0193b2c4-0000-7000-8000-0000000000b2"
	)
	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: orgA, Path: store.OrgPathLabel(orgA), Slug: "tenant-a", OrgKey: "aaaa",
		ConfigVersion: 1, Config: json.RawMessage(`{"version":1,"accepted_cert_org_names":["Charge Co","Charge Co Services"]}`),
	}))
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: orgB, Path: store.OrgPathLabel(orgB), Slug: "tenant-b", OrgKey: "bbbb",
	}))

	multi := certOrgNameFor(&tenancy.ConfigProvider{
		Resolver: &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine},
	}, "Configured")
	// the first name that the tenant of the charge station accepts in a certificate
	assert.Equal(t, "Charge Co", multi(ctx, "aaaa.cs001"))
	// a tenant without names, and a charge station without a tenant
	assert.Equal(t, "Configured", multi(ctx, "bbbb.cs001"))
	assert.Equal(t, "Configured", multi(ctx, "cs001"))

	single := certOrgNameFor(&tenancy.ConfigProvider{
		Resolver: &tenancy.Resolver{Mode: tenancy.ModeSingle, Tenants: engine, Stations: engine},
	}, "Configured")
	assert.Equal(t, "Configured", single(ctx, "aaaa.cs001"))
	assert.Equal(t, "Configured", certOrgNameFor(&tenancy.ConfigProvider{}, "Configured")(ctx, "aaaa.cs001"))

	failing := certOrgNameFor(&tenancy.ConfigProvider{
		Resolver: &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: failingTenantByOrg{TenantStore: engine}, Stations: engine},
	}, "Configured")
	assert.Equal(t, "Configured", failing(ctx, "aaaa.cs001"))
}
