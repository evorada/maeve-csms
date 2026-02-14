// SPDX-License-Identifier: Apache-2.0

package ocpp201

// ClearChargingProfileStatusEnumType indicates whether the CS was able to execute the ClearChargingProfile request.
type ClearChargingProfileStatusEnumType string

const (
	// ClearChargingProfileStatusEnumTypeAccepted means the CS successfully cleared matching profiles.
	ClearChargingProfileStatusEnumTypeAccepted ClearChargingProfileStatusEnumType = "Accepted"
	// ClearChargingProfileStatusEnumTypeUnknown means no matching profile was found on the CS.
	ClearChargingProfileStatusEnumTypeUnknown ClearChargingProfileStatusEnumType = "Unknown"
)

// ClearChargingProfileResponseJson is the response payload for the ClearChargingProfile message.
type ClearChargingProfileResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Status indicates whether the CS was able to execute the request.
	Status ClearChargingProfileStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// StatusInfo optionally provides additional information about the status.
	StatusInfo *StatusInfoType `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`
}

func (*ClearChargingProfileResponseJson) IsResponse() {}
