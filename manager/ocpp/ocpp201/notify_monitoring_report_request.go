// SPDX-License-Identifier: Apache-2.0

package ocpp201

// VariableMonitoringType represents a monitoring setting for a variable.
type VariableMonitoringType struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Id identifies the monitor.
	Id int `json:"id" yaml:"id" mapstructure:"id"`

	// Transaction indicates monitor activation only during a transaction.
	Transaction bool `json:"transaction" yaml:"transaction" mapstructure:"transaction"`

	// Value is threshold/delta value, or periodic interval in seconds.
	Value float64 `json:"value" yaml:"value" mapstructure:"value"`

	// Type identifies monitor type.
	Type MonitorEnumType `json:"type" yaml:"type" mapstructure:"type"`

	// Severity is 0-9 where 0 is highest severity.
	Severity int `json:"severity" yaml:"severity" mapstructure:"severity"`
}

// MonitoringDataType holds one monitored variable and its monitor definitions.
type MonitoringDataType struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Component identifies the component monitored.
	Component ComponentType `json:"component" yaml:"component" mapstructure:"component"`

	// Variable identifies the variable monitored.
	Variable VariableType `json:"variable" yaml:"variable" mapstructure:"variable"`

	// VariableMonitoring contains monitor definitions for this variable.
	VariableMonitoring []VariableMonitoringType `json:"variableMonitoring" yaml:"variableMonitoring" mapstructure:"variableMonitoring"`
}

// NotifyMonitoringReportRequestJson contains a monitoring report sent from CS to CSMS.
type NotifyMonitoringReportRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Monitor contains one or more monitoring report records.
	Monitor []MonitoringDataType `json:"monitor,omitempty" yaml:"monitor,omitempty" mapstructure:"monitor,omitempty"`

	// RequestId is the id of the originating GetMonitoringReport request.
	RequestId int `json:"requestId" yaml:"requestId" mapstructure:"requestId"`

	// Tbc indicates whether additional report parts will follow.
	Tbc bool `json:"tbc,omitempty" yaml:"tbc,omitempty" mapstructure:"tbc,omitempty"`

	// SeqNo is the sequence number of this report fragment.
	SeqNo int `json:"seqNo" yaml:"seqNo" mapstructure:"seqNo"`

	// GeneratedAt is when the report was generated at the charge station.
	GeneratedAt string `json:"generatedAt" yaml:"generatedAt" mapstructure:"generatedAt"`
}

func (*NotifyMonitoringReportRequestJson) IsRequest() {}
