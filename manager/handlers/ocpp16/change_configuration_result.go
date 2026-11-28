// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type ChangeConfigurationResultHandler struct {
	SettingsStore store.ChargeStationSettingsStore
	CallMaker     handlers.CallMaker
}

func (c ChangeConfigurationResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*ocpp16.ChangeConfigurationJson)
	resp := response.(*ocpp16.ChangeConfigurationResponseJson)

	return c.updateSetting(ctx, chargeStationId, req, store.ChargeStationSettingStatus(resp.Status))
}

// HandleCallError marks the setting as rejected when the charge station answers the
// ChangeConfiguration call with a CallError: only pending settings are (re)sent, so
// the call is not repeated.
func (c ChangeConfigurationResultHandler) HandleCallError(ctx context.Context, chargeStationId string, request ocpp.Request, callError *handlers.CallError, state any) error {
	req, ok := request.(*ocpp16.ChangeConfigurationJson)
	if !ok || req == nil {
		return fmt.Errorf("no request for call error: %w", callError)
	}

	return c.updateSetting(ctx, chargeStationId, req, store.ChargeStationSettingStatusRejected)
}

// secretConfigurationKeys are the configuration keys whose values are secrets: the
// password of the station (security profiles 1 and 2). Their values are neither stored
// nor traced.
var secretConfigurationKeys = map[string]bool{
	"AuthorizationKey": true,
}

// redactedSettingValue replaces the value of a secret configuration key.
const redactedSettingValue = "***"

func (c ChangeConfigurationResultHandler) updateSetting(ctx context.Context, chargeStationId string, req *ocpp16.ChangeConfigurationJson, status store.ChargeStationSettingStatus) error {
	value := req.Value
	if secretConfigurationKeys[req.Key] {
		value = redactedSettingValue
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("setting.key", req.Key),
		attribute.String("setting.value", value),
		attribute.String("setting.status", string(status)))

	err := c.SettingsStore.UpdateChargeStationSettings(ctx, chargeStationId, &store.ChargeStationSettings{
		ChargeStationId: chargeStationId,
		Settings: map[string]*store.ChargeStationSetting{
			req.Key: {
				Value:  value,
				Status: status,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("update charge station settings: %w", err)
	}

	settings, err := c.SettingsStore.LookupChargeStationSettings(ctx, chargeStationId)
	if err != nil {
		return fmt.Errorf("lookup charge station settings: %w", err)
	}

	// check if all settings are done and if so reboot the charge station if necessary
	allDone := true
	rebootRequired := false
	for _, setting := range settings.Settings {
		if setting.Status == store.ChargeStationSettingStatusRebootRequired {
			rebootRequired = true
		}
		if setting.Status == store.ChargeStationSettingStatusPending {
			allDone = false
			break
		}
	}
	if allDone && rebootRequired {
		err = c.CallMaker.Send(ctx, chargeStationId, &ocpp16.TriggerMessageJson{
			RequestedMessage: ocpp16.TriggerMessageJsonRequestedMessageBootNotification,
		})
	}

	return err
}
