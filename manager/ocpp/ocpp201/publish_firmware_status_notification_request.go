// SPDX-License-Identifier: Apache-2.0

package ocpp201

// PublishFirmwareStatusEnumType represents the status of a firmware publishing operation
// on a Local Controller.
type PublishFirmwareStatusEnumType string

const (
	PublishFirmwareStatusEnumTypeIdle              PublishFirmwareStatusEnumType = "Idle"
	PublishFirmwareStatusEnumTypeDownloadScheduled PublishFirmwareStatusEnumType = "DownloadScheduled"
	PublishFirmwareStatusEnumTypeDownloading       PublishFirmwareStatusEnumType = "Downloading"
	PublishFirmwareStatusEnumTypeDownloaded        PublishFirmwareStatusEnumType = "Downloaded"
	PublishFirmwareStatusEnumTypePublished         PublishFirmwareStatusEnumType = "Published"
	PublishFirmwareStatusEnumTypeDownloadFailed    PublishFirmwareStatusEnumType = "DownloadFailed"
	PublishFirmwareStatusEnumTypeDownloadPaused    PublishFirmwareStatusEnumType = "DownloadPaused"
	PublishFirmwareStatusEnumTypeInvalidChecksum   PublishFirmwareStatusEnumType = "InvalidChecksum"
	PublishFirmwareStatusEnumTypeChecksumVerified  PublishFirmwareStatusEnumType = "ChecksumVerified"
	PublishFirmwareStatusEnumTypePublishFailed     PublishFirmwareStatusEnumType = "PublishFailed"
)

// PublishFirmwareStatusNotificationRequestJson is sent by a Local Controller to the
// CSMS to report the progress of a PublishFirmware operation. The Local Controller
// downloads a firmware image and publishes it for other Charging Stations on its
// local network.
type PublishFirmwareStatusNotificationRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Status contains the progress status of the firmware publishing operation.
	Status PublishFirmwareStatusEnumType `json:"status" yaml:"status" mapstructure:"status"`

	// Location is required when Status is Published. Contains one or more URIs
	// from which the firmware can be retrieved (e.g. HTTP, HTTPS, FTP).
	Location []string `json:"location,omitempty" yaml:"location,omitempty" mapstructure:"location,omitempty"`

	// RequestId is the request id from the originating PublishFirmwareRequest,
	// if present.
	RequestId *int `json:"requestId,omitempty" yaml:"requestId,omitempty" mapstructure:"requestId,omitempty"`
}

func (*PublishFirmwareStatusNotificationRequestJson) IsRequest() {}
