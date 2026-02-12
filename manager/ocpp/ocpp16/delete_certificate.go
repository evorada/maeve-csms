// SPDX-License-Identifier: Apache-2.0

package ocpp16

type HashAlgorithmEnumType string

const HashAlgorithmEnumTypeSHA256 HashAlgorithmEnumType = "SHA256"
const HashAlgorithmEnumTypeSHA384 HashAlgorithmEnumType = "SHA384"
const HashAlgorithmEnumTypeSHA512 HashAlgorithmEnumType = "SHA512"

type CertificateHashDataType struct {
	// HashAlgorithm corresponds to the JSON schema field "hashAlgorithm".
	HashAlgorithm HashAlgorithmEnumType `json:"hashAlgorithm" yaml:"hashAlgorithm" mapstructure:"hashAlgorithm"`

	// IssuerNameHash corresponds to the JSON schema field "issuerNameHash".
	IssuerNameHash string `json:"issuerNameHash" yaml:"issuerNameHash" mapstructure:"issuerNameHash"`

	// IssuerKeyHash corresponds to the JSON schema field "issuerKeyHash".
	IssuerKeyHash string `json:"issuerKeyHash" yaml:"issuerKeyHash" mapstructure:"issuerKeyHash"`

	// SerialNumber corresponds to the JSON schema field "serialNumber".
	SerialNumber string `json:"serialNumber" yaml:"serialNumber" mapstructure:"serialNumber"`
}

type DeleteCertificateJson struct {
	// CertificateHashData corresponds to the JSON schema field "certificateHashData".
	CertificateHashData CertificateHashDataType `json:"certificateHashData" yaml:"certificateHashData" mapstructure:"certificateHashData"`
}

func (*DeleteCertificateJson) IsRequest() {}
