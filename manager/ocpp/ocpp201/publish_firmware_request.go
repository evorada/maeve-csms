// SPDX-License-Identifier: Apache-2.0

package ocpp201

// PublishFirmwareRequestJson is sent by the CSMS to instruct a Local Controller
// to download a firmware image and make it available for other Charging Stations
// on its local network. This enables local firmware distribution without each
// Charging Station requiring direct internet access.
type PublishFirmwareRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Location is the URI pointing to a location from which to retrieve the firmware.
	Location string `json:"location" yaml:"location" mapstructure:"location"`

	// Retries specifies how many times the Local Controller must try to download
	// the firmware before giving up. If not present, the Local Controller decides.
	Retries *int `json:"retries,omitempty" yaml:"retries,omitempty" mapstructure:"retries,omitempty"`

	// Checksum is the MD5 checksum over the entire firmware file as a hexadecimal
	// string of length 32.
	Checksum string `json:"checksum" yaml:"checksum" mapstructure:"checksum"`

	// RequestId is the identifier of this request.
	RequestId int `json:"requestId" yaml:"requestId" mapstructure:"requestId"`

	// RetryInterval is the interval in seconds after which a retry may be attempted.
	// If not present, the Local Controller decides how long to wait between attempts.
	RetryInterval *int `json:"retryInterval,omitempty" yaml:"retryInterval,omitempty" mapstructure:"retryInterval,omitempty"`
}

func (*PublishFirmwareRequestJson) IsRequest() {}
