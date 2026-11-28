// SPDX-License-Identifier: Apache-2.0

package rating

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// Values of the fields of a sampled value (the same in OCPP 1.6 and 2.0.1) that say that
// it is a reading of the energy register of the connector.
const (
	measurandEnergyRegister = "Energy.Active.Import.Register"
	locationOutlet          = "Outlet"
	contextTransactionEnd   = "Transaction.End"
	unitWh                  = "Wh"
	unitKWh                 = "kWh"
)

// meterValuesPageSize is the number of meter values that LoadEnergyPoints asks the meter
// values store for at a time.
const meterValuesPageSize = 1000

// LoadEnergyPoints returns the readings of the energy register of the connector during a
// transaction, in the order of their time.
//
// They come from the meter values that are stored with the transaction (tx.MeterValues)
// and from the meter values that the charge station sent on their own with the id of the
// transaction (OCPP 1.6 MeterValues), read from mv. mv may be nil, and a store without
// such a query (store.ErrNotImplemented) adds nothing. The first reading is the meter
// start at the start of the transaction, when both are known.
//
// A sampled value is a reading when its measurand is Energy.Active.Import.Register (or
// absent: that is the default of OCPP), it is not of one phase, and it was measured at
// the outlet (or says nothing about where). kWh is converted to Wh and the multiplier is
// applied; a value in another unit is not used.
//
// A value of the end of the transaction that is lower than the meter start is not used:
// it is not a reading, but the energy of the transaction, which the transactions of
// OCPP 1.6 were once stored with.
func LoadEnergyPoints(ctx context.Context, tx *store.Transaction, mv store.MeterValuesStore) ([]EnergyPoint, error) {
	var points []EnergyPoint
	if tx.StartedAt != nil && tx.MeterStartWh != nil {
		points = append(points, EnergyPoint{At: *tx.StartedAt, RegisterWh: float64(*tx.MeterStartWh)})
	}
	points = appendEnergyPoints(points, tx, tx.MeterValues)

	if mv != nil {
		sent, err := transactionMeterValues(ctx, mv, tx.ChargeStationId, tx.TransactionId)
		if err != nil {
			return nil, err
		}
		points = appendEnergyPoints(points, tx, sent)
	}

	sortPoints(points)
	return points, nil
}

// transactionMeterValues returns every meter value that the charge station sent on its
// own with the id of the transaction; none when the store has no such query.
func transactionMeterValues(ctx context.Context, mv store.MeterValuesStore, chargeStationId, transactionId string) ([]store.MeterValue, error) {
	filter := store.MeterValuesFilter{
		ChargeStationId: chargeStationId,
		TransactionId:   &transactionId,
		Limit:           meterValuesPageSize,
	}
	var values []store.MeterValue
	for {
		page, err := mv.QueryMeterValues(ctx, filter)
		if errors.Is(err, store.ErrNotImplemented) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("meter values of transaction %s/%s: %w", chargeStationId, transactionId, err)
		}
		if page == nil || len(page.MeterValues) == 0 {
			return values, nil
		}
		for _, stored := range page.MeterValues {
			values = append(values, stored.MeterValue)
		}
		filter.Offset += len(page.MeterValues)
		if filter.Offset >= page.Total {
			return values, nil
		}
	}
}

// appendEnergyPoints appends the readings of the energy register that the meter values
// hold. A meter value whose time cannot be read is passed over.
func appendEnergyPoints(points []EnergyPoint, tx *store.Transaction, meterValues []store.MeterValue) []EnergyPoint {
	for _, meterValue := range meterValues {
		at, err := time.Parse(time.RFC3339, meterValue.Timestamp)
		if err != nil {
			continue
		}
		for _, sv := range meterValue.SampledValues {
			wh, ok := registerWh(sv)
			if !ok {
				continue
			}
			if sv.Context != nil && *sv.Context == contextTransactionEnd && tx.MeterStartWh != nil && wh < float64(*tx.MeterStartWh) {
				// the energy of the transaction, not a reading of the register
				continue
			}
			points = append(points, EnergyPoint{At: at, RegisterWh: wh})
		}
	}
	return points
}

// registerWh returns the reading of the energy register of the connector that a sampled
// value holds, in Wh; ok is false for any other sampled value.
func registerWh(sv store.SampledValue) (wh float64, ok bool) {
	if sv.Measurand != nil && *sv.Measurand != measurandEnergyRegister {
		return 0, false
	}
	if sv.Phase != nil {
		return 0, false
	}
	if sv.Location != nil && *sv.Location != locationOutlet {
		return 0, false
	}

	wh = sv.Value
	if unit := sv.UnitOfMeasure; unit != nil {
		switch unit.Unit {
		case "", unitWh:
		case unitKWh:
			wh *= 1000
		default:
			return 0, false
		}
		wh *= math.Pow10(unit.Multipler)
	}
	if math.IsNaN(wh) || math.IsInf(wh, 0) {
		return 0, false
	}
	return wh, true
}
