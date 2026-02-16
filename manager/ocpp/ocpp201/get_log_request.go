// SPDX-License-Identifier: Apache-2.0

package ocpp201

type LogEnumType string

const LogEnumTypeDiagnosticsLog LogEnumType = "DiagnosticsLog"
const LogEnumTypeSecurityLog LogEnumType = "SecurityLog"

// Generic class for the configuration of logging entries.
type LogParametersType struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// The date and time of the latest logging information to include in the diagnostics.
	LatestTimestamp *string `json:"latestTimestamp,omitempty" yaml:"latestTimestamp,omitempty" mapstructure:"latestTimestamp,omitempty"`

	// The date and time of the oldest logging information to include in the diagnostics.
	OldestTimestamp *string `json:"oldestTimestamp,omitempty" yaml:"oldestTimestamp,omitempty" mapstructure:"oldestTimestamp,omitempty"`

	// The URL of the location at the remote system where the log should be stored.
	RemoteLocation string `json:"remoteLocation" yaml:"remoteLocation" mapstructure:"remoteLocation"`
}

type GetLogRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Log corresponds to the JSON schema field "log".
	Log LogParametersType `json:"log" yaml:"log" mapstructure:"log"`

	// The type of log file that the Charging Station should send.
	LogType LogEnumType `json:"logType" yaml:"logType" mapstructure:"logType"`

	// The Id of this request.
	RequestId int `json:"requestId" yaml:"requestId" mapstructure:"requestId"`

	// Number of times the Charging Station should try to upload the log before giving up.
	Retries *int `json:"retries,omitempty" yaml:"retries,omitempty" mapstructure:"retries,omitempty"`

	// Interval in seconds after which a retry may be attempted.
	RetryInterval *int `json:"retryInterval,omitempty" yaml:"retryInterval,omitempty" mapstructure:"retryInterval,omitempty"`
}

func (*GetLogRequestJson) IsRequest() {}
