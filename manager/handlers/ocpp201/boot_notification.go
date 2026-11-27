// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"
	"log/slog"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"k8s.io/utils/clock"
)

type BootNotificationHandler struct {
	Clock               clock.PassiveClock
	RuntimeDetailsStore store.ChargeStationRuntimeDetailsStore
	// HeartbeatInterval is the interval in seconds of a charge station whose tenant has
	// not set one.
	HeartbeatInterval int
	// TenantConfig gives the heartbeat interval of the tenant of a charge station. It
	// may be nil.
	TenantConfig *tenancy.ConfigProvider
}

func (b BootNotificationHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	span := trace.SpanFromContext(ctx)

	req := request.(*types.BootNotificationRequestJson)

	span.SetAttributes(
		attribute.String("request.status", string(types.RegistrationStatusEnumTypeAccepted)),
		attribute.String("boot.reason", string(req.Reason)),
		attribute.String("boot.vendor", req.ChargingStation.VendorName),
		attribute.String("boot.model", req.ChargingStation.Model))

	if req.ChargingStation.SerialNumber != nil {
		span.SetAttributes(attribute.String("boot.serial", *req.ChargingStation.SerialNumber))
	}
	if req.ChargingStation.FirmwareVersion != nil {
		span.SetAttributes(attribute.String("boot.firmware", *req.ChargingStation.FirmwareVersion))
	}

	// The identity that the station reports is recorded for the registry. The station is
	// answered the same when it cannot be recorded: a failure is only logged.
	now := b.Clock.Now()
	details := &store.ChargeStationRuntimeDetails{
		OcppVersion:     "2.0.1",
		FirmwareVersion: req.ChargingStation.FirmwareVersion,
		Model:           &req.ChargingStation.Model,
		Vendor:          &req.ChargingStation.VendorName,
		SerialNumber:    req.ChargingStation.SerialNumber,
		LastBootAt:      &now,
	}
	if modem := req.ChargingStation.Modem; modem != nil {
		details.Iccid = modem.Iccid
		details.Imsi = modem.Imsi
	}
	err := b.RuntimeDetailsStore.SetChargeStationRuntimeDetails(ctx, chargeStationId, details)
	if err != nil {
		span.RecordError(err)
		slog.Error("recording the boot identity of a charge station", "chargeStationId", chargeStationId, "err", err)
	}

	return &types.BootNotificationResponseJson{
		CurrentTime: b.Clock.Now().Format(time.RFC3339),
		Interval:    int(b.TenantConfig.HeartbeatIntervalForStation(ctx, chargeStationId, time.Duration(b.HeartbeatInterval)*time.Second).Seconds()),
		Status:      types.RegistrationStatusEnumTypeAccepted,
	}, nil
}
