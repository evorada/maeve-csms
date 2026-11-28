// SPDX-License-Identifier: Apache-2.0

package stations_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/stations"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"k8s.io/utils/clock"
)

func TestChargingAllowed(t *testing.T) {
	ctx := context.Background()
	const orgId = "0193b2c4-0000-7000-8000-0000000000a1"
	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.ACTIVE", &store.ChargeStationAuth{OrgId: orgId}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.RETIRED", &store.ChargeStationAuth{OrgId: orgId}))
	require.NoError(t, engine.DecommissionChargeStation(ctx, "aaaa.RETIRED", nil))
	_, err := engine.RecordOnboardingConnection(ctx, "aaaa.PENDING", orgId, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 50, time.Now())
	require.NoError(t, err)

	for station, want := range map[string]bool{
		"aaaa.ACTIVE":  true,
		"aaaa.UNKNOWN": true, // no record: as before there was a registry
		"aaaa.PENDING": false,
		"aaaa.RETIRED": false,
	} {
		got, err := stations.ChargingAllowed(ctx, engine, station)
		require.NoError(t, err)
		assert.Equal(t, want, got, station)
	}

	t.Run("without a registry", func(t *testing.T) {
		got, err := stations.ChargingAllowed(ctx, nil, "aaaa.PENDING")
		require.NoError(t, err)
		assert.True(t, got)
		got, err = stations.ChargingAllowed(ctx, registryStub{err: fmt.Errorf("lookup: %w", store.ErrNotImplemented)}, "aaaa.PENDING")
		require.NoError(t, err)
		assert.True(t, got)
	})

	t.Run("a registry that fails", func(t *testing.T) {
		registryErr := errors.New("database is down")
		got, err := stations.ChargingAllowed(ctx, registryStub{err: registryErr}, "aaaa.ACTIVE")
		assert.ErrorIs(t, err, registryErr)
		assert.False(t, got)
	})
}
