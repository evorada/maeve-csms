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
)

type NotifyReportHandler struct {
	Store store.ChargeStationSettingsStore
}

func (h NotifyReportHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (response ocpp.Response, err error) {
	req := request.(*ocpp201.NotifyReportRequestJson)

	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.String("notify_report.generated_at", req.GeneratedAt),
		attribute.Int("notify_report.request_id", req.RequestId),
		attribute.Int("notify_report.seq_no", req.SeqNo),
		attribute.Bool("notify_report.tbc", req.Tbc))

	if h.Store != nil {
		payload, marshalErr := json.Marshal(req.ReportData)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal notify report data: %w", marshalErr)
		}

		now := time.Now()
		key := fmt.Sprintf("ocpp201.notify_report.request.%d.seq.%d", req.RequestId, req.SeqNo)
		settings := &store.ChargeStationSettings{
			ChargeStationId: chargeStationId,
			Settings: map[string]*store.ChargeStationSetting{
				key: {
					Value:     string(payload),
					Status:    store.ChargeStationSettingStatusAccepted,
					SendAfter: now,
				},
			},
		}

		if updateErr := h.Store.UpdateChargeStationSettings(ctx, chargeStationId, settings); updateErr != nil {
			return nil, fmt.Errorf("persist notify report data: %w", updateErr)
		}
	}

	return &ocpp201.NotifyReportResponseJson{}, nil
}
