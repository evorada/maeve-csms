// SPDX-License-Identifier: Apache-2.0

package ocpp201

// GetChargingProfileStatusEnumType indicates whether the CS has profiles to report.
type GetChargingProfileStatusEnumType string

const (
	// GetChargingProfileStatusEnumTypeAccepted means the CS will send ReportChargingProfiles.
	GetChargingProfileStatusEnumTypeAccepted GetChargingProfileStatusEnumType = "Accepted"
	// GetChargingProfileStatusEnumTypeNoProfiles means no matching charging profiles exist.
	GetChargingProfileStatusEnumTypeNoProfiles GetChargingProfileStatusEnumType = "NoProfiles"
)

// GetChargingProfilesResponseJson is the response payload for the GetChargingProfiles message.
type GetChargingProfilesResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Status indicates whether the CS is able to process the request and will send
	// ReportChargingProfiles messages.
	Status GetChargingProfileStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// StatusInfo optionally provides additional information about the status.
	StatusInfo *StatusInfoType `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`
}

func (*GetChargingProfilesResponseJson) IsResponse() {}
