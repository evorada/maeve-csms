// SPDX-License-Identifier: Apache-2.0

package ocpp201

// SetChargingProfileRequestJson is the request payload for the SetChargingProfile message (CSMS→CS).
type SetChargingProfileRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// EvseId corresponds to the JSON schema field "evseId".
	// For TxDefaultProfile an evseId=0 applies the profile to each individual EVSE.
	// For ChargingStationMaxProfile and ChargingStationExternalConstraints an evseId=0 contains an overall limit for the whole Charging Station.
	EvseId int `json:"evseId" yaml:"evseId" mapstructure:"evseId"`

	// ChargingProfile corresponds to the JSON schema field "chargingProfile".
	ChargingProfile ChargingProfileType `json:"chargingProfile" yaml:"chargingProfile" mapstructure:"chargingProfile"`
}

func (*SetChargingProfileRequestJson) IsRequest() {}
