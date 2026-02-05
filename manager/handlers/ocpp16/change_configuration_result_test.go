// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// Mock for ChargeStationSettingsStore
type MockSettingsStore struct {
	mock.Mock
}

func (m *MockSettingsStore) UpdateChargeStationSettings(ctx context.Context, chargeStationId string, settings *store.ChargeStationSettings) error {
	args := m.Called(ctx, chargeStationId, settings)
	return args.Error(0)
}

func (m *MockSettingsStore) LookupChargeStationSettings(ctx context.Context, chargeStationId string) (*store.ChargeStationSettings, error) {
	args := m.Called(ctx, chargeStationId)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*store.ChargeStationSettings), args.Error(1)
}

func (m *MockSettingsStore) ListChargeStationSettings(ctx context.Context, pageSize int, previousChargeStationId string) ([]*store.ChargeStationSettings, error) {
	args := m.Called(ctx, pageSize, previousChargeStationId)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*store.ChargeStationSettings), args.Error(1)
}

func (m *MockSettingsStore) DeleteChargeStationSettings(ctx context.Context, chargeStationId string) error {
	args := m.Called(ctx, chargeStationId)
	return args.Error(0)
}

// Mock for CallMaker
type MockCallMaker struct {
	mock.Mock
}

func (m *MockCallMaker) Send(ctx context.Context, chargeStationId string, request ocpp.Request) error {
	args := m.Called(ctx, chargeStationId, request)
	return args.Error(0)
}

func TestChangeConfigurationHandlerAccepted(t *testing.T) {
	mockStore := new(MockSettingsStore)
	mockCallMaker := new(MockCallMaker)

	handler := handlers.ChangeConfigurationResultHandler{
		SettingsStore: mockStore,
		CallMaker:     mockCallMaker,
	}

	request := &types.ChangeConfigurationJson{
		Key:   "HeartbeatInterval",
		Value: "300",
	}

	response := &types.ChangeConfigurationResponseJson{
		Status: types.ChangeConfigurationResponseJsonStatusAccepted,
	}

	// Expect UpdateChargeStationSettings to be called
	mockStore.On("UpdateChargeStationSettings", mock.Anything, "cs001", mock.MatchedBy(func(settings *store.ChargeStationSettings) bool {
		return settings.ChargeStationId == "cs001" &&
			settings.Settings["HeartbeatInterval"].Value == "300" &&
			settings.Settings["HeartbeatInterval"].Status == store.ChargeStationSettingStatusAccepted
	})).Return(nil)

	// Expect LookupChargeStationSettings to be called
	mockStore.On("LookupChargeStationSettings", mock.Anything, "cs001").Return(&store.ChargeStationSettings{
		ChargeStationId: "cs001",
		Settings: map[string]*store.ChargeStationSetting{
			"HeartbeatInterval": {
				Value:  "300",
				Status: store.ChargeStationSettingStatusAccepted,
			},
		},
	}, nil)

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)

	mockStore.AssertExpectations(t)
	mockCallMaker.AssertExpectations(t)
}

func TestChangeConfigurationHandlerRejected(t *testing.T) {
	mockStore := new(MockSettingsStore)
	mockCallMaker := new(MockCallMaker)

	handler := handlers.ChangeConfigurationResultHandler{
		SettingsStore: mockStore,
		CallMaker:     mockCallMaker,
	}

	request := &types.ChangeConfigurationJson{
		Key:   "InvalidKey",
		Value: "value",
	}

	response := &types.ChangeConfigurationResponseJson{
		Status: types.ChangeConfigurationResponseJsonStatusRejected,
	}

	// Expect UpdateChargeStationSettings to be called
	mockStore.On("UpdateChargeStationSettings", mock.Anything, "cs001", mock.MatchedBy(func(settings *store.ChargeStationSettings) bool {
		return settings.Settings["InvalidKey"].Status == store.ChargeStationSettingStatusRejected
	})).Return(nil)

	// Expect LookupChargeStationSettings to be called
	mockStore.On("LookupChargeStationSettings", mock.Anything, "cs001").Return(&store.ChargeStationSettings{
		ChargeStationId: "cs001",
		Settings: map[string]*store.ChargeStationSetting{
			"InvalidKey": {
				Value:  "value",
				Status: store.ChargeStationSettingStatusRejected,
			},
		},
	}, nil)

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)

	mockStore.AssertExpectations(t)
	mockCallMaker.AssertExpectations(t)
}

func TestChangeConfigurationHandlerRebootRequired(t *testing.T) {
	mockStore := new(MockSettingsStore)
	mockCallMaker := new(MockCallMaker)

	handler := handlers.ChangeConfigurationResultHandler{
		SettingsStore: mockStore,
		CallMaker:     mockCallMaker,
	}

	request := &types.ChangeConfigurationJson{
		Key:   "MeterValueSampleInterval",
		Value: "60",
	}

	response := &types.ChangeConfigurationResponseJson{
		Status: types.ChangeConfigurationResponseJsonStatusRebootRequired,
	}

	// Expect UpdateChargeStationSettings to be called
	mockStore.On("UpdateChargeStationSettings", mock.Anything, "cs001", mock.MatchedBy(func(settings *store.ChargeStationSettings) bool {
		return settings.Settings["MeterValueSampleInterval"].Status == store.ChargeStationSettingStatusRebootRequired
	})).Return(nil)

	// Expect LookupChargeStationSettings to be called - return reboot required setting
	mockStore.On("LookupChargeStationSettings", mock.Anything, "cs001").Return(&store.ChargeStationSettings{
		ChargeStationId: "cs001",
		Settings: map[string]*store.ChargeStationSetting{
			"MeterValueSampleInterval": {
				Value:  "60",
				Status: store.ChargeStationSettingStatusRebootRequired,
			},
		},
	}, nil)

	// Expect TriggerMessage to be called for BootNotification
	mockCallMaker.On("Send", mock.Anything, "cs001", mock.MatchedBy(func(req interface{}) bool {
		triggerMsg, ok := req.(*types.TriggerMessageJson)
		return ok && triggerMsg.RequestedMessage == types.TriggerMessageJsonRequestedMessageBootNotification
	})).Return(nil)

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)

	mockStore.AssertExpectations(t)
	mockCallMaker.AssertExpectations(t)
}

func TestChangeConfigurationHandlerRebootRequiredNotApplied(t *testing.T) {
	mockStore := new(MockSettingsStore)
	mockCallMaker := new(MockCallMaker)

	handler := handlers.ChangeConfigurationResultHandler{
		SettingsStore: mockStore,
		CallMaker:     mockCallMaker,
	}

	request := &types.ChangeConfigurationJson{
		Key:   "Setting2",
		Value: "value2",
	}

	response := &types.ChangeConfigurationResponseJson{
		Status: types.ChangeConfigurationResponseJsonStatusRebootRequired,
	}

	// Expect UpdateChargeStationSettings to be called
	mockStore.On("UpdateChargeStationSettings", mock.Anything, "cs001", mock.Anything).Return(nil)

	// Expect LookupChargeStationSettings to be called - return mix of pending and reboot required
	mockStore.On("LookupChargeStationSettings", mock.Anything, "cs001").Return(&store.ChargeStationSettings{
		ChargeStationId: "cs001",
		Settings: map[string]*store.ChargeStationSetting{
			"Setting1": {
				Value:  "value1",
				Status: store.ChargeStationSettingStatusPending,
			},
			"Setting2": {
				Value:  "value2",
				Status: store.ChargeStationSettingStatusRebootRequired,
			},
		},
	}, nil)

	// TriggerMessage should NOT be called because there's still a pending setting
	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)

	mockStore.AssertExpectations(t)
	mockCallMaker.AssertExpectations(t)
}

func TestChangeConfigurationHandlerNotApplied(t *testing.T) {
	mockStore := new(MockSettingsStore)
	mockCallMaker := new(MockCallMaker)

	handler := handlers.ChangeConfigurationResultHandler{
		SettingsStore: mockStore,
		CallMaker:     mockCallMaker,
	}

	request := &types.ChangeConfigurationJson{
		Key:   "ReadOnlyKey",
		Value: "value",
	}

	response := &types.ChangeConfigurationResponseJson{
		Status: types.ChangeConfigurationResponseJsonStatusNotSupported,
	}

	// Expect UpdateChargeStationSettings to be called
	mockStore.On("UpdateChargeStationSettings", mock.Anything, "cs001", mock.MatchedBy(func(settings *store.ChargeStationSettings) bool {
		return settings.Settings["ReadOnlyKey"].Status == store.ChargeStationSettingStatusNotSupported
	})).Return(nil)

	// Expect LookupChargeStationSettings to be called
	mockStore.On("LookupChargeStationSettings", mock.Anything, "cs001").Return(&store.ChargeStationSettings{
		ChargeStationId: "cs001",
		Settings: map[string]*store.ChargeStationSetting{
			"ReadOnlyKey": {
				Value:  "value",
				Status: store.ChargeStationSettingStatusNotSupported,
			},
		},
	}, nil)

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)

	mockStore.AssertExpectations(t)
	mockCallMaker.AssertExpectations(t)
}

func TestChangeConfigurationHandlerMultipleSettingsAllDone(t *testing.T) {
	mockStore := new(MockSettingsStore)
	mockCallMaker := new(MockCallMaker)

	handler := handlers.ChangeConfigurationResultHandler{
		SettingsStore: mockStore,
		CallMaker:     mockCallMaker,
	}

	request := &types.ChangeConfigurationJson{
		Key:   "Setting3",
		Value: "value3",
	}

	response := &types.ChangeConfigurationResponseJson{
		Status: types.ChangeConfigurationResponseJsonStatusAccepted,
	}

	// Expect UpdateChargeStationSettings to be called
	mockStore.On("UpdateChargeStationSettings", mock.Anything, "cs001", mock.Anything).Return(nil)

	// Expect LookupChargeStationSettings to be called - return all accepted settings
	mockStore.On("LookupChargeStationSettings", mock.Anything, "cs001").Return(&store.ChargeStationSettings{
		ChargeStationId: "cs001",
		Settings: map[string]*store.ChargeStationSetting{
			"Setting1": {
				Value:  "value1",
				Status: store.ChargeStationSettingStatusAccepted,
			},
			"Setting2": {
				Value:  "value2",
				Status: store.ChargeStationSettingStatusAccepted,
			},
			"Setting3": {
				Value:  "value3",
				Status: store.ChargeStationSettingStatusAccepted,
			},
		},
	}, nil)

	// TriggerMessage should NOT be called (no reboot required)
	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	assert.NoError(t, err)

	mockStore.AssertExpectations(t)
	mockCallMaker.AssertExpectations(t)
}

func TestChangeConfigurationHandlerStoreUpdateError(t *testing.T) {
	mockStore := new(MockSettingsStore)
	mockCallMaker := new(MockCallMaker)

	handler := handlers.ChangeConfigurationResultHandler{
		SettingsStore: mockStore,
		CallMaker:     mockCallMaker,
	}

	request := &types.ChangeConfigurationJson{
		Key:   "Key",
		Value: "Value",
	}

	response := &types.ChangeConfigurationResponseJson{
		Status: types.ChangeConfigurationResponseJsonStatusAccepted,
	}

	// Expect UpdateChargeStationSettings to fail
	mockStore.On("UpdateChargeStationSettings", mock.Anything, "cs001", mock.Anything).Return(assert.AnError)

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update charge station settings")

	mockStore.AssertExpectations(t)
	mockCallMaker.AssertExpectations(t)
}

func TestChangeConfigurationHandlerStoreLookupError(t *testing.T) {
	mockStore := new(MockSettingsStore)
	mockCallMaker := new(MockCallMaker)

	handler := handlers.ChangeConfigurationResultHandler{
		SettingsStore: mockStore,
		CallMaker:     mockCallMaker,
	}

	request := &types.ChangeConfigurationJson{
		Key:   "Key",
		Value: "Value",
	}

	response := &types.ChangeConfigurationResponseJson{
		Status: types.ChangeConfigurationResponseJsonStatusAccepted,
	}

	// Expect UpdateChargeStationSettings to succeed
	mockStore.On("UpdateChargeStationSettings", mock.Anything, "cs001", mock.Anything).Return(nil)

	// Expect LookupChargeStationSettings to fail
	mockStore.On("LookupChargeStationSettings", mock.Anything, "cs001").Return(nil, assert.AnError)

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lookup charge station settings")

	mockStore.AssertExpectations(t)
	mockCallMaker.AssertExpectations(t)
}
