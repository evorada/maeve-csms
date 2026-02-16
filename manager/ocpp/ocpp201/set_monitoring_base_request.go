// SPDX-License-Identifier: Apache-2.0

package ocpp201

// MonitoringBaseEnumType specifies which monitoring base should be set.
type MonitoringBaseEnumType string

const MonitoringBaseEnumTypeAll MonitoringBaseEnumType = "All"
const MonitoringBaseEnumTypeFactoryDefault MonitoringBaseEnumType = "FactoryDefault"
const MonitoringBaseEnumTypeHardWiredOnly MonitoringBaseEnumType = "HardWiredOnly"

// SetMonitoringBaseRequestJson requests setting the monitoring base on a Charging Station.
type SetMonitoringBaseRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// MonitoringBase specifies which monitoring base should be applied.
	MonitoringBase MonitoringBaseEnumType `json:"monitoringBase" yaml:"monitoringBase" mapstructure:"monitoringBase"`
}

func (*SetMonitoringBaseRequestJson) IsRequest() {}
