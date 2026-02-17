// SPDX-License-Identifier: Apache-2.0

package ocpp201

// SetDisplayMessageRequestJson requests a charging station to display a message.
type SetDisplayMessageRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Message contains details of the message to display.
	Message MessageInfoType `json:"message" yaml:"message" mapstructure:"message"`
}

func (*SetDisplayMessageRequestJson) IsRequest() {}
