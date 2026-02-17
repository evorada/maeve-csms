// SPDX-License-Identifier: Apache-2.0

package ocpp201

// SetMonitoringBaseResponseJson is the CallResult for SetMonitoringBase.
type SetMonitoringBaseResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Status indicates whether the Charging Station accepted the request.
	Status GenericDeviceModelStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// StatusInfo contains additional details for the returned status.
	StatusInfo *StatusInfoType `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`
}

func (*SetMonitoringBaseResponseJson) IsResponse() {}
