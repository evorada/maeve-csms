// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"k8s.io/utils/clock"
)

// CommandKicker sends the next queued command of a charge station, if it has one and
// none is waiting for its answer.
type CommandKicker interface {
	Kick(ctx context.Context, stationKey string) error
}

type BootNotificationHandler struct {
	Clock               clock.PassiveClock
	RuntimeDetailsStore store.ChargeStationRuntimeDetailsStore
	SettingsStore       store.ChargeStationSettingsStore
	StatusStore         store.StatusStore
	// HeartbeatInterval is the interval in seconds of a charge station whose tenant has
	// not set one.
	HeartbeatInterval int
	// TenantConfig gives the heartbeat interval of the tenant of a charge station. It
	// may be nil.
	TenantConfig *tenancy.ConfigProvider
	// CommandKicker, when set, is asked to send the queued commands of a station that
	// booted. It runs in the background: the response to the station never waits for it.
	CommandKicker CommandKicker
}

func (b BootNotificationHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	span := trace.SpanFromContext(ctx)

	req := request.(*types.BootNotificationJson)

	span.SetAttributes(
		attribute.String("request.status", string(types.BootNotificationResponseJsonStatusAccepted)),
		attribute.String("boot.vendor", req.ChargePointVendor),
		attribute.String("boot.model", req.ChargePointModel))

	if req.ChargePointSerialNumber != nil {
		span.SetAttributes(attribute.String("boot.serial", *req.ChargePointSerialNumber))
	}
	if req.FirmwareVersion != nil {
		span.SetAttributes(attribute.String("boot.firmware", *req.FirmwareVersion))
	}

	err := b.RuntimeDetailsStore.SetChargeStationRuntimeDetails(ctx, chargeStationId, &store.ChargeStationRuntimeDetails{
		OcppVersion:     "1.6",
		FirmwareVersion: req.FirmwareVersion,
		Model:           &req.ChargePointModel,
		Vendor:          &req.ChargePointVendor,
		SerialNumber:    req.ChargePointSerialNumber,
	})
	if err != nil {
		return nil, err
	}

	// Update charge station status
	now := b.Clock.Now()
	err = b.StatusStore.SetChargeStationStatus(ctx, chargeStationId, &store.ChargeStationStatus{
		ChargeStationId: chargeStationId,
		Connected:       true,
		LastHeartbeat:   &now,
		FirmwareVersion: req.FirmwareVersion,
		Model:           &req.ChargePointModel,
		Vendor:          &req.ChargePointVendor,
		SerialNumber:    req.ChargePointSerialNumber,
		UpdatedAt:       now,
	})
	if err != nil {
		return nil, err
	}

	// remove any reboot required settings
	settings, err := b.SettingsStore.LookupChargeStationSettings(ctx, chargeStationId)
	if err != nil {
		return nil, err
	}

	if settings != nil && settings.Settings != nil {
		updated := false
		for _, setting := range settings.Settings {
			if setting.Status == store.ChargeStationSettingStatusRebootRequired {
				setting.Status = store.ChargeStationSettingStatusAccepted
				updated = true
			}
		}

		if updated {
			err = b.SettingsStore.UpdateChargeStationSettings(ctx, chargeStationId, settings)
			if err != nil {
				return nil, err
			}
		}
	}

	response := &types.BootNotificationResponseJson{
		CurrentTime: b.Clock.Now().Format(time.RFC3339),
		Interval:    int(b.TenantConfig.HeartbeatIntervalForStation(ctx, chargeStationId, time.Duration(b.HeartbeatInterval)*time.Second).Seconds()),
		Status:      types.BootNotificationResponseJsonStatusAccepted,
	}

	if b.CommandKicker != nil && response.Status == types.BootNotificationResponseJsonStatusAccepted {
		kickCtx := context.WithoutCancel(ctx)
		go func() {
			if err := b.CommandKicker.Kick(kickCtx, chargeStationId); err != nil {
				slog.Warn("sending the queued commands of a booted station", "chargeStationId", chargeStationId, "err", err)
			}
		}()
	}

	return response, nil
}
