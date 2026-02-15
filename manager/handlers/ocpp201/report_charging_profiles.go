// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ReportChargingProfilesHandler handles the ReportChargingProfiles message sent from
// a Charge Station to the CSMS. The CS sends this in response to a GetChargingProfiles
// request to report its locally stored charging profiles. Multiple messages may be
// sent when the tbc (To Be Continued) flag is true.
type ReportChargingProfilesHandler struct {
	Store store.Engine
}

func (h ReportChargingProfilesHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	req := request.(*ocpp201.ReportChargingProfilesRequestJson)

	tbc := false
	if req.Tbc != nil {
		tbc = *req.Tbc
	}

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("report_charging_profiles.request_id", req.RequestId),
		attribute.String("report_charging_profiles.charging_limit_source", string(req.ChargingLimitSource)),
		attribute.Int("report_charging_profiles.evse_id", req.EvseId),
		attribute.Int("report_charging_profiles.profile_count", len(req.ChargingProfile)),
		attribute.Bool("report_charging_profiles.tbc", tbc),
	)

	slog.Info("received charging profiles report",
		"chargeStationId", chargeStationId,
		"requestId", req.RequestId,
		"chargingLimitSource", req.ChargingLimitSource,
		"evseId", req.EvseId,
		"profileCount", len(req.ChargingProfile),
		"tbc", tbc,
	)

	if h.Store != nil {
		for _, profile := range req.ChargingProfile {
			storeReq := &ocpp201.SetChargingProfileRequestJson{
				EvseId:          req.EvseId,
				ChargingProfile: profile,
			}
			storeProfile, err := convertOcpp201ToStoreProfile(chargeStationId, storeReq)
			if err != nil {
				return nil, fmt.Errorf("converting reported charging profile %d: %w", profile.Id, err)
			}
			if err := h.Store.SetChargingProfile(ctx, storeProfile); err != nil {
				return nil, fmt.Errorf("storing reported charging profile %d: %w", profile.Id, err)
			}
		}
	}

	return &ocpp201.ReportChargingProfilesResponseJson{}, nil
}
