// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"time"
)

// ConnectorStatusType represents the status of a connector
type ConnectorStatusType string

var (
	ConnectorStatusAvailable     ConnectorStatusType = "Available"
	ConnectorStatusCharging      ConnectorStatusType = "Charging"
	ConnectorStatusFaulted       ConnectorStatusType = "Faulted"
	ConnectorStatusFinishing     ConnectorStatusType = "Finishing"
	ConnectorStatusPreparing     ConnectorStatusType = "Preparing"
	ConnectorStatusReserved      ConnectorStatusType = "Reserved"
	ConnectorStatusSuspendedEV   ConnectorStatusType = "SuspendedEV"
	ConnectorStatusSuspendedEVSE ConnectorStatusType = "SuspendedEVSE"
	ConnectorStatusUnavailable   ConnectorStatusType = "Unavailable"
	ConnectorStatusOccupied      ConnectorStatusType = "Occupied"
)

// ConnectorErrorCode represents the error code from StatusNotification
type ConnectorErrorCode string

var (
	ConnectorErrorCodeNoError              ConnectorErrorCode = "NoError"
	ConnectorErrorCodeConnectorLockFailure ConnectorErrorCode = "ConnectorLockFailure"
	ConnectorErrorCodeEVCommunicationError ConnectorErrorCode = "EVCommunicationError"
	ConnectorErrorCodeGroundFailure        ConnectorErrorCode = "GroundFailure"
	ConnectorErrorCodeHighTemperature      ConnectorErrorCode = "HighTemperature"
	ConnectorErrorCodeInternalError        ConnectorErrorCode = "InternalError"
	ConnectorErrorCodeLocalListConflict    ConnectorErrorCode = "LocalListConflict"
	ConnectorErrorCodeOtherError           ConnectorErrorCode = "OtherError"
	ConnectorErrorCodeOverCurrentFailure   ConnectorErrorCode = "OverCurrentFailure"
	ConnectorErrorCodeOverVoltage          ConnectorErrorCode = "OverVoltage"
	ConnectorErrorCodePowerMeterFailure    ConnectorErrorCode = "PowerMeterFailure"
	ConnectorErrorCodePowerSwitchFailure   ConnectorErrorCode = "PowerSwitchFailure"
	ConnectorErrorCodeReaderFailure        ConnectorErrorCode = "ReaderFailure"
	ConnectorErrorCodeResetFailure         ConnectorErrorCode = "ResetFailure"
	ConnectorErrorCodeUnderVoltage         ConnectorErrorCode = "UnderVoltage"
	ConnectorErrorCodeWeakSignal           ConnectorErrorCode = "WeakSignal"
)

// ConnectorStatus represents the current status of a specific connector
type ConnectorStatus struct {
	ChargeStationId      string
	ConnectorId          int
	Status               ConnectorStatusType
	ErrorCode            ConnectorErrorCode
	Info                 *string
	Timestamp            *time.Time
	VendorErrorCode      *string
	VendorId             *string
	CurrentTransactionId *string
	UpdatedAt            time.Time
}

// ChargeStationStatus represents the overall status of a charge station
type ChargeStationStatus struct {
	ChargeStationId string
	Connected       bool
	LastHeartbeat   *time.Time
	FirmwareVersion *string
	Model           *string
	Vendor          *string
	SerialNumber    *string
	UpdatedAt       time.Time
}

// StatusStore defines the interface for charge station and connector status tracking
type StatusStore interface {
	SetConnectorStatus(ctx context.Context, chargeStationId string, connectorId int, status *ConnectorStatus) error
	GetConnectorStatus(ctx context.Context, chargeStationId string, connectorId int) (*ConnectorStatus, error)
	ListConnectorStatuses(ctx context.Context, chargeStationId string) ([]*ConnectorStatus, error)
	SetChargeStationStatus(ctx context.Context, chargeStationId string, status *ChargeStationStatus) error
	GetChargeStationStatus(ctx context.Context, chargeStationId string) (*ChargeStationStatus, error)
	UpdateHeartbeat(ctx context.Context, chargeStationId string, timestamp time.Time) error
}
