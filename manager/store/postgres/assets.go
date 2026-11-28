// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

const foreignKeyViolationCode = "23503"

// likeEscaper escapes the characters that a LIKE pattern gives a meaning, so that the
// query of a location list matches as plain text.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// assetWriteError maps the constraint violations of a create or an update: a unique key
// that is taken to ErrAlreadyExists, a reference to a record that does not exist to
// ErrNotFound.
func assetWriteError(what string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case uniqueViolationCode:
			return fmt.Errorf("%s: %s: %w", what, pgErr.ConstraintName, store.ErrAlreadyExists)
		case foreignKeyViolationCode:
			return fmt.Errorf("%s: %s: %w", what, pgErr.ConstraintName, store.ErrNotFound)
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolationCode
}

// newAssetId returns id as a pgtype.UUID, or a new UUIDv7 when id is empty.
func newAssetId(id string) (pgtype.UUID, error) {
	if id == "" {
		generated, err := uuid.NewV7()
		if err != nil {
			return pgtype.UUID{}, err
		}
		return pgtype.UUID{Bytes: generated, Valid: true}, nil
	}
	return pgUUID(id)
}

// assetId converts the id of an asset to pgtype.UUID; false when it is not a UUID, which
// no stored asset has.
func assetId(id string) (pgtype.UUID, bool) {
	pgId, err := pgUUID(id)
	return pgId, err == nil
}

// assetNumber converts an OCPP id to int32; false when it does not fit, which no stored
// asset has.
func assetNumber(n int) (int32, bool) {
	n32, err := safeIntToInt32(n)
	return n32, err == nil
}

func optionalUUID(id *string) (pgtype.UUID, error) {
	if id == nil {
		return pgtype.UUID{}, nil
	}
	return pgUUID(*id)
}

func optionalUUIDString(id pgtype.UUID) *string {
	if !id.Valid {
		return nil
	}
	s := uuidString(id)
	return &s
}

func pgBool(b *bool) pgtype.Bool {
	if b == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *b, Valid: true}
}

func boolFromPg(b pgtype.Bool) *bool {
	if !b.Valid {
		return nil
	}
	v := b.Bool
	return &v
}

func pgInt4(i *int32) pgtype.Int4 {
	if i == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *i, Valid: true}
}

func int32FromPg(i pgtype.Int4) *int32 {
	if !i.Valid {
		return nil
	}
	v := i.Int32
	return &v
}

// textArray returns the list to store in a TEXT[] NOT NULL column: pgx sends a nil
// slice as NULL.
func textArray(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func openingTimesDocument(openingTimes *store.OpeningTimes) ([]byte, error) {
	if openingTimes == nil {
		return nil, nil
	}
	return json.Marshal(openingTimes)
}

func (s *Store) CreateAssetLocation(ctx context.Context, loc *store.AssetLocation) error {
	id, err := newAssetId(loc.Id)
	if err != nil {
		return fmt.Errorf("create asset location: invalid id %q: %w", loc.Id, err)
	}
	orgId, err := pgUUID(loc.OrgId)
	if err != nil {
		return fmt.Errorf("create asset location: invalid org id %q: %w", loc.OrgId, err)
	}
	openingTimes, err := openingTimesDocument(loc.OpeningTimes)
	if err != nil {
		return fmt.Errorf("create asset location: opening times: %w", err)
	}
	ocpiId := loc.OcpiId
	if ocpiId == "" {
		ocpiId = uuidString(id)
	}

	row, err := s.writeQueries(ctx).CreateAssetLocation(ctx, CreateAssetLocationParams{
		ID:                 id,
		OrgID:              orgId,
		PartyRef:           loc.PartyRef,
		OcpiID:             ocpiId,
		Name:               toPgText(loc.Name),
		Address:            loc.Address,
		City:               loc.City,
		PostalCode:         toPgText(loc.PostalCode),
		State:              toPgText(loc.State),
		Country:            loc.Country,
		Latitude:           loc.Latitude,
		Longitude:          loc.Longitude,
		TimeZone:           loc.TimeZone,
		ParkingType:        toPgText(loc.ParkingType),
		Publish:            loc.Publish,
		OpeningTimes:       openingTimes,
		Facilities:         textArray(loc.Facilities),
		ChargingWhenClosed: pgBool(loc.ChargingWhenClosed),
		ExternalRefs:       textArray(loc.ExternalRefs),
	})
	if err != nil {
		return assetWriteError("create asset location", err)
	}
	stored, err := assetLocationFromRow(row)
	if err != nil {
		return err
	}
	*loc = *stored
	return nil
}

func (s *Store) UpdateAssetLocation(ctx context.Context, loc *store.AssetLocation) error {
	id, ok := assetId(loc.Id)
	if !ok {
		return fmt.Errorf("update asset location %s: %w", loc.Id, store.ErrNotFound)
	}
	openingTimes, err := openingTimesDocument(loc.OpeningTimes)
	if err != nil {
		return fmt.Errorf("update asset location %s: opening times: %w", loc.Id, err)
	}
	ocpiId := loc.OcpiId
	if ocpiId == "" {
		ocpiId = uuidString(id)
	}

	q := s.writeQueries(ctx)
	row, err := q.UpdateAssetLocation(ctx, UpdateAssetLocationParams{
		ID:                 id,
		Version:            loc.Version,
		PartyRef:           loc.PartyRef,
		OcpiID:             ocpiId,
		Name:               toPgText(loc.Name),
		Address:            loc.Address,
		City:               loc.City,
		PostalCode:         toPgText(loc.PostalCode),
		State:              toPgText(loc.State),
		Country:            loc.Country,
		Latitude:           loc.Latitude,
		Longitude:          loc.Longitude,
		TimeZone:           loc.TimeZone,
		ParkingType:        toPgText(loc.ParkingType),
		Publish:            loc.Publish,
		OpeningTimes:       openingTimes,
		Facilities:         textArray(loc.Facilities),
		ChargingWhenClosed: pgBool(loc.ChargingWhenClosed),
		ExternalRefs:       textArray(loc.ExternalRefs),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		_, getErr := q.GetAssetLocation(ctx, id)
		return versionMismatch("update asset location "+loc.Id, getErr)
	}
	if err != nil {
		return assetWriteError("update asset location "+loc.Id, err)
	}
	stored, err := assetLocationFromRow(row)
	if err != nil {
		return err
	}
	*loc = *stored
	return nil
}

// versionMismatch is the error of an optimistic update that changed no row, given the
// error of the lookup that followed it: ErrNotFound when the record is not there any
// more, ErrVersionConflict when it is.
func versionMismatch(what string, lookupErr error) error {
	switch {
	case errors.Is(lookupErr, pgx.ErrNoRows):
		return fmt.Errorf("%s: %w", what, store.ErrNotFound)
	case lookupErr != nil:
		return fmt.Errorf("%s: %w", what, lookupErr)
	default:
		return fmt.Errorf("%s: %w", what, store.ErrVersionConflict)
	}
}

func (s *Store) LookupAssetLocation(ctx context.Context, locationId string) (*store.AssetLocation, error) {
	id, ok := assetId(locationId)
	if !ok {
		return nil, nil
	}
	row, err := s.readQueries(ctx).GetAssetLocation(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup asset location %s: %w", locationId, err)
	}
	return assetLocationFromRow(row)
}

func (s *Store) LookupAssetLocationByOcpiId(ctx context.Context, partyRef, ocpiId string) (*store.AssetLocation, error) {
	row, err := s.readQueries(ctx).GetAssetLocationByOcpiId(ctx, GetAssetLocationByOcpiIdParams{PartyRef: partyRef, OcpiID: ocpiId})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup asset location %s/%s: %w", partyRef, ocpiId, err)
	}
	return assetLocationFromRow(row)
}

func (s *Store) ListAssetLocations(ctx context.Context, filter store.AssetLocationFilter) ([]*store.AssetLocation, string, error) {
	afterId, err := store.DecodeCursor(filter.Page.Cursor)
	if err != nil {
		return nil, "", fmt.Errorf("list asset locations: %w", err)
	}
	pgAfterId, err := pgUUID(afterId)
	if err != nil {
		return nil, "", fmt.Errorf("list asset locations: %w", err)
	}
	orgIds := make([]pgtype.UUID, len(filter.OrgIds))
	for i, orgId := range filter.OrgIds {
		if orgIds[i], err = pgUUID(orgId); err != nil {
			return nil, "", fmt.Errorf("list asset locations: invalid org id %q: %w", orgId, err)
		}
	}
	if len(orgIds) == 0 {
		return []*store.AssetLocation{}, "", nil
	}
	limit := filter.Page.EffectiveLimit()
	// one more row than the page tells whether there is a next page
	rowLimit, err := safeIntToInt32(limit + 1)
	if err != nil {
		return nil, "", fmt.Errorf("list asset locations: %w", err)
	}
	query := pgtype.Text{}
	if filter.Query != nil {
		query = pgtype.Text{String: likeEscaper.Replace(*filter.Query), Valid: true}
	}

	rows, err := s.readQueries(ctx).ListAssetLocations(ctx, ListAssetLocationsParams{
		OrgIds:   orgIds,
		PartyRef: toPgText(filter.PartyRef),
		Publish:  pgBool(filter.Publish),
		Query:    query,
		AfterID:  pgAfterId,
		RowLimit: rowLimit,
	})
	if err != nil {
		return nil, "", fmt.Errorf("list asset locations: %w", err)
	}

	nextCursor := ""
	if len(rows) > limit {
		rows = rows[:limit]
		nextCursor = store.EncodeCursor(uuidString(rows[limit-1].ID))
	}
	locations := make([]*store.AssetLocation, len(rows))
	for i, row := range rows {
		if locations[i], err = assetLocationFromRow(row); err != nil {
			return nil, "", err
		}
	}
	return locations, nextCursor, nil
}

// DeleteAssetLocation counts the EVSEs at the location first, so that a location in use
// does not make a statement fail in the transaction of the caller. The foreign key of
// the EVSEs still refuses the delete when an EVSE arrives in between; the savepoint then
// keeps the transaction of the caller usable.
func (s *Store) DeleteAssetLocation(ctx context.Context, locationId string) error {
	id, ok := assetId(locationId)
	if !ok {
		return nil
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("delete asset location %s: begin transaction: %w", locationId, err)
	}
	defer rollback(ctx, tx)
	q := New(tx)

	evses, err := q.CountAssetEvsesForLocation(ctx, id)
	if err != nil {
		return fmt.Errorf("delete asset location %s: count EVSEs: %w", locationId, err)
	}
	if evses > 0 {
		return fmt.Errorf("delete asset location %s: %d EVSEs: %w", locationId, evses, store.ErrInUse)
	}
	err = q.DeleteAssetLocation(ctx, id)
	if isForeignKeyViolation(err) {
		return fmt.Errorf("delete asset location %s: %w", locationId, store.ErrInUse)
	}
	if err != nil {
		return fmt.Errorf("delete asset location %s: %w", locationId, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("delete asset location %s: commit: %w", locationId, err)
	}
	return nil
}

func assetLocationFromRow(row AssetLocation) (*store.AssetLocation, error) {
	loc := &store.AssetLocation{
		Id:                 uuidString(row.ID),
		OrgId:              uuidString(row.OrgID),
		PartyRef:           row.PartyRef,
		OcpiId:             row.OcpiID,
		Name:               fromPgText(row.Name),
		Address:            row.Address,
		City:               row.City,
		PostalCode:         fromPgText(row.PostalCode),
		State:              fromPgText(row.State),
		Country:            row.Country,
		Latitude:           row.Latitude,
		Longitude:          row.Longitude,
		TimeZone:           row.TimeZone,
		ParkingType:        fromPgText(row.ParkingType),
		Publish:            row.Publish,
		Facilities:         textArray(row.Facilities),
		ChargingWhenClosed: boolFromPg(row.ChargingWhenClosed),
		ExternalRefs:       textArray(row.ExternalRefs),
		Version:            row.Version,
		CreatedAt:          fromPgTimestamptz(row.CreatedAt),
		UpdatedAt:          fromPgTimestamptz(row.UpdatedAt),
	}
	if len(row.OpeningTimes) > 0 {
		loc.OpeningTimes = &store.OpeningTimes{}
		if err := json.Unmarshal(row.OpeningTimes, loc.OpeningTimes); err != nil {
			return nil, fmt.Errorf("asset location %s: opening times: %w", loc.Id, err)
		}
	}
	return loc, nil
}

// CreateAssetEvse writes the EVSE and its connectors in a transaction of its own, or in a
// savepoint inside the transaction of the caller.
func (s *Store) CreateAssetEvse(ctx context.Context, evse *store.AssetEvse) error {
	id, err := newAssetId(evse.Id)
	if err != nil {
		return fmt.Errorf("create asset EVSE: invalid id %q: %w", evse.Id, err)
	}
	orgId, err := pgUUID(evse.OrgId)
	if err != nil {
		return fmt.Errorf("create asset EVSE: invalid org id %q: %w", evse.OrgId, err)
	}
	locationId, err := pgUUID(evse.LocationId)
	if err != nil {
		return fmt.Errorf("create asset EVSE: location %q: %w", evse.LocationId, store.ErrNotFound)
	}
	ocppEvseId, err := safeIntToInt32(evse.OcppEvseId)
	if err != nil {
		return fmt.Errorf("create asset EVSE: OCPP EVSE id: %w", err)
	}
	status := evse.Status
	if status == "" {
		status = store.AssetEvseStatusActive
	}

	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("create asset EVSE: begin transaction: %w", err)
	}
	defer rollback(ctx, tx)
	q := New(tx)

	row, err := q.CreateAssetEvse(ctx, CreateAssetEvseParams{
		ID:                id,
		OrgID:             orgId,
		LocationID:        locationId,
		StationKey:        evse.StationKey,
		OcppEvseID:        ocppEvseId,
		EvseID:            toPgText(evse.EvseId),
		PhysicalReference: toPgText(evse.PhysicalReference),
		Status:            string(status),
	})
	if err != nil {
		return assetWriteError("create asset EVSE", err)
	}
	connectors := make([]*store.AssetConnector, len(evse.Connectors))
	for i, c := range evse.Connectors {
		withEvse := *c
		withEvse.EvseUid = uuidString(row.ID)
		if connectors[i], err = createAssetConnector(ctx, q, &withEvse); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("create asset EVSE: commit: %w", err)
	}

	stored := assetEvseFromRow(row)
	for i, c := range evse.Connectors {
		*c = *connectors[i]
	}
	stored.Connectors = evse.Connectors
	if stored.Connectors == nil {
		stored.Connectors = []*store.AssetConnector{}
	}
	*evse = *stored
	return nil
}

func (s *Store) UpdateAssetEvse(ctx context.Context, evse *store.AssetEvse) error {
	id, ok := assetId(evse.Id)
	if !ok {
		return fmt.Errorf("update asset EVSE %s: %w", evse.Id, store.ErrNotFound)
	}
	locationId, err := pgUUID(evse.LocationId)
	if err != nil {
		return fmt.Errorf("update asset EVSE %s: location %q: %w", evse.Id, evse.LocationId, store.ErrNotFound)
	}
	ocppEvseId, err := safeIntToInt32(evse.OcppEvseId)
	if err != nil {
		return fmt.Errorf("update asset EVSE %s: OCPP EVSE id: %w", evse.Id, err)
	}
	status := evse.Status
	if status == "" {
		status = store.AssetEvseStatusActive
	}

	q := s.writeQueries(ctx)
	row, err := q.UpdateAssetEvse(ctx, UpdateAssetEvseParams{
		ID:                id,
		Version:           evse.Version,
		LocationID:        locationId,
		StationKey:        evse.StationKey,
		OcppEvseID:        ocppEvseId,
		EvseID:            toPgText(evse.EvseId),
		PhysicalReference: toPgText(evse.PhysicalReference),
		Status:            string(status),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		_, getErr := q.GetAssetEvse(ctx, id)
		return versionMismatch("update asset EVSE "+evse.Id, getErr)
	}
	if err != nil {
		return assetWriteError("update asset EVSE "+evse.Id, err)
	}
	connectors, err := listAssetConnectors(ctx, q, row.ID)
	if err != nil {
		return fmt.Errorf("update asset EVSE %s: %w", evse.Id, err)
	}
	stored := assetEvseFromRow(row)
	stored.Connectors = connectors[uuidString(row.ID)]
	*evse = *stored
	return nil
}

func (s *Store) LookupAssetEvse(ctx context.Context, evseUid string) (*store.AssetEvse, error) {
	id, ok := assetId(evseUid)
	if !ok {
		return nil, nil
	}
	q := s.readQueries(ctx)
	row, err := q.GetAssetEvse(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup asset EVSE %s: %w", evseUid, err)
	}
	evses, err := withConnectors(ctx, q, []AssetEvse{row})
	if err != nil {
		return nil, fmt.Errorf("lookup asset EVSE %s: %w", evseUid, err)
	}
	return evses[0], nil
}

func (s *Store) LookupAssetEvseByOcpp(ctx context.Context, stationKey string, ocppEvseId int) (*store.AssetEvse, error) {
	evseId, ok := assetNumber(ocppEvseId)
	if !ok {
		return nil, nil
	}
	q := s.readQueries(ctx)
	row, err := q.GetAssetEvseByOcpp(ctx, GetAssetEvseByOcppParams{StationKey: stationKey, OcppEvseID: evseId})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup asset EVSE %s/%d: %w", stationKey, ocppEvseId, err)
	}
	evses, err := withConnectors(ctx, q, []AssetEvse{row})
	if err != nil {
		return nil, fmt.Errorf("lookup asset EVSE %s/%d: %w", stationKey, ocppEvseId, err)
	}
	return evses[0], nil
}

func (s *Store) ListAssetEvsesForLocation(ctx context.Context, locationId string) ([]*store.AssetEvse, error) {
	id, ok := assetId(locationId)
	if !ok {
		return []*store.AssetEvse{}, nil
	}
	q := s.readQueries(ctx)
	rows, err := q.ListAssetEvsesByLocation(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list asset EVSEs of location %s: %w", locationId, err)
	}
	evses, err := withConnectors(ctx, q, rows)
	if err != nil {
		return nil, fmt.Errorf("list asset EVSEs of location %s: %w", locationId, err)
	}
	return evses, nil
}

func (s *Store) ListAssetEvsesForStation(ctx context.Context, stationKey string) ([]*store.AssetEvse, error) {
	q := s.readQueries(ctx)
	rows, err := q.ListAssetEvsesByStation(ctx, stationKey)
	if err != nil {
		return nil, fmt.Errorf("list asset EVSEs of station %s: %w", stationKey, err)
	}
	evses, err := withConnectors(ctx, q, rows)
	if err != nil {
		return nil, fmt.Errorf("list asset EVSEs of station %s: %w", stationKey, err)
	}
	return evses, nil
}

// DeleteAssetEvse deletes the EVSE; its connectors go with it (ON DELETE CASCADE).
func (s *Store) DeleteAssetEvse(ctx context.Context, evseUid string) error {
	id, ok := assetId(evseUid)
	if !ok {
		return nil
	}
	if err := s.writeQueries(ctx).DeleteAssetEvse(ctx, id); err != nil {
		return fmt.Errorf("delete asset EVSE %s: %w", evseUid, err)
	}
	return nil
}

// withConnectors converts EVSE rows and fills in their connectors, read with one query.
func withConnectors(ctx context.Context, q *Queries, rows []AssetEvse) ([]*store.AssetEvse, error) {
	ids := make([]pgtype.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	connectors, err := listAssetConnectors(ctx, q, ids...)
	if err != nil {
		return nil, err
	}
	evses := make([]*store.AssetEvse, len(rows))
	for i, row := range rows {
		evses[i] = assetEvseFromRow(row)
		evses[i].Connectors = connectors[evses[i].Id]
	}
	return evses, nil
}

// listAssetConnectors returns the connectors of the EVSEs by EVSE id, ordered by OCPP
// connector id; an EVSE without connectors has an empty list.
func listAssetConnectors(ctx context.Context, q *Queries, evseUids ...pgtype.UUID) (map[string][]*store.AssetConnector, error) {
	byEvse := make(map[string][]*store.AssetConnector, len(evseUids))
	for _, id := range evseUids {
		byEvse[uuidString(id)] = []*store.AssetConnector{}
	}
	if len(evseUids) == 0 {
		return byEvse, nil
	}
	rows, err := q.ListAssetConnectorsByEvses(ctx, evseUids)
	if err != nil {
		return nil, fmt.Errorf("list connectors: %w", err)
	}
	for _, row := range rows {
		c := assetConnectorFromRow(row)
		byEvse[c.EvseUid] = append(byEvse[c.EvseUid], c)
	}
	return byEvse, nil
}

func assetEvseFromRow(row AssetEvse) *store.AssetEvse {
	return &store.AssetEvse{
		Id:                uuidString(row.ID),
		OrgId:             uuidString(row.OrgID),
		LocationId:        uuidString(row.LocationID),
		StationKey:        row.StationKey,
		OcppEvseId:        int(row.OcppEvseID),
		EvseId:            fromPgText(row.EvseID),
		PhysicalReference: fromPgText(row.PhysicalReference),
		Status:            store.AssetEvseStatus(row.Status),
		Connectors:        []*store.AssetConnector{},
		Version:           row.Version,
		CreatedAt:         fromPgTimestamptz(row.CreatedAt),
		UpdatedAt:         fromPgTimestamptz(row.UpdatedAt),
	}
}

func (s *Store) CreateAssetConnector(ctx context.Context, c *store.AssetConnector) error {
	stored, err := createAssetConnector(ctx, s.writeQueries(ctx), c)
	if err != nil {
		return err
	}
	*c = *stored
	return nil
}

// createAssetConnector inserts a connector of the EVSE c.EvseUid, in the org of the EVSE.
func createAssetConnector(ctx context.Context, q *Queries, c *store.AssetConnector) (*store.AssetConnector, error) {
	id, err := newAssetId(c.Id)
	if err != nil {
		return nil, fmt.Errorf("create asset connector: invalid id %q: %w", c.Id, err)
	}
	evseUid, ok := assetId(c.EvseUid)
	if !ok {
		return nil, fmt.Errorf("create asset connector: EVSE %q: %w", c.EvseUid, store.ErrNotFound)
	}
	ocppConnectorId, err := safeIntToInt32(c.OcppConnectorId)
	if err != nil {
		return nil, fmt.Errorf("create asset connector: OCPP connector id: %w", err)
	}
	tariffRef, err := optionalUUID(c.TariffRef)
	if err != nil {
		return nil, fmt.Errorf("create asset connector: invalid tariff ref %q: %w", *c.TariffRef, err)
	}
	adhocTariffRef, err := optionalUUID(c.AdhocTariffRef)
	if err != nil {
		return nil, fmt.Errorf("create asset connector: invalid ad hoc tariff ref %q: %w", *c.AdhocTariffRef, err)
	}
	ocpiId := c.OcpiId
	if ocpiId == "" {
		ocpiId = strconv.Itoa(c.OcppConnectorId)
	}

	row, err := q.CreateAssetConnector(ctx, CreateAssetConnectorParams{
		ID:               id,
		EvseUid:          evseUid,
		OcppConnectorID:  ocppConnectorId,
		OcpiID:           ocpiId,
		Standard:         c.Standard,
		Format:           c.Format,
		PowerType:        c.PowerType,
		MaxVoltage:       c.MaxVoltage,
		MaxAmperage:      c.MaxAmperage,
		MaxElectricPower: pgInt4(c.MaxElectricPower),
		TariffRef:        tariffRef,
		AdhocTariffRef:   adhocTariffRef,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("create asset connector: EVSE %s: %w", c.EvseUid, store.ErrNotFound)
	}
	if err != nil {
		return nil, assetWriteError("create asset connector", err)
	}
	return assetConnectorFromRow(row), nil
}

func (s *Store) UpdateAssetConnector(ctx context.Context, c *store.AssetConnector) error {
	id, ok := assetId(c.Id)
	if !ok {
		return fmt.Errorf("update asset connector %s: %w", c.Id, store.ErrNotFound)
	}
	ocppConnectorId, err := safeIntToInt32(c.OcppConnectorId)
	if err != nil {
		return fmt.Errorf("update asset connector %s: OCPP connector id: %w", c.Id, err)
	}
	tariffRef, err := optionalUUID(c.TariffRef)
	if err != nil {
		return fmt.Errorf("update asset connector %s: invalid tariff ref %q: %w", c.Id, *c.TariffRef, err)
	}
	adhocTariffRef, err := optionalUUID(c.AdhocTariffRef)
	if err != nil {
		return fmt.Errorf("update asset connector %s: invalid ad hoc tariff ref %q: %w", c.Id, *c.AdhocTariffRef, err)
	}
	ocpiId := c.OcpiId
	if ocpiId == "" {
		ocpiId = strconv.Itoa(c.OcppConnectorId)
	}

	q := s.writeQueries(ctx)
	row, err := q.UpdateAssetConnector(ctx, UpdateAssetConnectorParams{
		ID:               id,
		Version:          c.Version,
		OcppConnectorID:  ocppConnectorId,
		OcpiID:           ocpiId,
		Standard:         c.Standard,
		Format:           c.Format,
		PowerType:        c.PowerType,
		MaxVoltage:       c.MaxVoltage,
		MaxAmperage:      c.MaxAmperage,
		MaxElectricPower: pgInt4(c.MaxElectricPower),
		TariffRef:        tariffRef,
		AdhocTariffRef:   adhocTariffRef,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		_, getErr := q.GetAssetConnector(ctx, id)
		return versionMismatch("update asset connector "+c.Id, getErr)
	}
	if err != nil {
		return assetWriteError("update asset connector "+c.Id, err)
	}
	*c = *assetConnectorFromRow(row)
	return nil
}

func (s *Store) LookupAssetConnector(ctx context.Context, connectorUid string) (*store.AssetConnector, error) {
	id, ok := assetId(connectorUid)
	if !ok {
		return nil, nil
	}
	row, err := s.readQueries(ctx).GetAssetConnector(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup asset connector %s: %w", connectorUid, err)
	}
	return assetConnectorFromRow(row), nil
}

func (s *Store) LookupAssetConnectorByOcpp(ctx context.Context, stationKey string, ocppEvseId, ocppConnectorId int) (*store.AssetConnector, error) {
	evseId, ok := assetNumber(ocppEvseId)
	if !ok {
		return nil, nil
	}
	connectorId, ok := assetNumber(ocppConnectorId)
	if !ok {
		return nil, nil
	}
	row, err := s.readQueries(ctx).GetAssetConnectorByOcpp(ctx, GetAssetConnectorByOcppParams{
		StationKey:      stationKey,
		OcppEvseID:      evseId,
		OcppConnectorID: connectorId,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup asset connector %s/%d/%d: %w", stationKey, ocppEvseId, ocppConnectorId, err)
	}
	return assetConnectorFromRow(row), nil
}

func (s *Store) DeleteAssetConnector(ctx context.Context, connectorUid string) error {
	id, ok := assetId(connectorUid)
	if !ok {
		return nil
	}
	if err := s.writeQueries(ctx).DeleteAssetConnector(ctx, id); err != nil {
		return fmt.Errorf("delete asset connector %s: %w", connectorUid, err)
	}
	return nil
}

func assetConnectorFromRow(row AssetConnector) *store.AssetConnector {
	return &store.AssetConnector{
		Id:               uuidString(row.ID),
		OrgId:            uuidString(row.OrgID),
		EvseUid:          uuidString(row.EvseUid),
		OcppConnectorId:  int(row.OcppConnectorID),
		OcpiId:           row.OcpiID,
		Standard:         row.Standard,
		Format:           row.Format,
		PowerType:        row.PowerType,
		MaxVoltage:       row.MaxVoltage,
		MaxAmperage:      row.MaxAmperage,
		MaxElectricPower: int32FromPg(row.MaxElectricPower),
		TariffRef:        optionalUUIDString(row.TariffRef),
		AdhocTariffRef:   optionalUUIDString(row.AdhocTariffRef),
		Version:          row.Version,
		CreatedAt:        fromPgTimestamptz(row.CreatedAt),
		UpdatedAt:        fromPgTimestamptz(row.UpdatedAt),
	}
}

// MoveChargeStationToLocation sets the location of the station and moves its EVSEs, in a
// transaction of its own or in a savepoint inside the transaction of the caller.
func (s *Store) MoveChargeStationToLocation(ctx context.Context, stationKey, locationId string) error {
	id, ok := assetId(locationId)
	if !ok {
		// not a UUID: no location has this id
		return fmt.Errorf("move charge station %s: location %s: %w", stationKey, locationId, store.ErrNotFound)
	}

	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("move charge station %s: begin transaction: %w", stationKey, err)
	}
	defer rollback(ctx, tx)
	q := New(tx)

	rows, err := q.SetStationLocation(ctx, SetStationLocationParams{LocationID: id, ChargeStationID: stationKey})
	if err != nil {
		// a location that does not exist is a foreign key violation: ErrNotFound
		return assetWriteError("move charge station "+stationKey, err)
	}
	if rows == 0 {
		return fmt.Errorf("move charge station %s: %w", stationKey, store.ErrNotFound)
	}
	if err := q.MoveStationEvses(ctx, MoveStationEvsesParams{LocationID: id, StationKey: stationKey}); err != nil {
		return assetWriteError("move the EVSEs of charge station "+stationKey, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("move charge station %s: commit: %w", stationKey, err)
	}
	return nil
}

// DecommissionChargeStation changes the state of the station and removes its EVSEs, in a
// transaction of its own or in a savepoint inside the transaction of the caller.
func (s *Store) DecommissionChargeStation(ctx context.Context, stationKey string, reason *string) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("decommission charge station %s: begin transaction: %w", stationKey, err)
	}
	defer rollback(ctx, tx)
	q := New(tx)

	rows, err := q.SetChargeStationState(ctx, SetChargeStationStateParams{
		State:           string(store.StationStateDecommissioned),
		StateReason:     toPgText(reason),
		ChargeStationID: stationKey,
	})
	if err != nil {
		return fmt.Errorf("decommission charge station %s: %w", stationKey, err)
	}
	if rows == 0 {
		return fmt.Errorf("decommission charge station %s: %w", stationKey, store.ErrNotFound)
	}
	if err := q.RemoveStationEvses(ctx, stationKey); err != nil {
		return fmt.Errorf("remove the EVSEs of charge station %s: %w", stationKey, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("decommission charge station %s: commit: %w", stationKey, err)
	}
	return nil
}

// TransferChargeStation moves a charge station to another org, in a transaction of its
// own or in a savepoint inside the transaction of the caller. The station row is locked
// first. The rows that refer to the target location are written last, so that a target
// that does not exist fails after the other writes and undoes them.
func (s *Store) TransferChargeStation(ctx context.Context, p store.TransferParams) (*store.TransferResult, error) {
	what := "transfer charge station " + p.StationKey
	from, err := pgUUID(p.FromOrgId)
	if err != nil {
		return nil, fmt.Errorf("%s: from org %q: %w", what, p.FromOrgId, err)
	}
	to, err := pgUUID(p.ToOrgId)
	if err != nil {
		return nil, fmt.Errorf("%s: to org %q: %w", what, p.ToOrgId, err)
	}
	var target pgtype.UUID
	if !p.MoveLocation && p.TargetLocationId != nil {
		var ok bool
		if target, ok = assetId(*p.TargetLocationId); !ok {
			// not a UUID: no location has this id
			return nil, fmt.Errorf("%s: location %s: %w", what, *p.TargetLocationId, store.ErrNotFound)
		}
	}
	if p.MoveLocation && (p.TargetPartyRef == nil || *p.TargetPartyRef == "") {
		return nil, fmt.Errorf("%s: a location that moves needs the party of the target org", what)
	}

	tx, err := s.begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: begin transaction: %w", what, err)
	}
	defer rollback(ctx, tx)
	q := New(tx)

	station, err := q.LockChargeStation(ctx, p.StationKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%s: %w", what, store.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	if station.OrgID != from {
		return nil, fmt.Errorf("%s: the station belongs to org %s, not %s: %w", what, uuidString(station.OrgID), p.FromOrgId, store.ErrInvalidState)
	}
	if store.StationState(station.State) == store.StationStateDecommissioned {
		return nil, fmt.Errorf("%s: the station is decommissioned: %w", what, store.ErrInvalidState)
	}

	result := &store.TransferResult{StationKey: p.StationKey, ClearedTariffRefs: []string{}}
	if p.MoveLocation {
		if !station.LocationID.Valid {
			return nil, fmt.Errorf("%s: the station has no location to move: %w", what, store.ErrInvalidState)
		}
		target = station.LocationID
		rows, err := q.TransferLocation(ctx, TransferLocationParams{
			ToOrgID:    to,
			PartyRef:   *p.TargetPartyRef,
			LocationID: target,
			StationKey: p.StationKey,
		})
		if err != nil {
			// the party of the target has a location with the OCPI id: ErrAlreadyExists
			return nil, assetWriteError(what+": move location "+uuidString(target), err)
		}
		if rows == 0 {
			return nil, fmt.Errorf("%s: location %s is shared with other stations: %w", what, uuidString(target), store.ErrInvalidState)
		}
		result.LocationMoved = true
	}
	result.LocationId = optionalUUIDString(target)

	if err := q.TransferChargingProfiles(ctx, TransferChargingProfilesParams{ToOrgID: to, ChargeStationID: p.StationKey}); err != nil {
		return nil, fmt.Errorf("%s: charging profiles: %w", what, err)
	}

	connectors, err := q.LockStationConnectors(ctx, p.StationKey)
	if err != nil {
		return nil, fmt.Errorf("%s: connectors: %w", what, err)
	}
	ids := make([]pgtype.UUID, len(connectors))
	tariffs := map[string]bool{}
	for i, c := range connectors {
		ids[i] = c.ID
		for _, ref := range []pgtype.UUID{c.TariffRef, c.AdhocTariffRef} {
			if ref.Valid {
				tariffs[uuidString(ref)] = true
			}
		}
	}
	if len(ids) > 0 {
		moved, err := q.TransferStationConnectors(ctx, TransferStationConnectorsParams{ToOrgID: to, Ids: ids})
		if err != nil {
			return nil, fmt.Errorf("%s: connectors: %w", what, err)
		}
		result.ConnectorsMoved = int(moved)
	}
	for ref := range tariffs {
		result.ClearedTariffRefs = append(result.ClearedTariffRefs, ref)
	}
	slices.Sort(result.ClearedTariffRefs)

	evses, err := q.ListAssetEvsesByStation(ctx, p.StationKey)
	if err != nil {
		return nil, fmt.Errorf("%s: EVSEs: %w", what, err)
	}
	if len(evses) > 0 {
		if !target.Valid {
			return nil, fmt.Errorf("%s: the EVSEs of the station need a location: %w", what, store.ErrInvalidState)
		}
		moved, err := q.TransferStationEvses(ctx, TransferStationEvsesParams{ToOrgID: to, LocationID: target, StationKey: p.StationKey})
		if err != nil {
			// a target location that does not exist: ErrNotFound
			return nil, assetWriteError(what+": EVSEs", err)
		}
		result.EvsesMoved = int(moved)
	}

	if err := q.TransferStationRow(ctx, TransferStationRowParams{ToOrgID: to, LocationID: target, ChargeStationID: p.StationKey}); err != nil {
		return nil, assetWriteError(what, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("%s: commit: %w", what, err)
	}
	return result, nil
}
