// SPDX-License-Identifier: Apache-2.0

package ocpp201

// ChargingLimitSourceEnumType indicates the source of a charging limit.
type ChargingLimitSourceEnumType string

const (
	ChargingLimitSourceEnumTypeEMS   ChargingLimitSourceEnumType = "EMS"
	ChargingLimitSourceEnumTypeOther ChargingLimitSourceEnumType = "Other"
	ChargingLimitSourceEnumTypeSO    ChargingLimitSourceEnumType = "SO"
	ChargingLimitSourceEnumTypeCSO   ChargingLimitSourceEnumType = "CSO"
)

// ChargingProfileCriterionType defines the criteria for filtering charging profiles in a GetChargingProfiles request.
type ChargingProfileCriterionType struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// ChargingProfilePurpose optionally filters by charging profile purpose.
	ChargingProfilePurpose *ChargingProfilePurposeEnumType `json:"chargingProfilePurpose,omitempty" yaml:"chargingProfilePurpose,omitempty" mapstructure:"chargingProfilePurpose,omitempty"`

	// StackLevel optionally filters by stack level.
	StackLevel *int `json:"stackLevel,omitempty" yaml:"stackLevel,omitempty" mapstructure:"stackLevel,omitempty"`

	// ChargingProfileId is a list of charging profile IDs to filter on.
	// If omitted, no filter on chargingProfileId is applied.
	ChargingProfileId []int `json:"chargingProfileId,omitempty" yaml:"chargingProfileId,omitempty" mapstructure:"chargingProfileId,omitempty"`

	// ChargingLimitSource filters by the source(s) of charging limits.
	// If omitted, no filter on chargingLimitSource is applied.
	ChargingLimitSource []ChargingLimitSourceEnumType `json:"chargingLimitSource,omitempty" yaml:"chargingLimitSource,omitempty" mapstructure:"chargingLimitSource,omitempty"`
}

// GetChargingProfilesRequestJson is the request payload for the GetChargingProfiles message (CSMS→CS).
// The CS will respond with the status and, if Accepted, follow up with one or more ReportChargingProfiles messages.
type GetChargingProfilesRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// RequestId is a reference identification used by the CS in subsequent
	// ReportChargingProfiles messages.
	RequestId int `json:"requestId" yaml:"requestId" mapstructure:"requestId"`

	// EvseId optionally specifies for which EVSE charging profiles should be reported.
	// If 0, only profiles on the Charging Station itself (grid connection) are reported.
	// If omitted, all installed charging profiles are reported.
	EvseId *int `json:"evseId,omitempty" yaml:"evseId,omitempty" mapstructure:"evseId,omitempty"`

	// ChargingProfile defines the criteria for filtering charging profiles.
	ChargingProfile ChargingProfileCriterionType `json:"chargingProfile" yaml:"chargingProfile" mapstructure:"chargingProfile"`
}

func (*GetChargingProfilesRequestJson) IsRequest() {}
