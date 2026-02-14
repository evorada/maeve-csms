// SPDX-License-Identifier: Apache-2.0

package ocpp201

// CompositeScheduleType contains the composite charging schedule calculated by the Charging Station,
// merging all active charging profiles for the requested EVSE.
type CompositeScheduleType struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// EvseId is the ID of the EVSE for which the schedule was calculated.
	// When evseId=0, the schedule represents the grid connection consumption.
	EvseId int `json:"evseId" yaml:"evseId" mapstructure:"evseId"`

	// Duration is the duration of the schedule in seconds.
	Duration int `json:"duration" yaml:"duration" mapstructure:"duration"`

	// ScheduleStart is the date and time at which the schedule becomes active.
	// All time measurements within the schedule are relative to this timestamp.
	ScheduleStart string `json:"scheduleStart" yaml:"scheduleStart" mapstructure:"scheduleStart"`

	// ChargingRateUnit is the unit of measure in which Limit is expressed (W or A).
	ChargingRateUnit ChargingRateUnitEnumType `json:"chargingRateUnit" yaml:"chargingRateUnit" mapstructure:"chargingRateUnit"`

	// ChargingSchedulePeriod is the list of time periods defining the composite schedule.
	ChargingSchedulePeriod []ChargingSchedulePeriodType `json:"chargingSchedulePeriod" yaml:"chargingSchedulePeriod" mapstructure:"chargingSchedulePeriod"`
}

// GetCompositeScheduleResponseJson is the response payload for the GetCompositeSchedule message.
type GetCompositeScheduleResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Status indicates whether the Charging Station was able to process the request.
	Status GenericStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// StatusInfo optionally provides additional information about the status.
	StatusInfo *StatusInfoType `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`

	// Schedule contains the composite schedule when Status is Accepted.
	Schedule *CompositeScheduleType `json:"schedule,omitempty" yaml:"schedule,omitempty" mapstructure:"schedule,omitempty"`
}

func (*GetCompositeScheduleResponseJson) IsResponse() {}
