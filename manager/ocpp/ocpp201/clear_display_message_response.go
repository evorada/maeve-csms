// SPDX-License-Identifier: Apache-2.0

package ocpp201

// ClearMessageStatusEnumType indicates whether the charging station could remove the message.
type ClearMessageStatusEnumType string

const ClearMessageStatusEnumTypeAccepted ClearMessageStatusEnumType = "Accepted"
const ClearMessageStatusEnumTypeUnknown ClearMessageStatusEnumType = "Unknown"

// ClearDisplayMessageResponseJson is the call result for ClearDisplayMessage.
type ClearDisplayMessageResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Status indicates whether the display message was removed.
	Status ClearMessageStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// StatusInfo provides optional additional details.
	StatusInfo *StatusInfoType `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`
}

func (*ClearDisplayMessageResponseJson) IsResponse() {}
