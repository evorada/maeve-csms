// SPDX-License-Identifier: Apache-2.0

package ocpp201

// ClearMonitoringStatusEnumType indicates the result of clearing a specific monitor ID.
type ClearMonitoringStatusEnumType string

const ClearMonitoringStatusEnumTypeAccepted ClearMonitoringStatusEnumType = "Accepted"
const ClearMonitoringStatusEnumTypeRejected ClearMonitoringStatusEnumType = "Rejected"
const ClearMonitoringStatusEnumTypeNotFound ClearMonitoringStatusEnumType = "NotFound"

// ClearMonitoringResultType contains clear result details for one monitor ID.
type ClearMonitoringResultType struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Status indicates clear result status for this monitor ID.
	Status ClearMonitoringStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// Id is the monitor ID from the request.
	Id int `json:"id" yaml:"id" mapstructure:"id"`

	// StatusInfo contains additional details for the returned status.
	StatusInfo *StatusInfoType `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`
}

// ClearVariableMonitoringResponseJson is the call result for ClearVariableMonitoring.
type ClearVariableMonitoringResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// ClearMonitoringResult returns status for each requested monitor ID.
	ClearMonitoringResult []ClearMonitoringResultType `json:"clearMonitoringResult" yaml:"clearMonitoringResult" mapstructure:"clearMonitoringResult"`
}

func (*ClearVariableMonitoringResponseJson) IsResponse() {}
