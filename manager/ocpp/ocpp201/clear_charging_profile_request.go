// SPDX-License-Identifier: Apache-2.0

package ocpp201

// ClearChargingProfileType defines filter criteria for selecting charging profiles to clear.
type ClearChargingProfileType struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// EvseId optionally specifies the EVSE for which charging profiles should be cleared.
	// 0 means the Charging Station itself. If omitted, applies to all EVSEs.
	EvseId *int `json:"evseId,omitempty" yaml:"evseId,omitempty" mapstructure:"evseId,omitempty"`

	// ChargingProfilePurpose optionally filters by charging profile purpose.
	ChargingProfilePurpose *ChargingProfilePurposeEnumType `json:"chargingProfilePurpose,omitempty" yaml:"chargingProfilePurpose,omitempty" mapstructure:"chargingProfilePurpose,omitempty"`

	// StackLevel optionally filters by stack level.
	StackLevel *int `json:"stackLevel,omitempty" yaml:"stackLevel,omitempty" mapstructure:"stackLevel,omitempty"`
}

// ClearChargingProfileRequestJson is the request payload for the ClearChargingProfile message (CSMS→CS).
// The CSMS sends this to clear one or more charging profiles from a charge station.
type ClearChargingProfileRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// ChargingProfileId optionally specifies a single charging profile to clear by ID.
	// If omitted, ChargingProfileCriteria are used for matching.
	ChargingProfileId *int `json:"chargingProfileId,omitempty" yaml:"chargingProfileId,omitempty" mapstructure:"chargingProfileId,omitempty"`

	// ChargingProfileCriteria optionally provides filter criteria for selecting profiles to clear.
	// Used when ChargingProfileId is not specified.
	ChargingProfileCriteria *ClearChargingProfileType `json:"chargingProfileCriteria,omitempty" yaml:"chargingProfileCriteria,omitempty" mapstructure:"chargingProfileCriteria,omitempty"`
}

func (*ClearChargingProfileRequestJson) IsRequest() {}
