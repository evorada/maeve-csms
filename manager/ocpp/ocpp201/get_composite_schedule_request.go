// SPDX-License-Identifier: Apache-2.0

package ocpp201

// GetCompositeScheduleRequestJson is the request payload for the GetCompositeSchedule message (CSMS→CS).
// The CS will calculate the composite schedule (merging all active charging profiles) and respond
// with the resulting schedule for the requested EVSE and duration.
type GetCompositeScheduleRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Duration is the length of the requested schedule in seconds.
	Duration int `json:"duration" yaml:"duration" mapstructure:"duration"`

	// EvseId is the ID of the EVSE for which the schedule is requested.
	// When evseId=0, the Charging Station will calculate the expected consumption for the grid connection.
	EvseId int `json:"evseId" yaml:"evseId" mapstructure:"evseId"`

	// ChargingRateUnit optionally forces the result to be expressed in a specific power or current unit.
	// If omitted, the CS uses its default unit.
	ChargingRateUnit *ChargingRateUnitEnumType `json:"chargingRateUnit,omitempty" yaml:"chargingRateUnit,omitempty" mapstructure:"chargingRateUnit,omitempty"`
}

func (*GetCompositeScheduleRequestJson) IsRequest() {}
