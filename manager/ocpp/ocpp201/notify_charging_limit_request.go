// SPDX-License-Identifier: Apache-2.0

package ocpp201

// ChargingLimitType describes a charging limit, including its source and whether
// it is critical for the grid.
type ChargingLimitType struct {
	// ChargingLimitSource is the source of the charging limit.
	ChargingLimitSource ChargingLimitSourceEnumType `json:"chargingLimitSource" yaml:"chargingLimitSource" mapstructure:"chargingLimitSource"`
	// IsGridCritical indicates whether the charging limit is critical for the grid. Optional.
	IsGridCritical *bool `json:"isGridCritical,omitempty" yaml:"isGridCritical,omitempty" mapstructure:"isGridCritical,omitempty"`
}

// NotifyChargingLimitRequestJson is sent by a Charge Station to the CSMS to notify it
// of a charging limit imposed by an external source (e.g. an Energy Management System).
// The message may optionally include the charging schedules that the limit results in.
type NotifyChargingLimitRequestJson struct {
	// ChargingLimit contains details of the charging limit including its source.
	ChargingLimit ChargingLimitType `json:"chargingLimit" yaml:"chargingLimit" mapstructure:"chargingLimit"`
	// ChargingSchedule is an optional list of charging schedules resulting from the limit. Optional.
	ChargingSchedule []ChargingScheduleType `json:"chargingSchedule,omitempty" yaml:"chargingSchedule,omitempty" mapstructure:"chargingSchedule,omitempty"`
	// EvseId is the EVSE that the charging schedule applies to. Optional; > 0.
	EvseId *int `json:"evseId,omitempty" yaml:"evseId,omitempty" mapstructure:"evseId,omitempty"`
}

func (n *NotifyChargingLimitRequestJson) IsRequest() {}
