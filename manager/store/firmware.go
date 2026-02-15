// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"time"
)

// FirmwareUpdateStatus represents the status of firmware being downloaded/installed on a charge station
type FirmwareUpdateStatusType string

var (
	FirmwareUpdateStatusDownloading               FirmwareUpdateStatusType = "Downloading"
	FirmwareUpdateStatusDownloaded                FirmwareUpdateStatusType = "Downloaded"
	FirmwareUpdateStatusDownloadFailed            FirmwareUpdateStatusType = "DownloadFailed"
	FirmwareUpdateStatusInstallationFailed        FirmwareUpdateStatusType = "InstallationFailed"
	FirmwareUpdateStatusInstalling                FirmwareUpdateStatusType = "Installing"
	FirmwareUpdateStatusInstalled                 FirmwareUpdateStatusType = "Installed"
	FirmwareUpdateStatusIdle                      FirmwareUpdateStatusType = "Idle"
	FirmwareUpdateStatusDownloadScheduled         FirmwareUpdateStatusType = "DownloadScheduled"
	FirmwareUpdateStatusDownloadPaused            FirmwareUpdateStatusType = "DownloadPaused"
	FirmwareUpdateStatusInstallRebooting          FirmwareUpdateStatusType = "InstallRebooting"
	FirmwareUpdateStatusInstallScheduled          FirmwareUpdateStatusType = "InstallScheduled"
	FirmwareUpdateStatusInstallVerificationFailed FirmwareUpdateStatusType = "InstallVerificationFailed"
	FirmwareUpdateStatusInvalidSignature          FirmwareUpdateStatusType = "InvalidSignature"
	FirmwareUpdateStatusSignatureVerified         FirmwareUpdateStatusType = "SignatureVerified"
)

// FirmwareUpdateStatus tracks the firmware update status for a charge station
type FirmwareUpdateStatus struct {
	ChargeStationId string
	Status          FirmwareUpdateStatusType
	Location        string
	RetrieveDate    time.Time
	RetryCount      int
	UpdatedAt       time.Time
}

// DiagnosticsStatusType represents the status of diagnostics upload
type DiagnosticsStatusType string

var (
	DiagnosticsStatusIdle         DiagnosticsStatusType = "Idle"
	DiagnosticsStatusUploaded     DiagnosticsStatusType = "Uploaded"
	DiagnosticsStatusUploadFailed DiagnosticsStatusType = "UploadFailed"
	DiagnosticsStatusUploading    DiagnosticsStatusType = "Uploading"
)

// DiagnosticsStatus tracks the diagnostics upload status for a charge station
type DiagnosticsStatus struct {
	ChargeStationId string
	Status          DiagnosticsStatusType
	Location        string
	UpdatedAt       time.Time
}

// PublishFirmwareStatusType represents the status of a firmware publishing operation
type PublishFirmwareStatusType string

var (
	PublishFirmwareStatusIdle              PublishFirmwareStatusType = "Idle"
	PublishFirmwareStatusAccepted          PublishFirmwareStatusType = "Accepted"
	PublishFirmwareStatusRejected          PublishFirmwareStatusType = "Rejected"
	PublishFirmwareStatusDownloadScheduled PublishFirmwareStatusType = "DownloadScheduled"
	PublishFirmwareStatusDownloading       PublishFirmwareStatusType = "Downloading"
	PublishFirmwareStatusDownloaded        PublishFirmwareStatusType = "Downloaded"
	PublishFirmwareStatusPublished         PublishFirmwareStatusType = "Published"
	PublishFirmwareStatusDownloadFailed    PublishFirmwareStatusType = "DownloadFailed"
	PublishFirmwareStatusDownloadPaused    PublishFirmwareStatusType = "DownloadPaused"
	PublishFirmwareStatusInvalidChecksum   PublishFirmwareStatusType = "InvalidChecksum"
	PublishFirmwareStatusChecksumVerified  PublishFirmwareStatusType = "ChecksumVerified"
	PublishFirmwareStatusPublishFailed     PublishFirmwareStatusType = "PublishFailed"
	PublishFirmwareStatusFailed            PublishFirmwareStatusType = "Failed"
)

// PublishFirmwareStatus tracks the publish firmware status for a charge station (Local Controller)
type PublishFirmwareStatus struct {
	ChargeStationId string
	Status          PublishFirmwareStatusType
	Location        string
	Checksum        string
	RequestId       int
	UpdatedAt       time.Time
}

// FirmwareStore defines the interface for firmware and diagnostics status tracking
type FirmwareStore interface {
	SetFirmwareUpdateStatus(ctx context.Context, chargeStationId string, status *FirmwareUpdateStatus) error
	GetFirmwareUpdateStatus(ctx context.Context, chargeStationId string) (*FirmwareUpdateStatus, error)
	SetDiagnosticsStatus(ctx context.Context, chargeStationId string, status *DiagnosticsStatus) error
	GetDiagnosticsStatus(ctx context.Context, chargeStationId string) (*DiagnosticsStatus, error)
	SetPublishFirmwareStatus(ctx context.Context, chargeStationId string, status *PublishFirmwareStatus) error
	GetPublishFirmwareStatus(ctx context.Context, chargeStationId string) (*PublishFirmwareStatus, error)
}
