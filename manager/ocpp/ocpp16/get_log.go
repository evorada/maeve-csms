// SPDX-License-Identifier: Apache-2.0

package ocpp16

type LogEnumType string

const LogEnumTypeDiagnosticsLog LogEnumType = "DiagnosticsLog"
const LogEnumTypeSecurityLog LogEnumType = "SecurityLog"

type LogParametersType struct {
	// RemoteLocation corresponds to the JSON schema field "remoteLocation".
	RemoteLocation string `json:"remoteLocation" yaml:"remoteLocation" mapstructure:"remoteLocation"`

	// OldestTimestamp corresponds to the JSON schema field "oldestTimestamp".
	OldestTimestamp *string `json:"oldestTimestamp,omitempty" yaml:"oldestTimestamp,omitempty" mapstructure:"oldestTimestamp,omitempty"`

	// LatestTimestamp corresponds to the JSON schema field "latestTimestamp".
	LatestTimestamp *string `json:"latestTimestamp,omitempty" yaml:"latestTimestamp,omitempty" mapstructure:"latestTimestamp,omitempty"`
}

type GetLogJson struct {
	// Log corresponds to the JSON schema field "log".
	Log LogParametersType `json:"log" yaml:"log" mapstructure:"log"`

	// LogType corresponds to the JSON schema field "logType".
	LogType LogEnumType `json:"logType" yaml:"logType" mapstructure:"logType"`

	// RequestId corresponds to the JSON schema field "requestId".
	RequestId int `json:"requestId" yaml:"requestId" mapstructure:"requestId"`

	// Retries corresponds to the JSON schema field "retries".
	Retries *int `json:"retries,omitempty" yaml:"retries,omitempty" mapstructure:"retries,omitempty"`

	// RetryInterval corresponds to the JSON schema field "retryInterval".
	RetryInterval *int `json:"retryInterval,omitempty" yaml:"retryInterval,omitempty" mapstructure:"retryInterval,omitempty"`
}

func (*GetLogJson) IsRequest() {}
