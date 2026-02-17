// SPDX-License-Identifier: Apache-2.0

package ocpp201

// UpdateFirmwareResponseJson is sent by the Charging Station in response to an
// UpdateFirmwareRequest from the CSMS.
type UpdateFirmwareResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Status indicates whether the Charging Station was able to accept the request.
	Status UpdateFirmwareStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// StatusInfo provides more information about the status.
	StatusInfo *StatusInfoType `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`
}

func (*UpdateFirmwareResponseJson) IsResponse() {}
