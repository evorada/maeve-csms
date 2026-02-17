// SPDX-License-Identifier: Apache-2.0

package ocpp201

// NotifyChargingLimitResponseJson is the CSMS response to a NotifyChargingLimit message.
// The response has no fields; receipt is acknowledged by the empty response.
type NotifyChargingLimitResponseJson struct{}

func (n *NotifyChargingLimitResponseJson) IsResponse() {}
