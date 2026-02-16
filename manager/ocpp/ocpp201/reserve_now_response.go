// SPDX-License-Identifier: Apache-2.0

package ocpp201

// ReserveNowStatusEnumType indicates the result of reservation processing.
type ReserveNowStatusEnumType string

const ReserveNowStatusEnumTypeAccepted ReserveNowStatusEnumType = "Accepted"
const ReserveNowStatusEnumTypeFaulted ReserveNowStatusEnumType = "Faulted"
const ReserveNowStatusEnumTypeOccupied ReserveNowStatusEnumType = "Occupied"
const ReserveNowStatusEnumTypeRejected ReserveNowStatusEnumType = "Rejected"
const ReserveNowStatusEnumTypeUnavailable ReserveNowStatusEnumType = "Unavailable"

// ReserveNowResponseJson is the call result for ReserveNow.
type ReserveNowResponseJson struct {
	CustomData *CustomDataType          `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`
	Status     ReserveNowStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`
	StatusInfo *StatusInfoType          `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`
}

func (*ReserveNowResponseJson) IsResponse() {}
