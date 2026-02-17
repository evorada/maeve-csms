// SPDX-License-Identifier: Apache-2.0

package ocpp201

// SetDisplayMessageResponseJson is the CallResult for SetDisplayMessage.
type SetDisplayMessageResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Status indicates whether the charging station accepted the display message request.
	Status DisplayMessageStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// StatusInfo contains optional status details.
	StatusInfo *StatusInfoType `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`
}

func (*SetDisplayMessageResponseJson) IsResponse() {}
