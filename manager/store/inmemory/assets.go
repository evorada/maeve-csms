// SPDX-License-Identifier: Apache-2.0

package inmemory

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The asset store keeps the rules of the PostgreSQL tables: unique keys, optimistic
// versions, the references between records (a location in use cannot be deleted, an
// EVSE needs its location and charge station, a connector its EVSE) and the defaults.

// assetKey returns the canonical form of an asset id, under which the asset is stored;
// false when the id is not a UUID, which no stored asset has.
func assetKey(id string) (string, bool) {
	u, err := uuid.Parse(id)
	if err != nil {
		return "", false
	}
	return u.String(), true
}

// newAssetKey returns the canonical form of id, or a new UUIDv7 when id is empty.
func newAssetKey(id string) (string, error) {
	if id == "" {
		u, err := uuid.NewV7()
		if err != nil {
			return "", err
		}
		return u.String(), nil
	}
	u, err := uuid.Parse(id)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// optionalAssetKey returns the canonical form of an optional reference to a UUID.
func optionalAssetKey(id *string) (*string, error) {
	if id == nil {
		return nil, nil
	}
	u, err := uuid.Parse(*id)
	if err != nil {
		return nil, err
	}
	key := u.String()
	return &key, nil
}

func (s *Store) CreateAssetLocation(_ context.Context, loc *store.AssetLocation) error {
	id, err := newAssetKey(loc.Id)
	if err != nil {
		return fmt.Errorf("create asset location: invalid id %q: %w", loc.Id, err)
	}
	orgId, err := uuid.Parse(loc.OrgId)
	if err != nil {
		return fmt.Errorf("create asset location: invalid org id %q: %w", loc.OrgId, err)
	}
	stored := copyAssetLocation(loc)
	stored.Id = id
	stored.OrgId = orgId.String()
	if stored.OcpiId == "" {
		stored.OcpiId = id
	}

	s.Lock()
	defer s.Unlock()
	if _, found := s.assetLocations[id]; found {
		return fmt.Errorf("create asset location %s: %w", id, store.ErrAlreadyExists)
	}
	if s.assetLocationOcpiIdTaken(stored) {
		return fmt.Errorf("create asset location: OCPI id %s/%s: %w", stored.PartyRef, stored.OcpiId, store.ErrAlreadyExists)
	}
	now := s.clock.Now().UTC()
	stored.Version = 1
	stored.CreatedAt = now
	stored.UpdatedAt = now
	s.assetLocations[id] = stored
	*loc = *copyAssetLocation(stored)
	return nil
}

// assetLocationOcpiIdTaken reports whether another location has the party and the OCPI
// id of loc. The caller holds the lock.
func (s *Store) assetLocationOcpiIdTaken(loc *store.AssetLocation) bool {
	for _, other := range s.assetLocations {
		if other.Id != loc.Id && other.PartyRef == loc.PartyRef && other.OcpiId == loc.OcpiId {
			return true
		}
	}
	return false
}

func (s *Store) UpdateAssetLocation(_ context.Context, loc *store.AssetLocation) error {
	id, ok := assetKey(loc.Id)
	if !ok {
		return fmt.Errorf("update asset location %s: %w", loc.Id, store.ErrNotFound)
	}

	s.Lock()
	defer s.Unlock()
	current := s.assetLocations[id]
	if current == nil {
		return fmt.Errorf("update asset location %s: %w", loc.Id, store.ErrNotFound)
	}
	if current.Version != loc.Version {
		return fmt.Errorf("update asset location %s: %w", loc.Id, store.ErrVersionConflict)
	}
	updated := copyAssetLocation(loc)
	updated.Id = id
	updated.OrgId = current.OrgId
	if updated.OcpiId == "" {
		updated.OcpiId = id
	}
	if s.assetLocationOcpiIdTaken(updated) {
		return fmt.Errorf("update asset location %s: OCPI id %s/%s: %w", loc.Id, updated.PartyRef, updated.OcpiId, store.ErrAlreadyExists)
	}
	updated.Version = current.Version + 1
	updated.CreatedAt = current.CreatedAt
	updated.UpdatedAt = s.clock.Now().UTC()
	s.assetLocations[id] = updated
	*loc = *copyAssetLocation(updated)
	return nil
}

func (s *Store) LookupAssetLocation(_ context.Context, locationId string) (*store.AssetLocation, error) {
	id, ok := assetKey(locationId)
	if !ok {
		return nil, nil
	}
	s.Lock()
	defer s.Unlock()
	if loc := s.assetLocations[id]; loc != nil {
		return copyAssetLocation(loc), nil
	}
	return nil, nil
}

func (s *Store) LookupAssetLocationByOcpiId(_ context.Context, partyRef, ocpiId string) (*store.AssetLocation, error) {
	s.Lock()
	defer s.Unlock()
	for _, loc := range s.assetLocations {
		if loc.PartyRef == partyRef && loc.OcpiId == ocpiId {
			return copyAssetLocation(loc), nil
		}
	}
	return nil, nil
}

func (s *Store) ListAssetLocations(_ context.Context, filter store.AssetLocationFilter) ([]*store.AssetLocation, string, error) {
	afterId, err := store.DecodeCursor(filter.Page.Cursor)
	if err != nil {
		return nil, "", fmt.Errorf("list asset locations: %w", err)
	}
	orgIds := make(map[string]bool, len(filter.OrgIds))
	for _, orgId := range filter.OrgIds {
		u, err := uuid.Parse(orgId)
		if err != nil {
			return nil, "", fmt.Errorf("list asset locations: invalid org id %q: %w", orgId, err)
		}
		orgIds[u.String()] = true
	}
	query := ""
	if filter.Query != nil {
		query = strings.ToLower(*filter.Query)
	}

	s.Lock()
	defer s.Unlock()
	var matching []*store.AssetLocation
	for _, loc := range s.assetLocations {
		switch {
		case !orgIds[loc.OrgId],
			filter.PartyRef != nil && loc.PartyRef != *filter.PartyRef,
			filter.Publish != nil && loc.Publish != *filter.Publish,
			filter.Query != nil && !assetLocationMatches(loc, query),
			loc.Id <= afterId: // the canonical text of a UUID sorts like its bytes
			continue
		}
		matching = append(matching, loc)
	}
	slices.SortFunc(matching, func(a, b *store.AssetLocation) int { return strings.Compare(a.Id, b.Id) })

	limit := filter.Page.EffectiveLimit()
	nextCursor := ""
	if len(matching) > limit {
		matching = matching[:limit]
		nextCursor = store.EncodeCursor(matching[limit-1].Id)
	}
	page := make([]*store.AssetLocation, len(matching))
	for i, loc := range matching {
		page[i] = copyAssetLocation(loc)
	}
	return page, nextCursor, nil
}

// assetLocationMatches reports whether the name, the address, the city or the OCPI id of
// the location contains the lowercase query, ignoring case.
func assetLocationMatches(loc *store.AssetLocation, query string) bool {
	fields := []string{loc.Address, loc.City, loc.OcpiId}
	if loc.Name != nil {
		fields = append(fields, *loc.Name)
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

func (s *Store) DeleteAssetLocation(_ context.Context, locationId string) error {
	id, ok := assetKey(locationId)
	if !ok {
		return nil
	}
	s.Lock()
	defer s.Unlock()
	for _, evse := range s.assetEvses {
		if evse.LocationId == id {
			return fmt.Errorf("delete asset location %s: %w", locationId, store.ErrInUse)
		}
	}
	delete(s.assetLocations, id)
	// the stations at the location have no location any more (ON DELETE SET NULL)
	for _, cs := range s.chargeStations {
		if cs.LocationId != nil && *cs.LocationId == id {
			cs.LocationId = nil
		}
	}
	return nil
}

func (s *Store) CreateAssetEvse(_ context.Context, evse *store.AssetEvse) error {
	s.Lock()
	defer s.Unlock()
	return s.createAssetEvse(evse)
}

// createAssetEvse is CreateAssetEvse. The caller holds the lock.
func (s *Store) createAssetEvse(evse *store.AssetEvse) error {
	id, err := newAssetKey(evse.Id)
	if err != nil {
		return fmt.Errorf("create asset EVSE: invalid id %q: %w", evse.Id, err)
	}
	orgId, err := uuid.Parse(evse.OrgId)
	if err != nil {
		return fmt.Errorf("create asset EVSE: invalid org id %q: %w", evse.OrgId, err)
	}
	stored := copyAssetEvse(evse)
	stored.Id = id
	stored.OrgId = orgId.String()
	stored.Connectors = nil
	if stored.Status == "" {
		stored.Status = store.AssetEvseStatusActive
	}

	if err := s.checkAssetEvseReferences(stored); err != nil {
		return fmt.Errorf("create asset EVSE: %w", err)
	}
	if _, found := s.assetEvses[id]; found {
		return fmt.Errorf("create asset EVSE %s: %w", id, store.ErrAlreadyExists)
	}
	if err := s.checkAssetEvseKeys(stored); err != nil {
		return fmt.Errorf("create asset EVSE: %w", err)
	}
	now := s.clock.Now().UTC()
	stored.Version = 1
	stored.CreatedAt = now
	stored.UpdatedAt = now

	// every connector is checked before anything is stored, so that the EVSE and its
	// connectors are stored together or not at all
	connectors := make([]*store.AssetConnector, len(evse.Connectors))
	for i, c := range evse.Connectors {
		connector, err := s.newAssetConnector(c, stored, now)
		if err != nil {
			return fmt.Errorf("create asset EVSE: %w", err)
		}
		for _, other := range connectors[:i] {
			if other.Id == connector.Id {
				return fmt.Errorf("create asset EVSE: connector %s: %w", connector.Id, store.ErrAlreadyExists)
			}
			if err := checkAssetConnectorKeys(connector, other); err != nil {
				return fmt.Errorf("create asset EVSE: %w", err)
			}
		}
		connectors[i] = connector
	}

	s.assetEvses[id] = stored
	for i, connector := range connectors {
		s.assetConnectors[connector.Id] = connector
		*evse.Connectors[i] = *copyAssetConnector(connector)
	}
	result := copyAssetEvse(stored)
	result.Connectors = evse.Connectors
	if result.Connectors == nil {
		result.Connectors = []*store.AssetConnector{}
	}
	*evse = *result
	return nil
}

// checkAssetEvseReferences checks that the location and the charge station of the EVSE
// exist. The caller holds the lock.
func (s *Store) checkAssetEvseReferences(evse *store.AssetEvse) error {
	locationId, ok := assetKey(evse.LocationId)
	if !ok || s.assetLocations[locationId] == nil {
		return fmt.Errorf("location %s: %w", evse.LocationId, store.ErrNotFound)
	}
	evse.LocationId = locationId
	if s.chargeStationAuth[evse.StationKey] == nil {
		return fmt.Errorf("charge station %s: %w", evse.StationKey, store.ErrNotFound)
	}
	return nil
}

// checkAssetEvseKeys checks that no other EVSE has the OCPP EVSE id of the station or the
// eMI3 id. The caller holds the lock.
func (s *Store) checkAssetEvseKeys(evse *store.AssetEvse) error {
	for _, other := range s.assetEvses {
		if other.Id == evse.Id {
			continue
		}
		if other.StationKey == evse.StationKey && other.OcppEvseId == evse.OcppEvseId {
			return fmt.Errorf("EVSE %s/%d: %w", evse.StationKey, evse.OcppEvseId, store.ErrAlreadyExists)
		}
		if evse.EvseId != nil && other.EvseId != nil && *other.EvseId == *evse.EvseId {
			return fmt.Errorf("EVSE id %s: %w", *evse.EvseId, store.ErrAlreadyExists)
		}
	}
	return nil
}

func (s *Store) UpdateAssetEvse(_ context.Context, evse *store.AssetEvse) error {
	id, ok := assetKey(evse.Id)
	if !ok {
		return fmt.Errorf("update asset EVSE %s: %w", evse.Id, store.ErrNotFound)
	}

	s.Lock()
	defer s.Unlock()
	current := s.assetEvses[id]
	if current == nil {
		return fmt.Errorf("update asset EVSE %s: %w", evse.Id, store.ErrNotFound)
	}
	if current.Version != evse.Version {
		return fmt.Errorf("update asset EVSE %s: %w", evse.Id, store.ErrVersionConflict)
	}
	updated := copyAssetEvse(evse)
	updated.Id = id
	updated.OrgId = current.OrgId
	updated.Connectors = nil
	if updated.Status == "" {
		updated.Status = store.AssetEvseStatusActive
	}
	if err := s.checkAssetEvseReferences(updated); err != nil {
		return fmt.Errorf("update asset EVSE %s: %w", evse.Id, err)
	}
	if err := s.checkAssetEvseKeys(updated); err != nil {
		return fmt.Errorf("update asset EVSE %s: %w", evse.Id, err)
	}
	updated.Version = current.Version + 1
	updated.CreatedAt = current.CreatedAt
	updated.UpdatedAt = s.clock.Now().UTC()
	s.assetEvses[id] = updated
	*evse = *s.assetEvseWithConnectors(updated)
	return nil
}

func (s *Store) LookupAssetEvse(_ context.Context, evseUid string) (*store.AssetEvse, error) {
	id, ok := assetKey(evseUid)
	if !ok {
		return nil, nil
	}
	s.Lock()
	defer s.Unlock()
	if evse := s.assetEvses[id]; evse != nil {
		return s.assetEvseWithConnectors(evse), nil
	}
	return nil, nil
}

func (s *Store) LookupAssetEvseByOcpp(_ context.Context, stationKey string, ocppEvseId int) (*store.AssetEvse, error) {
	s.Lock()
	defer s.Unlock()
	if evse := s.assetEvseByOcpp(stationKey, ocppEvseId); evse != nil {
		return s.assetEvseWithConnectors(evse), nil
	}
	return nil, nil
}

// assetEvseByOcpp returns the stored EVSE with the OCPP EVSE id of the station, or nil.
// The caller holds the lock.
func (s *Store) assetEvseByOcpp(stationKey string, ocppEvseId int) *store.AssetEvse {
	for _, evse := range s.assetEvses {
		if evse.StationKey == stationKey && evse.OcppEvseId == ocppEvseId {
			return evse
		}
	}
	return nil
}

func (s *Store) ListAssetEvsesForLocation(_ context.Context, locationId string) ([]*store.AssetEvse, error) {
	id, ok := assetKey(locationId)
	if !ok {
		return []*store.AssetEvse{}, nil
	}
	s.Lock()
	defer s.Unlock()
	return s.listAssetEvses(func(evse *store.AssetEvse) bool { return evse.LocationId == id }), nil
}

func (s *Store) ListAssetEvsesForStation(_ context.Context, stationKey string) ([]*store.AssetEvse, error) {
	s.Lock()
	defer s.Unlock()
	return s.listAssetEvses(func(evse *store.AssetEvse) bool { return evse.StationKey == stationKey }), nil
}

// listAssetEvses returns copies of the matching EVSEs with their connectors, ordered by
// OCPP EVSE id, then station key. The caller holds the lock.
func (s *Store) listAssetEvses(match func(*store.AssetEvse) bool) []*store.AssetEvse {
	evses := []*store.AssetEvse{}
	for _, evse := range s.assetEvses {
		if match(evse) {
			evses = append(evses, s.assetEvseWithConnectors(evse))
		}
	}
	slices.SortFunc(evses, func(a, b *store.AssetEvse) int {
		return cmp.Or(cmp.Compare(a.OcppEvseId, b.OcppEvseId), strings.Compare(a.StationKey, b.StationKey))
	})
	return evses
}

func (s *Store) DeleteAssetEvse(_ context.Context, evseUid string) error {
	id, ok := assetKey(evseUid)
	if !ok {
		return nil
	}
	s.Lock()
	defer s.Unlock()
	s.deleteAssetEvse(id)
	return nil
}

// deleteAssetEvse deletes the EVSE with the id, and its connectors. The caller holds the
// lock.
func (s *Store) deleteAssetEvse(id string) {
	if s.assetEvses[id] == nil {
		return
	}
	delete(s.assetEvses, id)
	for connectorId, c := range s.assetConnectors {
		if c.EvseUid == id {
			delete(s.assetConnectors, connectorId)
		}
	}
}

// assetEvseWithConnectors returns a copy of the stored EVSE with copies of its
// connectors, ordered by OCPP connector id. The caller holds the lock.
func (s *Store) assetEvseWithConnectors(evse *store.AssetEvse) *store.AssetEvse {
	result := copyAssetEvse(evse)
	result.Connectors = []*store.AssetConnector{}
	for _, c := range s.assetConnectors {
		if c.EvseUid == evse.Id {
			result.Connectors = append(result.Connectors, copyAssetConnector(c))
		}
	}
	slices.SortFunc(result.Connectors, func(a, b *store.AssetConnector) int {
		return cmp.Compare(a.OcppConnectorId, b.OcppConnectorId)
	})
	return result
}

func (s *Store) CreateAssetConnector(_ context.Context, c *store.AssetConnector) error {
	s.Lock()
	defer s.Unlock()
	evseUid, ok := assetKey(c.EvseUid)
	if !ok || s.assetEvses[evseUid] == nil {
		return fmt.Errorf("create asset connector: EVSE %s: %w", c.EvseUid, store.ErrNotFound)
	}
	connector, err := s.newAssetConnector(c, s.assetEvses[evseUid], s.clock.Now().UTC())
	if err != nil {
		return fmt.Errorf("create asset connector: %w", err)
	}
	s.assetConnectors[connector.Id] = connector
	*c = *copyAssetConnector(connector)
	return nil
}

// newAssetConnector returns the connector to store for c, a new connector of the EVSE,
// after checking that its keys are free. The caller holds the lock.
func (s *Store) newAssetConnector(c *store.AssetConnector, evse *store.AssetEvse, now time.Time) (*store.AssetConnector, error) {
	id, err := newAssetKey(c.Id)
	if err != nil {
		return nil, fmt.Errorf("invalid connector id %q: %w", c.Id, err)
	}
	connector := copyAssetConnector(c)
	connector.Id = id
	connector.OrgId = evse.OrgId
	connector.EvseUid = evse.Id
	if connector.OcpiId == "" {
		connector.OcpiId = strconv.Itoa(connector.OcppConnectorId)
	}
	if connector.TariffRef, err = optionalAssetKey(c.TariffRef); err != nil {
		return nil, fmt.Errorf("invalid tariff ref %q: %w", *c.TariffRef, err)
	}
	if connector.AdhocTariffRef, err = optionalAssetKey(c.AdhocTariffRef); err != nil {
		return nil, fmt.Errorf("invalid ad hoc tariff ref %q: %w", *c.AdhocTariffRef, err)
	}
	if _, found := s.assetConnectors[id]; found {
		return nil, fmt.Errorf("connector %s: %w", id, store.ErrAlreadyExists)
	}
	if err := s.checkStoredAssetConnectorKeys(connector); err != nil {
		return nil, err
	}
	connector.Version = 1
	connector.CreatedAt = now
	connector.UpdatedAt = now
	return connector, nil
}

// checkStoredAssetConnectorKeys checks that no other stored connector of the EVSE has the
// OCPP or the OCPI id of c. The caller holds the lock.
func (s *Store) checkStoredAssetConnectorKeys(c *store.AssetConnector) error {
	for _, other := range s.assetConnectors {
		if err := checkAssetConnectorKeys(c, other); err != nil {
			return err
		}
	}
	return nil
}

// checkAssetConnectorKeys checks that other, unless it has the id of c, does not have the
// OCPP or the OCPI id of c on the same EVSE.
func checkAssetConnectorKeys(c, other *store.AssetConnector) error {
	switch {
	case other.Id == c.Id, other.EvseUid != c.EvseUid:
		return nil
	case other.OcppConnectorId == c.OcppConnectorId:
		return fmt.Errorf("connector %s/%d: %w", c.EvseUid, c.OcppConnectorId, store.ErrAlreadyExists)
	case other.OcpiId == c.OcpiId:
		return fmt.Errorf("connector %s/%s: %w", c.EvseUid, c.OcpiId, store.ErrAlreadyExists)
	}
	return nil
}

func (s *Store) UpdateAssetConnector(_ context.Context, c *store.AssetConnector) error {
	id, ok := assetKey(c.Id)
	if !ok {
		return fmt.Errorf("update asset connector %s: %w", c.Id, store.ErrNotFound)
	}

	s.Lock()
	defer s.Unlock()
	current := s.assetConnectors[id]
	if current == nil {
		return fmt.Errorf("update asset connector %s: %w", c.Id, store.ErrNotFound)
	}
	if current.Version != c.Version {
		return fmt.Errorf("update asset connector %s: %w", c.Id, store.ErrVersionConflict)
	}
	updated := copyAssetConnector(c)
	updated.Id = id
	updated.OrgId = current.OrgId
	updated.EvseUid = current.EvseUid
	if updated.OcpiId == "" {
		updated.OcpiId = strconv.Itoa(updated.OcppConnectorId)
	}
	var err error
	if updated.TariffRef, err = optionalAssetKey(c.TariffRef); err != nil {
		return fmt.Errorf("update asset connector %s: invalid tariff ref %q: %w", c.Id, *c.TariffRef, err)
	}
	if updated.AdhocTariffRef, err = optionalAssetKey(c.AdhocTariffRef); err != nil {
		return fmt.Errorf("update asset connector %s: invalid ad hoc tariff ref %q: %w", c.Id, *c.AdhocTariffRef, err)
	}
	if err := s.checkStoredAssetConnectorKeys(updated); err != nil {
		return fmt.Errorf("update asset connector %s: %w", c.Id, err)
	}
	updated.Version = current.Version + 1
	updated.CreatedAt = current.CreatedAt
	updated.UpdatedAt = s.clock.Now().UTC()
	s.assetConnectors[id] = updated
	*c = *copyAssetConnector(updated)
	return nil
}

func (s *Store) LookupAssetConnector(_ context.Context, connectorUid string) (*store.AssetConnector, error) {
	id, ok := assetKey(connectorUid)
	if !ok {
		return nil, nil
	}
	s.Lock()
	defer s.Unlock()
	if c := s.assetConnectors[id]; c != nil {
		return copyAssetConnector(c), nil
	}
	return nil, nil
}

func (s *Store) LookupAssetConnectorByOcpp(_ context.Context, stationKey string, ocppEvseId, ocppConnectorId int) (*store.AssetConnector, error) {
	s.Lock()
	defer s.Unlock()
	evse := s.assetEvseByOcpp(stationKey, ocppEvseId)
	if evse == nil {
		return nil, nil
	}
	for _, c := range s.assetConnectors {
		if c.EvseUid == evse.Id && c.OcppConnectorId == ocppConnectorId {
			return copyAssetConnector(c), nil
		}
	}
	return nil, nil
}

func (s *Store) DeleteAssetConnector(_ context.Context, connectorUid string) error {
	id, ok := assetKey(connectorUid)
	if !ok {
		return nil
	}
	s.Lock()
	defer s.Unlock()
	delete(s.assetConnectors, id)
	return nil
}

// copyAssetLocation returns a deep copy; nil lists become empty lists, as they are read
// back from PostgreSQL.
func copyAssetLocation(loc *store.AssetLocation) *store.AssetLocation {
	c := *loc
	c.Name = copyOrNil(loc.Name)
	c.PostalCode = copyOrNil(loc.PostalCode)
	c.State = copyOrNil(loc.State)
	c.ParkingType = copyOrNil(loc.ParkingType)
	c.ChargingWhenClosed = copyOrNil(loc.ChargingWhenClosed)
	c.Facilities = append([]string{}, loc.Facilities...)
	c.ExternalRefs = append([]string{}, loc.ExternalRefs...)
	if loc.OpeningTimes != nil {
		openingTimes := *loc.OpeningTimes
		openingTimes.RegularHours = slices.Clone(loc.OpeningTimes.RegularHours)
		openingTimes.ExceptionalOpenings = slices.Clone(loc.OpeningTimes.ExceptionalOpenings)
		openingTimes.ExceptionalClosings = slices.Clone(loc.OpeningTimes.ExceptionalClosings)
		c.OpeningTimes = &openingTimes
	}
	return &c
}

// copyAssetEvse returns a deep copy, connectors included.
func copyAssetEvse(evse *store.AssetEvse) *store.AssetEvse {
	c := *evse
	c.EvseId = copyOrNil(evse.EvseId)
	c.PhysicalReference = copyOrNil(evse.PhysicalReference)
	if evse.Connectors != nil {
		c.Connectors = make([]*store.AssetConnector, len(evse.Connectors))
		for i, connector := range evse.Connectors {
			c.Connectors[i] = copyAssetConnector(connector)
		}
	}
	return &c
}

func copyAssetConnector(connector *store.AssetConnector) *store.AssetConnector {
	c := *connector
	c.MaxElectricPower = copyOrNil(connector.MaxElectricPower)
	c.TariffRef = copyOrNil(connector.TariffRef)
	c.AdhocTariffRef = copyOrNil(connector.AdhocTariffRef)
	return &c
}

func (s *Store) MoveChargeStationToLocation(_ context.Context, stationKey, locationId string) error {
	id, ok := assetKey(locationId)
	if !ok {
		return fmt.Errorf("move charge station %s: location %s: %w", stationKey, locationId, store.ErrNotFound)
	}
	s.Lock()
	defer s.Unlock()
	registered := s.chargeStations[stationKey]
	if registered == nil {
		return fmt.Errorf("move charge station %s: %w", stationKey, store.ErrNotFound)
	}
	if s.assetLocations[id] == nil {
		return fmt.Errorf("move charge station %s: location %s: %w", stationKey, locationId, store.ErrNotFound)
	}
	now := s.clock.Now().UTC()
	registered.LocationId = &id
	registered.UpdatedAt = now
	// the stored EVSEs are only ever handed out as copies, so they change in place
	for _, evse := range s.assetEvses {
		if evse.StationKey == stationKey && evse.LocationId != id {
			evse.LocationId = id
			evse.Version++
			evse.UpdatedAt = now
		}
	}
	return nil
}

func (s *Store) DecommissionChargeStation(_ context.Context, stationKey string, reason *string) error {
	s.Lock()
	defer s.Unlock()
	registered := s.chargeStations[stationKey]
	if registered == nil {
		return fmt.Errorf("decommission charge station %s: %w", stationKey, store.ErrNotFound)
	}
	now := s.clock.Now().UTC()
	registered.State = store.StationStateDecommissioned
	registered.StateReason = clonePtr(reason)
	registered.StateChangedAt = now
	registered.UpdatedAt = now
	for _, evse := range s.assetEvses {
		if evse.StationKey == stationKey && evse.Status != store.AssetEvseStatusRemoved {
			evse.Status = store.AssetEvseStatusRemoved
			evse.Version++
			evse.UpdatedAt = now
		}
	}
	return nil
}

// TransferChargeStation checks everything before it writes anything, under the lock, so
// that a transfer that is refused changes nothing.
func (s *Store) TransferChargeStation(_ context.Context, p store.TransferParams) (*store.TransferResult, error) {
	what := "transfer charge station " + p.StationKey
	from, err := uuid.Parse(p.FromOrgId)
	if err != nil {
		return nil, fmt.Errorf("%s: from org %q: %w", what, p.FromOrgId, err)
	}
	to, err := uuid.Parse(p.ToOrgId)
	if err != nil {
		return nil, fmt.Errorf("%s: to org %q: %w", what, p.ToOrgId, err)
	}
	if p.MoveLocation && (p.TargetPartyRef == nil || *p.TargetPartyRef == "") {
		return nil, fmt.Errorf("%s: a location that moves needs the party of the target org", what)
	}

	s.Lock()
	defer s.Unlock()
	registered := s.chargeStations[p.StationKey]
	// a station record always comes with auth details
	if registered == nil || s.chargeStationAuth[p.StationKey] == nil {
		return nil, fmt.Errorf("%s: %w", what, store.ErrNotFound)
	}
	if owner := stationOrgId(s.orgIdForStation(p.StationKey)); owner != from.String() {
		return nil, fmt.Errorf("%s: the station belongs to org %s, not %s: %w", what, owner, p.FromOrgId, store.ErrInvalidState)
	}
	if registered.State == store.StationStateDecommissioned {
		return nil, fmt.Errorf("%s: the station is decommissioned: %w", what, store.ErrInvalidState)
	}

	var target *string
	var moved *store.AssetLocation
	switch {
	case p.MoveLocation:
		if registered.LocationId == nil {
			return nil, fmt.Errorf("%s: the station has no location to move: %w", what, store.ErrInvalidState)
		}
		target = registered.LocationId
		if s.assetLocationShared(*target, p.StationKey) {
			return nil, fmt.Errorf("%s: location %s is shared with other stations: %w", what, *target, store.ErrInvalidState)
		}
		moved = copyAssetLocation(s.assetLocations[*target])
		moved.OrgId = to.String()
		moved.PartyRef = *p.TargetPartyRef
		if s.assetLocationOcpiIdTaken(moved) {
			return nil, fmt.Errorf("%s: move location %s: OCPI id %s/%s: %w", what, *target, moved.PartyRef, moved.OcpiId, store.ErrAlreadyExists)
		}
	case p.TargetLocationId != nil:
		id, ok := assetKey(*p.TargetLocationId)
		if !ok || s.assetLocations[id] == nil {
			return nil, fmt.Errorf("%s: location %s: %w", what, *p.TargetLocationId, store.ErrNotFound)
		}
		target = &id
	}
	evses := s.listAssetEvses(func(evse *store.AssetEvse) bool { return evse.StationKey == p.StationKey })
	if len(evses) > 0 && target == nil {
		return nil, fmt.Errorf("%s: the EVSEs of the station need a location: %w", what, store.ErrInvalidState)
	}

	now := s.clock.Now().UTC()
	result := &store.TransferResult{StationKey: p.StationKey, LocationId: clonePtr(target), ClearedTariffRefs: []string{}}
	if moved != nil {
		moved.Version++
		moved.UpdatedAt = now
		s.assetLocations[moved.Id] = moved
		result.LocationMoved = true
	}
	for key, profile := range s.chargingProfiles {
		if profile.ChargeStationId == p.StationKey {
			// the stored profile is the one that its caller set: it is replaced, not changed
			transferred := *profile
			transferred.OrgId = to.String()
			s.chargingProfiles[key] = &transferred
		}
	}
	tariffs := map[string]bool{}
	for _, evse := range evses {
		stored := s.assetEvses[evse.Id]
		stored.OrgId = to.String()
		stored.LocationId = *target
		stored.Version++
		stored.UpdatedAt = now
		result.EvsesMoved++
		for _, c := range s.assetConnectors {
			if c.EvseUid != evse.Id {
				continue
			}
			for _, ref := range []*string{c.TariffRef, c.AdhocTariffRef} {
				if ref != nil {
					tariffs[*ref] = true
				}
			}
			c.OrgId = to.String()
			c.TariffRef = nil
			c.AdhocTariffRef = nil
			c.Version++
			c.UpdatedAt = now
			result.ConnectorsMoved++
		}
	}
	for ref := range tariffs {
		result.ClearedTariffRefs = append(result.ClearedTariffRefs, ref)
	}
	slices.Sort(result.ClearedTariffRefs)

	// the stored auth details may be those that their caller set: they are replaced
	auth := *s.chargeStationAuth[p.StationKey]
	auth.OrgId = to.String()
	s.chargeStationAuth[p.StationKey] = &auth
	registered.LocationId = clonePtr(target)
	registered.UpdatedAt = now
	return result, nil
}

// assetLocationShared reports whether another station than stationKey, or an EVSE of
// another station, is at the location. The caller holds the lock.
func (s *Store) assetLocationShared(locationId, stationKey string) bool {
	for _, evse := range s.assetEvses {
		if evse.LocationId == locationId && evse.StationKey != stationKey {
			return true
		}
	}
	for key, registered := range s.chargeStations {
		if key != stationKey && registered.LocationId != nil && *registered.LocationId == locationId {
			return true
		}
	}
	return false
}
