// SPDX-License-Identifier: Apache-2.0

package ocpp201

// UnpublishFirmwareStatusEnumType indicates whether the Local Controller succeeded
// in unpublishing the firmware.
type UnpublishFirmwareStatusEnumType string

const (
	// UnpublishFirmwareStatusDownloadOngoing indicates a firmware download is ongoing
	// and the firmware cannot be unpublished at this time.
	UnpublishFirmwareStatusDownloadOngoing UnpublishFirmwareStatusEnumType = "DownloadOngoing"

	// UnpublishFirmwareStatusNoFirmware indicates the Local Controller has no firmware
	// matching the provided checksum (not currently publishing it).
	UnpublishFirmwareStatusNoFirmware UnpublishFirmwareStatusEnumType = "NoFirmware"

	// UnpublishFirmwareStatusUnpublished indicates the firmware was successfully unpublished.
	UnpublishFirmwareStatusUnpublished UnpublishFirmwareStatusEnumType = "Unpublished"
)

// UnpublishFirmwareResponseJson is sent by the Local Controller in response to an
// UnpublishFirmwareRequest from the CSMS.
type UnpublishFirmwareResponseJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Status indicates whether the Local Controller succeeded in unpublishing the firmware.
	Status UnpublishFirmwareStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`
}

func (*UnpublishFirmwareResponseJson) IsResponse() {}
