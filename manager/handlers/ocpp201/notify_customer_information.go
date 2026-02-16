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

type NotifyCustomerInformationHandler struct {
	Store store.ChargeStationSettingsStore
}

func (h NotifyCustomerInformationHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (response ocpp.Response, err error) {
	req := request.(*ocpp201.NotifyCustomerInformationRequestJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("notify_customer_information.request_id", req.RequestId),
		attribute.Int("notify_customer_information.seq_no", req.SeqNo),
		attribute.Bool("notify_customer_information.tbc", req.Tbc),
		attribute.String("notify_customer_information.generated_at", req.GeneratedAt),
		attribute.Int("notify_customer_information.data_length", len(req.Data)),
	)

	payload, err := json.Marshal(map[string]any{
		"data":        req.Data,
		"generatedAt": req.GeneratedAt,
		"tbc":         req.Tbc,
	})
	if err != nil {
		slog.Error("failed to marshal notify customer information payload", "chargeStationId", chargeStationId, "error", err)
		return &ocpp201.NotifyCustomerInformationResponseJson{}, err
	}

	settingKey := fmt.Sprintf("ocpp201.customer_information.%d.%d", req.RequestId, req.SeqNo)
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
		slog.Error("failed to store notify customer information data", "chargeStationId", chargeStationId, "error", err)
		return &ocpp201.NotifyCustomerInformationResponseJson{}, err
	}

	return &ocpp201.NotifyCustomerInformationResponseJson{}, nil
}
