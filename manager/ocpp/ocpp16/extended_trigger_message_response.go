// SPDX-License-Identifier: Apache-2.0

package ocpp16

type ExtendedTriggerMessageResponseJsonStatus string

type ExtendedTriggerMessageResponseJson struct {
	// Status corresponds to the JSON schema field "status".
	Status ExtendedTriggerMessageResponseJsonStatus `json:"status" yaml:"status" mapstructure:"status"`
}

const ExtendedTriggerMessageResponseJsonStatusAccepted ExtendedTriggerMessageResponseJsonStatus = "Accepted"
const ExtendedTriggerMessageResponseJsonStatusRejected ExtendedTriggerMessageResponseJsonStatus = "Rejected"
const ExtendedTriggerMessageResponseJsonStatusNotImplemented ExtendedTriggerMessageResponseJsonStatus = "NotImplemented"

func (*ExtendedTriggerMessageResponseJson) IsResponse() {}
