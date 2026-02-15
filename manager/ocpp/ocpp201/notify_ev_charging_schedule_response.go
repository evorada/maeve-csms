// SPDX-License-Identifier: Apache-2.0

package ocpp201

// NotifyEVChargingScheduleResponseJson is the response payload for the
// NotifyEVChargingSchedule message. The CSMS returns whether it was able
// to process the message successfully. This does not imply any approval
// of the charging schedule.
type NotifyEVChargingScheduleResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Status indicates whether the CSMS has been able to process the message
	// successfully. Accepted or Rejected.
	Status GenericStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// StatusInfo optionally provides additional information about the status.
	StatusInfo *StatusInfoType `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`
}

func (n *NotifyEVChargingScheduleResponseJson) IsResponse() {}
