// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SetChargingProfileResultHandler handles the response from a SetChargingProfile request sent to a charge station.
type SetChargingProfileResultHandler struct {
	Store store.Engine
}

func (h SetChargingProfileResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.SetChargingProfileRequestJson)
	resp := response.(*types.SetChargingProfileResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("set_charging_profile.evse_id", req.EvseId),
		attribute.Int("set_charging_profile.profile_id", req.ChargingProfile.Id),
		attribute.String("set_charging_profile.purpose", string(req.ChargingProfile.ChargingProfilePurpose)),
		attribute.String("set_charging_profile.kind", string(req.ChargingProfile.ChargingProfileKind)),
		attribute.String("set_charging_profile.status", string(resp.Status)),
	)

	if resp.Status == types.ChargingProfileStatusEnumTypeAccepted {
		slog.Info("set charging profile accepted",
			"chargeStationId", chargeStationId,
			"evseId", req.EvseId,
			"chargingProfileId", req.ChargingProfile.Id,
			"purpose", req.ChargingProfile.ChargingProfilePurpose,
			"stackLevel", req.ChargingProfile.StackLevel,
		)

		if h.Store != nil {
			profile, err := convertOcpp201ToStoreProfile(chargeStationId, req)
			if err != nil {
				return fmt.Errorf("converting charging profile: %w", err)
			}
			if err := h.Store.SetChargingProfile(ctx, profile); err != nil {
				return fmt.Errorf("storing charging profile: %w", err)
			}
		}
	} else {
		slog.Warn("set charging profile rejected",
			"chargeStationId", chargeStationId,
			"evseId", req.EvseId,
			"chargingProfileId", req.ChargingProfile.Id,
			"status", resp.Status,
		)
	}

	return nil
}

// convertOcpp201ToStoreProfile converts an OCPP 2.0.1 SetChargingProfile request to the store representation.
// In OCPP 2.0.1, a profile may contain multiple schedules; we persist the first one (highest priority).
func convertOcpp201ToStoreProfile(chargeStationId string, req *types.SetChargingProfileRequestJson) (*store.ChargingProfile, error) {
	cp := req.ChargingProfile

	// Map OCPP 2.0.1 purpose to store purpose
	var purpose store.ChargingProfilePurpose
	switch cp.ChargingProfilePurpose {
	case types.ChargingProfilePurposeEnumTypeTxProfile:
		purpose = store.ChargingProfilePurposeTxProfile
	case types.ChargingProfilePurposeEnumTypeTxDefaultProfile:
		purpose = store.ChargingProfilePurposeTxDefaultProfile
	case types.ChargingProfilePurposeEnumTypeChargingStationMaxProfile,
		types.ChargingProfilePurposeEnumTypeChargingStationExternalConstraints:
		// Map station-level profiles to ChargePointMaxProfile (OCPP 1.6 equivalent)
		purpose = store.ChargingProfilePurposeChargePointMaxProfile
	default:
		purpose = store.ChargingProfilePurposeTxDefaultProfile
	}

	profile := &store.ChargingProfile{
		ChargeStationId:        chargeStationId,
		ConnectorId:            req.EvseId,
		ChargingProfileId:      cp.Id,
		StackLevel:             cp.StackLevel,
		ChargingProfilePurpose: purpose,
		ChargingProfileKind:    store.ChargingProfileKind(cp.ChargingProfileKind),
	}

	if cp.RecurrencyKind != nil {
		rk := store.RecurrencyKind(*cp.RecurrencyKind)
		profile.RecurrencyKind = &rk
	}

	if cp.ValidFrom != nil {
		t, err := time.Parse(time.RFC3339, *cp.ValidFrom)
		if err != nil {
			return nil, fmt.Errorf("parsing validFrom: %w", err)
		}
		profile.ValidFrom = &t
	}

	if cp.ValidTo != nil {
		t, err := time.Parse(time.RFC3339, *cp.ValidTo)
		if err != nil {
			return nil, fmt.Errorf("parsing validTo: %w", err)
		}
		profile.ValidTo = &t
	}

	// Use the first charging schedule (OCPP 2.0.1 can have multiple, we store the first)
	if len(cp.ChargingSchedule) > 0 {
		sched := cp.ChargingSchedule[0]

		profile.ChargingSchedule = store.ChargingSchedule{
			ChargingRateUnit: store.ChargingRateUnit(sched.ChargingRateUnit),
			Duration:         sched.Duration,
			MinChargingRate:  sched.MinChargingRate,
		}

		if sched.StartSchedule != nil {
			t, err := time.Parse(time.RFC3339, *sched.StartSchedule)
			if err != nil {
				return nil, fmt.Errorf("parsing startSchedule: %w", err)
			}
			profile.ChargingSchedule.StartSchedule = &t
		}

		for _, period := range sched.ChargingSchedulePeriod {
			profile.ChargingSchedule.ChargingSchedulePeriod = append(
				profile.ChargingSchedule.ChargingSchedulePeriod,
				store.ChargingSchedulePeriod{
					StartPeriod:  period.StartPeriod,
					Limit:        period.Limit,
					NumberPhases: period.NumberPhases,
				},
			)
		}
	}

	return profile, nil
}
