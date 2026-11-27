// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/utils/clock"
)

type LogStatusNotificationHandler struct {
	Clock         clock.PassiveClock
	FirmwareStore store.FirmwareStore
	EventStore    store.ChargeStationEventStore
}

// logStatusEventData is the event data of a log status event
type logStatusEventData struct {
	Status    string `json:"status"`
	RequestId int    `json:"requestId"`
}

// HandleCall stores the status as the current log status of the station and
// adds it to the station's events, which keep the history.
func (h LogStatusNotificationHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	req := request.(*ocpp16.LogStatusNotificationJson)

	span := trace.SpanFromContext(ctx)

	span.SetAttributes(attribute.String("log_status.status", string(req.Status)))
	if req.RequestId != nil {
		span.SetAttributes(attribute.Int("log_status.request_id", *req.RequestId))
	}

	// a station leaves the request id out when the notification was triggered and no upload is going on
	requestId := 0
	if req.RequestId != nil {
		requestId = *req.RequestId
	}
	now := h.Clock.Now()

	err := h.FirmwareStore.SetLogStatus(ctx, chargeStationId, &store.LogStatus{
		ChargeStationId: chargeStationId,
		Status:          store.LogStatusType(req.Status),
		RequestId:       requestId,
		UpdatedAt:       now,
	})
	if err != nil {
		return nil, fmt.Errorf("storing log status: %w", err)
	}

	eventData, err := json.Marshal(logStatusEventData{Status: string(req.Status), RequestId: requestId})
	if err != nil {
		return nil, fmt.Errorf("encoding log status event: %w", err)
	}
	eventDataJson := string(eventData)

	err = h.EventStore.AddChargeStationEvent(ctx, chargeStationId, &store.ChargeStationEvent{
		Timestamp: now,
		EventType: "LogStatusNotification",
		EventData: &eventDataJson,
		Category:  store.ChargeStationEventCategoryLogStatus,
	})
	if err != nil {
		return nil, fmt.Errorf("storing log status event: %w", err)
	}

	return &ocpp16.LogStatusNotificationResponseJson{}, nil
}
