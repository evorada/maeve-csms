// SPDX-License-Identifier: Apache-2.0

package ocpp201

// ReservationUpdateStatusEnumType indicates the updated reservation status.
type ReservationUpdateStatusEnumType string

const ReservationUpdateStatusEnumTypeExpired ReservationUpdateStatusEnumType = "Expired"
const ReservationUpdateStatusEnumTypeRemoved ReservationUpdateStatusEnumType = "Removed"

// ReservationStatusUpdateRequestJson is sent by the charge station when a reservation state changes.
type ReservationStatusUpdateRequestJson struct {
	CustomData              *CustomDataType                 `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`
	ReservationId           int                             `json:"reservationId" yaml:"reservationId" mapstructure:"reservationId"`
	ReservationUpdateStatus ReservationUpdateStatusEnumType `json:"reservationUpdateStatus" yaml:"reservationUpdateStatus" mapstructure:"reservationUpdateStatus"`
}

func (*ReservationStatusUpdateRequestJson) IsRequest() {}
