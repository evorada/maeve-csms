// SPDX-License-Identifier: Apache-2.0

package ocpp201

// UnpublishFirmwareRequestJson is sent by the CSMS to instruct a Local Controller
// to stop publishing a firmware image. The Local Controller should stop making
// the firmware available to Charging Stations on its local network.
type UnpublishFirmwareRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Checksum is the MD5 checksum over the entire firmware file as a hexadecimal
	// string of length 32. Used to identify which firmware to unpublish.
	Checksum string `json:"checksum" yaml:"checksum" mapstructure:"checksum"`
}

func (*UnpublishFirmwareRequestJson) IsRequest() {}
