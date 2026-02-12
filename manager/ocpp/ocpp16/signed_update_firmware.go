// SPDX-License-Identifier: Apache-2.0

package ocpp16

type SignedUpdateFirmwareFirmwareType struct {
	// InstallDateTime corresponds to the JSON schema field "installDateTime".
	InstallDateTime *string `json:"installDateTime,omitempty" yaml:"installDateTime,omitempty" mapstructure:"installDateTime"`

	// Location corresponds to the JSON schema field "location".
	Location string `json:"location" yaml:"location" mapstructure:"location"`

	// RetrieveDateTime corresponds to the JSON schema field "retrieveDateTime".
	RetrieveDateTime string `json:"retrieveDateTime" yaml:"retrieveDateTime" mapstructure:"retrieveDateTime"`

	// Signature corresponds to the JSON schema field "signature".
	Signature string `json:"signature" yaml:"signature" mapstructure:"signature"`

	// SigningCertificate corresponds to the JSON schema field "signingCertificate".
	SigningCertificate string `json:"signingCertificate" yaml:"signingCertificate" mapstructure:"signingCertificate"`
}

type SignedUpdateFirmwareJson struct {
	// Firmware corresponds to the JSON schema field "firmware".
	Firmware SignedUpdateFirmwareFirmwareType `json:"firmware" yaml:"firmware" mapstructure:"firmware"`

	// RequestId corresponds to the JSON schema field "requestId".
	RequestId int `json:"requestId" yaml:"requestId" mapstructure:"requestId"`

	// Retries corresponds to the JSON schema field "retries".
	Retries *int `json:"retries,omitempty" yaml:"retries,omitempty" mapstructure:"retries"`

	// RetryInterval corresponds to the JSON schema field "retryInterval".
	RetryInterval *int `json:"retryInterval,omitempty" yaml:"retryInterval,omitempty" mapstructure:"retryInterval"`
}

func (*SignedUpdateFirmwareJson) IsRequest() {}
