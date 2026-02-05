// SPDX-License-Identifier: Apache-2.0

package ocpp16

type ClearCacheResponseJsonStatus string

type ClearCacheResponseJson struct {
	// Status corresponds to the JSON schema field "status".
	Status ClearCacheResponseJsonStatus `json:"status" yaml:"status" mapstructure:"status"`
}

const ClearCacheResponseJsonStatusAccepted ClearCacheResponseJsonStatus = "Accepted"
const ClearCacheResponseJsonStatusRejected ClearCacheResponseJsonStatus = "Rejected"

func (*ClearCacheResponseJson) IsResponse() {}
