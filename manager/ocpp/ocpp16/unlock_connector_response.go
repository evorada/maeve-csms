// SPDX-License-Identifier: Apache-2.0

package ocpp16

type UnlockConnectorResponseJsonStatus string

type UnlockConnectorResponseJson struct {
	// Status corresponds to the JSON schema field "status".
	Status UnlockConnectorResponseJsonStatus `json:"status" yaml:"status" mapstructure:"status"`
}

const UnlockConnectorResponseJsonStatusUnlocked UnlockConnectorResponseJsonStatus = "Unlocked"
const UnlockConnectorResponseJsonStatusUnlockFailed UnlockConnectorResponseJsonStatus = "UnlockFailed"
const UnlockConnectorResponseJsonStatusNotSupported UnlockConnectorResponseJsonStatus = "NotSupported"

func (*UnlockConnectorResponseJson) IsResponse() {}
