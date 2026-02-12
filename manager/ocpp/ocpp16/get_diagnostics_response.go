// SPDX-License-Identifier: Apache-2.0

package ocpp16

type GetDiagnosticsResponseJson struct {
	// FileName corresponds to the JSON schema field "fileName".
	FileName *string `json:"fileName,omitempty" yaml:"fileName,omitempty" mapstructure:"fileName"`
}

func (*GetDiagnosticsResponseJson) IsResponse() {}
