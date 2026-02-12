// SPDX-License-Identifier: Apache-2.0

package ocpp16

type DeleteCertificateResponseJsonStatus string

type DeleteCertificateResponseJson struct {
	// Status corresponds to the JSON schema field "status".
	Status DeleteCertificateResponseJsonStatus `json:"status" yaml:"status" mapstructure:"status"`
}

const DeleteCertificateResponseJsonStatusAccepted DeleteCertificateResponseJsonStatus = "Accepted"
const DeleteCertificateResponseJsonStatusFailed DeleteCertificateResponseJsonStatus = "Failed"
const DeleteCertificateResponseJsonStatusNotFound DeleteCertificateResponseJsonStatus = "NotFound"

func (*DeleteCertificateResponseJson) IsResponse() {}
