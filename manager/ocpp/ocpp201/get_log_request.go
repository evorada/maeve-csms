// SPDX-License-Identifier: Apache-2.0

package ocpp201

type LogEnumType string

const LogEnumTypeDiagnosticsLog LogEnumType = "DiagnosticsLog"
const LogEnumTypeSecurityLog LogEnumType = "SecurityLog"

// Log configuration details for GetLog request.
type LogParametersType struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Remote location where the log should be uploaded.
	RemoteLocation string `json:"remoteLocation" yaml:"remoteLocation" mapstructure:"remoteLocation"`

	// Oldest timestamp to include in the log upload.
	OldestTimestamp *string `json:"oldestTimestamp,omitempty" yaml:"oldestTimestamp,omitempty" mapstructure:"oldestTimestamp,omitempty"`

	// Latest timestamp to include in the log upload.
	LatestTimestamp *string `json:"latestTimestamp,omitempty" yaml:"latestTimestamp,omitempty" mapstructure:"latestTimestamp,omitempty"`
}

type GetLogRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Log parameters for the requested upload.
	Log LogParametersType `json:"log" yaml:"log" mapstructure:"log"`

	// Type of log to upload.
	LogType LogEnumType `json:"logType" yaml:"logType" mapstructure:"logType"`

	// The id of this request.
	RequestId int `json:"requestId" yaml:"requestId" mapstructure:"requestId"`

	// Optional retry count.
	Retries *int `json:"retries,omitempty" yaml:"retries,omitempty" mapstructure:"retries,omitempty"`

	// Optional retry interval (seconds).
	RetryInterval *int `json:"retryInterval,omitempty" yaml:"retryInterval,omitempty" mapstructure:"retryInterval,omitempty"`
}

func (*GetLogRequestJson) IsRequest() {}
