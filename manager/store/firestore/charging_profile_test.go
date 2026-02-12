// SPDX-License-Identifier: Apache-2.0

//go:build integration

package firestore_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/firestore"
	"k8s.io/utils/clock"
)

func intPtr(i int) *int {
	return &i
}

func purposePtr(p store.ChargingProfilePurpose) *store.ChargingProfilePurpose {
	return &p
}

func float64Ptr(f float64) *float64 {
	return &f
}

func newTestProfile(csId string, connectorId, profileId, stackLevel int, purpose store.ChargingProfilePurpose) *store.ChargingProfile {
	return &store.ChargingProfile{
		ChargeStationId:        csId,
		ConnectorId:            connectorId,
		ChargingProfileId:      profileId,
		StackLevel:             stackLevel,
		ChargingProfilePurpose: purpose,
		ChargingProfileKind:    store.ChargingProfileKindAbsolute,
		ChargingSchedule: store.ChargingSchedule{
			ChargingRateUnit: store.ChargingRateUnitW,
			ChargingSchedulePeriod: []store.ChargingSchedulePeriod{
				{StartPeriod: 0, Limit: 11000.0, NumberPhases: intPtr(3)},
				{StartPeriod: 3600, Limit: 7000.0, NumberPhases: intPtr(3)},
			},
		},
	}
}

func TestSetAndGetChargingProfileFirestore(t *testing.T) {
	defer cleanupAllCollections(t, "myproject")
	ctx := context.Background()

	s, err := firestore.NewStore(ctx, "myproject", clock.RealClock{})
	require.NoError(t, err)

	profile := newTestProfile("cs001", 1, 100, 0, store.ChargingProfilePurposeTxDefaultProfile)
	err = s.SetChargingProfile(ctx, profile)
	require.NoError(t, err)

	profiles, err := s.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, profiles, 1)
	assert.Equal(t, 100, profiles[0].ChargingProfileId)
}

func TestGetChargingProfilesFilterByConnectorFirestore(t *testing.T) {
	defer cleanupAllCollections(t, "myproject")
	ctx := context.Background()

	s, err := firestore.NewStore(ctx, "myproject", clock.RealClock{})
	require.NoError(t, err)

	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 1, 100, 0, store.ChargingProfilePurposeTxDefaultProfile))
	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 2, 101, 0, store.ChargingProfilePurposeTxDefaultProfile))

	profiles, err := s.GetChargingProfiles(ctx, "cs001", intPtr(1), nil, nil)
	require.NoError(t, err)
	assert.Len(t, profiles, 1)
	assert.Equal(t, 1, profiles[0].ConnectorId)
}

func TestClearChargingProfileFirestore(t *testing.T) {
	defer cleanupAllCollections(t, "myproject")
	ctx := context.Background()

	s, err := firestore.NewStore(ctx, "myproject", clock.RealClock{})
	require.NoError(t, err)

	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 1, 100, 0, store.ChargingProfilePurposeTxDefaultProfile))
	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 2, 101, 0, store.ChargingProfilePurposeTxProfile))

	count, err := s.ClearChargingProfile(ctx, "cs001", nil, nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	profiles, err := s.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, profiles)
}

func TestClearChargingProfileByIdFirestore(t *testing.T) {
	defer cleanupAllCollections(t, "myproject")
	ctx := context.Background()

	s, err := firestore.NewStore(ctx, "myproject", clock.RealClock{})
	require.NoError(t, err)

	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 1, 100, 0, store.ChargingProfilePurposeTxDefaultProfile))
	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 2, 101, 0, store.ChargingProfilePurposeTxProfile))

	count, err := s.ClearChargingProfile(ctx, "cs001", intPtr(100), nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestGetCompositeScheduleFirestore(t *testing.T) {
	defer cleanupAllCollections(t, "myproject")
	ctx := context.Background()

	s, err := firestore.NewStore(ctx, "myproject", clock.RealClock{})
	require.NoError(t, err)

	profile := newTestProfile("cs001", 1, 100, 0, store.ChargingProfilePurposeTxDefaultProfile)
	_ = s.SetChargingProfile(ctx, profile)

	schedule, err := s.GetCompositeSchedule(ctx, "cs001", 1, 7200, nil)
	require.NoError(t, err)
	require.NotNil(t, schedule)
	assert.Equal(t, store.ChargingRateUnitW, schedule.ChargingRateUnit)
	assert.Len(t, schedule.ChargingSchedulePeriod, 2)
}

func TestGetCompositeScheduleNoProfilesFirestore(t *testing.T) {
	defer cleanupAllCollections(t, "myproject")
	ctx := context.Background()

	s, err := firestore.NewStore(ctx, "myproject", clock.RealClock{})
	require.NoError(t, err)

	schedule, err := s.GetCompositeSchedule(ctx, "cs001", 1, 3600, nil)
	require.NoError(t, err)
	assert.Nil(t, schedule)
}

// Ensure time import is used
var _ = time.Now
