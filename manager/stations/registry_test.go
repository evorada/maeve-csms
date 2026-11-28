// SPDX-License-Identifier: Apache-2.0

package stations_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/clock"

	"github.com/thoughtworks/maeve-csms/manager/stations"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
)

// registryStub answers LookupChargeStation with record and err.
type registryStub struct {
	store.ChargeStationRegistryStore
	record *store.ChargeStation
	err    error
}

func (r registryStub) LookupChargeStation(context.Context, string) (*store.ChargeStation, error) {
	return r.record, r.err
}

func TestLookupReturnsTheRecord(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, engine.SetChargeStationAuth(ctx, "k7f2.CP001", &store.ChargeStationAuth{}))
	stored, err := engine.LookupChargeStation(ctx, "k7f2.CP001")
	require.NoError(t, err)
	stored.AllowAnyToken = true
	require.NoError(t, engine.UpdateChargeStation(ctx, stored))
	require.NoError(t, engine.SetChargeStationState(ctx, "k7f2.CP001", store.StationStatePending, nil))

	got, err := stations.Lookup(ctx, engine, "k7f2.CP001")
	require.NoError(t, err)

	want, err := engine.LookupChargeStation(ctx, "k7f2.CP001")
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, store.StationStatePending, got.State)
	assert.True(t, got.AllowAnyToken)
}

func TestLookupGivesAnActiveRecordWithoutRegistry(t *testing.T) {
	want := &store.ChargeStation{StationKey: "k7f2.CP001", State: store.StationStateActive}

	for name, registry := range map[string]store.ChargeStationRegistryStore{
		"station without record": inmemory.NewStore(clock.RealClock{}),
		"backend without registry": registryStub{
			err: fmt.Errorf("LookupChargeStation: %w", store.ErrNotImplemented),
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := stations.Lookup(context.Background(), registry, "k7f2.CP001")
			require.NoError(t, err)
			assert.Equal(t, want, got)
			assert.False(t, got.AllowAnyToken, "only the tokens of the token store")
		})
	}
}

func TestLookupReturnsStoreErrors(t *testing.T) {
	storeErr := errors.New("connection refused")

	got, err := stations.Lookup(context.Background(), registryStub{err: storeErr}, "k7f2.CP001")

	assert.ErrorIs(t, err, storeErr)
	assert.Nil(t, got)
}

func TestDecommissioned(t *testing.T) {
	ctx := context.Background()
	storeErr := errors.New("connection refused")

	for name, tc := range map[string]struct {
		registry store.ChargeStationRegistryStore
		want     bool
		wantErr  error
	}{
		"decommissioned": {registry: registryStub{record: &store.ChargeStation{State: store.StationStateDecommissioned}}, want: true},
		"active":         {registry: registryStub{record: &store.ChargeStation{State: store.StationStateActive}}},
		"pending":        {registry: registryStub{record: &store.ChargeStation{State: store.StationStatePending}}},
		"without record": {registry: inmemory.NewStore(clock.RealClock{})},
		"without registry": {registry: registryStub{
			err: fmt.Errorf("LookupChargeStation: %w", store.ErrNotImplemented),
		}},
		"a store error": {registry: registryStub{err: storeErr}, wantErr: storeErr},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := stations.Decommissioned(ctx, tc.registry, "k7f2.CP001")
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
