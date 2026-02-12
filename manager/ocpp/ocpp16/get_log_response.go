// SPDX-License-Identifier: Apache-2.0

package ocpp16

type GetLogResponseJsonStatus string

const GetLogResponseJsonStatusAccepted GetLogResponseJsonStatus = "Accepted"
const GetLogResponseJsonStatusRejected GetLogResponseJsonStatus = "Rejected"
const GetLogResponseJsonStatusAcceptedCanceled GetLogResponseJsonStatus = "AcceptedCanceled"

type GetLogResponseJson struct {
	// Status corresponds to the JSON schema field "status".
	Status GetLogResponseJsonStatus `json:"status" yaml:"status" mapstructure:"status"`

	// Filename corresponds to the JSON schema field "filename".
	Filename *string `json:"filename,omitempty" yaml:"filename,omitempty" mapstructure:"filename,omitempty"`
}

func (*GetLogResponseJson) IsResponse() {}
