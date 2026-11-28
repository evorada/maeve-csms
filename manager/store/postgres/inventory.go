// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// ListInventory returns a page of what the org owns of the type, ordered by id. It asks
// for one more row than the page, which tells whether there is a next page.
func (s *Store) ListInventory(ctx context.Context, orgId, typ string, includeInactive bool, page store.PageRequest) ([]store.InventoryItem, string, error) {
	afterId, err := store.DecodeKeyCursor(page.Cursor)
	if err != nil {
		return nil, "", fmt.Errorf("list inventory: %w", err)
	}
	pgOrgId, err := pgUUID(orgId)
	if err != nil {
		return nil, "", fmt.Errorf("list inventory: invalid org id %q: %w", orgId, err)
	}
	limit := store.InventoryPageLimit(page)
	rowLimit, err := safeIntToInt32(limit + 1)
	if err != nil {
		return nil, "", fmt.Errorf("list inventory: %w", err)
	}

	items, err := s.listInventory(ctx, pgOrgId, typ, includeInactive, afterId, rowLimit)
	if err != nil {
		return nil, "", fmt.Errorf("list inventory of type %s of org %s: %w", typ, orgId, err)
	}
	nextCursor := ""
	if len(items) > limit {
		items = items[:limit]
		nextCursor = store.EncodeKeyCursor(items[limit-1].Id)
	}
	return items, nextCursor, nil
}

func (s *Store) listInventory(ctx context.Context, orgId pgtype.UUID, typ string, includeInactive bool, afterId string, rowLimit int32) ([]store.InventoryItem, error) {
	q := s.readQueries(ctx)
	switch typ {
	case store.InventoryStation:
		rows, err := q.ListInventoryStations(ctx, ListInventoryStationsParams{
			OrgID: orgId, IncludeInactive: includeInactive, AfterID: afterId, RowLimit: rowLimit,
		})
		if err != nil {
			return nil, err
		}
		items := make([]store.InventoryItem, len(rows))
		for i, row := range rows {
			attrs := map[string]string{"state": row.State}
			if row.LocationID.Valid {
				attrs["location_id"] = uuidString(row.LocationID)
			}
			items[i] = store.InventoryItem{Id: row.ChargeStationID, Attrs: attrs}
		}
		return items, nil
	case store.InventoryConnector:
		rows, err := q.ListInventoryConnectors(ctx, ListInventoryConnectorsParams{
			OrgID: orgId, IncludeInactive: includeInactive, AfterID: afterId, RowLimit: rowLimit,
		})
		if err != nil {
			return nil, err
		}
		items := make([]store.InventoryItem, len(rows))
		for i, row := range rows {
			items[i] = store.InventoryItem{Id: row.ID, Attrs: map[string]string{
				"station_key":   row.StationKey,
				"evse_uid":      uuidString(row.EvseUid),
				"connector_uid": uuidString(row.ConnectorUid),
			}}
		}
		return items, nil
	case store.InventoryLocation:
		rows, err := q.ListInventoryLocations(ctx, ListInventoryLocationsParams{
			OrgID: orgId, AfterID: afterId, RowLimit: rowLimit,
		})
		if err != nil {
			return nil, err
		}
		items := make([]store.InventoryItem, len(rows))
		for i, row := range rows {
			items[i] = store.InventoryItem{Id: uuidString(row.ID), Attrs: map[string]string{
				"ocpi_id":   row.OcpiID,
				"party_ref": row.PartyRef,
				"publish":   strconv.FormatBool(row.Publish),
			}}
		}
		return items, nil
	case store.InventoryToken:
		rows, err := q.ListInventoryTokens(ctx, ListInventoryTokensParams{
			OrgID: orgId, IncludeInactive: includeInactive, AfterID: afterId, RowLimit: rowLimit,
		})
		if err != nil {
			return nil, err
		}
		items := make([]store.InventoryItem, len(rows))
		for i, row := range rows {
			items[i] = store.InventoryItem{Id: row.ID, Attrs: map[string]string{
				"type":  row.Type,
				"valid": strconv.FormatBool(row.Valid),
			}}
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unknown inventory type %q", typ)
	}
}

// CountInventory counts what the org owns of the type.
func (s *Store) CountInventory(ctx context.Context, orgId, typ string, includeInactive bool) (int, error) {
	pgOrgId, err := pgUUID(orgId)
	if err != nil {
		return 0, fmt.Errorf("count inventory: invalid org id %q: %w", orgId, err)
	}
	q := s.readQueries(ctx)
	var count int64
	switch typ {
	case store.InventoryStation:
		count, err = q.CountInventoryStations(ctx, CountInventoryStationsParams{OrgID: pgOrgId, IncludeInactive: includeInactive})
	case store.InventoryConnector:
		count, err = q.CountInventoryConnectors(ctx, CountInventoryConnectorsParams{OrgID: pgOrgId, IncludeInactive: includeInactive})
	case store.InventoryLocation:
		count, err = q.CountInventoryLocations(ctx, pgOrgId)
	case store.InventoryToken:
		count, err = q.CountInventoryTokens(ctx, CountInventoryTokensParams{OrgID: pgOrgId, IncludeInactive: includeInactive})
	default:
		return 0, fmt.Errorf("count inventory: unknown inventory type %q", typ)
	}
	if err != nil {
		return 0, fmt.Errorf("count inventory of type %s of org %s: %w", typ, orgId, err)
	}
	return int(count), nil
}
