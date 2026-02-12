// SPDX-License-Identifier: Apache-2.0

package ocpp16

type FirmwareStatusNotificationJsonStatus string

const (
	FirmwareStatusNotificationJsonStatusDownloaded         FirmwareStatusNotificationJsonStatus = "Downloaded"
	FirmwareStatusNotificationJsonStatusDownloadFailed     FirmwareStatusNotificationJsonStatus = "DownloadFailed"
	FirmwareStatusNotificationJsonStatusDownloading        FirmwareStatusNotificationJsonStatus = "Downloading"
	FirmwareStatusNotificationJsonStatusIdle               FirmwareStatusNotificationJsonStatus = "Idle"
	FirmwareStatusNotificationJsonStatusInstallationFailed FirmwareStatusNotificationJsonStatus = "InstallationFailed"
	FirmwareStatusNotificationJsonStatusInstalling         FirmwareStatusNotificationJsonStatus = "Installing"
	FirmwareStatusNotificationJsonStatusInstalled          FirmwareStatusNotificationJsonStatus = "Installed"
)

type FirmwareStatusNotificationJson struct {
	Status FirmwareStatusNotificationJsonStatus `json:"status" validate:"required"`
}

func (f *FirmwareStatusNotificationJson) IsRequest() {}
