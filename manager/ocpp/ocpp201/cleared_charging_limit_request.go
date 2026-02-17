// SPDX-License-Identifier: Apache-2.0

package ocpp201

// ClearedChargingLimitRequestJson is sent by a Charge Station to inform the CSMS
// that a previously set charging limit has been cleared.
type ClearedChargingLimitRequestJson struct {
	// ChargingLimitSource indicates the source whose limit was cleared.
	ChargingLimitSource ChargingLimitSourceEnumType `json:"chargingLimitSource" yaml:"chargingLimitSource" mapstructure:"chargingLimitSource"`
	// EvseId is the EVSE identifier whose charging limit was cleared. Optional.
	EvseId *int `json:"evseId,omitempty" yaml:"evseId,omitempty" mapstructure:"evseId,omitempty"`
}

func (c *ClearedChargingLimitRequestJson) IsRequest() {}
