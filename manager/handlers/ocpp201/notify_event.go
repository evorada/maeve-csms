// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
)

type NotifyEventHandler struct {
	Store store.ChargeStationSettingsStore
}

func (h NotifyEventHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (response ocpp.Response, err error) {
	req := request.(*ocpp201.NotifyEventRequestJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("notify_event.seq_no", req.SeqNo),
		attribute.Bool("notify_event.tbc", req.Tbc),
		attribute.String("notify_event.generated_at", req.GeneratedAt),
		attribute.Int("notify_event.event_count", len(req.EventData)),
	)

	payload, err := json.Marshal(req.EventData)
	if err != nil {
		slog.Error("failed to marshal notify event payload", "chargeStationId", chargeStationId, "error", err)
		return &ocpp201.NotifyEventResponseJson{}, err
	}

	settingKey := fmt.Sprintf("ocpp201.notify_event.%d", req.SeqNo)
	now := time.Now()
	settings := &store.ChargeStationSettings{
		ChargeStationId: chargeStationId,
		Settings: map[string]*store.ChargeStationSetting{
			settingKey: {
				Value:     string(payload),
				Status:    store.ChargeStationSettingStatusAccepted,
				SendAfter: now,
			},
		},
	}

	if err := h.Store.UpdateChargeStationSettings(ctx, chargeStationId, settings); err != nil {
		slog.Error("failed to store notify event data", "chargeStationId", chargeStationId, "error", err)
		return &ocpp201.NotifyEventResponseJson{}, err
	}

	return &ocpp201.NotifyEventResponseJson{}, nil
}
