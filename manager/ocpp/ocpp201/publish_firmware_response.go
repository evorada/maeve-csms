// SPDX-License-Identifier: Apache-2.0

package ocpp201

// PublishFirmwareResponseJson is sent by the Local Controller (Charging Station)
// in response to a PublishFirmwareRequest from the CSMS.
type PublishFirmwareResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Status indicates whether the Local Controller was able to accept the request.
	// Uses GenericStatusEnumType: Accepted or Rejected.
	Status GenericStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// StatusInfo provides more information about the status.
	StatusInfo *StatusInfoType `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`
}

func (*PublishFirmwareResponseJson) IsResponse() {}
