// SPDX-License-Identifier: Apache-2.0

package ocpp201

// CostUpdatedRequestJson is the request body for the CostUpdated CSMS-to-CS call.
// The CSMS sends this to update the running cost of a transaction on the charge station's display.
type CostUpdatedRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// TotalCost is the current total cost, based on the information known by the CSMS, of the
	// transaction including taxes. In the currency configured with the configuration Variable Currency.
	TotalCost float64 `json:"totalCost" yaml:"totalCost" mapstructure:"totalCost"`

	// TransactionId is the transaction ID of the transaction for which the cost is updated.
	TransactionId string `json:"transactionId" yaml:"transactionId" mapstructure:"transactionId"`
}

func (c *CostUpdatedRequestJson) IsRequest() {}
