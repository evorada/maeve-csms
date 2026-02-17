// SPDX-License-Identifier: Apache-2.0

package ocpp201

import "encoding/json"

type DataTransferStatusEnumType string

const DataTransferStatusEnumTypeAccepted DataTransferStatusEnumType = "Accepted"
const DataTransferStatusEnumTypeRejected DataTransferStatusEnumType = "Rejected"
const DataTransferStatusEnumTypeUnknownMessageId DataTransferStatusEnumType = "UnknownMessageId"
const DataTransferStatusEnumTypeUnknownVendorId DataTransferStatusEnumType = "UnknownVendorId"

type DataTransferResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Data without specified length or format, in response to request.
	Data json.RawMessage `json:"data,omitempty" yaml:"data,omitempty" mapstructure:"data,omitempty"`

	// Status corresponds to the JSON schema field "status".
	Status DataTransferStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// StatusInfo corresponds to the JSON schema field "statusInfo".
	StatusInfo *StatusInfoType `json:"statusInfo,omitempty" yaml:"statusInfo,omitempty" mapstructure:"statusInfo,omitempty"`
}

func (*DataTransferResponseJson) IsResponse() {}
