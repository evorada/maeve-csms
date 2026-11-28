// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/stations"
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

// BootNotificationHandler records what a charge station reports when it boots and
// answers it by the state of the station in the registry: an active station (and a
// station without a record) is Accepted, a pending station Pending (it may not charge
// until it is approved, and boots again after the boot interval of its tenant), a
// decommissioned station Rejected.
type BootNotificationHandler struct {
	Clock               clock.PassiveClock
	RuntimeDetailsStore store.ChargeStationRuntimeDetailsStore
	SettingsStore       store.ChargeStationSettingsStore
	StatusStore         store.StatusStore
	// RegistryStore holds the state of the charge station. When it is nil every station
	// is Accepted.
	RegistryStore store.ChargeStationRegistryStore
	// Tx records the boot of a pending station and its station.pending event together;
	// nil records them without a transaction.
	Tx store.Transactor
	// Events receives the station.pending event of a pending station; nil emits none.
	Events events.Emitter
	// HeartbeatInterval is the interval in seconds of a charge station whose tenant has
	// not set one.
	HeartbeatInterval int
	// TenantConfig gives the heartbeat interval of the tenant of a charge station, and the
	// boot interval of a pending one. It may be nil.
	TenantConfig *tenancy.ConfigProvider
	// CommandKicker, when set, is asked to send the queued commands of a station that
	// booted. It runs in the background: the response to the station never waits for it.
	CommandKicker CommandKicker
}

func (b BootNotificationHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	span := trace.SpanFromContext(ctx)

	req := request.(*types.BootNotificationJson)

	span.SetAttributes(
		attribute.String("boot.vendor", req.ChargePointVendor),
		attribute.String("boot.model", req.ChargePointModel))

	if req.ChargePointSerialNumber != nil {
		span.SetAttributes(attribute.String("boot.serial", *req.ChargePointSerialNumber))
	}
	if req.FirmwareVersion != nil {
		span.SetAttributes(attribute.String("boot.firmware", *req.FirmwareVersion))
	}

	now := b.Clock.Now()

	// The identity that the station reports is recorded for the registry. The station is
	// answered the same when it cannot be recorded: a failure is only logged.
	err := b.RuntimeDetailsStore.SetChargeStationRuntimeDetails(ctx, chargeStationId, &store.ChargeStationRuntimeDetails{
		OcppVersion:       "1.6",
		FirmwareVersion:   req.FirmwareVersion,
		Model:             &req.ChargePointModel,
		Vendor:            &req.ChargePointVendor,
		SerialNumber:      req.ChargePointSerialNumber,
		Iccid:             req.Iccid,
		Imsi:              req.Imsi,
		MeterSerialNumber: req.MeterSerialNumber,
		MeterType:         req.MeterType,
		LastBootAt:        &now,
	})
	if err != nil {
		span.RecordError(err)
		slog.Error("recording the boot identity of a charge station", "chargeStationId", chargeStationId, "err", err)
	}

	// Update charge station status
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

	state := store.StationStateActive
	var orgId string
	if b.RegistryStore != nil {
		record, err := stations.Lookup(ctx, b.RegistryStore, chargeStationId)
		if err != nil {
			return nil, err
		}
		state, orgId = record.State, record.OrgId
	}

	response := &types.BootNotificationResponseJson{
		CurrentTime: b.Clock.Now().Format(time.RFC3339),
		Status:      types.BootNotificationResponseJsonStatusAccepted,
	}
	switch state {
	case store.StationStatePending:
		response.Status = types.BootNotificationResponseJsonStatusPending
		response.Interval = int(b.pendingBootInterval(ctx, chargeStationId, orgId).Seconds())
		b.recordPendingBoot(ctx, chargeStationId, req, now)
	case store.StationStateDecommissioned:
		response.Status = types.BootNotificationResponseJsonStatusRejected
	}
	if response.Status != types.BootNotificationResponseJsonStatusPending {
		response.Interval = int(b.TenantConfig.HeartbeatIntervalForStation(ctx, chargeStationId, time.Duration(b.HeartbeatInterval)*time.Second).Seconds())
	}
	span.SetAttributes(
		attribute.String("request.status", string(response.Status)),
		attribute.String("station.state", string(state)))

	// a pending station may be sent commands too (a change of its configuration, for
	// instance): only a rejected one is not
	if b.CommandKicker != nil && response.Status != types.BootNotificationResponseJsonStatusRejected {
		kickCtx := context.WithoutCancel(ctx)
		go func() {
			if err := b.CommandKicker.Kick(kickCtx, chargeStationId); err != nil {
				slog.Warn("sending the queued commands of a booted station", "chargeStationId", chargeStationId, "err", err)
			}
		}()
	}

	return response, nil
}

// pendingBootInterval returns the boot interval of the tenant of a pending station. A
// station is never refused because the settings of its tenant cannot be looked up: the
// failure is logged and the station gets the default interval.
func (b BootNotificationHandler) pendingBootInterval(ctx context.Context, chargeStationId, orgId string) time.Duration {
	settings, err := stations.LoadOnboardingSettings(ctx, b.TenantConfig, orgId)
	if err != nil {
		slog.Warn("looking up the onboarding settings of a pending charge station, using the default boot interval",
			"chargeStationId", chargeStationId, "err", err)
		return tenancy.DefaultOnboardingBootInterval
	}
	return settings.BootInterval
}

// recordPendingBoot records the boot of a pending station and, at its first boot while it
// is pending, emits its station.pending event in the same transaction. The station is
// answered Pending also when this fails: the failure is logged, and the next boot tries
// again to emit the event.
func (b BootNotificationHandler) recordPendingBoot(ctx context.Context, chargeStationId string, req *types.BootNotificationJson, now time.Time) {
	record := func(ctx context.Context) error {
		onboarding, notify, err := b.RegistryStore.RecordOnboardingBoot(ctx, chargeStationId, now)
		if err != nil || !notify || b.Events == nil {
			return err
		}
		return b.Events.Emit(ctx, events.Event{
			Type:         events.TypeStationPending,
			StationKey:   chargeStationId,
			ResourceType: events.ResourceStation,
			ResourceId:   chargeStationId,
			Time:         now,
			Data: events.StationPendingData{
				StationKey:      chargeStationId,
				StationId:       events.StationId(chargeStationId),
				Vendor:          req.ChargePointVendor,
				Model:           req.ChargePointModel,
				SerialNumber:    req.ChargePointSerialNumber,
				FirmwareVersion: req.FirmwareVersion,
				Iccid:           req.Iccid,
				Imsi:            req.Imsi,
				FirstSeenAt:     onboarding.FirstSeenAt.UTC(),
				Transport:       onboarding.Transport,
				Credential:      onboarding.Credential,
			},
		})
	}

	var err error
	if b.Tx != nil {
		err = b.Tx.RunInTx(ctx, record)
	} else {
		err = record(ctx)
	}
	if err != nil {
		trace.SpanFromContext(ctx).RecordError(err)
		slog.Error("recording the boot of a pending charge station", "chargeStationId", chargeStationId, "err", err)
	}
}
