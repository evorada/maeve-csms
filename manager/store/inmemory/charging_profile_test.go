// SPDX-License-Identifier: Apache-2.0

package inmemory_test

import (
	"context"
	"testing"

	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	clocktesting "k8s.io/utils/clock/testing"
)

func intPtr(i int) *int {
	return &i
}

func float64Ptr(f float64) *float64 {
	return &f
}

func purposePtr(p store.ChargingProfilePurpose) *store.ChargingProfilePurpose {
	return &p
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

func TestSetAndGetChargingProfile(t *testing.T) {
	ctx := context.Background()
	fakeClock := clocktesting.NewFakeClock(time.Now())
	s := inmemory.NewStore(fakeClock)

	profile := newTestProfile("cs001", 1, 100, 0, store.ChargingProfilePurposeTxDefaultProfile)
	err := s.SetChargingProfile(ctx, profile)
	require.NoError(t, err)

	profiles, err := s.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, profiles, 1)
	assert.Equal(t, profile, profiles[0])
}

func TestSetChargingProfileReplaces(t *testing.T) {
	ctx := context.Background()
	fakeClock := clocktesting.NewFakeClock(time.Now())
	s := inmemory.NewStore(fakeClock)

	profile1 := newTestProfile("cs001", 1, 100, 0, store.ChargingProfilePurposeTxDefaultProfile)
	err := s.SetChargingProfile(ctx, profile1)
	require.NoError(t, err)

	profile2 := newTestProfile("cs001", 2, 100, 1, store.ChargingProfilePurposeTxProfile)
	err = s.SetChargingProfile(ctx, profile2)
	require.NoError(t, err)

	profiles, err := s.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, profiles, 1)
	assert.Equal(t, 2, profiles[0].ConnectorId)
}

func TestGetChargingProfilesFilterByConnector(t *testing.T) {
	ctx := context.Background()
	fakeClock := clocktesting.NewFakeClock(time.Now())
	s := inmemory.NewStore(fakeClock)

	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 1, 100, 0, store.ChargingProfilePurposeTxDefaultProfile))
	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 2, 101, 0, store.ChargingProfilePurposeTxDefaultProfile))

	profiles, err := s.GetChargingProfiles(ctx, "cs001", intPtr(1), nil, nil)
	require.NoError(t, err)
	assert.Len(t, profiles, 1)
	assert.Equal(t, 1, profiles[0].ConnectorId)
}

func TestGetChargingProfilesFilterByPurpose(t *testing.T) {
	ctx := context.Background()
	fakeClock := clocktesting.NewFakeClock(time.Now())
	s := inmemory.NewStore(fakeClock)

	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 1, 100, 0, store.ChargingProfilePurposeTxDefaultProfile))
	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 1, 101, 0, store.ChargingProfilePurposeTxProfile))

	profiles, err := s.GetChargingProfiles(ctx, "cs001", nil, purposePtr(store.ChargingProfilePurposeTxProfile), nil)
	require.NoError(t, err)
	assert.Len(t, profiles, 1)
	assert.Equal(t, store.ChargingProfilePurposeTxProfile, profiles[0].ChargingProfilePurpose)
}

func TestGetChargingProfilesFilterByStackLevel(t *testing.T) {
	ctx := context.Background()
	fakeClock := clocktesting.NewFakeClock(time.Now())
	s := inmemory.NewStore(fakeClock)

	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 1, 100, 0, store.ChargingProfilePurposeTxDefaultProfile))
	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 1, 101, 5, store.ChargingProfilePurposeTxDefaultProfile))

	profiles, err := s.GetChargingProfiles(ctx, "cs001", nil, nil, intPtr(5))
	require.NoError(t, err)
	assert.Len(t, profiles, 1)
	assert.Equal(t, 5, profiles[0].StackLevel)
}

func TestGetChargingProfilesEmpty(t *testing.T) {
	ctx := context.Background()
	fakeClock := clocktesting.NewFakeClock(time.Now())
	s := inmemory.NewStore(fakeClock)

	profiles, err := s.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, profiles)
}

func TestClearChargingProfileAll(t *testing.T) {
	ctx := context.Background()
	fakeClock := clocktesting.NewFakeClock(time.Now())
	s := inmemory.NewStore(fakeClock)

	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 1, 100, 0, store.ChargingProfilePurposeTxDefaultProfile))
	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 2, 101, 0, store.ChargingProfilePurposeTxProfile))

	count, err := s.ClearChargingProfile(ctx, "cs001", nil, nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	profiles, err := s.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, profiles)
}

func TestClearChargingProfileById(t *testing.T) {
	ctx := context.Background()
	fakeClock := clocktesting.NewFakeClock(time.Now())
	s := inmemory.NewStore(fakeClock)

	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 1, 100, 0, store.ChargingProfilePurposeTxDefaultProfile))
	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 2, 101, 0, store.ChargingProfilePurposeTxProfile))

	count, err := s.ClearChargingProfile(ctx, "cs001", intPtr(100), nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	profiles, err := s.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, profiles, 1)
	assert.Equal(t, 101, profiles[0].ChargingProfileId)
}

func TestClearChargingProfileByPurpose(t *testing.T) {
	ctx := context.Background()
	fakeClock := clocktesting.NewFakeClock(time.Now())
	s := inmemory.NewStore(fakeClock)

	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 1, 100, 0, store.ChargingProfilePurposeTxDefaultProfile))
	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 1, 101, 0, store.ChargingProfilePurposeTxProfile))

	count, err := s.ClearChargingProfile(ctx, "cs001", nil, nil, purposePtr(store.ChargingProfilePurposeTxProfile), nil)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestClearChargingProfileNoneMatch(t *testing.T) {
	ctx := context.Background()
	fakeClock := clocktesting.NewFakeClock(time.Now())
	s := inmemory.NewStore(fakeClock)

	_ = s.SetChargingProfile(ctx, newTestProfile("cs001", 1, 100, 0, store.ChargingProfilePurposeTxDefaultProfile))

	count, err := s.ClearChargingProfile(ctx, "cs002", nil, nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestGetCompositeSchedule(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fakeClock := clocktesting.NewFakeClock(now)
	s := inmemory.NewStore(fakeClock)

	profile := newTestProfile("cs001", 1, 100, 0, store.ChargingProfilePurposeTxDefaultProfile)
	profile.ChargingSchedule.MinChargingRate = float64Ptr(6.0)
	_ = s.SetChargingProfile(ctx, profile)

	schedule, err := s.GetCompositeSchedule(ctx, "cs001", 1, 7200, nil)
	require.NoError(t, err)
	require.NotNil(t, schedule)
	assert.Equal(t, store.ChargingRateUnitW, schedule.ChargingRateUnit)
	assert.Equal(t, 7200, *schedule.Duration)
	assert.Len(t, schedule.ChargingSchedulePeriod, 2)
}

func TestGetCompositeScheduleNoProfiles(t *testing.T) {
	ctx := context.Background()
	fakeClock := clocktesting.NewFakeClock(time.Now())
	s := inmemory.NewStore(fakeClock)

	schedule, err := s.GetCompositeSchedule(ctx, "cs001", 1, 3600, nil)
	require.NoError(t, err)
	assert.Nil(t, schedule)
}

func TestGetCompositeSchedulePriority(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fakeClock := clocktesting.NewFakeClock(now)
	s := inmemory.NewStore(fakeClock)

	// Lower priority
	txDefault := newTestProfile("cs001", 1, 100, 0, store.ChargingProfilePurposeTxDefaultProfile)
	txDefault.ChargingSchedule.ChargingSchedulePeriod = []store.ChargingSchedulePeriod{
		{StartPeriod: 0, Limit: 11000.0},
	}
	_ = s.SetChargingProfile(ctx, txDefault)

	// Higher priority
	maxProfile := newTestProfile("cs001", 0, 101, 0, store.ChargingProfilePurposeChargePointMaxProfile)
	maxProfile.ChargingSchedule.ChargingSchedulePeriod = []store.ChargingSchedulePeriod{
		{StartPeriod: 0, Limit: 32000.0},
	}
	_ = s.SetChargingProfile(ctx, maxProfile)

	schedule, err := s.GetCompositeSchedule(ctx, "cs001", 1, 3600, nil)
	require.NoError(t, err)
	require.NotNil(t, schedule)
	// Should use ChargePointMaxProfile (higher priority)
	assert.Equal(t, 32000.0, schedule.ChargingSchedulePeriod[0].Limit)
}
