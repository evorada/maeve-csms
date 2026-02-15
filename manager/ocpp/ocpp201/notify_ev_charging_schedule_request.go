// SPDX-License-Identifier: Apache-2.0

package ocpp201

// NotifyEVChargingScheduleRequestJson is sent by a Charge Station to the CSMS to
// report the charging schedule calculated by the EV (via ISO 15118). This allows
// the CSMS to be aware of the EV's negotiated charging schedule.
//
// Required fields: TimeBase, EvseId, ChargingSchedule.
type NotifyEVChargingScheduleRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// TimeBase is the point in time relative to which all periods in the charging
	// schedule are defined. Must be provided in UTC.
	TimeBase string `json:"timeBase" yaml:"timeBase" mapstructure:"timeBase"`

	// EvseId identifies the EVSE and connector to which the EV is connected.
	// EvseId must be > 0.
	EvseId int `json:"evseId" yaml:"evseId" mapstructure:"evseId"`

	// ChargingSchedule is the charging schedule reported by the EV.
	ChargingSchedule ChargingScheduleType `json:"chargingSchedule" yaml:"chargingSchedule" mapstructure:"chargingSchedule"`
}

func (n *NotifyEVChargingScheduleRequestJson) IsRequest() {}
