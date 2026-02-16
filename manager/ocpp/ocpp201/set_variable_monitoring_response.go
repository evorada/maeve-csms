// SPDX-License-Identifier: Apache-2.0

package ocpp201

// SetMonitoringStatusEnumType indicates if a monitoring entry was accepted.
type SetMonitoringStatusEnumType string

const SetMonitoringStatusEnumTypeAccepted SetMonitoringStatusEnumType = "Accepted"
const SetMonitoringStatusEnumTypeUnknownComponent SetMonitoringStatusEnumType = "UnknownComponent"
const SetMonitoringStatusEnumTypeUnknownVariable SetMonitoringStatusEnumType = "UnknownVariable"
const SetMonitoringStatusEnumTypeUnsupportedMonitorType SetMonitoringStatusEnumType = "UnsupportedMonitorType"
const SetMonitoringStatusEnumTypeRejected SetMonitoringStatusEnumType = "Rejected"
const SetMonitoringStatusEnumTypeDuplicate SetMonitoringStatusEnumType = "Duplicate"

// SetMonitoringResultType contains result details for a configured monitor.
type SetMonitoringResultType struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Id returned by the Charging Station when status is accepted.
	Id *int `json:"id,omitempty" yaml:"id,omitempty" mapstructure:"id,omitempty"`

	// StatusInfo contains additional status details.
	StatusInfo *StatusInfoType `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`

	// Status indicates result of monitor creation/update.
	Status SetMonitoringStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// Type identifies monitor type.
	Type MonitorEnumType `json:"type" yaml:"type" mapstructure:"type"`

	// Component identifies the component monitored.
	Component ComponentType `json:"component" yaml:"component" mapstructure:"component"`

	// Variable identifies the variable monitored.
	Variable VariableType `json:"variable" yaml:"variable" mapstructure:"variable"`

	// Severity is 0-9 where 0 is highest severity.
	Severity int `json:"severity" yaml:"severity" mapstructure:"severity"`
}

// SetVariableMonitoringResponseJson is the call result for SetVariableMonitoring.
type SetVariableMonitoringResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// SetMonitoringResult returns status for each requested monitor item.
	SetMonitoringResult []SetMonitoringResultType `json:"setMonitoringResult" yaml:"setMonitoringResult" mapstructure:"setMonitoringResult"`
}

func (*SetVariableMonitoringResponseJson) IsResponse() {}
