// SPDX-License-Identifier: Apache-2.0

package ocpp201

import "time"

// EnergyTransferModeEnumType represents the mode of energy transfer requested by the EV.
type EnergyTransferModeEnumType string

const (
	EnergyTransferModeEnumTypeDC           EnergyTransferModeEnumType = "DC"
	EnergyTransferModeEnumTypeACSinglePhase EnergyTransferModeEnumType = "AC_single_phase"
	EnergyTransferModeEnumTypeACTwoPhase   EnergyTransferModeEnumType = "AC_two_phase"
	EnergyTransferModeEnumTypeACThreePhase EnergyTransferModeEnumType = "AC_three_phase"
)

// ACChargingParametersType contains EV AC charging parameters.
type ACChargingParametersType struct {
	// EnergyAmount is the amount of energy requested (in Wh), including energy for preconditioning.
	EnergyAmount int `json:"energyAmount" yaml:"energyAmount" mapstructure:"energyAmount"`
	// EVMinCurrent is the minimum current (amps) supported by the EV per phase.
	EVMinCurrent int `json:"evMinCurrent" yaml:"evMinCurrent" mapstructure:"evMinCurrent"`
	// EVMaxCurrent is the maximum current (amps) supported by the EV per phase (includes cable capacity).
	EVMaxCurrent int `json:"evMaxCurrent" yaml:"evMaxCurrent" mapstructure:"evMaxCurrent"`
	// EVMaxVoltage is the maximum voltage supported by the EV.
	EVMaxVoltage int `json:"evMaxVoltage" yaml:"evMaxVoltage" mapstructure:"evMaxVoltage"`
}

// DCChargingParametersType contains EV DC charging parameters.
type DCChargingParametersType struct {
	// EVMaxCurrent is the maximum current (amps) supported by the EV (includes cable capacity).
	EVMaxCurrent int `json:"evMaxCurrent" yaml:"evMaxCurrent" mapstructure:"evMaxCurrent"`
	// EVMaxVoltage is the maximum voltage supported by the EV.
	EVMaxVoltage int `json:"evMaxVoltage" yaml:"evMaxVoltage" mapstructure:"evMaxVoltage"`
	// EnergyAmount is the amount of energy requested (in Wh). Optional.
	EnergyAmount *int `json:"energyAmount,omitempty" yaml:"energyAmount,omitempty" mapstructure:"energyAmount,omitempty"`
	// EVMaxPower is the maximum power (in W) supported by the EV. Optional.
	EVMaxPower *int `json:"evMaxPower,omitempty" yaml:"evMaxPower,omitempty" mapstructure:"evMaxPower,omitempty"`
	// StateOfCharge is the energy available in the battery (0-100%). Optional.
	StateOfCharge *int `json:"stateOfCharge,omitempty" yaml:"stateOfCharge,omitempty" mapstructure:"stateOfCharge,omitempty"`
	// EVEnergyCapacity is the capacity of the EV battery (in Wh). Optional.
	EVEnergyCapacity *int `json:"evEnergyCapacity,omitempty" yaml:"evEnergyCapacity,omitempty" mapstructure:"evEnergyCapacity,omitempty"`
	// FullSoC is the SoC at which the EV considers the battery fully charged (0-100%). Optional.
	FullSoC *int `json:"fullSoC,omitempty" yaml:"fullSoC,omitempty" mapstructure:"fullSoC,omitempty"`
	// BulkSoC is the SoC at which the EV considers a fast charging process to end (0-100%). Optional.
	BulkSoC *int `json:"bulkSoC,omitempty" yaml:"bulkSoC,omitempty" mapstructure:"bulkSoC,omitempty"`
}

// ChargingNeedsType describes the charging needs reported by the EV.
type ChargingNeedsType struct {
	// RequestedEnergyTransfer is the mode of energy transfer requested by the EV.
	RequestedEnergyTransfer EnergyTransferModeEnumType `json:"requestedEnergyTransfer" yaml:"requestedEnergyTransfer" mapstructure:"requestedEnergyTransfer"`
	// DepartureTime is the estimated departure time of the EV. Optional.
	DepartureTime *time.Time `json:"departureTime,omitempty" yaml:"departureTime,omitempty" mapstructure:"departureTime,omitempty"`
	// ACChargingParameters contains AC charging parameters. Optional (set for AC charging).
	ACChargingParameters *ACChargingParametersType `json:"acChargingParameters,omitempty" yaml:"acChargingParameters,omitempty" mapstructure:"acChargingParameters,omitempty"`
	// DCChargingParameters contains DC charging parameters. Optional (set for DC charging).
	DCChargingParameters *DCChargingParametersType `json:"dcChargingParameters,omitempty" yaml:"dcChargingParameters,omitempty" mapstructure:"dcChargingParameters,omitempty"`
}

// NotifyEVChargingNeedsRequestJson is sent by a Charge Station to the CSMS to notify it
// of the EV's charging needs. This enables the CSMS to generate an appropriate charging
// profile (Smart Charging). The CS sends this after the EV communicates its needs via
// ISO 15118 (AC/DC).
type NotifyEVChargingNeedsRequestJson struct {
	// EVSeId identifies the EVSE and connector to which the EV is connected. Must not be 0.
	EvseId int `json:"evseId" yaml:"evseId" mapstructure:"evseId"`
	// ChargingNeeds contains the charging needs reported by the EV.
	ChargingNeeds ChargingNeedsType `json:"chargingNeeds" yaml:"chargingNeeds" mapstructure:"chargingNeeds"`
	// MaxScheduleTuples is the maximum number of schedule tuples the car supports per schedule. Optional.
	MaxScheduleTuples *int `json:"maxScheduleTuples,omitempty" yaml:"maxScheduleTuples,omitempty" mapstructure:"maxScheduleTuples,omitempty"`
}

func (n *NotifyEVChargingNeedsRequestJson) IsRequest() {}
