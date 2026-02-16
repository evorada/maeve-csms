// SPDX-License-Identifier: Apache-2.0

package ocpp201

import "encoding/json"

type DataTransferRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Data without specified length or format.
	Data json.RawMessage `json:"data,omitempty" yaml:"data,omitempty" mapstructure:"data,omitempty"`

	// May be used to indicate a specific message or implementation.
	MessageId *string `json:"messageId,omitempty" yaml:"messageId,omitempty" mapstructure:"messageId,omitempty"`

	// This identifies the Vendor specific implementation.
	VendorId string `json:"vendorId" yaml:"vendorId" mapstructure:"vendorId"`
}

func (*DataTransferRequestJson) IsRequest() {}
