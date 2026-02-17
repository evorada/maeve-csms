// SPDX-License-Identifier: Apache-2.0

package ocpp201

// CancelReservationStatusEnumType indicates the outcome of a reservation cancellation request.
type CancelReservationStatusEnumType string

const CancelReservationStatusEnumTypeAccepted CancelReservationStatusEnumType = "Accepted"
const CancelReservationStatusEnumTypeRejected CancelReservationStatusEnumType = "Rejected"

// CancelReservationResponseJson is the call result for CancelReservation.
type CancelReservationResponseJson struct {
	CustomData *CustomDataType                 `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`
	Status     CancelReservationStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`
	StatusInfo *StatusInfoType                 `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`
}

func (*CancelReservationResponseJson) IsResponse() {}
