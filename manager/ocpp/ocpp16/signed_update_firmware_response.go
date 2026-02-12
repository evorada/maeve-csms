// SPDX-License-Identifier: Apache-2.0

package ocpp16

type SignedUpdateFirmwareResponseJsonStatus string

const (
	SignedUpdateFirmwareResponseJsonStatusAccepted           SignedUpdateFirmwareResponseJsonStatus = "Accepted"
	SignedUpdateFirmwareResponseJsonStatusRejected           SignedUpdateFirmwareResponseJsonStatus = "Rejected"
	SignedUpdateFirmwareResponseJsonStatusAcceptedCanceled   SignedUpdateFirmwareResponseJsonStatus = "AcceptedCanceled"
	SignedUpdateFirmwareResponseJsonStatusInvalidCertificate SignedUpdateFirmwareResponseJsonStatus = "InvalidCertificate"
	SignedUpdateFirmwareResponseJsonStatusRevokedCertificate SignedUpdateFirmwareResponseJsonStatus = "RevokedCertificate"
)

type SignedUpdateFirmwareResponseJson struct {
	// Status corresponds to the JSON schema field "status".
	Status SignedUpdateFirmwareResponseJsonStatus `json:"status" yaml:"status" mapstructure:"status"`
}

func (*SignedUpdateFirmwareResponseJson) IsResponse() {}
