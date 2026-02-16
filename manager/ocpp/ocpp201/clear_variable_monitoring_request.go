// SPDX-License-Identifier: Apache-2.0

package ocpp201

// ClearVariableMonitoringRequestJson requests deletion of monitor configurations by monitor IDs.
type ClearVariableMonitoringRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Id contains one or more monitor IDs that should be removed.
	Id []int `json:"id" yaml:"id" mapstructure:"id"`
}

func (*ClearVariableMonitoringRequestJson) IsRequest() {}
