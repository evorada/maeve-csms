// SPDX-License-Identifier: Apache-2.0

package ocpp201

// ClearedChargingLimitResponseJson is the CSMS response to a ClearedChargingLimit message.
// The response has no fields; receipt is acknowledged by the empty response.
type ClearedChargingLimitResponseJson struct{}

func (c *ClearedChargingLimitResponseJson) IsResponse() {}
