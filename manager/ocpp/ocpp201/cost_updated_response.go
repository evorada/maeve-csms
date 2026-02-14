// SPDX-License-Identifier: Apache-2.0

package ocpp201

// CostUpdatedResponseJson is the response body for the CostUpdated CSMS-to-CS call.
// The charge station responds with an empty body to acknowledge receipt.
type CostUpdatedResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`
}

func (c *CostUpdatedResponseJson) IsResponse() {}
