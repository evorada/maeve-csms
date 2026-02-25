// SPDX-License-Identifier: Apache-2.0

package store

type Engine interface {
	ChargeStationAuthStore
	ChargeStationSettingsStore
	ChargeStationRuntimeDetailsStore
	ChargeStationInstallCertificatesStore
	ChargeStationTriggerMessageStore
	ChargeStationDataTransferStore
	ChargeStationClearCacheStore
	ChargeStationChangeAvailabilityStore
	TokenStore
	TransactionStore
	CertificateStore
	OcpiStore
	LocationStore
	ChargingProfileStore
	FirmwareStore
	FirmwareUpdateRequestStore
	LocalAuthListStore
	ReservationStore
	MeterValuesStore
	DisplayMessageStore
	ResetRequestStore
	UnlockConnectorRequestStore
}
