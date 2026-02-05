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
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"k8s.io/utils/clock"
)

func TestGetConfigurationHandler_HandleCallResult(t *testing.T) {
	ctx := context.Background()
	chargeStationId := "cs001"
	engine := inmemory.NewStore(clock.RealClock{})

	handler := ocpp16.GetConfigurationHandler{
		SettingsStore: engine,
	}

	tests := []struct {
		name          string
		request       ocpp.Request
		response      ocpp.Response
		expectedError error
		validate      func(t *testing.T, engine *inmemory.Store)
	}{
		{
			name: "successful retrieval of all configuration",
			request: &types.GetConfigurationJson{
				Key: nil, // Request all keys
			},
			response: &types.GetConfigurationResponseJson{
				ConfigurationKey: []types.GetConfigurationResponseJsonConfigurationKeyElem{
					{
						Key:      "HeartbeatInterval",
						Value:    stringPtr("300"),
						Readonly: false,
					},
					{
						Key:      "MeterValueSampleInterval",
						Value:    stringPtr("60"),
						Readonly: false,
					},
					{
						Key:      "NumberOfConnectors",
						Value:    stringPtr("2"),
						Readonly: true,
					},
				},
				UnknownKey: nil,
			},
			expectedError: nil,
			validate: func(t *testing.T, engine *inmemory.Store) {
				settings, err := engine.LookupChargeStationSettings(ctx, chargeStationId)
				require.NoError(t, err)
				assert.Equal(t, 3, len(settings.Settings))
				assert.Equal(t, "300", settings.Settings["HeartbeatInterval"].Value)
				assert.Equal(t, "60", settings.Settings["MeterValueSampleInterval"].Value)
				assert.Equal(t, "2", settings.Settings["NumberOfConnectors"].Value)
			},
		},
		{
			name: "successful retrieval of specific keys",
			request: &types.GetConfigurationJson{
				Key: []string{"HeartbeatInterval", "WebSocketPingInterval"},
			},
			response: &types.GetConfigurationResponseJson{
				ConfigurationKey: []types.GetConfigurationResponseJsonConfigurationKeyElem{
					{
						Key:      "HeartbeatInterval",
						Value:    stringPtr("300"),
						Readonly: false,
					},
				},
				UnknownKey: []string{"WebSocketPingInterval"},
			},
			expectedError: nil,
			validate: func(t *testing.T, engine *inmemory.Store) {
				settings, err := engine.LookupChargeStationSettings(ctx, chargeStationId)
				require.NoError(t, err)
				assert.Equal(t, 1, len(settings.Settings))
				assert.Equal(t, "300", settings.Settings["HeartbeatInterval"].Value)
			},
		},
		{
			name: "configuration key with nil value",
			request: &types.GetConfigurationJson{
				Key: []string{"OptionalSetting"},
			},
			response: &types.GetConfigurationResponseJson{
				ConfigurationKey: []types.GetConfigurationResponseJsonConfigurationKeyElem{
					{
						Key:      "OptionalSetting",
						Value:    nil, // No value set
						Readonly: false,
					},
				},
				UnknownKey: nil,
			},
			expectedError: nil,
			validate: func(t *testing.T, engine *inmemory.Store) {
				settings, err := engine.LookupChargeStationSettings(ctx, chargeStationId)
				require.NoError(t, err)
				assert.Equal(t, 1, len(settings.Settings))
				assert.Equal(t, "", settings.Settings["OptionalSetting"].Value)
			},
		},
		{
			name: "all requested keys are unknown",
			request: &types.GetConfigurationJson{
				Key: []string{"UnknownKey1", "UnknownKey2"},
			},
			response: &types.GetConfigurationResponseJson{
				ConfigurationKey: nil,
				UnknownKey:       []string{"UnknownKey1", "UnknownKey2"},
			},
			expectedError: nil,
			validate: func(t *testing.T, engine *inmemory.Store) {
				// No settings should be stored
				settings, err := engine.LookupChargeStationSettings(ctx, chargeStationId)
				require.NoError(t, err)
				if settings != nil {
					assert.Equal(t, 0, len(settings.Settings))
				}
			},
		},
		{
			name: "empty response - no configuration keys returned",
			request: &types.GetConfigurationJson{
				Key: nil,
			},
			response: &types.GetConfigurationResponseJson{
				ConfigurationKey: []types.GetConfigurationResponseJsonConfigurationKeyElem{},
				UnknownKey:       nil,
			},
			expectedError: nil,
			validate: func(t *testing.T, engine *inmemory.Store) {
				settings, err := engine.LookupChargeStationSettings(ctx, chargeStationId)
				require.NoError(t, err)
				if settings != nil {
					assert.Equal(t, 0, len(settings.Settings))
				}
			},
		},
		{
			name: "readonly configuration keys",
			request: &types.GetConfigurationJson{
				Key: []string{"ChargePointModel", "ChargePointVendor"},
			},
			response: &types.GetConfigurationResponseJson{
				ConfigurationKey: []types.GetConfigurationResponseJsonConfigurationKeyElem{
					{
						Key:      "ChargePointModel",
						Value:    stringPtr("ModelX"),
						Readonly: true,
					},
					{
						Key:      "ChargePointVendor",
						Value:    stringPtr("VendorY"),
						Readonly: true,
					},
				},
				UnknownKey: nil,
			},
			expectedError: nil,
			validate: func(t *testing.T, engine *inmemory.Store) {
				settings, err := engine.LookupChargeStationSettings(ctx, chargeStationId)
				require.NoError(t, err)
				assert.Equal(t, 2, len(settings.Settings))
				assert.Equal(t, "ModelX", settings.Settings["ChargePointModel"].Value)
				assert.Equal(t, "VendorY", settings.Settings["ChargePointVendor"].Value)
			},
		},
		{
			name: "mixed readonly and writable keys",
			request: &types.GetConfigurationJson{
				Key: []string{"HeartbeatInterval", "NumberOfConnectors"},
			},
			response: &types.GetConfigurationResponseJson{
				ConfigurationKey: []types.GetConfigurationResponseJsonConfigurationKeyElem{
					{
						Key:      "HeartbeatInterval",
						Value:    stringPtr("300"),
						Readonly: false,
					},
					{
						Key:      "NumberOfConnectors",
						Value:    stringPtr("4"),
						Readonly: true,
					},
				},
				UnknownKey: nil,
			},
			expectedError: nil,
			validate: func(t *testing.T, engine *inmemory.Store) {
				settings, err := engine.LookupChargeStationSettings(ctx, chargeStationId)
				require.NoError(t, err)
				assert.Equal(t, 2, len(settings.Settings))
				assert.Equal(t, "300", settings.Settings["HeartbeatInterval"].Value)
				assert.Equal(t, "4", settings.Settings["NumberOfConnectors"].Value)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset store for each test
			engine = inmemory.NewStore(clock.RealClock{})
			handler.SettingsStore = engine

			err := handler.HandleCallResult(ctx, chargeStationId, tt.request, tt.response, nil)

			if tt.expectedError != nil {
				require.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
			} else {
				require.NoError(t, err)
			}

			if tt.validate != nil {
				tt.validate(t, engine)
			}
		})
	}
}

func TestGetConfigurationHandler_ValidRequestTypes(t *testing.T) {
	ctx := context.Background()
	chargeStationId := "cs001"
	engine := inmemory.NewStore(clock.RealClock{})

	handler := ocpp16.GetConfigurationHandler{
		SettingsStore: engine,
	}

	// Test with nil key list (request all configurations)
	request := &types.GetConfigurationJson{
		Key: nil,
	}
	response := &types.GetConfigurationResponseJson{
		ConfigurationKey: []types.GetConfigurationResponseJsonConfigurationKeyElem{
			{
				Key:      "SomeKey",
				Value:    stringPtr("SomeValue"),
				Readonly: false,
			},
		},
	}

	err := handler.HandleCallResult(ctx, chargeStationId, request, response, nil)
	require.NoError(t, err)

	// Verify stored
	settings, err := engine.LookupChargeStationSettings(ctx, chargeStationId)
	require.NoError(t, err)
	assert.Equal(t, 1, len(settings.Settings))
	assert.Equal(t, "SomeValue", settings.Settings["SomeKey"].Value)
}

func TestGetConfigurationHandler_UpdateExistingSettings(t *testing.T) {
	ctx := context.Background()
	chargeStationId := "cs001"
	engine := inmemory.NewStore(clock.RealClock{})

	handler := ocpp16.GetConfigurationHandler{
		SettingsStore: engine,
	}

	// First call - initial configuration
	request1 := &types.GetConfigurationJson{Key: nil}
	response1 := &types.GetConfigurationResponseJson{
		ConfigurationKey: []types.GetConfigurationResponseJsonConfigurationKeyElem{
			{
				Key:      "HeartbeatInterval",
				Value:    stringPtr("300"),
				Readonly: false,
			},
		},
	}

	err := handler.HandleCallResult(ctx, chargeStationId, request1, response1, nil)
	require.NoError(t, err)

	// Second call - updated configuration
	request2 := &types.GetConfigurationJson{Key: []string{"HeartbeatInterval"}}
	response2 := &types.GetConfigurationResponseJson{
		ConfigurationKey: []types.GetConfigurationResponseJsonConfigurationKeyElem{
			{
				Key:      "HeartbeatInterval",
				Value:    stringPtr("600"), // Updated value
				Readonly: false,
			},
		},
	}

	err = handler.HandleCallResult(ctx, chargeStationId, request2, response2, nil)
	require.NoError(t, err)

	// Verify updated value
	settings, err := engine.LookupChargeStationSettings(ctx, chargeStationId)
	require.NoError(t, err)
	assert.Equal(t, "600", settings.Settings["HeartbeatInterval"].Value)
}

func stringPtr(s string) *string {
	return &s
}
