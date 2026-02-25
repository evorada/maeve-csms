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

	// QueryMeterValues retrieves meter values with advanced filtering and pagination.
	// Filters can be empty strings/zero values to skip that filter.
	QueryMeterValues(ctx context.Context, filter MeterValuesFilter) (*MeterValuesResult, error)
}

// MeterValuesFilter defines filtering and pagination options for meter values queries.
type MeterValuesFilter struct {
	ChargeStationId string  // Required
	ConnectorId     *int    // Optional: filter by connector (OCPP 1.6) or EVSE (OCPP 2.0.1)
	TransactionId   *string // Optional: filter by transaction
	StartTime       *string // Optional: ISO 8601 timestamp
	EndTime         *string // Optional: ISO 8601 timestamp
	Limit           int     // Max results per page (default: 100)
	Offset          int     // Number of results to skip
}

// MeterValuesResult contains paginated meter values query results.
type MeterValuesResult struct {
	MeterValues []StoredMeterValue
	Total       int // Total matching records (before pagination)
}

// StoredMeterValue extends MeterValue with metadata about when and where it was stored.
type StoredMeterValue struct {
	ChargeStationId string
	EvseId          int
	TransactionId   string // May be empty if not associated with a transaction
	MeterValue      MeterValue
}
