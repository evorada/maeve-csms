// SPDX-License-Identifier: Apache-2.0

package ocpp201

// ReportChargingProfilesRequestJson is sent by a Charge Station to the CSMS to report
// its locally stored charging profiles in response to a GetChargingProfiles request.
// The tbc (To Be Continued) flag indicates whether more messages will follow.
//
// Required fields: RequestId, ChargingLimitSource, EvseId, ChargingProfile.
type ReportChargingProfilesRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// RequestId matches the requestId from the GetChargingProfiles request.
	RequestId int `json:"requestId" yaml:"requestId" mapstructure:"requestId"`

	// ChargingLimitSource identifies the source of the charging profile limit.
	ChargingLimitSource ChargingLimitSourceEnumType `json:"chargingLimitSource" yaml:"chargingLimitSource" mapstructure:"chargingLimitSource"`

	// ChargingProfile is the list of charging profiles stored in the charge station.
	ChargingProfile []ChargingProfileType `json:"chargingProfile" yaml:"chargingProfile" mapstructure:"chargingProfile"`

	// Tbc is the "To Be Continued" flag. When true, more ReportChargingProfiles
	// messages will follow for this requestId. Defaults to false.
	Tbc *bool `json:"tbc,omitempty" yaml:"tbc,omitempty" mapstructure:"tbc,omitempty"`

	// EvseId identifies the EVSE to which the reported charging profiles apply.
	// evseId = 0 means the profiles apply to the whole Charging Station.
	EvseId int `json:"evseId" yaml:"evseId" mapstructure:"evseId"`
}

func (r *ReportChargingProfilesRequestJson) IsRequest() {}
