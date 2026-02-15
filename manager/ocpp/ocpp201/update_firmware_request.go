// SPDX-License-Identifier: Apache-2.0

package ocpp201

// UpdateFirmwareStatusEnumType represents the status returned by the Charging Station
// in response to an UpdateFirmwareRequest.
type UpdateFirmwareStatusEnumType string

const UpdateFirmwareStatusEnumTypeAccepted UpdateFirmwareStatusEnumType = "Accepted"
const UpdateFirmwareStatusEnumTypeRejected UpdateFirmwareStatusEnumType = "Rejected"
const UpdateFirmwareStatusEnumTypeAcceptedCanceled UpdateFirmwareStatusEnumType = "AcceptedCanceled"
const UpdateFirmwareStatusEnumTypeInvalidCertificate UpdateFirmwareStatusEnumType = "InvalidCertificate"
const UpdateFirmwareStatusEnumTypeRevokedCertificate UpdateFirmwareStatusEnumType = "RevokedCertificate"

// FirmwareType represents firmware that can be loaded/updated on the Charging Station.
type FirmwareType struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Location is the URI defining the origin of the firmware.
	Location string `json:"location" yaml:"location" mapstructure:"location"`

	// RetrieveDateTime is the date and time at which the firmware shall be retrieved.
	RetrieveDateTime string `json:"retrieveDateTime" yaml:"retrieveDateTime" mapstructure:"retrieveDateTime"`

	// InstallDateTime is the date and time at which the firmware shall be installed.
	InstallDateTime *string `json:"installDateTime,omitempty" yaml:"installDateTime,omitempty" mapstructure:"installDateTime,omitempty"`

	// SigningCertificate is the PEM encoded X.509 certificate with which the firmware was signed.
	SigningCertificate *string `json:"signingCertificate,omitempty" yaml:"signingCertificate,omitempty" mapstructure:"signingCertificate,omitempty"`

	// Signature is the base64 encoded firmware signature.
	Signature *string `json:"signature,omitempty" yaml:"signature,omitempty" mapstructure:"signature,omitempty"`
}

// UpdateFirmwareRequestJson is sent by the CSMS to instruct the Charging Station
// to download and install a new firmware version.
type UpdateFirmwareRequestJson struct {
	// CustomData corresponds to the JSON schema field "customData".
	CustomData *CustomDataType `json:"customData,omitempty" yaml:"customData,omitempty" mapstructure:"customData,omitempty"`

	// Retries specifies how many times the Charging Station must try to download
	// the firmware before giving up.
	Retries *int `json:"retries,omitempty" yaml:"retries,omitempty" mapstructure:"retries,omitempty"`

	// RetryInterval is the interval in seconds after which a retry may be attempted.
	RetryInterval *int `json:"retryInterval,omitempty" yaml:"retryInterval,omitempty" mapstructure:"retryInterval,omitempty"`

	// RequestId is the identifier of this request.
	RequestId int `json:"requestId" yaml:"requestId" mapstructure:"requestId"`

	// Firmware contains the firmware to be installed.
	Firmware FirmwareType `json:"firmware" yaml:"firmware" mapstructure:"firmware"`
}

func (*UpdateFirmwareRequestJson) IsRequest() {}
