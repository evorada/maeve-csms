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

func TestSetChargingProfileHandler_HandleCallResult(t *testing.T) {
	ctx := context.Background()
	chargeStationId := "cs001"

	tests := []struct {
		name          string
		request       ocpp.Request
		response      ocpp.Response
		setupStore    func() store.ChargingProfileStore
		expectStored  bool
		expectedError bool
	}{
		{
			name: "accepted - profile stored",
			request: &types.SetChargingProfileJson{
				ConnectorId: 1,
				CsChargingProfiles: types.SetChargingProfileJsonCsChargingProfiles{
					ChargingProfileId:      1,
					StackLevel:             0,
					ChargingProfilePurpose: types.SetChargingProfileJsonChargingProfilePurposeTxDefaultProfile,
					ChargingProfileKind:    types.SetChargingProfileJsonChargingProfileKindAbsolute,
					ChargingSchedule: types.SetChargingProfileJsonChargingSchedule{
						ChargingRateUnit: types.SetChargingProfileJsonChargingRateUnitW,
						ChargingSchedulePeriod: []types.SetChargingProfileJsonChargingSchedulePeriod{
							{StartPeriod: 0, Limit: 7400.0},
						},
					},
				},
			},
			response: &types.SetChargingProfileResponseJson{
				Status: types.SetChargingProfileResponseJsonStatusAccepted,
			},
			setupStore: func() store.ChargingProfileStore {
				return inmemory.NewStore(clock.RealClock{})
			},
			expectStored: true,
		},
		{
			name: "rejected - profile not stored",
			request: &types.SetChargingProfileJson{
				ConnectorId: 1,
				CsChargingProfiles: types.SetChargingProfileJsonCsChargingProfiles{
					ChargingProfileId:      2,
					StackLevel:             0,
					ChargingProfilePurpose: types.SetChargingProfileJsonChargingProfilePurposeTxProfile,
					ChargingProfileKind:    types.SetChargingProfileJsonChargingProfileKindAbsolute,
					ChargingSchedule: types.SetChargingProfileJsonChargingSchedule{
						ChargingRateUnit: types.SetChargingProfileJsonChargingRateUnitA,
						ChargingSchedulePeriod: []types.SetChargingProfileJsonChargingSchedulePeriod{
							{StartPeriod: 0, Limit: 32.0},
						},
					},
				},
			},
			response: &types.SetChargingProfileResponseJson{
				Status: types.SetChargingProfileResponseJsonStatusRejected,
			},
			setupStore: func() store.ChargingProfileStore {
				return inmemory.NewStore(clock.RealClock{})
			},
			expectStored: false,
		},
		{
			name: "not supported - profile not stored",
			request: &types.SetChargingProfileJson{
				ConnectorId: 0,
				CsChargingProfiles: types.SetChargingProfileJsonCsChargingProfiles{
					ChargingProfileId:      3,
					StackLevel:             0,
					ChargingProfilePurpose: types.SetChargingProfileJsonChargingProfilePurposeChargePointMaxProfile,
					ChargingProfileKind:    types.SetChargingProfileJsonChargingProfileKindAbsolute,
					ChargingSchedule: types.SetChargingProfileJsonChargingSchedule{
						ChargingRateUnit: types.SetChargingProfileJsonChargingRateUnitW,
						ChargingSchedulePeriod: []types.SetChargingProfileJsonChargingSchedulePeriod{
							{StartPeriod: 0, Limit: 11000.0},
						},
					},
				},
			},
			response: &types.SetChargingProfileResponseJson{
				Status: types.SetChargingProfileResponseJsonStatusNotSupported,
			},
			setupStore: func() store.ChargingProfileStore {
				return inmemory.NewStore(clock.RealClock{})
			},
			expectStored: false,
		},
		{
			name: "accepted without store - no error",
			request: &types.SetChargingProfileJson{
				ConnectorId: 1,
				CsChargingProfiles: types.SetChargingProfileJsonCsChargingProfiles{
					ChargingProfileId:      4,
					StackLevel:             0,
					ChargingProfilePurpose: types.SetChargingProfileJsonChargingProfilePurposeTxDefaultProfile,
					ChargingProfileKind:    types.SetChargingProfileJsonChargingProfileKindAbsolute,
					ChargingSchedule: types.SetChargingProfileJsonChargingSchedule{
						ChargingRateUnit: types.SetChargingProfileJsonChargingRateUnitW,
						ChargingSchedulePeriod: []types.SetChargingProfileJsonChargingSchedulePeriod{
							{StartPeriod: 0, Limit: 7400.0},
						},
					},
				},
			},
			response: &types.SetChargingProfileResponseJson{
				Status: types.SetChargingProfileResponseJsonStatusAccepted,
			},
			setupStore: func() store.ChargingProfileStore {
				return nil
			},
			expectStored: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profileStore := tt.setupStore()
			handler := ocpp16.SetChargingProfileHandler{
				ChargingProfileStore: profileStore,
			}

			err := handler.HandleCallResult(ctx, chargeStationId, tt.request, tt.response, nil)

			if tt.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tt.expectStored && profileStore != nil {
				req := tt.request.(*types.SetChargingProfileJson)
				profiles, err := profileStore.GetChargingProfiles(ctx, chargeStationId, nil, nil, nil)
				require.NoError(t, err)
				assert.Len(t, profiles, 1)
				assert.Equal(t, req.CsChargingProfiles.ChargingProfileId, profiles[0].ChargingProfileId)
			}
		})
	}
}

func TestSetChargingProfileHandler_ProfileConversion(t *testing.T) {
	ctx := context.Background()
	chargeStationId := "cs001"

	validFrom := "2026-01-01T00:00:00Z"
	validTo := "2026-12-31T23:59:59Z"
	startSchedule := "2026-01-01T08:00:00Z"
	duration := 3600
	minRate := 1000.0
	txId := 12345
	numPhases := 3
	recurrency := types.SetChargingProfileJsonRecurrencyKindDaily

	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp16.SetChargingProfileHandler{
		ChargingProfileStore: engine,
	}

	request := &types.SetChargingProfileJson{
		ConnectorId: 2,
		CsChargingProfiles: types.SetChargingProfileJsonCsChargingProfiles{
			ChargingProfileId:      10,
			TransactionId:          &txId,
			StackLevel:             5,
			ChargingProfilePurpose: types.SetChargingProfileJsonChargingProfilePurposeTxProfile,
			ChargingProfileKind:    types.SetChargingProfileJsonChargingProfileKindRecurring,
			RecurrencyKind:         &recurrency,
			ValidFrom:              &validFrom,
			ValidTo:                &validTo,
			ChargingSchedule: types.SetChargingProfileJsonChargingSchedule{
				Duration:         &duration,
				StartSchedule:    &startSchedule,
				ChargingRateUnit: types.SetChargingProfileJsonChargingRateUnitA,
				ChargingSchedulePeriod: []types.SetChargingProfileJsonChargingSchedulePeriod{
					{StartPeriod: 0, Limit: 32.0, NumberPhases: &numPhases},
					{StartPeriod: 1800, Limit: 16.0},
					{StartPeriod: 3600, Limit: 32.0, NumberPhases: &numPhases},
				},
				MinChargingRate: &minRate,
			},
		},
	}
	response := &types.SetChargingProfileResponseJson{
		Status: types.SetChargingProfileResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(ctx, chargeStationId, request, response, nil)
	require.NoError(t, err)

	profiles, err := engine.GetChargingProfiles(ctx, chargeStationId, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, profiles, 1)

	p := profiles[0]
	assert.Equal(t, chargeStationId, p.ChargeStationId)
	assert.Equal(t, 2, p.ConnectorId)
	assert.Equal(t, 10, p.ChargingProfileId)
	assert.NotNil(t, p.TransactionId)
	assert.Equal(t, 12345, *p.TransactionId)
	assert.Equal(t, 5, p.StackLevel)
	assert.Equal(t, store.ChargingProfilePurposeTxProfile, p.ChargingProfilePurpose)
	assert.Equal(t, store.ChargingProfileKindRecurring, p.ChargingProfileKind)
	assert.NotNil(t, p.RecurrencyKind)
	assert.Equal(t, store.RecurrencyKindDaily, *p.RecurrencyKind)
	assert.NotNil(t, p.ValidFrom)
	assert.NotNil(t, p.ValidTo)
	assert.NotNil(t, p.ChargingSchedule.StartSchedule)
	assert.NotNil(t, p.ChargingSchedule.Duration)
	assert.Equal(t, 3600, *p.ChargingSchedule.Duration)
	assert.Equal(t, store.ChargingRateUnitA, p.ChargingSchedule.ChargingRateUnit)
	assert.NotNil(t, p.ChargingSchedule.MinChargingRate)
	assert.Equal(t, 1000.0, *p.ChargingSchedule.MinChargingRate)
	assert.Len(t, p.ChargingSchedule.ChargingSchedulePeriod, 3)
	assert.Equal(t, 0, p.ChargingSchedule.ChargingSchedulePeriod[0].StartPeriod)
	assert.Equal(t, 32.0, p.ChargingSchedule.ChargingSchedulePeriod[0].Limit)
	assert.NotNil(t, p.ChargingSchedule.ChargingSchedulePeriod[0].NumberPhases)
	assert.Equal(t, 3, *p.ChargingSchedule.ChargingSchedulePeriod[0].NumberPhases)
	assert.Nil(t, p.ChargingSchedule.ChargingSchedulePeriod[1].NumberPhases)
}

func TestSetChargingProfileHandler_InvalidDateFormat(t *testing.T) {
	ctx := context.Background()
	chargeStationId := "cs001"

	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp16.SetChargingProfileHandler{
		ChargingProfileStore: engine,
	}

	invalidDate := "not-a-date"
	request := &types.SetChargingProfileJson{
		ConnectorId: 1,
		CsChargingProfiles: types.SetChargingProfileJsonCsChargingProfiles{
			ChargingProfileId:      1,
			StackLevel:             0,
			ChargingProfilePurpose: types.SetChargingProfileJsonChargingProfilePurposeTxDefaultProfile,
			ChargingProfileKind:    types.SetChargingProfileJsonChargingProfileKindAbsolute,
			ValidFrom:              &invalidDate,
			ChargingSchedule: types.SetChargingProfileJsonChargingSchedule{
				ChargingRateUnit: types.SetChargingProfileJsonChargingRateUnitW,
				ChargingSchedulePeriod: []types.SetChargingProfileJsonChargingSchedulePeriod{
					{StartPeriod: 0, Limit: 7400.0},
				},
			},
		},
	}
	response := &types.SetChargingProfileResponseJson{
		Status: types.SetChargingProfileResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(ctx, chargeStationId, request, response, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing validFrom")
}

func TestSetChargingProfileHandler_ReplaceExistingProfile(t *testing.T) {
	ctx := context.Background()
	chargeStationId := "cs001"

	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp16.SetChargingProfileHandler{
		ChargingProfileStore: engine,
	}

	makeRequest := func(limit float64) *types.SetChargingProfileJson {
		return &types.SetChargingProfileJson{
			ConnectorId: 1,
			CsChargingProfiles: types.SetChargingProfileJsonCsChargingProfiles{
				ChargingProfileId:      1,
				StackLevel:             0,
				ChargingProfilePurpose: types.SetChargingProfileJsonChargingProfilePurposeTxDefaultProfile,
				ChargingProfileKind:    types.SetChargingProfileJsonChargingProfileKindAbsolute,
				ChargingSchedule: types.SetChargingProfileJsonChargingSchedule{
					ChargingRateUnit: types.SetChargingProfileJsonChargingRateUnitW,
					ChargingSchedulePeriod: []types.SetChargingProfileJsonChargingSchedulePeriod{
						{StartPeriod: 0, Limit: limit},
					},
				},
			},
		}
	}

	accepted := &types.SetChargingProfileResponseJson{
		Status: types.SetChargingProfileResponseJsonStatusAccepted,
	}

	// Set first profile
	err := handler.HandleCallResult(ctx, chargeStationId, makeRequest(7400.0), accepted, nil)
	require.NoError(t, err)

	// Replace with updated limit
	err = handler.HandleCallResult(ctx, chargeStationId, makeRequest(11000.0), accepted, nil)
	require.NoError(t, err)

	profiles, err := engine.GetChargingProfiles(ctx, chargeStationId, nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, profiles, 1)
	assert.Equal(t, 11000.0, profiles[0].ChargingSchedule.ChargingSchedulePeriod[0].Limit)
}
