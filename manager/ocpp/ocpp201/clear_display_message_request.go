// SPDX-License-Identifier: Apache-2.0

package ocpp201

// ClearDisplayMessageRequestJson requests removal of a display message by ID.
type ClearDisplayMessageRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Id of the message that SHALL be removed from the Charging Station.
	Id int `json:"id" yaml:"id" mapstructure:"id"`
}

func (*ClearDisplayMessageRequestJson) IsRequest() {}
