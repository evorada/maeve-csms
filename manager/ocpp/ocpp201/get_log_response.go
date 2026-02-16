// SPDX-License-Identifier: Apache-2.0

package ocpp201

// LogStatusEnumType indicates if the Charging Station accepted the GetLog request.
type LogStatusEnumType string

const LogStatusEnumTypeAccepted LogStatusEnumType = "Accepted"
const LogStatusEnumTypeRejected LogStatusEnumType = "Rejected"
const LogStatusEnumTypeAcceptedCanceled LogStatusEnumType = "AcceptedCanceled"

// GetLogResponseJson is the call result for GetLog.
type GetLogResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Status indicates whether the request was accepted.
	Status LogStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// StatusInfo contains additional status details.
	StatusInfo *StatusInfoType `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`

	// Filename is the name of the generated log file, if available.
	Filename *string `json:"filename,omitempty" yaml:"filename,omitempty" mapstructure:"filename,omitempty"`
}

func (*GetLogResponseJson) IsResponse() {}
