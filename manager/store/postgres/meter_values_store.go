// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// StoreMeterValues stores meter values received from a charge station. They belong to
// the org of their transaction when it is known, else to the org of the charge station
// (see the StoreMeterValue query).
func (s *Store) StoreMeterValues(ctx context.Context, chargeStationId string, evseId int, transactionId string, meterValues []store.MeterValue) error {
	for _, mv := range meterValues {
		// Marshal sampled values to JSON
		sampledValuesJSON, err := json.Marshal(mv.SampledValues)
		if err != nil {
			return err
		}

		// Parse timestamp. The column has no time zone and pgx drops the zone of the
		// value it is given, so the instant is stored in UTC.
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
		err = s.writeQueries(ctx).StoreMeterValue(ctx, StoreMeterValueParams{
			ChargeStationID: chargeStationId,
			EvseID:          int32(evseId),
			TransactionID:   txIdParam,
			Timestamp:       pgtype.Timestamp{Time: timestamp.UTC(), Valid: true},
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
		rows, err = s.readQueries(ctx).GetMeterValuesByStationAndEvse(ctx, GetMeterValuesByStationAndEvseParams{
			ChargeStationID: chargeStationId,
			EvseID:          int32(evseId),
			Limit:           int32(limit),
		})
	} else {
		rows, err = s.readQueries(ctx).GetAllMeterValuesByStation(ctx, GetAllMeterValuesByStationParams{
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

// QueryMeterValues retrieves meter values with advanced filtering and pagination.
func (s *Store) QueryMeterValues(ctx context.Context, filter store.MeterValuesFilter) (*store.MeterValuesResult, error) {
	orgIds, err := orgIdArray(filter.OrgIds)
	if err != nil {
		return nil, fmt.Errorf("query meter values: %w", err)
	}
	params := QueryMeterValuesParams{
		ChargeStationID: filter.ChargeStationId,
		OrgIds:          orgIds,
		Limit:           int32(filter.Limit),
		Offset:          int32(filter.Offset),
	}

	// Optional filters: a parameter that is not set stays NULL, which the query reads as "no filter"
	if filter.ConnectorId != nil {
		params.ConnectorID = pgtype.Int4{Int32: int32(*filter.ConnectorId), Valid: true}
	}
	if filter.TransactionId != nil {
		params.TransactionID = pgtype.Text{String: *filter.TransactionId, Valid: true}
	}
	if filter.StartTime != nil {
		startTime, err := time.Parse(time.RFC3339, *filter.StartTime)
		if err != nil {
			return nil, err
		}
		params.StartTime = pgtype.Timestamp{Time: startTime.UTC(), Valid: true}
	}
	if filter.EndTime != nil {
		endTime, err := time.Parse(time.RFC3339, *filter.EndTime)
		if err != nil {
			return nil, err
		}
		params.EndTime = pgtype.Timestamp{Time: endTime.UTC(), Valid: true}
	}

	// Get paginated results
	rows, err := s.readQueries(ctx).QueryMeterValues(ctx, params)
	if err != nil {
		return nil, err
	}

	// Get total count for the same filters (without limit/offset)
	countParams := CountMeterValuesParams{
		ChargeStationID: params.ChargeStationID,
		OrgIds:          params.OrgIds,
		ConnectorID:     params.ConnectorID,
		TransactionID:   params.TransactionID,
		StartTime:       params.StartTime,
		EndTime:         params.EndTime,
	}
	total, err := s.readQueries(ctx).CountMeterValues(ctx, countParams)
	if err != nil {
		return nil, err
	}

	// Convert to store types
	result := make([]store.StoredMeterValue, len(rows))
	for i, row := range rows {
		var sampledValues []store.SampledValue
		if err := json.Unmarshal(row.SampledValues, &sampledValues); err != nil {
			return nil, err
		}

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

	return &store.MeterValuesResult{
		MeterValues: result,
		Total:       int(total),
	}, nil
}
