// SPDX-License-Identifier: Apache-2.0

package stations_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/stations"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
)

func TestLoadOnboardingSettings(t *testing.T) {
	ctx := context.Background()
	const orgId = "0193b2c4-0000-7000-8000-0000000000a1"
	disabled := stations.OnboardingSettings{OpenSP1: true, MaxPending: 50, BootInterval: time.Minute}

	for name, tc := range map[string]struct {
		config string
		want   stations.OnboardingSettings
	}{
		"a tenant without a config": {`{}`, stations.OnboardingSettings{Enabled: true, OpenSP1: true, MaxPending: 50, BootInterval: time.Minute}},
		"every setting": {
			`{"version":1,"onboarding_mode":"pending_approval","onboarding_open_sp1":false,` +
				`"onboarding_password_sha256":"XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg=","onboarding_max_pending":5,"onboarding_boot_interval_s":30}`,
			stations.OnboardingSettings{Enabled: true, OpenSP1: false, PasswordSHA256: "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg=", MaxPending: 5, BootInterval: 30 * time.Second},
		},
		"preregistered only": {`{"version":1,"onboarding_mode":"preregistered_only","onboarding_max_pending":5}`,
			stations.OnboardingSettings{OpenSP1: true, MaxPending: 5, BootInterval: time.Minute}},
		// what the tenant wants is not known: nothing is onboarded
		"a config that cannot be read": {`{"version":1,"onboarding_mode":"open"}`, disabled},
	} {
		t.Run(name, func(t *testing.T) {
			engine := inmemory.NewStore(clock.RealClock{})
			require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
				OrgId: orgId, Path: store.OrgPathLabel(orgId), Slug: "tenant-a", OrgKey: "aaaa", Hostnames: []string{"a.example"},
				ConfigVersion: 1, Config: json.RawMessage(tc.config),
			}))
			provider := &tenancy.ConfigProvider{Resolver: &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine}}
			got, err := stations.LoadOnboardingSettings(ctx, provider, orgId)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("nothing is onboarded without a tenant", func(t *testing.T) {
		engine := inmemory.NewStore(clock.RealClock{})
		require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
			OrgId: orgId, Path: store.OrgPathLabel(orgId), Slug: "tenant-a", OrgKey: "aaaa", Hostnames: []string{"a.example"},
		}))
		for name, provider := range map[string]*tenancy.ConfigProvider{
			"single mode":        {Resolver: &tenancy.Resolver{Mode: tenancy.ModeSingle, Tenants: engine, Stations: engine}},
			"no provider":        nil,
			"an org that is not": {Resolver: &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: inmemory.NewStore(clock.RealClock{}), Stations: engine}},
		} {
			got, err := stations.LoadOnboardingSettings(ctx, provider, orgId)
			require.NoError(t, err, name)
			assert.Equal(t, disabled, got, name)
		}
	})

	t.Run("a tenant store that fails", func(t *testing.T) {
		provider := &tenancy.ConfigProvider{Resolver: &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: failingTenants{}}}
		_, err := stations.LoadOnboardingSettings(ctx, provider, orgId)
		assert.Error(t, err)
	})
}

// failingTenants cannot read a tenant.
type failingTenants struct {
	store.TenantStore
}

func (failingTenants) GetTenant(context.Context, string) (*store.Tenant, error) {
	return nil, errors.New("database is down")
}
