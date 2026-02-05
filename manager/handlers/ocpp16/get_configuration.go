// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type GetConfigurationHandler struct {
	SettingsStore store.ChargeStationSettingsStore
}

func (g GetConfigurationHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*ocpp16.GetConfigurationJson)
	resp := response.(*ocpp16.GetConfigurationResponseJson)

	span := trace.SpanFromContext(ctx)

	// Log the retrieved configuration
	span.SetAttributes(
		attribute.Int("config.returned_keys", len(resp.ConfigurationKey)),
		attribute.Int("config.unknown_keys", len(resp.UnknownKey)))

	if len(resp.UnknownKey) > 0 {
		slog.Warn("some configuration keys are unknown",
			"chargeStationId", chargeStationId,
			"unknownKeys", resp.UnknownKey,
			"requestedKeys", req.Key)
	}

	// If configuration keys were returned, store them
	if len(resp.ConfigurationKey) > 0 {
		settings := make(map[string]*store.ChargeStationSetting)
		for _, configKey := range resp.ConfigurationKey {
			var value string
			if configKey.Value != nil {
				value = *configKey.Value
			}
			settings[configKey.Key] = &store.ChargeStationSetting{
				Value:  value,
				Status: store.ChargeStationSettingStatusAccepted,
			}
		}

		err := g.SettingsStore.UpdateChargeStationSettings(ctx, chargeStationId, &store.ChargeStationSettings{
			ChargeStationId: chargeStationId,
			Settings:        settings,
		})
		if err != nil {
			return fmt.Errorf("update charge station settings: %w", err)
		}

		slog.Info("stored configuration from charge station",
			"chargeStationId", chargeStationId,
			"keyCount", len(resp.ConfigurationKey))
	}

	return nil
}
