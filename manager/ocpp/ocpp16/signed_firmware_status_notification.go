// SPDX-License-Identifier: Apache-2.0

package ocpp16

type SignedFirmwareStatusNotificationJsonStatus string

const (
	SignedFirmwareStatusNotificationJsonStatusDownloaded                SignedFirmwareStatusNotificationJsonStatus = "Downloaded"
	SignedFirmwareStatusNotificationJsonStatusDownloadFailed            SignedFirmwareStatusNotificationJsonStatus = "DownloadFailed"
	SignedFirmwareStatusNotificationJsonStatusDownloading               SignedFirmwareStatusNotificationJsonStatus = "Downloading"
	SignedFirmwareStatusNotificationJsonStatusDownloadScheduled         SignedFirmwareStatusNotificationJsonStatus = "DownloadScheduled"
	SignedFirmwareStatusNotificationJsonStatusDownloadPaused            SignedFirmwareStatusNotificationJsonStatus = "DownloadPaused"
	SignedFirmwareStatusNotificationJsonStatusIdle                      SignedFirmwareStatusNotificationJsonStatus = "Idle"
	SignedFirmwareStatusNotificationJsonStatusInstallationFailed        SignedFirmwareStatusNotificationJsonStatus = "InstallationFailed"
	SignedFirmwareStatusNotificationJsonStatusInstalling                SignedFirmwareStatusNotificationJsonStatus = "Installing"
	SignedFirmwareStatusNotificationJsonStatusInstalled                 SignedFirmwareStatusNotificationJsonStatus = "Installed"
	SignedFirmwareStatusNotificationJsonStatusInstallRebooting          SignedFirmwareStatusNotificationJsonStatus = "InstallRebooting"
	SignedFirmwareStatusNotificationJsonStatusInstallScheduled          SignedFirmwareStatusNotificationJsonStatus = "InstallScheduled"
	SignedFirmwareStatusNotificationJsonStatusInstallVerificationFailed SignedFirmwareStatusNotificationJsonStatus = "InstallVerificationFailed"
	SignedFirmwareStatusNotificationJsonStatusInvalidSignature          SignedFirmwareStatusNotificationJsonStatus = "InvalidSignature"
	SignedFirmwareStatusNotificationJsonStatusSignatureVerified         SignedFirmwareStatusNotificationJsonStatus = "SignatureVerified"
)

type SignedFirmwareStatusNotificationJson struct {
	Status    SignedFirmwareStatusNotificationJsonStatus `json:"status" validate:"required"`
	RequestId *int                                       `json:"requestId,omitempty" yaml:"requestId,omitempty" mapstructure:"requestId,omitempty"`
}

func (f *SignedFirmwareStatusNotificationJson) IsRequest() {}
