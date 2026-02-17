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

type NotifyMonitoringReportHandler struct {
	Store store.ChargeStationSettingsStore
}

func (h NotifyMonitoringReportHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (response ocpp.Response, err error) {
	req := request.(*ocpp201.NotifyMonitoringReportRequestJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("notify_monitoring_report.request_id", req.RequestId),
		attribute.Int("notify_monitoring_report.seq_no", req.SeqNo),
		attribute.Bool("notify_monitoring_report.tbc", req.Tbc),
		attribute.String("notify_monitoring_report.generated_at", req.GeneratedAt),
		attribute.Int("notify_monitoring_report.monitor_count", len(req.Monitor)),
	)

	payload, err := json.Marshal(req.Monitor)
	if err != nil {
		slog.Error("failed to marshal monitoring report payload", "chargeStationId", chargeStationId, "error", err)
		return &ocpp201.NotifyMonitoringReportResponseJson{}, err
	}

	settingKey := fmt.Sprintf("ocpp201.monitoring_report.%d.%d", req.RequestId, req.SeqNo)
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
		slog.Error("failed to store monitoring report data", "chargeStationId", chargeStationId, "error", err)
		return &ocpp201.NotifyMonitoringReportResponseJson{}, err
	}

	return &ocpp201.NotifyMonitoringReportResponseJson{}, nil
}
