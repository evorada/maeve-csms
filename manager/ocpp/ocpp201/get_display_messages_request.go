// SPDX-License-Identifier: Apache-2.0

package ocpp201

type GetDisplayMessagesRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// If provided the Charging Station shall return Display Messages of the given
	// ids.
	Id []int `json:"id,omitempty" yaml:"id,omitempty" mapstructure:"id,omitempty"`

	// The Id of this request.
	RequestId int `json:"requestId" yaml:"requestId" mapstructure:"requestId"`

	// If provided the Charging Station shall return Display Messages with the given
	// priority only.
	Priority *MessagePriorityEnumType `json:"priority,omitempty" yaml:"priority,omitempty" mapstructure:"priority,omitempty"`

	// If provided the Charging Station shall return Display Messages with the given
	// state only.
	State *MessageStateEnumType `json:"state,omitempty" yaml:"state,omitempty" mapstructure:"state,omitempty"`
}

func (*GetDisplayMessagesRequestJson) IsRequest() {}
