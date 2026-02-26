// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

type StatusNotificationHandler struct {
	StatusStore store.StatusStore
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
	}

	if err := h.StatusStore.SetConnectorStatus(ctx, chargeStationId, req.ConnectorId, connectorStatus); err != nil {
		return nil, err
	}

	return &types.StatusNotificationResponseJson{}, nil
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
