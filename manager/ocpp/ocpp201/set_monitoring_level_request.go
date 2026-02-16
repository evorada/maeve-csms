// SPDX-License-Identifier: Apache-2.0

package ocpp201

// SetMonitoringLevelRequestJson requests setting the severity threshold for monitoring events.
type SetMonitoringLevelRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Severity is the highest severity number the Charging Station should report (range 0-9).
	Severity int `json:"severity" yaml:"severity" mapstructure:"severity"`
}

func (*SetMonitoringLevelRequestJson) IsRequest() {}
