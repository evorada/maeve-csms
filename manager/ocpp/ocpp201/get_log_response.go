// SPDX-License-Identifier: Apache-2.0

package ocpp201

type LogStatusEnumType string

const LogStatusEnumTypeAccepted LogStatusEnumType = "Accepted"
const LogStatusEnumTypeRejected LogStatusEnumType = "Rejected"
const LogStatusEnumTypeAcceptedCanceled LogStatusEnumType = "AcceptedCanceled"

type GetLogResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Status corresponds to the JSON schema field "status".
	Status LogStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// StatusInfo corresponds to the JSON schema field "statusInfo".
	StatusInfo *StatusInfoType `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`

	// Filename corresponds to the JSON schema field "filename".
	Filename *string `json:"filename,omitempty" yaml:"filename,omitempty" mapstructure:"filename,omitempty"`
}

func (*GetLogResponseJson) IsResponse() {}
