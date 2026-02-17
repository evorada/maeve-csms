// SPDX-License-Identifier: Apache-2.0

package ocpp201

// CancelReservationRequestJson requests cancellation of an existing reservation.
type CancelReservationRequestJson struct {
	CustomData    *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`
	ReservationId int             `json:"reservationId" yaml:"reservationId" mapstructure:"reservationId"`
}

func (*CancelReservationRequestJson) IsRequest() {}
