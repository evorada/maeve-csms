// SPDX-License-Identifier: Apache-2.0

package ocpp201

// ReportChargingProfilesResponseJson is the acknowledgement sent by the CSMS to the
// Charge Station after receiving a ReportChargingProfiles message.
// The response body contains no required fields.
type ReportChargingProfilesResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`
}

func (r *ReportChargingProfilesResponseJson) IsResponse() {}
