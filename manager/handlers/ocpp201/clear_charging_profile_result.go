// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ClearChargingProfileResultHandler handles the response from a ClearChargingProfile request sent to a charge station.
// The CS clears matching charging profiles and responds with Accepted or Unknown.
type ClearChargingProfileResultHandler struct {
	Store store.Engine
}

func (h ClearChargingProfileResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.ClearChargingProfileRequestJson)
	resp := response.(*types.ClearChargingProfileResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("clear_charging_profile.status", string(resp.Status)),
	)

	if req.ChargingProfileId != nil {
		span.SetAttributes(attribute.Int("clear_charging_profile.profile_id", *req.ChargingProfileId))
	}

	switch resp.Status {
	case types.ClearChargingProfileStatusEnumTypeAccepted:
		slog.Info("clear charging profile accepted",
			"chargeStationId", chargeStationId,
		)

		if h.Store != nil {
			// Build filter parameters from the request
			var profileId *int
			var connectorId *int
			var purpose *store.ChargingProfilePurpose
			var stackLevel *int

			if req.ChargingProfileId != nil {
				profileId = req.ChargingProfileId
			}

			if req.ChargingProfileCriteria != nil {
				crit := req.ChargingProfileCriteria
				if crit.EvseId != nil {
					connectorId = crit.EvseId
				}
				if crit.StackLevel != nil {
					stackLevel = crit.StackLevel
				}
				if crit.ChargingProfilePurpose != nil {
					p := mapOcpp201PurposeToStore(*crit.ChargingProfilePurpose)
					purpose = &p
				}
			}

			count, err := h.Store.ClearChargingProfile(ctx, chargeStationId, profileId, connectorId, purpose, stackLevel)
			if err != nil {
				return fmt.Errorf("clearing charging profiles from store: %w", err)
			}

			span.SetAttributes(attribute.Int("clear_charging_profile.cleared_count", count))
			slog.Info("cleared charging profiles from store",
				"chargeStationId", chargeStationId,
				"clearedCount", count,
			)
		}

	case types.ClearChargingProfileStatusEnumTypeUnknown:
		slog.Warn("clear charging profile: no matching profile found on charge station",
			"chargeStationId", chargeStationId,
		)
		if resp.StatusInfo != nil {
			span.SetAttributes(attribute.String("clear_charging_profile.reason_code", resp.StatusInfo.ReasonCode))
			slog.Warn("clear charging profile rejection reason",
				"chargeStationId", chargeStationId,
				"reasonCode", resp.StatusInfo.ReasonCode,
			)
		}

	default:
		slog.Warn("clear charging profile: unexpected status",
			"chargeStationId", chargeStationId,
			"status", resp.Status,
		)
	}

	return nil
}

// mapOcpp201PurposeToStore maps an OCPP 2.0.1 ChargingProfilePurposeEnumType to a store.ChargingProfilePurpose.
func mapOcpp201PurposeToStore(purpose types.ChargingProfilePurposeEnumType) store.ChargingProfilePurpose {
	switch purpose {
	case types.ChargingProfilePurposeEnumTypeTxProfile:
		return store.ChargingProfilePurposeTxProfile
	case types.ChargingProfilePurposeEnumTypeTxDefaultProfile:
		return store.ChargingProfilePurposeTxDefaultProfile
	case types.ChargingProfilePurposeEnumTypeChargingStationMaxProfile,
		types.ChargingProfilePurposeEnumTypeChargingStationExternalConstraints:
		return store.ChargingProfilePurposeChargePointMaxProfile
	default:
		return store.ChargingProfilePurposeTxDefaultProfile
	}
}
