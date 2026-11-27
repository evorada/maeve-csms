// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"errors"
	"testing"

	clone "github.com/huandu/go-clone/generic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
)

// orgAssigningEngine is a storage engine that stores the org of a row.
type orgAssigningEngine struct {
	store.Engine
	assigned []string
	err      error
}

func (e *orgAssigningEngine) AssignLegacyOrg(_ context.Context, orgId string) (int64, error) {
	e.assigned = append(e.assigned, orgId)
	return 3, e.err
}

func TestAssignLegacyOrg(t *testing.T) {
	const defaultOrgId = "0193b2c4-0000-7000-8000-00000000000a"

	t.Run("moves the legacy rows to the configured default org", func(t *testing.T) {
		engine := &orgAssigningEngine{}
		require.NoError(t, assignLegacyOrg(context.Background(), engine, defaultOrgId))
		assert.Equal(t, []string{defaultOrgId}, engine.assigned)
	})

	t.Run("does nothing when the default org is the legacy org", func(t *testing.T) {
		engine := &orgAssigningEngine{}
		require.NoError(t, assignLegacyOrg(context.Background(), engine, store.LegacyOrgId))
		assert.Empty(t, engine.assigned)
	})

	t.Run("does nothing for an engine that does not store orgs", func(t *testing.T) {
		require.NoError(t, assignLegacyOrg(context.Background(), inmemory.NewStore(clock.RealClock{}), defaultOrgId))
	})

	t.Run("fails when the rows cannot be moved", func(t *testing.T) {
		engine := &orgAssigningEngine{err: errors.New("connection lost")}
		err := assignLegacyOrg(context.Background(), engine, defaultOrgId)
		require.Error(t, err)
		assert.ErrorContains(t, err, "connection lost")
	})
}

// The example for manual runs with two tenants must stay a configuration that starts.
func TestMultiTenantExampleConfig(t *testing.T) {
	ctx := context.Background()
	cfg := clone.Clone(&DefaultConfig)
	require.NoError(t, cfg.LoadFromFile("../../config/manager/config-multitenant.toml"))
	require.NoError(t, cfg.Validate())
	assert.Equal(t, "multi", cfg.Tenancy.Mode)
	assert.Equal(t, ":9412", cfg.Api.InternalAddr)

	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, configureTenants(ctx, &cfg.Tenancy, "in_memory", engine))

	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine}
	for host, wantKey := range map[string]string{
		"localhost:9310": "dev1.cs001",
		"127.0.0.1:9310": "dev2.cs001",
	} {
		res, err := resolver.ResolveStation(ctx, host, "cs001")
		require.NoError(t, err, host)
		assert.Equal(t, wantKey, res.StationKey)
	}
}
