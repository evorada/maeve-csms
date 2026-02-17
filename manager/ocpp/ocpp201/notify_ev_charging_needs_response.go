// SPDX-License-Identifier: Apache-2.0

package ocpp201

// NotifyEVChargingNeedsStatusEnumType represents the CSMS's ability to process
// the NotifyEVChargingNeeds message.
type NotifyEVChargingNeedsStatusEnumType string

const (
	// NotifyEVChargingNeedsStatusEnumTypeAccepted indicates the CSMS successfully processed the charging needs.
	NotifyEVChargingNeedsStatusEnumTypeAccepted NotifyEVChargingNeedsStatusEnumType = "Accepted"
	// NotifyEVChargingNeedsStatusEnumTypeRejected indicates the CSMS rejected the charging needs.
	NotifyEVChargingNeedsStatusEnumTypeRejected NotifyEVChargingNeedsStatusEnumType = "Rejected"
	// NotifyEVChargingNeedsStatusEnumTypeProcessing indicates the CSMS is still processing the charging needs.
	NotifyEVChargingNeedsStatusEnumTypeProcessing NotifyEVChargingNeedsStatusEnumType = "Processing"
)

// NotifyEVChargingNeedsResponseJson is the CSMS response to a NotifyEVChargingNeeds message.
// The status indicates whether the CSMS processed the message, but does not imply
// the EV's charging needs can be met with the current charging profile.
type NotifyEVChargingNeedsResponseJson struct {
	// Status indicates whether the CSMS was able to process the message.
	Status NotifyEVChargingNeedsStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`
	// StatusInfo provides additional information about the status. Optional.
	StatusInfo *StatusInfoType `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`
}

func (n *NotifyEVChargingNeedsResponseJson) IsResponse() {}
