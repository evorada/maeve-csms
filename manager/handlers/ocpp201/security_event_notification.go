// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/utils/clock"
)

type SecurityEventNotificationHandler struct {
	Clock      clock.PassiveClock
	EventStore store.ChargeStationEventStore
}

func (s SecurityEventNotificationHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (response ocpp.Response, err error) {
	req := request.(*ocpp201.SecurityEventNotificationRequestJson)

	span := trace.SpanFromContext(ctx)

	span.SetAttributes(attribute.String("security_event.timestamp", req.Timestamp), attribute.String("security_event.type", req.Type))
	if req.TechInfo != nil {
		span.SetAttributes(attribute.String("security_event.tech_info", *req.TechInfo))
	}

	timestamp, err := time.Parse(time.RFC3339, req.Timestamp)
	if err != nil {
		timestamp = s.Clock.Now()
		slog.Warn("invalid security event timestamp, using the current time",
			slog.String("chargeStationId", chargeStationId), slog.String("timestamp", req.Timestamp), "err", err)
	}

	err = s.EventStore.AddChargeStationEvent(ctx, chargeStationId, &store.ChargeStationEvent{
		Timestamp: timestamp,
		EventType: req.Type,
		TechInfo:  req.TechInfo,
		Category:  store.ChargeStationEventCategorySecurity,
	})
	if err != nil {
		return nil, fmt.Errorf("storing security event: %w", err)
	}

	return &ocpp201.SecurityEventNotificationResponseJson{}, nil
}
