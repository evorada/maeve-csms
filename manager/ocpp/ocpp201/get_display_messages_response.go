// SPDX-License-Identifier: Apache-2.0

package ocpp201

type GetDisplayMessagesStatusEnumType string

const GetDisplayMessagesStatusEnumTypeAccepted GetDisplayMessagesStatusEnumType = "Accepted"
const GetDisplayMessagesStatusEnumTypeUnknown GetDisplayMessagesStatusEnumType = "Unknown"

type GetDisplayMessagesResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Indicates if the Charging Station has Display Messages that match the request
	// criteria.
	Status GetDisplayMessagesStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// StatusInfo corresponds to the JSON schema field "statusInfo".
	StatusInfo *StatusInfoType `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`
}

func (*GetDisplayMessagesResponseJson) IsResponse() {}
