// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// ChargeStationRegistryStore implementation. The record of a charge station is its row
// in charge_stations, which SetChargeStationAuth creates (state active by default); the
// runtime details are joined from charge_station_runtime.

func (s *Store) LookupChargeStation(ctx context.Context, stationKey string) (*store.ChargeStation, error) {
	row, err := s.readQueries(ctx).GetChargeStationRecord(ctx, stationKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup charge station %s: %w", stationKey, err)
	}
	return chargeStationFromRow(row)
}

func (s *Store) UpdateChargeStation(ctx context.Context, cs *store.ChargeStation) error {
	var locationId pgtype.UUID
	if cs.LocationId != nil {
		var ok bool
		if locationId, ok = assetId(*cs.LocationId); !ok {
			// not a UUID: no location has this id
			return fmt.Errorf("update charge station %s: location %s: %w", cs.StationKey, *cs.LocationId, store.ErrNotFound)
		}
	}
	maxCapacityKw := pgtype.Float8{}
	if cs.MaxCapacityKw != nil {
		maxCapacityKw = pgtype.Float8{Float64: *cs.MaxCapacityKw, Valid: true}
	}

	updatedAt, err := s.writeQueries(ctx).UpdateChargeStationRecord(ctx, UpdateChargeStationRecordParams{
		LocationID:      locationId,
		DisplayName:     toPgText(cs.DisplayName),
		MaxCapacityKw:   maxCapacityKw,
		AllowAnyToken:   cs.AllowAnyToken,
		ChargeStationID: cs.StationKey,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("update charge station %s: %w", cs.StationKey, store.ErrNotFound)
		}
		// a location that does not exist is a foreign key violation: ErrNotFound
		return assetWriteError(fmt.Sprintf("update charge station %s", cs.StationKey), err)
	}
	cs.UpdatedAt = fromPgTimestamptz(updatedAt)
	return nil
}

func (s *Store) SetChargeStationState(ctx context.Context, stationKey string, state store.StationState, reason *string) error {
	if !state.Valid() {
		return fmt.Errorf("set state of charge station %s: unknown state %q", stationKey, state)
	}
	rows, err := s.writeQueries(ctx).SetChargeStationState(ctx, SetChargeStationStateParams{
		State:           string(state),
		StateReason:     toPgText(reason),
		ChargeStationID: stationKey,
	})
	if err != nil {
		return fmt.Errorf("set state of charge station %s: %w", stationKey, err)
	}
	if rows == 0 {
		return fmt.Errorf("set state of charge station %s: %w", stationKey, store.ErrNotFound)
	}
	return nil
}

func (s *Store) ListChargeStations(ctx context.Context, filter store.ChargeStationFilter) ([]*store.ChargeStation, string, error) {
	afterKey, err := store.DecodeKeyCursor(filter.Page.Cursor)
	if err != nil {
		return nil, "", fmt.Errorf("list charge stations: %w", err)
	}
	orgIds := make([]pgtype.UUID, len(filter.OrgIds))
	for i, orgId := range filter.OrgIds {
		if orgIds[i], err = pgUUID(orgId); err != nil {
			return nil, "", fmt.Errorf("list charge stations: invalid org id %q: %w", orgId, err)
		}
	}
	if len(orgIds) == 0 {
		return []*store.ChargeStation{}, "", nil
	}
	var locationId pgtype.UUID
	if filter.LocationId != nil {
		var ok bool
		if locationId, ok = assetId(*filter.LocationId); !ok {
			// not a UUID: no station is at a location with this id
			return []*store.ChargeStation{}, "", nil
		}
	}
	limit := filter.Page.EffectiveLimit()
	// one more row than the page tells whether there is a next page
	rowLimit, err := safeIntToInt32(limit + 1)
	if err != nil {
		return nil, "", fmt.Errorf("list charge stations: %w", err)
	}
	states := make([]string, len(filter.States))
	for i, state := range filter.States {
		states[i] = string(state)
	}
	statuses := make([]string, len(filter.StatusAnyOf))
	for i, status := range filter.StatusAnyOf {
		statuses[i] = string(status)
	}
	query := pgtype.Text{}
	if filter.Query != nil {
		query = pgtype.Text{String: likeEscaper.Replace(*filter.Query), Valid: true}
	}

	rows, err := s.readQueries(ctx).ListChargeStationRecords(ctx, ListChargeStationRecordsParams{
		OrgIds:     orgIds,
		LocationID: locationId,
		States:     states,
		Vendor:     toPgText(filter.Vendor),
		Model:      toPgText(filter.Model),
		Connected:  pgBool(filter.Connected),
		Statuses:   statuses,
		Query:      query,
		AfterKey:   afterKey,
		RowLimit:   rowLimit,
	})
	if err != nil {
		return nil, "", fmt.Errorf("list charge stations: %w", err)
	}

	nextCursor := ""
	if len(rows) > limit {
		rows = rows[:limit]
		nextCursor = store.EncodeKeyCursor(rows[limit-1].ChargeStationID)
	}
	stations := make([]*store.ChargeStation, len(rows))
	for i, row := range rows {
		if stations[i], err = chargeStationFromRow(GetChargeStationRecordRow(row)); err != nil {
			return nil, "", err
		}
	}
	return stations, nextCursor, nil
}

func (s *Store) CountChargeStations(ctx context.Context, orgId string, states ...store.StationState) (int, error) {
	pgOrgId, err := pgUUID(orgId)
	if err != nil {
		return 0, fmt.Errorf("count charge stations: invalid org id %q: %w", orgId, err)
	}
	stateNames := make([]string, len(states))
	for i, state := range states {
		stateNames[i] = string(state)
	}
	count, err := s.readQueries(ctx).CountChargeStationRecords(ctx, CountChargeStationRecordsParams{
		OrgID:  pgOrgId,
		States: stateNames,
	})
	if err != nil {
		return 0, fmt.Errorf("count charge stations of org %s: %w", orgId, err)
	}
	return int(count), nil
}

func chargeStationFromRow(row GetChargeStationRecordRow) (*store.ChargeStation, error) {
	cs := &store.ChargeStation{
		StationKey:     row.ChargeStationID,
		OrgId:          uuidString(row.OrgID),
		LocationId:     optionalUUIDString(row.LocationID),
		DisplayName:    fromPgText(row.DisplayName),
		AllowAnyToken:  row.AllowAnyToken,
		State:          store.StationState(row.State),
		StateChangedAt: fromPgTimestamptz(row.StateChangedAt),
		StateReason:    fromPgText(row.StateReason),
		CreatedAt:      fromPgTimestamptz(row.CreatedAt),
		UpdatedAt:      fromPgTimestamptz(row.UpdatedAt),
	}
	if row.MaxCapacityKw.Valid {
		kw, err := row.MaxCapacityKw.Float64Value()
		if err != nil {
			return nil, fmt.Errorf("charge station %s: max capacity: %w", row.ChargeStationID, err)
		}
		cs.MaxCapacityKw = &kw.Float64
	}
	// the OCPP version is set in every runtime row: without it the station has not booted
	if row.OcppVersion.Valid {
		cs.Runtime = &store.ChargeStationRuntimeDetails{
			OcppVersion:       row.OcppVersion.String,
			FirmwareVersion:   fromPgText(row.FirmwareVersion),
			Model:             fromPgText(row.Model),
			Vendor:            fromPgText(row.Vendor),
			SerialNumber:      fromPgText(row.SerialNumber),
			Iccid:             fromPgText(row.Iccid),
			Imsi:              fromPgText(row.Imsi),
			MeterSerialNumber: fromPgText(row.MeterSerialNumber),
			MeterType:         fromPgText(row.MeterType),
			LastBootAt:        fromNullTimestamptz(row.LastBootAt),
		}
	}
	return cs, nil
}
