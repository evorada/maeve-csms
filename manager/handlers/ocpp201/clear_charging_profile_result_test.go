// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"k8s.io/utils/clock"
)

// seedChargingProfile creates and stores a charging profile for testing.
func seedChargingProfile(t *testing.T, eng store.Engine, chargeStationId string, profileId int, evseId int, purpose store.ChargingProfilePurpose, stackLevel int) {
	t.Helper()
	ctx := context.Background()
	profile := &store.ChargingProfile{
		ChargeStationId:        chargeStationId,
		ConnectorId:            evseId,
		ChargingProfileId:      profileId,
		StackLevel:             stackLevel,
		ChargingProfilePurpose: purpose,
		ChargingProfileKind:    store.ChargingProfileKindAbsolute,
		ChargingSchedule: store.ChargingSchedule{
			ChargingRateUnit: store.ChargingRateUnitW,
			ChargingSchedulePeriod: []store.ChargingSchedulePeriod{
				{StartPeriod: 0, Limit: 7400.0},
			},
		},
	}
	err := eng.SetChargingProfile(ctx, profile)
	require.NoError(t, err)
}

func TestClearChargingProfileResultHandler_AcceptedById_ClearsProfile(t *testing.T) {
	ctx := context.Background()
	eng := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.ClearChargingProfileResultHandler{Store: eng}

	// Seed two profiles
	seedChargingProfile(t, eng, "cs001", 10, 1, store.ChargingProfilePurposeTxDefaultProfile, 0)
	seedChargingProfile(t, eng, "cs001", 20, 2, store.ChargingProfilePurposeTxProfile, 1)

	profileId := 10
	req := &types.ClearChargingProfileRequestJson{
		ChargingProfileId: &profileId,
	}
	resp := &types.ClearChargingProfileResponseJson{
		Status: types.ClearChargingProfileStatusEnumTypeAccepted,
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	require.NoError(t, err)

	// Profile 10 should be gone; profile 20 should remain
	remaining, err := eng.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, remaining, 1)
	assert.Equal(t, 20, remaining[0].ChargingProfileId)
}

func TestClearChargingProfileResultHandler_AcceptedByCriteria_ClearsByPurpose(t *testing.T) {
	ctx := context.Background()
	eng := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.ClearChargingProfileResultHandler{Store: eng}

	// Seed profiles with different purposes
	seedChargingProfile(t, eng, "cs001", 1, 1, store.ChargingProfilePurposeTxDefaultProfile, 0)
	seedChargingProfile(t, eng, "cs001", 2, 1, store.ChargingProfilePurposeTxProfile, 1)

	purpose := types.ChargingProfilePurposeEnumTypeTxDefaultProfile
	req := &types.ClearChargingProfileRequestJson{
		ChargingProfileCriteria: &types.ClearChargingProfileType{
			ChargingProfilePurpose: &purpose,
		},
	}
	resp := &types.ClearChargingProfileResponseJson{
		Status: types.ClearChargingProfileStatusEnumTypeAccepted,
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	require.NoError(t, err)

	remaining, err := eng.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, remaining, 1)
	assert.Equal(t, 2, remaining[0].ChargingProfileId)
}

func TestClearChargingProfileResultHandler_AcceptedByCriteria_ClearsByEvseId(t *testing.T) {
	ctx := context.Background()
	eng := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.ClearChargingProfileResultHandler{Store: eng}

	// Seed profiles on different EVSEs
	seedChargingProfile(t, eng, "cs001", 1, 1, store.ChargingProfilePurposeTxDefaultProfile, 0)
	seedChargingProfile(t, eng, "cs001", 2, 2, store.ChargingProfilePurposeTxDefaultProfile, 0)

	evseId := 1
	req := &types.ClearChargingProfileRequestJson{
		ChargingProfileCriteria: &types.ClearChargingProfileType{
			EvseId: &evseId,
		},
	}
	resp := &types.ClearChargingProfileResponseJson{
		Status: types.ClearChargingProfileStatusEnumTypeAccepted,
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	require.NoError(t, err)

	remaining, err := eng.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, remaining, 1)
	assert.Equal(t, 2, remaining[0].ChargingProfileId)
}

func TestClearChargingProfileResultHandler_Unknown_NoStoreChanges(t *testing.T) {
	ctx := context.Background()
	eng := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.ClearChargingProfileResultHandler{Store: eng}

	// Seed one profile
	seedChargingProfile(t, eng, "cs001", 99, 1, store.ChargingProfilePurposeTxProfile, 0)

	profileId := 99
	req := &types.ClearChargingProfileRequestJson{
		ChargingProfileId: &profileId,
	}
	resp := &types.ClearChargingProfileResponseJson{
		Status: types.ClearChargingProfileStatusEnumTypeUnknown,
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	require.NoError(t, err)

	// Store should be unchanged since the CS reported Unknown
	remaining, err := eng.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, remaining, 1, "store should not be modified when CS reports Unknown")
}

func TestClearChargingProfileResultHandler_Unknown_WithStatusInfo(t *testing.T) {
	ctx := context.Background()
	eng := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.ClearChargingProfileResultHandler{Store: eng}

	profileId := 999
	req := &types.ClearChargingProfileRequestJson{
		ChargingProfileId: &profileId,
	}
	resp := &types.ClearChargingProfileResponseJson{
		Status: types.ClearChargingProfileStatusEnumTypeUnknown,
		StatusInfo: &types.StatusInfoType{
			ReasonCode: "NoProfile",
		},
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	assert.NoError(t, err)
}

func TestClearChargingProfileResultHandler_NoStore_NoError(t *testing.T) {
	ctx := context.Background()
	handler := ocpp201.ClearChargingProfileResultHandler{Store: nil}

	profileId := 1
	req := &types.ClearChargingProfileRequestJson{
		ChargingProfileId: &profileId,
	}
	resp := &types.ClearChargingProfileResponseJson{
		Status: types.ClearChargingProfileStatusEnumTypeAccepted,
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	assert.NoError(t, err)
}

func TestClearChargingProfileResultHandler_ImplementsInterface(t *testing.T) {
	eng := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.ClearChargingProfileResultHandler{Store: eng}
	_ = handler
}
