// SPDX-License-Identifier: Apache-2.0

package inmemory

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// ListInventory returns a page of what the org owns of the type, ordered by id.
func (s *Store) ListInventory(_ context.Context, orgId, typ string, includeInactive bool, page store.PageRequest) ([]store.InventoryItem, string, error) {
	afterId, err := store.DecodeKeyCursor(page.Cursor)
	if err != nil {
		return nil, "", fmt.Errorf("list inventory: %w", err)
	}
	items, err := s.inventory(orgId, typ, includeInactive)
	if err != nil {
		return nil, "", fmt.Errorf("list inventory: %w", err)
	}
	items = slices.DeleteFunc(items, func(item store.InventoryItem) bool { return item.Id <= afterId })
	slices.SortFunc(items, func(a, b store.InventoryItem) int { return strings.Compare(a.Id, b.Id) })

	limit := store.InventoryPageLimit(page)
	nextCursor := ""
	if len(items) > limit {
		items = items[:limit]
		nextCursor = store.EncodeKeyCursor(items[limit-1].Id)
	}
	return items, nextCursor, nil
}

// CountInventory counts what the org owns of the type.
func (s *Store) CountInventory(_ context.Context, orgId, typ string, includeInactive bool) (int, error) {
	items, err := s.inventory(orgId, typ, includeInactive)
	if err != nil {
		return 0, fmt.Errorf("count inventory: %w", err)
	}
	return len(items), nil
}

// inventory returns every item of the type that the org owns, in no order.
func (s *Store) inventory(orgId, typ string, includeInactive bool) ([]store.InventoryItem, error) {
	org, err := uuid.Parse(orgId)
	if err != nil {
		return nil, fmt.Errorf("invalid org id %q: %w", orgId, err)
	}
	s.Lock()
	defer s.Unlock()
	switch typ {
	case store.InventoryStation:
		return s.stationInventory(org.String(), includeInactive), nil
	case store.InventoryConnector:
		return s.connectorInventory(org.String(), includeInactive), nil
	case store.InventoryLocation:
		return s.locationInventory(org.String()), nil
	case store.InventoryToken:
		return s.tokenInventory(org.String(), includeInactive), nil
	default:
		return nil, fmt.Errorf("unknown inventory type %q", typ)
	}
}

// stationInventory returns the charge stations of the org. The caller holds the lock.
func (s *Store) stationInventory(orgId string, includeInactive bool) []store.InventoryItem {
	items := []store.InventoryItem{}
	for stationKey, registered := range s.chargeStations {
		if stationOrgId(s.orgIdForStation(stationKey)) != orgId {
			continue
		}
		if !includeInactive && registered.State != store.StationStatePending && registered.State != store.StationStateActive {
			continue
		}
		attrs := map[string]string{"state": string(registered.State)}
		if registered.LocationId != nil {
			attrs["location_id"] = *registered.LocationId
		}
		items = append(items, store.InventoryItem{Id: stationKey, Attrs: attrs})
	}
	return items
}

// connectorInventory returns the connectors of the org, by their own org. The caller
// holds the lock.
func (s *Store) connectorInventory(orgId string, includeInactive bool) []store.InventoryItem {
	connectorsOfEvse := map[string]int{}
	for _, c := range s.assetConnectors {
		connectorsOfEvse[c.EvseUid]++
	}
	items := []store.InventoryItem{}
	for _, c := range s.assetConnectors {
		if c.OrgId != orgId {
			continue
		}
		evse := s.assetEvses[c.EvseUid]
		if evse == nil {
			continue
		}
		// an EVSE always belongs to a charge station that is registered
		registered := s.chargeStations[evse.StationKey]
		if registered == nil {
			continue
		}
		if !includeInactive && (evse.Status == store.AssetEvseStatusRemoved || registered.State != store.StationStateActive) {
			continue
		}
		items = append(items, store.InventoryItem{
			Id: store.ConnectorSubjectKey(evse.StationKey, evse.OcppEvseId, c.OcppConnectorId, connectorsOfEvse[c.EvseUid]),
			Attrs: map[string]string{
				"station_key":   evse.StationKey,
				"evse_uid":      evse.Id,
				"connector_uid": c.Id,
			},
		})
	}
	return items
}

// locationInventory returns the asset locations of the org. The caller holds the lock.
func (s *Store) locationInventory(orgId string) []store.InventoryItem {
	items := []store.InventoryItem{}
	for _, loc := range s.assetLocations {
		if loc.OrgId != orgId {
			continue
		}
		items = append(items, store.InventoryItem{
			Id: loc.Id,
			Attrs: map[string]string{
				"ocpi_id":   loc.OcpiId,
				"party_ref": loc.PartyRef,
				"publish":   strconv.FormatBool(loc.Publish),
			},
		})
	}
	return items
}

// tokenInventory returns the tokens of the org. The caller holds the lock.
func (s *Store) tokenInventory(orgId string, includeInactive bool) []store.InventoryItem {
	items := []store.InventoryItem{}
	for _, entry := range s.tokens {
		token := entry.token
		if stationOrgId(token.OrgId) != orgId || (!includeInactive && !token.Valid) {
			continue
		}
		items = append(items, store.InventoryItem{
			Id: store.TokenInventoryId(token.CountryCode, token.PartyId, token.Uid),
			Attrs: map[string]string{
				"type":  token.Type,
				"valid": strconv.FormatBool(token.Valid),
			},
		})
	}
	return items
}
