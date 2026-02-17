// SPDX-License-Identifier: Apache-2.0

package ocpp201

// NotifyCustomerInformationRequestJson carries customer info report fragments from CS to CSMS.
type NotifyCustomerInformationRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Data is (part of) the requested customer information payload.
	Data string `json:"data" yaml:"data" mapstructure:"data"`

	// Tbc indicates whether additional fragments will follow.
	Tbc bool `json:"tbc,omitempty" yaml:"tbc,omitempty" mapstructure:"tbc,omitempty"`

	// SeqNo is the sequence number of this fragment.
	SeqNo int `json:"seqNo" yaml:"seqNo" mapstructure:"seqNo"`

	// GeneratedAt is when this message was generated at the charge station.
	GeneratedAt string `json:"generatedAt" yaml:"generatedAt" mapstructure:"generatedAt"`

	// RequestId is the id of the originating CustomerInformation request.
	RequestId int `json:"requestId" yaml:"requestId" mapstructure:"requestId"`
}

func (*NotifyCustomerInformationRequestJson) IsRequest() {}
