// SPDX-License-Identifier: Apache-2.0

package ocpp201

// MonitorEnumType is the type of monitor (threshold, delta, or periodic).
type MonitorEnumType string

const MonitorEnumTypeUpperThreshold MonitorEnumType = "UpperThreshold"
const MonitorEnumTypeLowerThreshold MonitorEnumType = "LowerThreshold"
const MonitorEnumTypeDelta MonitorEnumType = "Delta"
const MonitorEnumTypePeriodic MonitorEnumType = "Periodic"
const MonitorEnumTypePeriodicClockAligned MonitorEnumType = "PeriodicClockAligned"

// SetMonitoringDataType contains one variable monitoring configuration request.
type SetMonitoringDataType struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Id is present when replacing an existing monitor.
	Id *int `json:"id,omitempty" yaml:"id,omitempty" mapstructure:"id,omitempty"`

	// Transaction indicates monitor activation only during transaction.
	Transaction *bool `json:"transaction,omitempty" yaml:"transaction,omitempty" mapstructure:"transaction,omitempty"`

	// Value is threshold/delta value, or periodic interval in seconds.
	Value float64 `json:"value" yaml:"value" mapstructure:"value"`

	// Type identifies monitor type.
	Type MonitorEnumType `json:"type" yaml:"type" mapstructure:"type"`

	// Severity is 0-9 where 0 is highest severity.
	Severity int `json:"severity" yaml:"severity" mapstructure:"severity"`

	// Component identifies the component monitored.
	Component ComponentType `json:"component" yaml:"component" mapstructure:"component"`

	// Variable identifies the variable monitored.
	Variable VariableType `json:"variable" yaml:"variable" mapstructure:"variable"`
}

// SetVariableMonitoringRequestJson requests variable monitoring configuration on a Charging Station.
type SetVariableMonitoringRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// SetMonitoringData contains one or more monitoring configurations.
	SetMonitoringData []SetMonitoringDataType `json:"setMonitoringData" yaml:"setMonitoringData" mapstructure:"setMonitoringData"`
}

func (*SetVariableMonitoringRequestJson) IsRequest() {}
