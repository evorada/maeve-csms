// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"k8s.io/utils/clock"
)

func seedProfile(t *testing.T, ctx context.Context, s store.ChargingProfileStore, csId string, profileId, connectorId, stackLevel int, purpose store.ChargingProfilePurpose) {
	t.Helper()
	err := s.SetChargingProfile(ctx, &store.ChargingProfile{
		ChargeStationId:        csId,
		ConnectorId:            connectorId,
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
	})
	require.NoError(t, err)
}

func TestClearChargingProfileHandler_HandleCallResult(t *testing.T) {
	ctx := context.Background()
	chargeStationId := "cs001"

	tests := []struct {
		name              string
		request           ocpp.Request
		response          ocpp.Response
		seedProfiles      func(t *testing.T, s store.ChargingProfileStore)
		useStore          bool
		expectedError     bool
		expectedRemaining int
	}{
		{
			name:    "accepted - clear all profiles",
			request: &types.ClearChargingProfileJson{},
			response: &types.ClearChargingProfileResponseJson{
				Status: types.ClearChargingProfileResponseJsonStatusAccepted,
			},
			seedProfiles: func(t *testing.T, s store.ChargingProfileStore) {
				seedProfile(t, ctx, s, chargeStationId, 1, 1, 0, store.ChargingProfilePurposeTxDefaultProfile)
				seedProfile(t, ctx, s, chargeStationId, 2, 2, 1, store.ChargingProfilePurposeTxProfile)
			},
			useStore:          true,
			expectedRemaining: 0,
		},
		{
			name: "accepted - clear by profile id",
			request: &types.ClearChargingProfileJson{
				Id: clearProfileIntPtr(1),
			},
			response: &types.ClearChargingProfileResponseJson{
				Status: types.ClearChargingProfileResponseJsonStatusAccepted,
			},
			seedProfiles: func(t *testing.T, s store.ChargingProfileStore) {
				seedProfile(t, ctx, s, chargeStationId, 1, 1, 0, store.ChargingProfilePurposeTxDefaultProfile)
				seedProfile(t, ctx, s, chargeStationId, 2, 2, 1, store.ChargingProfilePurposeTxProfile)
			},
			useStore:          true,
			expectedRemaining: 1,
		},
		{
			name: "accepted - clear by connector id",
			request: &types.ClearChargingProfileJson{
				ConnectorId: clearProfileIntPtr(1),
			},
			response: &types.ClearChargingProfileResponseJson{
				Status: types.ClearChargingProfileResponseJsonStatusAccepted,
			},
			seedProfiles: func(t *testing.T, s store.ChargingProfileStore) {
				seedProfile(t, ctx, s, chargeStationId, 1, 1, 0, store.ChargingProfilePurposeTxDefaultProfile)
				seedProfile(t, ctx, s, chargeStationId, 2, 2, 1, store.ChargingProfilePurposeTxProfile)
			},
			useStore:          true,
			expectedRemaining: 1,
		},
		{
			name: "accepted - clear by purpose",
			request: &types.ClearChargingProfileJson{
				ChargingProfilePurpose: purposePtr(types.ClearChargingProfileJsonChargingProfilePurposeTxProfile),
			},
			response: &types.ClearChargingProfileResponseJson{
				Status: types.ClearChargingProfileResponseJsonStatusAccepted,
			},
			seedProfiles: func(t *testing.T, s store.ChargingProfileStore) {
				seedProfile(t, ctx, s, chargeStationId, 1, 1, 0, store.ChargingProfilePurposeTxDefaultProfile)
				seedProfile(t, ctx, s, chargeStationId, 2, 2, 1, store.ChargingProfilePurposeTxProfile)
				seedProfile(t, ctx, s, chargeStationId, 3, 1, 2, store.ChargingProfilePurposeTxProfile)
			},
			useStore:          true,
			expectedRemaining: 1,
		},
		{
			name: "accepted - clear by stack level",
			request: &types.ClearChargingProfileJson{
				StackLevel: clearProfileIntPtr(0),
			},
			response: &types.ClearChargingProfileResponseJson{
				Status: types.ClearChargingProfileResponseJsonStatusAccepted,
			},
			seedProfiles: func(t *testing.T, s store.ChargingProfileStore) {
				seedProfile(t, ctx, s, chargeStationId, 1, 1, 0, store.ChargingProfilePurposeTxDefaultProfile)
				seedProfile(t, ctx, s, chargeStationId, 2, 2, 1, store.ChargingProfilePurposeTxProfile)
			},
			useStore:          true,
			expectedRemaining: 1,
		},
		{
			name:    "unknown status - profiles not cleared",
			request: &types.ClearChargingProfileJson{},
			response: &types.ClearChargingProfileResponseJson{
				Status: types.ClearChargingProfileResponseJsonStatusUnknown,
			},
			seedProfiles: func(t *testing.T, s store.ChargingProfileStore) {
				seedProfile(t, ctx, s, chargeStationId, 1, 1, 0, store.ChargingProfilePurposeTxDefaultProfile)
			},
			useStore:          true,
			expectedRemaining: 1,
		},
		{
			name:    "accepted without store - no error",
			request: &types.ClearChargingProfileJson{},
			response: &types.ClearChargingProfileResponseJson{
				Status: types.ClearChargingProfileResponseJsonStatusAccepted,
			},
			useStore:          false,
			expectedRemaining: -1, // not checked
		},
		{
			name: "accepted - clear by multiple filters",
			request: &types.ClearChargingProfileJson{
				ConnectorId:            clearProfileIntPtr(1),
				ChargingProfilePurpose: purposePtr(types.ClearChargingProfileJsonChargingProfilePurposeTxDefaultProfile),
				StackLevel:             clearProfileIntPtr(0),
			},
			response: &types.ClearChargingProfileResponseJson{
				Status: types.ClearChargingProfileResponseJsonStatusAccepted,
			},
			seedProfiles: func(t *testing.T, s store.ChargingProfileStore) {
				seedProfile(t, ctx, s, chargeStationId, 1, 1, 0, store.ChargingProfilePurposeTxDefaultProfile)
				seedProfile(t, ctx, s, chargeStationId, 2, 1, 1, store.ChargingProfilePurposeTxDefaultProfile)
				seedProfile(t, ctx, s, chargeStationId, 3, 2, 0, store.ChargingProfilePurposeTxProfile)
			},
			useStore:          true,
			expectedRemaining: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var profileStore store.ChargingProfileStore
			var engine *inmemory.Store
			if tt.useStore {
				engine = inmemory.NewStore(clock.RealClock{})
				profileStore = engine
				if tt.seedProfiles != nil {
					tt.seedProfiles(t, engine)
				}
			}

			handler := ocpp16.ClearChargingProfileHandler{
				ChargingProfileStore: profileStore,
			}

			err := handler.HandleCallResult(ctx, chargeStationId, tt.request, tt.response, nil)

			if tt.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tt.expectedRemaining >= 0 && engine != nil {
				profiles, err := engine.GetChargingProfiles(ctx, chargeStationId, nil, nil, nil)
				require.NoError(t, err)
				assert.Len(t, profiles, tt.expectedRemaining)
			}
		})
	}
}

func clearProfileIntPtr(v int) *int {
	return &v
}

func purposePtr(v types.ClearChargingProfileJsonChargingProfilePurpose) *types.ClearChargingProfileJsonChargingProfilePurpose {
	return &v
}
