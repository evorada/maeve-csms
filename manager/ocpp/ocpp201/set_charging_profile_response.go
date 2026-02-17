// SPDX-License-Identifier: Apache-2.0

package ocpp201

// ChargingProfileStatusEnumType indicates whether the CS has been able to process the SetChargingProfile message.
type ChargingProfileStatusEnumType string

const (
	ChargingProfileStatusEnumTypeAccepted ChargingProfileStatusEnumType = "Accepted"
	ChargingProfileStatusEnumTypeRejected ChargingProfileStatusEnumType = "Rejected"
)

// SetChargingProfileResponseJson is the response payload for the SetChargingProfile message.
type SetChargingProfileResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Status corresponds to the JSON schema field "status".
	// Returns whether the Charging Station has been able to process the message successfully.
	Status ChargingProfileStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// StatusInfo corresponds to the JSON schema field "statusInfo".
	StatusInfo *StatusInfoType `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`
}

func (*SetChargingProfileResponseJson) IsResponse() {}
