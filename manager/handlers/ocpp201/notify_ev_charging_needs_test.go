// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
)

func TestNotifyEVChargingNeedsACCharging(t *testing.T) {
	handler := ocpp201.NotifyEVChargingNeedsHandler{}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.NotifyEVChargingNeedsRequestJson{
			EvseId: 1,
			ChargingNeeds: types.ChargingNeedsType{
				RequestedEnergyTransfer: types.EnergyTransferModeEnumTypeACThreePhase,
				ACChargingParameters: &types.ACChargingParametersType{
					EnergyAmount: 50000,
					EVMinCurrent: 6,
					EVMaxCurrent: 32,
					EVMaxVoltage: 400,
				},
			},
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		got := resp.(*types.NotifyEVChargingNeedsResponseJson)
		assert.Equal(t, types.NotifyEVChargingNeedsStatusEnumTypeAccepted, got.Status)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"notify_ev_charging_needs.evse_id":                     1,
		"notify_ev_charging_needs.requested_energy_transfer":   "AC_three_phase",
		"notify_ev_charging_needs.ac.energy_amount":            50000,
		"notify_ev_charging_needs.ac.ev_min_current":           6,
		"notify_ev_charging_needs.ac.ev_max_current":           32,
		"notify_ev_charging_needs.ac.ev_max_voltage":           400,
	})
}

func TestNotifyEVChargingNeedsDCCharging(t *testing.T) {
	handler := ocpp201.NotifyEVChargingNeedsHandler{}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		evMaxPower := 150000
		stateOfCharge := 20
		energyAmount := 60000

		req := &types.NotifyEVChargingNeedsRequestJson{
			EvseId: 1,
			ChargingNeeds: types.ChargingNeedsType{
				RequestedEnergyTransfer: types.EnergyTransferModeEnumTypeDC,
				DCChargingParameters: &types.DCChargingParametersType{
					EVMaxCurrent:  400,
					EVMaxVoltage:  500,
					EVMaxPower:    &evMaxPower,
					StateOfCharge: &stateOfCharge,
					EnergyAmount:  &energyAmount,
				},
			},
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		got := resp.(*types.NotifyEVChargingNeedsResponseJson)
		assert.Equal(t, types.NotifyEVChargingNeedsStatusEnumTypeAccepted, got.Status)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"notify_ev_charging_needs.evse_id":                   1,
		"notify_ev_charging_needs.requested_energy_transfer": "DC",
		"notify_ev_charging_needs.dc.ev_max_current":         400,
		"notify_ev_charging_needs.dc.ev_max_voltage":         500,
		"notify_ev_charging_needs.dc.ev_max_power":           150000,
		"notify_ev_charging_needs.dc.state_of_charge":        20,
		"notify_ev_charging_needs.dc.energy_amount":          60000,
	})
}

func TestNotifyEVChargingNeedsWithDepartureTimeAndMaxTuples(t *testing.T) {
	handler := ocpp201.NotifyEVChargingNeedsHandler{}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	departure := time.Date(2026, 2, 15, 8, 0, 0, 0, time.UTC)

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		maxTuples := 10

		req := &types.NotifyEVChargingNeedsRequestJson{
			EvseId: 2,
			ChargingNeeds: types.ChargingNeedsType{
				RequestedEnergyTransfer: types.EnergyTransferModeEnumTypeACSinglePhase,
				DepartureTime:           &departure,
			},
			MaxScheduleTuples: &maxTuples,
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		got := resp.(*types.NotifyEVChargingNeedsResponseJson)
		assert.Equal(t, types.NotifyEVChargingNeedsStatusEnumTypeAccepted, got.Status)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"notify_ev_charging_needs.evse_id":                   2,
		"notify_ev_charging_needs.requested_energy_transfer": "AC_single_phase",
		"notify_ev_charging_needs.departure_time":            departure.String(),
		"notify_ev_charging_needs.max_schedule_tuples":       10,
	})
}

func TestNotifyEVChargingNeedsMinimalRequest(t *testing.T) {
	handler := ocpp201.NotifyEVChargingNeedsHandler{}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.NotifyEVChargingNeedsRequestJson{
			EvseId: 1,
			ChargingNeeds: types.ChargingNeedsType{
				RequestedEnergyTransfer: types.EnergyTransferModeEnumTypeACTwoPhase,
			},
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		got := resp.(*types.NotifyEVChargingNeedsResponseJson)
		assert.Equal(t, types.NotifyEVChargingNeedsStatusEnumTypeAccepted, got.Status)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"notify_ev_charging_needs.evse_id":                   1,
		"notify_ev_charging_needs.requested_energy_transfer": "AC_two_phase",
	})
}

func TestNotifyEVChargingNeedsHandlerInterface(t *testing.T) {
	handler := ocpp201.NotifyEVChargingNeedsHandler{}
	req := &types.NotifyEVChargingNeedsRequestJson{
		EvseId: 1,
		ChargingNeeds: types.ChargingNeedsType{
			RequestedEnergyTransfer: types.EnergyTransferModeEnumTypeDC,
		},
	}

	resp, err := handler.HandleCall(context.Background(), "cs001", req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
}
