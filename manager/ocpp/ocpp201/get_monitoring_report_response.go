// SPDX-License-Identifier: Apache-2.0

package ocpp201

// GetMonitoringReportResponseJson is the CallResult for GetMonitoringReport.
type GetMonitoringReportResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Status indicates whether the request was accepted by the Charging Station.
	Status GenericDeviceModelStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// StatusInfo contains additional details for the returned status.
	StatusInfo *StatusInfoType `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`
}

func (*GetMonitoringReportResponseJson) IsResponse() {}
