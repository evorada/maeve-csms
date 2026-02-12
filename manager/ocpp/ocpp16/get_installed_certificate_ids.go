// SPDX-License-Identifier: Apache-2.0

package ocpp16

type CertificateUseEnumType string

const CertificateUseEnumTypeCentralSystemRootCertificate CertificateUseEnumType = "CentralSystemRootCertificate"
const CertificateUseEnumTypeManufacturerRootCertificate CertificateUseEnumType = "ManufacturerRootCertificate"

type GetInstalledCertificateIdsJson struct {
	// CertificateType corresponds to the JSON schema field "certificateType".
	CertificateType CertificateUseEnumType `json:"certificateType" yaml:"certificateType" mapstructure:"certificateType"`
}

func (*GetInstalledCertificateIdsJson) IsRequest() {}
