// SPDX-License-Identifier: Apache-2.0

package store

import "context"

// MeterValuesStore stores standalone meter values received via MeterValues messages.
// These are separate from transaction-associated meter values which are stored
// as part of TransactionEvent messages.
type MeterValuesStore interface {
	// StoreMeterValues stores meter values received from a charge station.
	// The transactionId can be empty if the meter values are not associated with a transaction.
	StoreMeterValues(ctx context.Context, chargeStationId string, evseId int, transactionId string, meterValues []MeterValue) error
	
	// GetMeterValues retrieves meter values for a specific charge station and EVSE.
	// If evseId is 0, it retrieves meter values for the main power meter.
	// Limit controls how many records to return (0 = all).
	GetMeterValues(ctx context.Context, chargeStationId string, evseId int, limit int) ([]StoredMeterValue, error)
}

// StoredMeterValue extends MeterValue with metadata about when and where it was stored.
type StoredMeterValue struct {
	ChargeStationId string
	EvseId          int
	TransactionId   string // May be empty if not associated with a transaction
	MeterValue      MeterValue
}
