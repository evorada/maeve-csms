// SPDX-License-Identifier: Apache-2.0

package ocpp201

// NotifyEventResponseJson acknowledges NotifyEvent.
type NotifyEventResponseJson struct {
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`
}

func (*NotifyEventResponseJson) IsResponse() {}
