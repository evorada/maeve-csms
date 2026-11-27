// SPDX-License-Identifier: Apache-2.0

package tenancy_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// putConfig stores a config document for a tenant of newMultiResolver, the way the
// control plane does, without telling the resolver.
func putConfig(t *testing.T, engine *inmemory.Store, orgId string, version int64, config string) {
	t.Helper()
	ctx := context.Background()
	tenant, err := engine.GetTenant(ctx, orgId)
	require.NoError(t, err)
	require.NotNil(t, tenant)
	tenant.ConfigVersion = version
	tenant.Config = json.RawMessage(config)
	require.NoError(t, engine.PutTenant(ctx, tenant))
}

func TestConfigProviderReturnsTheConfigOfEachOrg(t *testing.T) {
	ctx := context.Background()
	resolver, tenants, engine, fakeClock := newMultiResolver(t)
	putConfig(t, engine, orgA, 3, `{"version":3,"default_security_profile":"SP3","accepted_cert_org_names":["Charge Co"],"heartbeat_interval_s":120}`)
	provider := &tenancy.ConfigProvider{Resolver: resolver}

	cfgA, err := provider.ConfigForOrg(ctx, strings.ToUpper(orgA))
	require.NoError(t, err)
	assert.EqualValues(t, 3, cfgA.Version)
	assert.Equal(t, store.TLSWithClientSideCertificates, cfgA.SecurityProfile())
	assert.Equal(t, 2*time.Minute, cfgA.HeartbeatInterval(5*time.Minute))
	names, known, err := provider.AcceptedCertOrgNames(ctx, orgA)
	require.NoError(t, err)
	assert.True(t, known)
	assert.Equal(t, []string{"Charge Co"}, names)

	// the other tenant was never given a config: the defaults, and a list that is empty
	cfgB, err := provider.ConfigForOrg(ctx, orgB)
	require.NoError(t, err)
	assert.Equal(t, &tenancy.TenantConfig{}, cfgB)
	assert.Equal(t, store.TLSWithBasicAuth, cfgB.SecurityProfile())
	assert.Equal(t, 5*time.Minute, cfgB.HeartbeatInterval(5*time.Minute))
	names, known, err = provider.AcceptedCertOrgNames(ctx, orgB)
	require.NoError(t, err)
	assert.True(t, known)
	assert.Equal(t, []string{}, names)

	// an org that is not a tenant: the defaults, and no list at all
	for _, orgId := range []string{defaultOrg, store.LegacyOrgId, ""} {
		cfg, err := provider.ConfigForOrg(ctx, orgId)
		require.NoError(t, err)
		assert.Equal(t, &tenancy.TenantConfig{}, cfg, orgId)
		names, known, err = provider.AcceptedCertOrgNames(ctx, orgId)
		require.NoError(t, err)
		assert.False(t, known, orgId)
		assert.Nil(t, names, orgId)
	}

	t.Run("the config is kept with the tenant", func(t *testing.T) {
		before := tenants.byOrgId
		for i := 0; i < 3; i++ {
			_, err := provider.ConfigForOrg(ctx, orgA)
			require.NoError(t, err)
		}
		assert.Equal(t, before, tenants.byOrgId)

		// another instance of the manager stored a new config: seen when the tenant is read again
		putConfig(t, engine, orgA, 4, `{"version":4,"heartbeat_interval_s":600}`)
		cfg, err := provider.ConfigForOrg(ctx, orgA)
		require.NoError(t, err)
		assert.EqualValues(t, 3, cfg.Version)

		fakeClock.Step(30 * time.Second)
		cfg, err = provider.ConfigForOrg(ctx, orgA)
		require.NoError(t, err)
		assert.EqualValues(t, 4, cfg.Version)
		assert.Equal(t, 10*time.Minute, cfg.HeartbeatInterval(5*time.Minute))
		// what the new config does not set is back at its default
		assert.Equal(t, store.TLSWithBasicAuth, cfg.SecurityProfile())
		assert.Equal(t, before+1, tenants.byOrgId)
	})

	t.Run("a caller cannot change the names that are kept", func(t *testing.T) {
		putConfig(t, engine, orgB, 1, `{"version":1,"accepted_cert_org_names":["Other"]}`)
		resolver.Invalidate(orgB)
		names, _, err := provider.AcceptedCertOrgNames(ctx, orgB)
		require.NoError(t, err)
		names[0] = "changed"
		names, _, err = provider.AcceptedCertOrgNames(ctx, orgB)
		require.NoError(t, err)
		assert.Equal(t, []string{"Other"}, names)
	})
}

func TestConfigProviderInSingleModeHasTheDefaults(t *testing.T) {
	ctx := context.Background()
	resolver, tenants, engine, _ := newMultiResolver(t)
	putConfig(t, engine, orgA, 3, `{"version":3,"default_security_profile":"SP1","accepted_cert_org_names":["Charge Co"],"heartbeat_interval_s":120}`)
	require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.CP001", &store.ChargeStationAuth{OrgId: orgA}))
	resolver.Mode = tenancy.ModeSingle

	providers := map[string]*tenancy.ConfigProvider{
		"single mode":         {Resolver: resolver},
		"without a resolver":  {},
		"without a provider":  nil,
		"an unset mode":       {Resolver: &tenancy.Resolver{Tenants: engine, Stations: engine}},
		"no tenants, no mode": {Resolver: &tenancy.Resolver{Stations: engine}},
	}
	for name, provider := range providers {
		t.Run(name, func(t *testing.T) {
			cfg, err := provider.ConfigForOrg(ctx, orgA)
			require.NoError(t, err)
			assert.Equal(t, &tenancy.TenantConfig{}, cfg)
			assert.Equal(t, store.TLSWithBasicAuth, cfg.SecurityProfile())

			cfg, err = provider.ConfigForStation(ctx, "aaaa.CP001")
			require.NoError(t, err)
			assert.Equal(t, &tenancy.TenantConfig{}, cfg)
			assert.Equal(t, 5*time.Minute, provider.HeartbeatIntervalForStation(ctx, "aaaa.CP001", 5*time.Minute))

			names, known, err := provider.AcceptedCertOrgNames(ctx, orgA)
			require.NoError(t, err)
			assert.False(t, known)
			assert.Nil(t, names)
		})
	}
	// single mode never reads a tenant
	assert.Zero(t, tenants.byOrgId+tenants.byHost+tenants.byOrgKey)
}

func TestConfigProviderSeesANewConfigAfterInvalidate(t *testing.T) {
	ctx := context.Background()
	resolver, _, engine, _ := newMultiResolver(t)
	provider := &tenancy.ConfigProvider{Resolver: resolver}
	putConfig(t, engine, orgA, 1, `{"version":1,"default_security_profile":"SP3","accepted_cert_org_names":["Charge Co"]}`)

	cfg, err := provider.ConfigForOrg(ctx, orgA)
	require.NoError(t, err)
	assert.Equal(t, store.TLSWithClientSideCertificates, cfg.SecurityProfile())

	putConfig(t, engine, orgA, 2, `{"version":2,"default_security_profile":"SP1","accepted_cert_org_names":["Other"]}`)
	// still the config that is kept
	cfg, err = provider.ConfigForOrg(ctx, orgA)
	require.NoError(t, err)
	assert.EqualValues(t, 1, cfg.Version)

	resolver.Invalidate(orgA)
	cfg, err = provider.ConfigForOrg(ctx, orgA)
	require.NoError(t, err)
	assert.EqualValues(t, 2, cfg.Version)
	assert.Equal(t, store.UnsecuredTransportWithBasicAuth, cfg.SecurityProfile())
	names, known, err := provider.AcceptedCertOrgNames(ctx, orgA)
	require.NoError(t, err)
	assert.True(t, known)
	assert.Equal(t, []string{"Other"}, names)
}

func TestConfigProviderForStation(t *testing.T) {
	ctx := context.Background()
	resolver, _, engine, _ := newMultiResolver(t)
	provider := &tenancy.ConfigProvider{Resolver: resolver}
	putConfig(t, engine, orgA, 1, `{"version":1,"heartbeat_interval_s":120}`)
	putConfig(t, engine, orgB, 1, `{"version":1,"heartbeat_interval_s":900}`)
	// a station that tenant B owns although its key has the org key of tenant A
	require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.MOVED", &store.ChargeStationAuth{OrgId: orgB}))

	global := 5 * time.Minute
	for stationKey, want := range map[string]time.Duration{
		"aaaa.CP001": 2 * time.Minute,  // not registered: the tenant of the key
		"aaaa.MOVED": 15 * time.Minute, // the tenant that owns it
		"CP001":      global,           // no tenant
		"zzzz.CP001": global,           // no tenant has the org key
	} {
		cfg, err := provider.ConfigForStation(ctx, stationKey)
		require.NoError(t, err, stationKey)
		assert.Equal(t, want, cfg.HeartbeatInterval(global), stationKey)
		assert.Equal(t, want, provider.HeartbeatIntervalForStation(ctx, stationKey, global), stationKey)
	}
}

func TestConfigProviderFallsBackWhenTheStoredConfigCannotBeRead(t *testing.T) {
	ctx := context.Background()
	for name, stored := range map[string]string{
		// a tenant asked for client certificates of one organisation, and the document
		// has something that the schema refuses
		"a setting that does not exist": `{"version":1,"default_security_profile":"SP1","accepted_cert_org_names":["Charge Co"],"heartbeat_interval_s":120,"session_rules":{}}`,
		"a profile that does not exist": `{"version":1,"default_security_profile":"SP0","accepted_cert_org_names":["Charge Co"],"heartbeat_interval_s":120}`,
		"no version":                    `{"default_security_profile":"SP1","accepted_cert_org_names":["Charge Co"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			resolver, _, engine, _ := newMultiResolver(t)
			provider := &tenancy.ConfigProvider{Resolver: resolver}
			putConfig(t, engine, orgA, 1, stored)

			cfg, err := provider.ConfigForOrg(ctx, orgA)
			require.NoError(t, err)
			// the defaults, not a part of the document: never the profile without TLS
			assert.Equal(t, &tenancy.TenantConfig{}, cfg)
			assert.Equal(t, store.TLSWithBasicAuth, cfg.SecurityProfile())
			assert.Equal(t, 5*time.Minute, cfg.HeartbeatInterval(5*time.Minute))

			// and not an empty list of names, which would switch the check off
			names, known, err := provider.AcceptedCertOrgNames(ctx, orgA)
			require.NoError(t, err)
			assert.False(t, known)
			assert.Nil(t, names)
		})
	}
}

// failingTenantsByOrg is a tenant store that cannot read a tenant by its org.
type failingTenantsByOrg struct {
	store.TenantStore
}

func (failingTenantsByOrg) GetTenant(context.Context, string) (*store.Tenant, error) {
	return nil, errors.New("database is down")
}

func TestConfigProviderReturnsStoreErrors(t *testing.T) {
	ctx := context.Background()
	resolver, _, engine, _ := newMultiResolver(t)
	resolver.Tenants = failingTenantsByOrg{TenantStore: engine}
	provider := &tenancy.ConfigProvider{Resolver: resolver}

	cfg, err := provider.ConfigForOrg(ctx, orgA)
	assert.ErrorContains(t, err, "database is down")
	assert.Nil(t, cfg)

	_, err = provider.ConfigForStation(ctx, "aaaa.CP001")
	assert.ErrorContains(t, err, "database is down")

	names, known, err := provider.AcceptedCertOrgNames(ctx, orgA)
	assert.ErrorContains(t, err, "database is down")
	assert.False(t, known)
	assert.Nil(t, names)

	// a heartbeat interval is never a reason to refuse a charge station
	assert.Equal(t, 5*time.Minute, provider.HeartbeatIntervalForStation(ctx, "aaaa.CP001", 5*time.Minute))
}
