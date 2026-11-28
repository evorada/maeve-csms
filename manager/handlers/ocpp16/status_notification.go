// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"

	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// StatusNotificationHandler stores the status of a connector and, when its status or
// error code changed, emits connector.status_changed in the same transaction. Once it is
// stored, the status is given to the CommandTracker (when there is one), which marks the
// changes of availability that it fulfils; nothing the tracker does changes the answer.
type StatusNotificationHandler struct {
	Clock          clock.PassiveClock
	StatusStore    store.StatusStore
	Tx             store.Transactor
	Events         events.Emitter
	CommandTracker CommandTracker // nil: commands are not followed
}

func (h StatusNotificationHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	span := trace.SpanFromContext(ctx)

	req := request.(*types.StatusNotificationJson)

	span.SetAttributes(
		attribute.Int("status.connector_id", req.ConnectorId),
		attribute.String("status.connector_status", string(req.Status)))

	// Map OCPP status to store status
	storeStatus := mapStatusNotificationStatus(req.Status)
	errorCode := mapStatusNotificationErrorCode(req.ErrorCode)

	receivedAt := h.Clock.Now()

	// Parse timestamp if provided
	var timestamp *time.Time
	if req.Timestamp != nil {
		if t, err := time.Parse(time.RFC3339, *req.Timestamp); err == nil {
			timestamp = &t
		}
	}

	// Store connector status
	connectorStatus := &store.ConnectorStatus{
		ChargeStationId: chargeStationId,
		ConnectorId:     req.ConnectorId,
		Status:          storeStatus,
		ErrorCode:       errorCode,
		Info:            req.Info,
		Timestamp:       timestamp,
		VendorErrorCode: req.VendorErrorCode,
		VendorId:        req.VendorId,
		UpdatedAt:       receivedAt,
	}

	err := h.Tx.RunInTx(ctx, func(ctx context.Context) error {
		previous := h.previousStatus(ctx, chargeStationId, req.ConnectorId)
		if err := h.StatusStore.SetConnectorStatus(ctx, chargeStationId, req.ConnectorId, connectorStatus); err != nil {
			return err
		}
		if previous != nil && previous.Status == connectorStatus.Status && previous.ErrorCode == connectorStatus.ErrorCode {
			return nil
		}
		return h.Events.Emit(ctx, connectorStatusChangedEvent(chargeStationId, connectorStatus, previous, receivedAt))
	})
	if err != nil {
		return nil, err
	}

	trackCommand(ctx, h.CommandTracker, chargeStationId, "StatusNotification", func(tracker CommandTracker) {
		tracker.OnConnectorStatus(ctx, chargeStationId, req.ConnectorId, string(req.Status))
	})

	return &types.StatusNotificationResponseJson{}, nil
}

// previousStatus returns the stored status of a connector, or nil when it has none. It
// is only read for the event: when it cannot be read, the status is stored all the same
// and the event has no previous status.
func (h StatusNotificationHandler) previousStatus(ctx context.Context, chargeStationId string, connectorId int) *store.ConnectorStatus {
	statuses, err := h.StatusStore.ListConnectorStatuses(ctx, chargeStationId)
	if err != nil {
		slog.Warn("cannot read the previous status of the connector", slog.String("chargeStationId", chargeStationId),
			slog.Int("connectorId", connectorId), "err", err)
		trace.SpanFromContext(ctx).RecordError(err)
		return nil
	}
	for _, status := range statuses {
		if status != nil && status.ConnectorId == connectorId {
			return status
		}
	}
	return nil
}

// connectorStatusChangedEvent is the connector.status_changed event of a stored status.
// Its timestamp is the time of the station, else the time the message was received.
func connectorStatusChangedEvent(chargeStationId string, status, previous *store.ConnectorStatus, receivedAt time.Time) events.Event {
	timestamp := receivedAt
	if status.Timestamp != nil {
		timestamp = *status.Timestamp
	}
	data := events.ConnectorStatusChangedData{
		StationKey: chargeStationId,
		StationId:  events.StationId(chargeStationId),
		// OCPP 1.6 has no EVSEs: the connector id is used for both
		EvseId:          status.ConnectorId,
		ConnectorId:     status.ConnectorId,
		Status:          string(status.Status),
		ErrorCode:       string(status.ErrorCode),
		Info:            status.Info,
		VendorId:        status.VendorId,
		VendorErrorCode: status.VendorErrorCode,
		Timestamp:       timestamp.UTC(),
	}
	if previous != nil {
		data.PreviousStatus = string(previous.Status)
	}
	return events.Event{
		Type:         events.TypeConnectorStatusChanged,
		StationKey:   chargeStationId,
		ResourceType: events.ResourceConnector,
		ResourceId:   events.ConnectorResourceId(chargeStationId, status.ConnectorId),
		Actor:        events.StationActor(chargeStationId),
		Data:         data,
	}
}

func mapStatusNotificationStatus(status types.StatusNotificationJsonStatus) store.ConnectorStatusType {
	switch status {
	case types.StatusNotificationJsonStatusAvailable:
		return store.ConnectorStatusAvailable
	case types.StatusNotificationJsonStatusCharging:
		return store.ConnectorStatusCharging
	case types.StatusNotificationJsonStatusFaulted:
		return store.ConnectorStatusFaulted
	case types.StatusNotificationJsonStatusFinishing:
		return store.ConnectorStatusFinishing
	case types.StatusNotificationJsonStatusPreparing:
		return store.ConnectorStatusPreparing
	case types.StatusNotificationJsonStatusReserved:
		return store.ConnectorStatusReserved
	case types.StatusNotificationJsonStatusSuspendedEV:
		return store.ConnectorStatusSuspendedEV
	case types.StatusNotificationJsonStatusSuspendedEVSE:
		return store.ConnectorStatusSuspendedEVSE
	case types.StatusNotificationJsonStatusUnavailable:
		return store.ConnectorStatusUnavailable
	default:
		return store.ConnectorStatusUnavailable
	}
}

func mapStatusNotificationErrorCode(errorCode types.StatusNotificationJsonErrorCode) store.ConnectorErrorCode {
	return store.ConnectorErrorCode(string(errorCode))
}
