// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// NotifyEVChargingNeedsHandler handles the NotifyEVChargingNeeds message sent from a
// Charge Station to the CSMS. The CS sends this to notify the CSMS of the EV's charging
// needs (energy transfer mode, AC/DC parameters, departure time), enabling the CSMS to
// create an appropriate Smart Charging profile via ISO 15118.
type NotifyEVChargingNeedsHandler struct{}

func (h NotifyEVChargingNeedsHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	req := request.(*ocpp201.NotifyEVChargingNeedsRequestJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("notify_ev_charging_needs.evse_id", req.EvseId),
		attribute.String("notify_ev_charging_needs.requested_energy_transfer", string(req.ChargingNeeds.RequestedEnergyTransfer)),
	)

	if req.ChargingNeeds.DepartureTime != nil {
		span.SetAttributes(attribute.String("notify_ev_charging_needs.departure_time", req.ChargingNeeds.DepartureTime.String()))
	}

	if req.MaxScheduleTuples != nil {
		span.SetAttributes(attribute.Int("notify_ev_charging_needs.max_schedule_tuples", *req.MaxScheduleTuples))
	}

	if ac := req.ChargingNeeds.ACChargingParameters; ac != nil {
		span.SetAttributes(
			attribute.Int("notify_ev_charging_needs.ac.energy_amount", ac.EnergyAmount),
			attribute.Int("notify_ev_charging_needs.ac.ev_min_current", ac.EVMinCurrent),
			attribute.Int("notify_ev_charging_needs.ac.ev_max_current", ac.EVMaxCurrent),
			attribute.Int("notify_ev_charging_needs.ac.ev_max_voltage", ac.EVMaxVoltage),
		)
	}

	if dc := req.ChargingNeeds.DCChargingParameters; dc != nil {
		span.SetAttributes(
			attribute.Int("notify_ev_charging_needs.dc.ev_max_current", dc.EVMaxCurrent),
			attribute.Int("notify_ev_charging_needs.dc.ev_max_voltage", dc.EVMaxVoltage),
		)
		if dc.EVMaxPower != nil {
			span.SetAttributes(attribute.Int("notify_ev_charging_needs.dc.ev_max_power", *dc.EVMaxPower))
		}
		if dc.StateOfCharge != nil {
			span.SetAttributes(attribute.Int("notify_ev_charging_needs.dc.state_of_charge", *dc.StateOfCharge))
		}
		if dc.EnergyAmount != nil {
			span.SetAttributes(attribute.Int("notify_ev_charging_needs.dc.energy_amount", *dc.EnergyAmount))
		}
	}

	return &ocpp201.NotifyEVChargingNeedsResponseJson{
		Status: ocpp201.NotifyEVChargingNeedsStatusEnumTypeAccepted,
	}, nil
}
