// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	basehandlers "github.com/thoughtworks/maeve-csms/manager/handlers"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"k8s.io/utils/clock"
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

func changeConfigurationCallError() *basehandlers.CallError {
	return &basehandlers.CallError{
		MessageId:        "msg-001",
		Action:           "ChangeConfiguration",
		ErrorCode:        transport.ErrorNotSupported,
		ErrorDescription: "configuration key is not supported",
	}
}

func TestChangeConfigurationHandlerCallErrorRejectsSetting(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	mockCallMaker := new(MockCallMaker)

	handler := handlers.ChangeConfigurationResultHandler{
		SettingsStore: engine,
		CallMaker:     mockCallMaker,
	}

	err := engine.UpdateChargeStationSettings(ctx, "cs001", &store.ChargeStationSettings{
		Settings: map[string]*store.ChargeStationSetting{
			"UnknownKey":        {Value: "value", Status: store.ChargeStationSettingStatusPending},
			"HeartbeatInterval": {Value: "300", Status: store.ChargeStationSettingStatusPending},
		},
	})
	require.NoError(t, err)

	request := &types.ChangeConfigurationJson{
		Key:   "UnknownKey",
		Value: "value",
	}

	err = handler.HandleCallError(ctx, "cs001", request, changeConfigurationCallError(), nil)
	require.NoError(t, err)

	settings, err := engine.LookupChargeStationSettings(ctx, "cs001")
	require.NoError(t, err)
	require.Len(t, settings.Settings, 2)
	assert.Equal(t, store.ChargeStationSettingStatusRejected, settings.Settings["UnknownKey"].Status)
	assert.Equal(t, "value", settings.Settings["UnknownKey"].Value)
	// the other setting is still waiting for its own answer
	assert.Equal(t, store.ChargeStationSettingStatusPending, settings.Settings["HeartbeatInterval"].Status)

	mockCallMaker.AssertNotCalled(t, "Send", mock.Anything, mock.Anything, mock.Anything)
}

func TestChangeConfigurationHandlerCallErrorTriggersBootWhenAllDone(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	mockCallMaker := new(MockCallMaker)

	handler := handlers.ChangeConfigurationResultHandler{
		SettingsStore: engine,
		CallMaker:     mockCallMaker,
	}

	err := engine.UpdateChargeStationSettings(ctx, "cs001", &store.ChargeStationSettings{
		Settings: map[string]*store.ChargeStationSetting{
			"UnknownKey":            {Value: "value", Status: store.ChargeStationSettingStatusPending},
			"WebSocketPingInterval": {Value: "60", Status: store.ChargeStationSettingStatusRebootRequired},
		},
	})
	require.NoError(t, err)

	// the refused setting was the last one waiting for an answer, so the reboot
	// that the other setting needs is triggered just like for a rejected result
	mockCallMaker.On("Send", mock.Anything, "cs001", mock.MatchedBy(func(req *types.TriggerMessageJson) bool {
		return req.RequestedMessage == types.TriggerMessageJsonRequestedMessageBootNotification
	})).Return(nil)

	request := &types.ChangeConfigurationJson{
		Key:   "UnknownKey",
		Value: "value",
	}

	err = handler.HandleCallError(ctx, "cs001", request, changeConfigurationCallError(), nil)
	require.NoError(t, err)

	settings, err := engine.LookupChargeStationSettings(ctx, "cs001")
	require.NoError(t, err)
	assert.Equal(t, store.ChargeStationSettingStatusRejected, settings.Settings["UnknownKey"].Status)

	mockCallMaker.AssertExpectations(t)
}

func TestChangeConfigurationHandlerCallErrorWithoutRequest(t *testing.T) {
	mockStore := new(MockSettingsStore)
	mockCallMaker := new(MockCallMaker)

	handler := handlers.ChangeConfigurationResultHandler{
		SettingsStore: mockStore,
		CallMaker:     mockCallMaker,
	}

	// the router passes a nil request when the payload of the original call cannot be decoded
	err := handler.HandleCallError(context.Background(), "cs001", nil, changeConfigurationCallError(), nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "ChangeConfiguration msg-001: NotSupported: configuration key is not supported")

	mockStore.AssertNotCalled(t, "UpdateChargeStationSettings", mock.Anything, mock.Anything, mock.Anything)
	mockCallMaker.AssertNotCalled(t, "Send", mock.Anything, mock.Anything, mock.Anything)
}

// The password of a station (AuthorizationKey) is never stored or traced, whatever sent
// it and however the station answered.
func TestChangeConfigurationHandlerKeepsNoAuthorizationKey(t *testing.T) {
	const password = "0123456789abcdef0123456789abcdef01234567"
	request := &types.ChangeConfigurationJson{Key: "AuthorizationKey", Value: password}

	for name, answer := range map[string]func(ctx context.Context, h handlers.ChangeConfigurationResultHandler) error{
		"accepted": func(ctx context.Context, h handlers.ChangeConfigurationResultHandler) error {
			return h.HandleCallResult(ctx, "cs001", request, &types.ChangeConfigurationResponseJson{
				Status: types.ChangeConfigurationResponseJsonStatusAccepted}, nil)
		},
		"reboot required": func(ctx context.Context, h handlers.ChangeConfigurationResultHandler) error {
			return h.HandleCallResult(ctx, "cs001", request, &types.ChangeConfigurationResponseJson{
				Status: types.ChangeConfigurationResponseJsonStatusRebootRequired}, nil)
		},
		"not supported": func(ctx context.Context, h handlers.ChangeConfigurationResultHandler) error {
			return h.HandleCallResult(ctx, "cs001", request, &types.ChangeConfigurationResponseJson{
				Status: types.ChangeConfigurationResponseJsonStatusNotSupported}, nil)
		},
		"call error": func(ctx context.Context, h handlers.ChangeConfigurationResultHandler) error {
			return h.HandleCallError(ctx, "cs001", request, changeConfigurationCallError(), nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			engine := inmemory.NewStore(clock.RealClock{})
			callMaker := new(MockCallMaker)
			callMaker.On("Send", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			tracer, exporter := testutil.GetTracer()
			ctx, span := tracer.Start(context.Background(), "result")

			require.NoError(t, answer(ctx, handlers.ChangeConfigurationResultHandler{SettingsStore: engine, CallMaker: callMaker}))
			span.End()

			settings, err := engine.LookupChargeStationSettings(context.Background(), "cs001")
			require.NoError(t, err)
			require.Contains(t, settings.Settings, "AuthorizationKey")
			assert.Equal(t, "***", settings.Settings["AuthorizationKey"].Value)
			spans := exporter.GetSpans()
			require.Len(t, spans, 1)
			traced := map[string]string{}
			for _, attribute := range spans[0].Attributes {
				traced[string(attribute.Key)] = attribute.Value.Emit()
				assert.NotContains(t, attribute.Value.Emit(), password, string(attribute.Key))
			}
			assert.Equal(t, "***", traced["setting.value"])
		})
	}

	t.Run("other keys keep their values", func(t *testing.T) {
		engine := inmemory.NewStore(clock.RealClock{})
		err := handlers.ChangeConfigurationResultHandler{SettingsStore: engine, CallMaker: new(MockCallMaker)}.HandleCallResult(
			context.Background(), "cs001", &types.ChangeConfigurationJson{Key: "HeartbeatInterval", Value: "300"},
			&types.ChangeConfigurationResponseJson{Status: types.ChangeConfigurationResponseJsonStatusAccepted}, nil)
		require.NoError(t, err)
		settings, err := engine.LookupChargeStationSettings(context.Background(), "cs001")
		require.NoError(t, err)
		assert.Equal(t, "300", settings.Settings["HeartbeatInterval"].Value)
	})
}
