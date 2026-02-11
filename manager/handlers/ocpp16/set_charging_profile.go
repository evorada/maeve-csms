// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type SetChargingProfileHandler struct {
	ChargingProfileStore store.ChargingProfileStore
}

func (h SetChargingProfileHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.SetChargingProfileJson)
	resp := response.(*types.SetChargingProfileResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("request.connectorId", req.ConnectorId),
		attribute.Int("request.chargingProfileId", req.CsChargingProfiles.ChargingProfileId),
		attribute.String("request.chargingProfilePurpose", string(req.CsChargingProfiles.ChargingProfilePurpose)),
		attribute.String("response.status", string(resp.Status)),
	)

	if resp.Status == types.SetChargingProfileResponseJsonStatusAccepted {
		slog.Info("set charging profile accepted",
			"chargeStationId", chargeStationId,
			"connectorId", req.ConnectorId,
			"chargingProfileId", req.CsChargingProfiles.ChargingProfileId,
			"purpose", req.CsChargingProfiles.ChargingProfilePurpose,
			"stackLevel", req.CsChargingProfiles.StackLevel,
		)

		if h.ChargingProfileStore != nil {
			profile, err := convertToStoreProfile(chargeStationId, req)
			if err != nil {
				return fmt.Errorf("converting charging profile: %w", err)
			}
			if err := h.ChargingProfileStore.SetChargingProfile(ctx, profile); err != nil {
				return fmt.Errorf("storing charging profile: %w", err)
			}
		}
	} else {
		slog.Warn("set charging profile not accepted",
			"chargeStationId", chargeStationId,
			"connectorId", req.ConnectorId,
			"chargingProfileId", req.CsChargingProfiles.ChargingProfileId,
			"status", resp.Status,
		)
	}

	return nil
}

func convertToStoreProfile(chargeStationId string, req *types.SetChargingProfileJson) (*store.ChargingProfile, error) {
	cp := req.CsChargingProfiles

	profile := &store.ChargingProfile{
		ChargeStationId:        chargeStationId,
		ConnectorId:            req.ConnectorId,
		ChargingProfileId:      cp.ChargingProfileId,
		TransactionId:          cp.TransactionId,
		StackLevel:             cp.StackLevel,
		ChargingProfilePurpose: store.ChargingProfilePurpose(cp.ChargingProfilePurpose),
		ChargingProfileKind:    store.ChargingProfileKind(cp.ChargingProfileKind),
		ChargingSchedule: store.ChargingSchedule{
			ChargingRateUnit: store.ChargingRateUnit(cp.ChargingSchedule.ChargingRateUnit),
			Duration:         cp.ChargingSchedule.Duration,
			MinChargingRate:  cp.ChargingSchedule.MinChargingRate,
		},
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

	if cp.ChargingSchedule.StartSchedule != nil {
		t, err := time.Parse(time.RFC3339, *cp.ChargingSchedule.StartSchedule)
		if err != nil {
			return nil, fmt.Errorf("parsing startSchedule: %w", err)
		}
		profile.ChargingSchedule.StartSchedule = &t
	}

	for _, period := range cp.ChargingSchedule.ChargingSchedulePeriod {
		profile.ChargingSchedule.ChargingSchedulePeriod = append(profile.ChargingSchedule.ChargingSchedulePeriod, store.ChargingSchedulePeriod{
			StartPeriod:  period.StartPeriod,
			Limit:        period.Limit,
			NumberPhases: period.NumberPhases,
		})
	}

	return profile, nil
}
