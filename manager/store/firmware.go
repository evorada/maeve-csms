// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"time"
)

// FirmwareUpdateStatus represents the status of firmware being downloaded/installed on a charge station
type FirmwareUpdateStatusType string

var (
	FirmwareUpdateStatusDownloading        FirmwareUpdateStatusType = "Downloading"
	FirmwareUpdateStatusDownloaded         FirmwareUpdateStatusType = "Downloaded"
	FirmwareUpdateStatusInstallationFailed FirmwareUpdateStatusType = "InstallationFailed"
	FirmwareUpdateStatusInstalling         FirmwareUpdateStatusType = "Installing"
	FirmwareUpdateStatusInstalled          FirmwareUpdateStatusType = "Installed"
	FirmwareUpdateStatusIdle               FirmwareUpdateStatusType = "Idle"
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

// FirmwareStore defines the interface for firmware and diagnostics status tracking
type FirmwareStore interface {
	SetFirmwareUpdateStatus(ctx context.Context, chargeStationId string, status *FirmwareUpdateStatus) error
	GetFirmwareUpdateStatus(ctx context.Context, chargeStationId string) (*FirmwareUpdateStatus, error)
	SetDiagnosticsStatus(ctx context.Context, chargeStationId string, status *DiagnosticsStatus) error
	GetDiagnosticsStatus(ctx context.Context, chargeStationId string) (*DiagnosticsStatus, error)
}
