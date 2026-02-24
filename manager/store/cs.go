// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"time"
)

type SecurityProfile int8

const (
	UnsecuredTransportWithBasicAuth SecurityProfile = iota
	TLSWithBasicAuth
	TLSWithClientSideCertificates
)

type ChargeStationAuth struct {
	SecurityProfile        SecurityProfile
	Base64SHA256Password   string
	InvalidUsernameAllowed bool
}

type ChargeStationAuthStore interface {
	SetChargeStationAuth(ctx context.Context, chargeStationId string, auth *ChargeStationAuth) error
	LookupChargeStationAuth(ctx context.Context, chargeStationId string) (*ChargeStationAuth, error)
}

type ChargeStationSettingStatus string

var (
	ChargeStationSettingStatusPending        ChargeStationSettingStatus = "Pending"
	ChargeStationSettingStatusAccepted       ChargeStationSettingStatus = "Accepted"
	ChargeStationSettingStatusRejected       ChargeStationSettingStatus = "Rejected"
	ChargeStationSettingStatusRebootRequired ChargeStationSettingStatus = "RebootRequired"
	ChargeStationSettingStatusNotSupported   ChargeStationSettingStatus = "NotSupported"
)

type ChargeStationSetting struct {
	Value     string
	Status    ChargeStationSettingStatus
	SendAfter time.Time
}

type ChargeStationSettings struct {
	ChargeStationId string
	Settings        map[string]*ChargeStationSetting
}

type ChargeStationSettingsStore interface {
	UpdateChargeStationSettings(ctx context.Context, chargeStationId string, settings *ChargeStationSettings) error
	LookupChargeStationSettings(ctx context.Context, chargeStationId string) (*ChargeStationSettings, error)
	ListChargeStationSettings(ctx context.Context, pageSize int, previousChargeStationId string) ([]*ChargeStationSettings, error)
	DeleteChargeStationSettings(ctx context.Context, chargeStationId string) error
}

type ChargeStationRuntimeDetails struct {
	OcppVersion string
}

type ChargeStationRuntimeDetailsStore interface {
	SetChargeStationRuntimeDetails(ctx context.Context, chargeStationId string, details *ChargeStationRuntimeDetails) error
	LookupChargeStationRuntimeDetails(ctx context.Context, chargeStationId string) (*ChargeStationRuntimeDetails, error)
}

type CertificateType string

var (
	CertificateTypeChargeStation CertificateType = "ChargeStation"
	CertificateTypeEVCC          CertificateType = "EVCC"
	CertificateTypeV2G           CertificateType = "V2G"
	CertificateTypeMO            CertificateType = "MO"
	CertificateTypeMF            CertificateType = "MF"
	CertificateTypeCSMS          CertificateType = "CSMS"
)

type CertificateInstallationStatus string

var (
	CertificateInstallationPending  CertificateInstallationStatus = "Pending"
	CertificateInstallationAccepted CertificateInstallationStatus = "Accepted"
	CertificateInstallationRejected CertificateInstallationStatus = "Rejected"
)

type ChargeStationInstallCertificate struct {
	CertificateType               CertificateType
	CertificateId                 string
	CertificateData               string
	CertificateInstallationStatus CertificateInstallationStatus
	SendAfter                     time.Time
}

type ChargeStationInstallCertificates struct {
	ChargeStationId string
	Certificates    []*ChargeStationInstallCertificate
}

type ChargeStationInstallCertificatesStore interface {
	UpdateChargeStationInstallCertificates(ctx context.Context, chargeStationId string, certificates *ChargeStationInstallCertificates) error
	LookupChargeStationInstallCertificates(ctx context.Context, chargeStationId string) (*ChargeStationInstallCertificates, error)
	ListChargeStationInstallCertificates(ctx context.Context, pageSize int, previousChargeStationId string) ([]*ChargeStationInstallCertificates, error)
}

type TriggerStatus string

var (
	TriggerStatusPending        TriggerStatus = "Pending"
	TriggerStatusAccepted       TriggerStatus = "Accepted"
	TriggerStatusRejected       TriggerStatus = "Rejected"
	TriggerStatusNotImplemented TriggerStatus = "NotImplemented"
)

type TriggerMessage string

var (
	TriggerMessageBootNotification                  TriggerMessage = "BootNotification"
	TriggerMessageHeartbeat                         TriggerMessage = "Heartbeat"
	TriggerMessageStatusNotification                TriggerMessage = "StatusNotification"
	TriggerMessageFirmwareStatusNotification        TriggerMessage = "FirmwareStatusNotification"
	TriggerMessageDiagnosticStatusNotification      TriggerMessage = "DiagnosticStatusNotification"
	TriggerMessageMeterValues                       TriggerMessage = "MeterValues"
	TriggerMessageSignChargingStationCertificate    TriggerMessage = "SignChargingStationCertificate"
	TriggerMessageSignV2GCertificate                TriggerMessage = "SignV2GCertificate"
	TriggerMessageSignCombinedCertificate           TriggerMessage = "SignCombinedCertificate"
	TriggerMessagePublishFirmwareStatusNotification TriggerMessage = "PublishFirmwareStatusNotification"
)

type ChargeStationTriggerMessage struct {
	ChargeStationId string
	TriggerMessage  TriggerMessage
	TriggerStatus   TriggerStatus
	SendAfter       time.Time
}

type ChargeStationTriggerMessageStore interface {
	SetChargeStationTriggerMessage(ctx context.Context, chargeStationId string, triggerMessage *ChargeStationTriggerMessage) error
	DeleteChargeStationTriggerMessage(ctx context.Context, chargeStationId string) error
	LookupChargeStationTriggerMessage(ctx context.Context, chargeStationId string) (*ChargeStationTriggerMessage, error)
	ListChargeStationTriggerMessages(ctx context.Context, pageSize int, previousChargeStationId string) ([]*ChargeStationTriggerMessage, error)
}

// DataTransferStatus represents the status of a data transfer request
type DataTransferStatus string

var (
	DataTransferStatusPending          DataTransferStatus = "Pending"
	DataTransferStatusAccepted         DataTransferStatus = "Accepted"
	DataTransferStatusRejected         DataTransferStatus = "Rejected"
	DataTransferStatusUnknownMessageId DataTransferStatus = "UnknownMessageId"
	DataTransferStatusUnknownVendorId  DataTransferStatus = "UnknownVendorId"
)

// ChargeStationDataTransfer represents a pending data transfer request
type ChargeStationDataTransfer struct {
	ChargeStationId string
	VendorId        string
	MessageId       *string
	Data            *string
	Status          DataTransferStatus
	ResponseData    *string
	SendAfter       time.Time
}

type ChargeStationDataTransferStore interface {
	SetChargeStationDataTransfer(ctx context.Context, chargeStationId string, dataTransfer *ChargeStationDataTransfer) error
	LookupChargeStationDataTransfer(ctx context.Context, chargeStationId string) (*ChargeStationDataTransfer, error)
	ListChargeStationDataTransfers(ctx context.Context, pageSize int, previousChargeStationId string) ([]*ChargeStationDataTransfer, error)
	DeleteChargeStationDataTransfer(ctx context.Context, chargeStationId string) error
}

// ClearCacheStatus represents the status of a clear cache request
type ClearCacheStatus string

var (
	ClearCacheStatusPending  ClearCacheStatus = "Pending"
	ClearCacheStatusAccepted ClearCacheStatus = "Accepted"
	ClearCacheStatusRejected ClearCacheStatus = "Rejected"
)

// ChargeStationClearCache represents a pending clear cache request
type ChargeStationClearCache struct {
	ChargeStationId string
	Status          ClearCacheStatus
	SendAfter       time.Time
}

type ChargeStationClearCacheStore interface {
	SetChargeStationClearCache(ctx context.Context, chargeStationId string, clearCache *ChargeStationClearCache) error
	LookupChargeStationClearCache(ctx context.Context, chargeStationId string) (*ChargeStationClearCache, error)
	ListChargeStationClearCaches(ctx context.Context, pageSize int, previousChargeStationId string) ([]*ChargeStationClearCache, error)
	DeleteChargeStationClearCache(ctx context.Context, chargeStationId string) error
}

// AvailabilityType represents the requested availability state
type AvailabilityType string

var (
	AvailabilityTypeOperative   AvailabilityType = "Operative"
	AvailabilityTypeInoperative AvailabilityType = "Inoperative"
)

// AvailabilityStatus represents the status of a change availability request
type AvailabilityStatus string

var (
	AvailabilityStatusPending   AvailabilityStatus = "Pending"
	AvailabilityStatusAccepted  AvailabilityStatus = "Accepted"
	AvailabilityStatusRejected  AvailabilityStatus = "Rejected"
	AvailabilityStatusScheduled AvailabilityStatus = "Scheduled"
)

// ChargeStationChangeAvailability represents a pending change availability request
type ChargeStationChangeAvailability struct {
	ChargeStationId string
	ConnectorId     *int // nil or 0 = entire station
	EvseId          *int // OCPP 2.0.1 only
	Type            AvailabilityType
	Status          AvailabilityStatus
	SendAfter       time.Time
}

type ChargeStationChangeAvailabilityStore interface {
	SetChargeStationChangeAvailability(ctx context.Context, chargeStationId string, changeAvailability *ChargeStationChangeAvailability) error
	LookupChargeStationChangeAvailability(ctx context.Context, chargeStationId string) (*ChargeStationChangeAvailability, error)
	ListChargeStationChangeAvailabilities(ctx context.Context, pageSize int, previousChargeStationId string) ([]*ChargeStationChangeAvailability, error)
	DeleteChargeStationChangeAvailability(ctx context.Context, chargeStationId string) error
}
