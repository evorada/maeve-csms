// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// StoreMeterValues stores meter values received from a charge station.
func (s *Store) StoreMeterValues(ctx context.Context, chargeStationId string, evseId int, transactionId string, meterValues []store.MeterValue) error {
	for _, mv := range meterValues {
		// Marshal sampled values to JSON
		sampledValuesJSON, err := json.Marshal(mv.SampledValues)
		if err != nil {
			return err
		}

		// Parse timestamp
		timestamp, err := time.Parse(time.RFC3339, mv.Timestamp)
		if err != nil {
			return err
		}

		// Prepare transaction ID (may be empty/null)
		var txIdParam pgtype.Text
		if transactionId != "" {
			txIdParam = pgtype.Text{String: transactionId, Valid: true}
		}

		// Store the meter value
		err = s.q.StoreMeterValue(ctx, StoreMeterValueParams{
			ChargeStationID: chargeStationId,
			EvseID:          int32(evseId),
			TransactionID:   txIdParam,
			Timestamp:       pgtype.Timestamp{Time: timestamp, Valid: true},
			SampledValues:   sampledValuesJSON,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// GetMeterValues retrieves meter values for a specific charge station and EVSE.
func (s *Store) GetMeterValues(ctx context.Context, chargeStationId string, evseId int, limit int) ([]store.StoredMeterValue, error) {
	var rows []MeterValue
	var err error

	if limit > 0 {
		rows, err = s.q.GetMeterValuesByStationAndEvse(ctx, GetMeterValuesByStationAndEvseParams{
			ChargeStationID: chargeStationId,
			EvseID:          int32(evseId),
			Limit:           int32(limit),
		})
	} else {
		rows, err = s.q.GetAllMeterValuesByStation(ctx, GetAllMeterValuesByStationParams{
			ChargeStationID: chargeStationId,
			EvseID:          int32(evseId),
		})
	}

	if err != nil {
		return nil, err
	}

	// Convert database rows to store.StoredMeterValue
	result := make([]store.StoredMeterValue, len(rows))
	for i, row := range rows {
		// Unmarshal sampled values
		var sampledValues []store.SampledValue
		if err := json.Unmarshal(row.SampledValues, &sampledValues); err != nil {
			return nil, err
		}

		// Extract transaction ID (may be null)
		transactionId := ""
		if row.TransactionID.Valid {
			transactionId = row.TransactionID.String
		}

		result[i] = store.StoredMeterValue{
			ChargeStationId: row.ChargeStationID,
			EvseId:          int(row.EvseID),
			TransactionId:   transactionId,
			MeterValue: store.MeterValue{
				SampledValues: sampledValues,
				Timestamp:     row.Timestamp.Time.Format(time.RFC3339),
			},
		}
	}

	return result, nil
}
