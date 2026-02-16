// SPDX-License-Identifier: Apache-2.0

package ocpp201

// LogEnumType indicates the type of log file requested from the Charging Station.
type LogEnumType string

const LogEnumTypeDiagnosticsLog LogEnumType = "DiagnosticsLog"
const LogEnumTypeSecurityLog LogEnumType = "SecurityLog"

// LogParametersType configures the log upload target and optional time bounds.
type LogParametersType struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// RemoteLocation is the URI where the log should be uploaded.
	RemoteLocation string `json:"remoteLocation" yaml:"remoteLocation" mapstructure:"remoteLocation"`

	// OldestTimestamp is the oldest timestamp to include in the exported logs.
	OldestTimestamp *string `json:"oldestTimestamp,omitempty" yaml:"oldestTimestamp,omitempty" mapstructure:"oldestTimestamp,omitempty"`

	// LatestTimestamp is the newest timestamp to include in the exported logs.
	LatestTimestamp *string `json:"latestTimestamp,omitempty" yaml:"latestTimestamp,omitempty" mapstructure:"latestTimestamp,omitempty"`
}

// GetLogRequestJson requests a Charging Station to upload diagnostics or security logs.
type GetLogRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Log contains upload location and optional timestamp filters.
	Log LogParametersType `json:"log" yaml:"log" mapstructure:"log"`

	// LogType identifies which log class should be uploaded.
	LogType LogEnumType `json:"logType" yaml:"logType" mapstructure:"logType"`

	// RequestId identifies this request.
	RequestId int `json:"requestId" yaml:"requestId" mapstructure:"requestId"`

	// Retries is the maximum number of upload retries.
	Retries *int `json:"retries,omitempty" yaml:"retries,omitempty" mapstructure:"retries,omitempty"`

	// RetryInterval is the retry interval in seconds.
	RetryInterval *int `json:"retryInterval,omitempty" yaml:"retryInterval,omitempty" mapstructure:"retryInterval,omitempty"`
}

func (*GetLogRequestJson) IsRequest() {}
