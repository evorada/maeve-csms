// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"k8s.io/utils/clock"
)

func makeSetChargingProfileRequest(profileId int, evseId int, purpose types.ChargingProfilePurposeEnumType, limit float64) *types.SetChargingProfileRequestJson {
	return &types.SetChargingProfileRequestJson{
		EvseId: evseId,
		ChargingProfile: types.ChargingProfileType{
			Id:                     profileId,
			StackLevel:             0,
			ChargingProfilePurpose: purpose,
			ChargingProfileKind:    types.ChargingProfileKindEnumTypeAbsolute,
			ChargingSchedule: []types.ChargingScheduleType{
				{
					Id:               1,
					ChargingRateUnit: types.ChargingRateUnitEnumTypeW,
					ChargingSchedulePeriod: []types.ChargingSchedulePeriodType{
						{StartPeriod: 0, Limit: limit},
					},
				},
			},
		},
	}
}

func TestSetChargingProfileResultHandler_Accepted_StoresProfile(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.SetChargingProfileResultHandler{Store: engine}

	req := makeSetChargingProfileRequest(1, 1, types.ChargingProfilePurposeEnumTypeTxDefaultProfile, 7400.0)
	resp := &types.SetChargingProfileResponseJson{
		Status: types.ChargingProfileStatusEnumTypeAccepted,
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	require.NoError(t, err)

	profiles, err := engine.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, profiles, 1)
	assert.Equal(t, 1, profiles[0].ChargingProfileId)
	assert.Equal(t, store.ChargingProfilePurposeTxDefaultProfile, profiles[0].ChargingProfilePurpose)
	assert.Equal(t, 7400.0, profiles[0].ChargingSchedule.ChargingSchedulePeriod[0].Limit)
}

func TestSetChargingProfileResultHandler_Rejected_DoesNotStore(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.SetChargingProfileResultHandler{Store: engine}

	req := makeSetChargingProfileRequest(2, 0, types.ChargingProfilePurposeEnumTypeChargingStationMaxProfile, 11000.0)
	resp := &types.SetChargingProfileResponseJson{
		Status: types.ChargingProfileStatusEnumTypeRejected,
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	require.NoError(t, err)

	profiles, err := engine.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, profiles)
}

func TestSetChargingProfileResultHandler_NoStore_NoError(t *testing.T) {
	ctx := context.Background()
	handler := ocpp201.SetChargingProfileResultHandler{Store: nil}

	req := makeSetChargingProfileRequest(3, 1, types.ChargingProfilePurposeEnumTypeTxProfile, 32.0)
	resp := &types.SetChargingProfileResponseJson{
		Status: types.ChargingProfileStatusEnumTypeAccepted,
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	require.NoError(t, err)
}

func TestSetChargingProfileResultHandler_FullProfile_Stored(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.SetChargingProfileResultHandler{Store: engine}

	validFrom := "2026-01-01T00:00:00Z"
	validTo := "2026-12-31T23:59:59Z"
	startSchedule := "2026-01-01T08:00:00Z"
	duration := 3600
	minRate := 1000.0
	numPhases := 3
	recurrency := types.RecurrencyKindEnumTypeDaily

	req := &types.SetChargingProfileRequestJson{
		EvseId: 2,
		ChargingProfile: types.ChargingProfileType{
			Id:                     10,
			StackLevel:             5,
			ChargingProfilePurpose: types.ChargingProfilePurposeEnumTypeTxProfile,
			ChargingProfileKind:    types.ChargingProfileKindEnumTypeRecurring,
			RecurrencyKind:         &recurrency,
			ValidFrom:              &validFrom,
			ValidTo:                &validTo,
			ChargingSchedule: []types.ChargingScheduleType{
				{
					Id:              1,
					StartSchedule:   &startSchedule,
					Duration:        &duration,
					ChargingRateUnit: types.ChargingRateUnitEnumTypeA,
					MinChargingRate: &minRate,
					ChargingSchedulePeriod: []types.ChargingSchedulePeriodType{
						{StartPeriod: 0, Limit: 32.0, NumberPhases: &numPhases},
						{StartPeriod: 1800, Limit: 16.0},
					},
				},
			},
		},
	}
	resp := &types.SetChargingProfileResponseJson{
		Status: types.ChargingProfileStatusEnumTypeAccepted,
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	require.NoError(t, err)

	profiles, err := engine.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, profiles, 1)

	p := profiles[0]
	assert.Equal(t, "cs001", p.ChargeStationId)
	assert.Equal(t, 2, p.ConnectorId)
	assert.Equal(t, 10, p.ChargingProfileId)
	assert.Equal(t, 5, p.StackLevel)
	assert.Equal(t, store.ChargingProfilePurposeTxProfile, p.ChargingProfilePurpose)
	assert.Equal(t, store.ChargingProfileKindRecurring, p.ChargingProfileKind)
	assert.NotNil(t, p.RecurrencyKind)
	assert.Equal(t, store.RecurrencyKindDaily, *p.RecurrencyKind)
	assert.NotNil(t, p.ValidFrom)
	assert.NotNil(t, p.ValidTo)
	assert.Equal(t, store.ChargingRateUnitA, p.ChargingSchedule.ChargingRateUnit)
	assert.NotNil(t, p.ChargingSchedule.Duration)
	assert.Equal(t, 3600, *p.ChargingSchedule.Duration)
	assert.NotNil(t, p.ChargingSchedule.MinChargingRate)
	assert.Equal(t, 1000.0, *p.ChargingSchedule.MinChargingRate)
	assert.NotNil(t, p.ChargingSchedule.StartSchedule)
	assert.Len(t, p.ChargingSchedule.ChargingSchedulePeriod, 2)
	assert.Equal(t, 0, p.ChargingSchedule.ChargingSchedulePeriod[0].StartPeriod)
	assert.Equal(t, 32.0, p.ChargingSchedule.ChargingSchedulePeriod[0].Limit)
	assert.NotNil(t, p.ChargingSchedule.ChargingSchedulePeriod[0].NumberPhases)
	assert.Equal(t, 3, *p.ChargingSchedule.ChargingSchedulePeriod[0].NumberPhases)
	assert.Nil(t, p.ChargingSchedule.ChargingSchedulePeriod[1].NumberPhases)
}

func TestSetChargingProfileResultHandler_ReplacesExistingProfile(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.SetChargingProfileResultHandler{Store: engine}

	accepted := &types.SetChargingProfileResponseJson{Status: types.ChargingProfileStatusEnumTypeAccepted}

	// Store initial profile
	req1 := makeSetChargingProfileRequest(1, 1, types.ChargingProfilePurposeEnumTypeTxDefaultProfile, 7400.0)
	require.NoError(t, handler.HandleCallResult(ctx, "cs001", req1, accepted, nil))

	// Replace with updated limit
	req2 := makeSetChargingProfileRequest(1, 1, types.ChargingProfilePurposeEnumTypeTxDefaultProfile, 11000.0)
	require.NoError(t, handler.HandleCallResult(ctx, "cs001", req2, accepted, nil))

	profiles, err := engine.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, profiles, 1)
	assert.Equal(t, 11000.0, profiles[0].ChargingSchedule.ChargingSchedulePeriod[0].Limit)
}

func TestSetChargingProfileResultHandler_InvalidDate_ReturnsError(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.SetChargingProfileResultHandler{Store: engine}

	badDate := "not-a-date"
	req := &types.SetChargingProfileRequestJson{
		EvseId: 1,
		ChargingProfile: types.ChargingProfileType{
			Id:                     1,
			StackLevel:             0,
			ChargingProfilePurpose: types.ChargingProfilePurposeEnumTypeTxDefaultProfile,
			ChargingProfileKind:    types.ChargingProfileKindEnumTypeAbsolute,
			ValidFrom:              &badDate,
			ChargingSchedule: []types.ChargingScheduleType{
				{
					Id:               1,
					ChargingRateUnit: types.ChargingRateUnitEnumTypeW,
					ChargingSchedulePeriod: []types.ChargingSchedulePeriodType{
						{StartPeriod: 0, Limit: 7400.0},
					},
				},
			},
		},
	}
	resp := &types.SetChargingProfileResponseJson{
		Status: types.ChargingProfileStatusEnumTypeAccepted,
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing validFrom")
}

func TestSetChargingProfileResultHandler_HandlesRequest(t *testing.T) {
	var req ocpp.Request = &types.SetChargingProfileRequestJson{}
	var resp ocpp.Response = &types.SetChargingProfileResponseJson{Status: types.ChargingProfileStatusEnumTypeAccepted}
	assert.NotNil(t, req)
	assert.NotNil(t, resp)
}
