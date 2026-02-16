// SPDX-License-Identifier: Apache-2.0

package ocpp201

type NotifyDisplayMessagesRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// MessageInfo corresponds to the JSON schema field "messageInfo".
	MessageInfo []MessageInfoType `json:"messageInfo,omitempty" yaml:"messageInfo,omitempty" mapstructure:"messageInfo,omitempty"`

	// The id of the GetDisplayMessagesRequest that requested this message.
	RequestId int `json:"requestId" yaml:"requestId" mapstructure:"requestId"`

	// "to be continued" indicator.
	Tbc *bool `json:"tbc,omitempty" yaml:"tbc,omitempty" mapstructure:"tbc,omitempty"`
}

func (*NotifyDisplayMessagesRequestJson) IsRequest() {}
