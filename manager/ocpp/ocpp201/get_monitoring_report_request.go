// SPDX-License-Identifier: Apache-2.0

package ocpp201

// MonitoringCriterionEnumType describes which monitoring configurations should be included in the report.
type MonitoringCriterionEnumType string

const MonitoringCriterionEnumTypeThresholdMonitoring MonitoringCriterionEnumType = "ThresholdMonitoring"
const MonitoringCriterionEnumTypeDeltaMonitoring MonitoringCriterionEnumType = "DeltaMonitoring"
const MonitoringCriterionEnumTypePeriodicMonitoring MonitoringCriterionEnumType = "PeriodicMonitoring"

// GetMonitoringReportRequestJson requests monitoring configurations from a Charging Station.
type GetMonitoringReportRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// ComponentVariable filters the report by specific component/variable pairs.
	ComponentVariable []ComponentVariableType `json:"componentVariable,omitempty" yaml:"componentVariable,omitempty" mapstructure:"componentVariable,omitempty"`

	// RequestId is the identifier of this request.
	RequestId int `json:"requestId" yaml:"requestId" mapstructure:"requestId"`

	// MonitoringCriteria filters the report by criterion class.
	MonitoringCriteria []MonitoringCriterionEnumType `json:"monitoringCriteria,omitempty" yaml:"monitoringCriteria,omitempty" mapstructure:"monitoringCriteria,omitempty"`
}

func (*GetMonitoringReportRequestJson) IsRequest() {}
