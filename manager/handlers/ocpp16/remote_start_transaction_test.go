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

func TestRemoteStartTransactionHandler_HandleCallResult(t *testing.T) {
	ctx := context.Background()
	chargeStationId := "cs001"
	idTag := "VALID_TOKEN"
	connectorId := 1

	tests := []struct {
		name          string
		request       ocpp.Request
		response      ocpp.Response
		setupStore    func() store.TokenStore
		expectedError error
	}{
		{
			name: "successful remote start - accepted",
			request: &types.RemoteStartTransactionJson{
				IdTag:       idTag,
				ConnectorId: nil,
			},
			response: &types.RemoteStartTransactionResponseJson{
				Status: types.RemoteStartTransactionResponseJsonStatusAccepted,
			},
			setupStore: func() store.TokenStore {
				engine := inmemory.NewStore(clock.RealClock{})
				_ = engine.SetToken(ctx, &store.Token{
					CountryCode: "US",
					PartyId:     "ABC",
					Type:        "RFID",
					Uid:         idTag,
					ContractId:  "CONTRACT123",
					Valid:       true,
					CacheMode:   store.CacheModeAlways,
				})
				return engine
			},
			expectedError: nil,
		},
		{
			name: "remote start rejected",
			request: &types.RemoteStartTransactionJson{
				IdTag:       idTag,
				ConnectorId: nil,
			},
			response: &types.RemoteStartTransactionResponseJson{
				Status: types.RemoteStartTransactionResponseJsonStatusRejected,
			},
			setupStore: func() store.TokenStore {
				engine := inmemory.NewStore(clock.RealClock{})
				_ = engine.SetToken(ctx, &store.Token{
					CountryCode: "US",
					PartyId:     "ABC",
					Type:        "RFID",
					Uid:         idTag,
					ContractId:  "CONTRACT123",
					Valid:       true,
					CacheMode:   store.CacheModeAlways,
				})
				return engine
			},
			expectedError: nil,
		},
		{
			name: "remote start with specific connector - accepted",
			request: &types.RemoteStartTransactionJson{
				IdTag:       idTag,
				ConnectorId: &connectorId,
			},
			response: &types.RemoteStartTransactionResponseJson{
				Status: types.RemoteStartTransactionResponseJsonStatusAccepted,
			},
			setupStore: func() store.TokenStore {
				engine := inmemory.NewStore(clock.RealClock{})
				_ = engine.SetToken(ctx, &store.Token{
					CountryCode: "US",
					PartyId:     "ABC",
					Type:        "RFID",
					Uid:         idTag,
					ContractId:  "CONTRACT123",
					Valid:       true,
					CacheMode:   store.CacheModeAlways,
				})
				return engine
			},
			expectedError: nil,
		},
		{
			name: "remote start with charging profile - accepted",
			request: &types.RemoteStartTransactionJson{
				IdTag:       idTag,
				ConnectorId: &connectorId,
				ChargingProfile: &types.RemoteStartTransactionJsonChargingProfile{
					ChargingProfileId:      1,
					ChargingProfileKind:    types.RemoteStartTransactionJsonChargingProfileChargingProfileKindAbsolute,
					ChargingProfilePurpose: types.RemoteStartTransactionJsonChargingProfileChargingProfilePurposeTxProfile,
					ChargingSchedule: types.RemoteStartTransactionJsonChargingProfileChargingSchedule{
						ChargingRateUnit: types.RemoteStartTransactionJsonChargingProfileChargingScheduleChargingRateUnitW,
						ChargingSchedulePeriod: []types.RemoteStartTransactionJsonChargingProfileChargingScheduleChargingSchedulePeriodElem{
							{
								StartPeriod: 0,
								Limit:       7400.0,
							},
						},
					},
				},
			},
			response: &types.RemoteStartTransactionResponseJson{
				Status: types.RemoteStartTransactionResponseJsonStatusAccepted,
			},
			setupStore: func() store.TokenStore {
				engine := inmemory.NewStore(clock.RealClock{})
				_ = engine.SetToken(ctx, &store.Token{
					CountryCode: "US",
					PartyId:     "ABC",
					Type:        "RFID",
					Uid:         idTag,
					ContractId:  "CONTRACT123",
					Valid:       true,
					CacheMode:   store.CacheModeAlways,
				})
				return engine
			},
			expectedError: nil,
		},
		{
			name: "remote start with unknown token - accepted by charge station",
			request: &types.RemoteStartTransactionJson{
				IdTag:       "UNKNOWN_TOKEN",
				ConnectorId: nil,
			},
			response: &types.RemoteStartTransactionResponseJson{
				Status: types.RemoteStartTransactionResponseJsonStatusAccepted,
			},
			setupStore: func() store.TokenStore {
				return inmemory.NewStore(clock.RealClock{})
			},
			expectedError: nil,
		},
		{
			name: "remote start with invalid token - rejected by charge station",
			request: &types.RemoteStartTransactionJson{
				IdTag:       "INVALID_TOKEN",
				ConnectorId: nil,
			},
			response: &types.RemoteStartTransactionResponseJson{
				Status: types.RemoteStartTransactionResponseJsonStatusRejected,
			},
			setupStore: func() store.TokenStore {
				engine := inmemory.NewStore(clock.RealClock{})
				_ = engine.SetToken(ctx, &store.Token{
					CountryCode: "US",
					PartyId:     "ABC",
					Type:        "RFID",
					Uid:         "INVALID_TOKEN",
					ContractId:  "CONTRACT123",
					Valid:       false, // Invalid token
					CacheMode:   store.CacheModeAlways,
				})
				return engine
			},
			expectedError: nil,
		},
		{
			name: "remote start with invalid token - accepted by charge station",
			request: &types.RemoteStartTransactionJson{
				IdTag:       "INVALID_TOKEN",
				ConnectorId: nil,
			},
			response: &types.RemoteStartTransactionResponseJson{
				Status: types.RemoteStartTransactionResponseJsonStatusAccepted,
			},
			setupStore: func() store.TokenStore {
				engine := inmemory.NewStore(clock.RealClock{})
				_ = engine.SetToken(ctx, &store.Token{
					CountryCode: "US",
					PartyId:     "ABC",
					Type:        "RFID",
					Uid:         "INVALID_TOKEN",
					ContractId:  "CONTRACT123",
					Valid:       false, // Invalid token but charge station accepts
					CacheMode:   store.CacheModeAlways,
				})
				return engine
			},
			expectedError: nil,
		},
		{
			name: "remote start without TokenStore - accepted",
			request: &types.RemoteStartTransactionJson{
				IdTag:       idTag,
				ConnectorId: nil,
			},
			response: &types.RemoteStartTransactionResponseJson{
				Status: types.RemoteStartTransactionResponseJsonStatusAccepted,
			},
			setupStore: func() store.TokenStore {
				return nil // No TokenStore
			},
			expectedError: nil,
		},
		{
			name: "remote start without TokenStore - rejected",
			request: &types.RemoteStartTransactionJson{
				IdTag:       idTag,
				ConnectorId: nil,
			},
			response: &types.RemoteStartTransactionResponseJson{
				Status: types.RemoteStartTransactionResponseJsonStatusRejected,
			},
			setupStore: func() store.TokenStore {
				return nil // No TokenStore
			},
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := ocpp16.RemoteStartTransactionHandler{
				TokenStore: tt.setupStore(),
			}

			err := handler.HandleCallResult(ctx, chargeStationId, tt.request, tt.response, nil)

			if tt.expectedError != nil {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRemoteStartTransactionHandler_TokenValidation(t *testing.T) {
	ctx := context.Background()
	chargeStationId := "cs001"

	tests := []struct {
		name        string
		idTag       string
		setupToken  func(engine *inmemory.Store)
		expectError bool
	}{
		{
			name:  "valid token exists",
			idTag: "VALID_TOKEN",
			setupToken: func(engine *inmemory.Store) {
				_ = engine.SetToken(ctx, &store.Token{
					CountryCode: "US",
					PartyId:     "ABC",
					Type:        "RFID",
					Uid:         "VALID_TOKEN",
					ContractId:  "CONTRACT123",
					Valid:       true,
					CacheMode:   store.CacheModeAlways,
				})
			},
			expectError: false,
		},
		{
			name:  "invalid token exists",
			idTag: "INVALID_TOKEN",
			setupToken: func(engine *inmemory.Store) {
				_ = engine.SetToken(ctx, &store.Token{
					CountryCode: "US",
					PartyId:     "ABC",
					Type:        "RFID",
					Uid:         "INVALID_TOKEN",
					ContractId:  "CONTRACT123",
					Valid:       false,
					CacheMode:   store.CacheModeAlways,
				})
			},
			expectError: false, // Handler logs warning but doesn't error
		},
		{
			name:       "token not found",
			idTag:      "UNKNOWN_TOKEN",
			setupToken: func(engine *inmemory.Store) {},
			expectError: false, // Handler logs warning but doesn't error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := inmemory.NewStore(clock.RealClock{})
			tt.setupToken(engine)

			handler := ocpp16.RemoteStartTransactionHandler{
				TokenStore: engine,
			}

			request := &types.RemoteStartTransactionJson{
				IdTag:       tt.idTag,
				ConnectorId: nil,
			}
			response := &types.RemoteStartTransactionResponseJson{
				Status: types.RemoteStartTransactionResponseJsonStatusAccepted,
			}

			err := handler.HandleCallResult(ctx, chargeStationId, request, response, nil)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRemoteStartTransactionHandler_ChargingProfileVariations(t *testing.T) {
	ctx := context.Background()
	chargeStationId := "cs001"
	idTag := "VALID_TOKEN"
	connectorId := 1

	engine := inmemory.NewStore(clock.RealClock{})
	_ = engine.SetToken(ctx, &store.Token{
		CountryCode: "US",
		PartyId:     "ABC",
		Type:        "RFID",
		Uid:         idTag,
		ContractId:  "CONTRACT123",
		Valid:       true,
		CacheMode:   store.CacheModeAlways,
	})

	tests := []struct {
		name            string
		chargingProfile *types.RemoteStartTransactionJsonChargingProfile
	}{
		{
			name:            "no charging profile",
			chargingProfile: nil,
		},
		{
			name: "absolute charging profile with single period",
			chargingProfile: &types.RemoteStartTransactionJsonChargingProfile{
				ChargingProfileId:      1,
				ChargingProfileKind:    types.RemoteStartTransactionJsonChargingProfileChargingProfileKindAbsolute,
				ChargingProfilePurpose: types.RemoteStartTransactionJsonChargingProfileChargingProfilePurposeTxProfile,
				ChargingSchedule: types.RemoteStartTransactionJsonChargingProfileChargingSchedule{
					ChargingRateUnit: types.RemoteStartTransactionJsonChargingProfileChargingScheduleChargingRateUnitW,
					ChargingSchedulePeriod: []types.RemoteStartTransactionJsonChargingProfileChargingScheduleChargingSchedulePeriodElem{
						{StartPeriod: 0, Limit: 7400.0},
					},
				},
			},
		},
		{
			name: "recurring charging profile with multiple periods",
			chargingProfile: &types.RemoteStartTransactionJsonChargingProfile{
				ChargingProfileId:      2,
				ChargingProfileKind:    types.RemoteStartTransactionJsonChargingProfileChargingProfileKindRecurring,
				ChargingProfilePurpose: types.RemoteStartTransactionJsonChargingProfileChargingProfilePurposeTxDefaultProfile,
				RecurrencyKind:         ptrRecurrencyKind(types.RemoteStartTransactionJsonChargingProfileRecurrencyKindDaily),
				ChargingSchedule: types.RemoteStartTransactionJsonChargingProfileChargingSchedule{
					ChargingRateUnit: types.RemoteStartTransactionJsonChargingProfileChargingScheduleChargingRateUnitA,
					ChargingSchedulePeriod: []types.RemoteStartTransactionJsonChargingProfileChargingScheduleChargingSchedulePeriodElem{
						{StartPeriod: 0, Limit: 32.0},
						{StartPeriod: 3600, Limit: 16.0},
						{StartPeriod: 7200, Limit: 32.0},
					},
				},
			},
		},
		{
			name: "relative charging profile",
			chargingProfile: &types.RemoteStartTransactionJsonChargingProfile{
				ChargingProfileId:      3,
				ChargingProfileKind:    types.RemoteStartTransactionJsonChargingProfileChargingProfileKindRelative,
				ChargingProfilePurpose: types.RemoteStartTransactionJsonChargingProfileChargingProfilePurposeChargePointMaxProfile,
				ChargingSchedule: types.RemoteStartTransactionJsonChargingProfileChargingSchedule{
					ChargingRateUnit: types.RemoteStartTransactionJsonChargingProfileChargingScheduleChargingRateUnitW,
					ChargingSchedulePeriod: []types.RemoteStartTransactionJsonChargingProfileChargingScheduleChargingSchedulePeriodElem{
						{StartPeriod: 0, Limit: 11000.0},
					},
					Duration:        ptrInt(7200),
					MinChargingRate: ptrFloat(1000.0),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := ocpp16.RemoteStartTransactionHandler{
				TokenStore: engine,
			}

			request := &types.RemoteStartTransactionJson{
				IdTag:           idTag,
				ConnectorId:     &connectorId,
				ChargingProfile: tt.chargingProfile,
			}
			response := &types.RemoteStartTransactionResponseJson{
				Status: types.RemoteStartTransactionResponseJsonStatusAccepted,
			}

			err := handler.HandleCallResult(ctx, chargeStationId, request, response, nil)
			require.NoError(t, err)
		})
	}
}

func ptrInt(i int) *int {
	return &i
}

func ptrFloat(f float64) *float64 {
	return &f
}

func ptrRecurrencyKind(r types.RemoteStartTransactionJsonChargingProfileRecurrencyKind) *types.RemoteStartTransactionJsonChargingProfileRecurrencyKind {
	return &r
}
