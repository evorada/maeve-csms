// SPDX-License-Identifier: Apache-2.0

package ocpp16

type GetInstalledCertificateIdsResponseJsonStatus string

const GetInstalledCertificateIdsResponseJsonStatusAccepted GetInstalledCertificateIdsResponseJsonStatus = "Accepted"
const GetInstalledCertificateIdsResponseJsonStatusNotFound GetInstalledCertificateIdsResponseJsonStatus = "NotFound"

type GetInstalledCertificateIdsResponseJson struct {
	// CertificateHashData corresponds to the JSON schema field "certificateHashData".
	CertificateHashData []CertificateHashDataType `json:"certificateHashData,omitempty" yaml:"certificateHashData,omitempty" mapstructure:"certificateHashData"`

	// Status corresponds to the JSON schema field "status".
	Status GetInstalledCertificateIdsResponseJsonStatus `json:"status" yaml:"status" mapstructure:"status"`
}

func (*GetInstalledCertificateIdsResponseJson) IsResponse() {}
