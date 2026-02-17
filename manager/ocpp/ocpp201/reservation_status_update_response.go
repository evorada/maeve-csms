// SPDX-License-Identifier: Apache-2.0

package ocpp201

// ReservationStatusUpdateResponseJson is the call response for ReservationStatusUpdate.
type ReservationStatusUpdateResponseJson struct {
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`
}

func (*ReservationStatusUpdateResponseJson) IsResponse() {}
