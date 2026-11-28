// SPDX-License-Identifier: Apache-2.0

package inmemory

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The tariff store keeps the rules of the PostgreSQL tables: the OCPI id of a tariff is
// unique for its party, a version is stored once and never changed, a new version needs
// the current version that the caller saw, and a tariff that a connector, a location or a
// token group refers to cannot be deleted.

// tariffTime returns the time as PostgreSQL reads it back: in UTC, to the microsecond.
func tariffTime(t time.Time) time.Time {
	return t.UTC().Truncate(time.Microsecond)
}

// copyTariffVersion returns a deep copy of the version as PostgreSQL reads it back: the
// lists are empty instead of nil, the times are in UTC, and the parts that PostgreSQL
// holds as JSON documents are what their JSON says. A version that cannot be written as
// JSON is an error.
func copyTariffVersion(v *store.TariffVersion) (*store.TariffVersion, error) {
	document, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	c := &store.TariffVersion{}
	if err := json.Unmarshal(document, c); err != nil {
		return nil, err
	}
	if c.AltText == nil {
		c.AltText = []store.DisplayText{}
	}
	if c.Elements == nil {
		c.Elements = []store.TariffElement{}
	}
	if c.StartDateTime != nil {
		*c.StartDateTime = tariffTime(*c.StartDateTime)
	}
	if c.EndDateTime != nil {
		*c.EndDateTime = tariffTime(*c.EndDateTime)
	}
	c.CreatedAt = tariffTime(v.CreatedAt)
	return c, nil
}

// tariffWithCurrent returns a copy of the stored tariff with a copy of its current
// version. The caller holds the lock.
func (s *Store) tariffWithCurrent(t *store.Tariff) (*store.Tariff, error) {
	c := *t
	c.DeletedAt = copyOrNil(t.DeletedAt)
	current, err := copyTariffVersion(s.tariffVersions[t.Id][t.CurrentVersion-1])
	if err != nil {
		return nil, fmt.Errorf("tariff %s: %w", t.Id, err)
	}
	c.Current = current
	return &c, nil
}

func (s *Store) CreateTariff(_ context.Context, t *store.Tariff, v *store.TariffVersion) error {
	id, err := newAssetKey(t.Id)
	if err != nil {
		return fmt.Errorf("create tariff: invalid id %q: %w", t.Id, err)
	}
	orgId, err := uuid.Parse(t.OrgId)
	if err != nil {
		return fmt.Errorf("create tariff: invalid org id %q: %w", t.OrgId, err)
	}
	version, err := copyTariffVersion(v)
	if err != nil {
		return fmt.Errorf("create tariff: version 1: %w", err)
	}

	s.Lock()
	defer s.Unlock()
	stored := &store.Tariff{
		Id:             id,
		OrgId:          orgId.String(),
		PartyRef:       t.PartyRef,
		OcpiId:         t.OcpiId,
		Name:           t.Name,
		CurrentVersion: 1,
	}
	if stored.OcpiId == "" {
		stored.OcpiId = id
	}
	if _, found := s.tariffs[id]; found {
		return fmt.Errorf("create tariff %s: %w", id, store.ErrAlreadyExists)
	}
	for _, other := range s.tariffs {
		// a deleted tariff keeps its OCPI id
		if other.PartyRef == stored.PartyRef && other.OcpiId == stored.OcpiId {
			return fmt.Errorf("create tariff: OCPI id %s/%s: %w", stored.PartyRef, stored.OcpiId, store.ErrAlreadyExists)
		}
	}
	now := tariffTime(s.clock.Now())
	stored.CreatedAt = now
	stored.UpdatedAt = now
	version.TariffId = id
	version.Version = 1
	version.CreatedAt = now
	s.tariffs[id] = stored
	s.tariffVersions[id] = []*store.TariffVersion{version}

	result, err := s.tariffWithCurrent(stored)
	if err != nil {
		return err
	}
	// the caller gets a version of its own, not the one that the tariff points to
	given, err := copyTariffVersion(version)
	if err != nil {
		return err
	}
	*t = *result
	*v = *given
	return nil
}

func (s *Store) AddTariffVersion(_ context.Context, tariffId string, expectedCurrent int, v *store.TariffVersion) error {
	what := "add a version to tariff " + tariffId
	version, err := copyTariffVersion(v)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}

	s.Lock()
	defer s.Unlock()
	id, _ := assetKey(tariffId)
	t := s.tariffs[id]
	switch {
	case t == nil:
		return fmt.Errorf("%s: %w", what, store.ErrNotFound)
	case t.DeletedAt != nil:
		return fmt.Errorf("%s: the tariff is deleted: %w", what, store.ErrNotFound)
	case t.CurrentVersion != expectedCurrent:
		return fmt.Errorf("%s: the current version is %d, not %d: %w", what, t.CurrentVersion, expectedCurrent, store.ErrVersionConflict)
	}
	now := tariffTime(s.clock.Now())
	version.TariffId = id
	version.Version = t.CurrentVersion + 1
	version.CreatedAt = now
	// the versions that are stored already are not touched
	s.tariffVersions[id] = append(s.tariffVersions[id], version)
	t.CurrentVersion = version.Version
	t.UpdatedAt = now

	given, err := copyTariffVersion(version)
	if err != nil {
		return err
	}
	*v = *given
	return nil
}

func (s *Store) RenameTariff(_ context.Context, tariffId, name string) error {
	s.Lock()
	defer s.Unlock()
	id, _ := assetKey(tariffId)
	t := s.tariffs[id]
	if t == nil || t.DeletedAt != nil {
		return fmt.Errorf("rename tariff %s: %w", tariffId, store.ErrNotFound)
	}
	t.Name = name
	t.UpdatedAt = tariffTime(s.clock.Now())
	return nil
}

func (s *Store) LookupTariff(_ context.Context, tariffId string) (*store.Tariff, error) {
	id, ok := assetKey(tariffId)
	if !ok {
		return nil, nil
	}
	s.Lock()
	defer s.Unlock()
	if t := s.tariffs[id]; t != nil {
		return s.tariffWithCurrent(t)
	}
	return nil, nil
}

func (s *Store) LookupTariffByOcpiId(_ context.Context, partyRef, ocpiId string) (*store.Tariff, error) {
	s.Lock()
	defer s.Unlock()
	for _, t := range s.tariffs {
		if t.PartyRef == partyRef && t.OcpiId == ocpiId {
			return s.tariffWithCurrent(t)
		}
	}
	return nil, nil
}

func (s *Store) LookupTariffVersion(_ context.Context, tariffId string, version int) (*store.TariffVersion, error) {
	id, ok := assetKey(tariffId)
	if !ok {
		return nil, nil
	}
	s.Lock()
	defer s.Unlock()
	versions := s.tariffVersions[id]
	if version < 1 || version > len(versions) {
		return nil, nil
	}
	return copyTariffVersion(versions[version-1])
}

func (s *Store) ListTariffs(_ context.Context, filter store.TariffFilter) ([]*store.Tariff, string, error) {
	afterId, err := store.DecodeCursor(filter.Page.Cursor)
	if err != nil {
		return nil, "", fmt.Errorf("list tariffs: %w", err)
	}
	orgIds := make(map[string]bool, len(filter.OrgIds))
	for _, orgId := range filter.OrgIds {
		u, err := uuid.Parse(orgId)
		if err != nil {
			return nil, "", fmt.Errorf("list tariffs: invalid org id %q: %w", orgId, err)
		}
		orgIds[u.String()] = true
	}
	query := ""
	if filter.Query != nil {
		query = strings.ToLower(*filter.Query)
	}

	s.Lock()
	defer s.Unlock()
	var matching []*store.Tariff
	for _, t := range s.tariffs {
		current := s.tariffVersions[t.Id][t.CurrentVersion-1]
		switch {
		case !orgIds[t.OrgId],
			filter.PartyRef != nil && t.PartyRef != *filter.PartyRef,
			filter.Type != nil && (current.Type == nil || *current.Type != *filter.Type),
			filter.Query != nil && !strings.Contains(strings.ToLower(t.Name), query) && !strings.Contains(strings.ToLower(t.OcpiId), query),
			!filter.IncludeDeleted && t.DeletedAt != nil,
			t.Id <= afterId: // the canonical text of a UUID sorts like its bytes
			continue
		}
		matching = append(matching, t)
	}
	slices.SortFunc(matching, func(a, b *store.Tariff) int { return strings.Compare(a.Id, b.Id) })

	limit := filter.Page.EffectiveLimit()
	nextCursor := ""
	if len(matching) > limit {
		matching = matching[:limit]
		nextCursor = store.EncodeCursor(matching[limit-1].Id)
	}
	page := make([]*store.Tariff, len(matching))
	for i, t := range matching {
		if page[i], err = s.tariffWithCurrent(t); err != nil {
			return nil, "", err
		}
	}
	return page, nextCursor, nil
}

func (s *Store) ListTariffVersions(_ context.Context, tariffId string) ([]*store.TariffVersion, error) {
	s.Lock()
	defer s.Unlock()
	id, _ := assetKey(tariffId)
	stored := s.tariffVersions[id]
	versions := make([]*store.TariffVersion, len(stored))
	for i, v := range stored {
		c, err := copyTariffVersion(v)
		if err != nil {
			return nil, err
		}
		// the newest first
		versions[len(stored)-1-i] = c
	}
	return versions, nil
}

func (s *Store) DeleteTariff(_ context.Context, tariffId string) error {
	s.Lock()
	defer s.Unlock()
	id, _ := assetKey(tariffId)
	t := s.tariffs[id]
	if t == nil || t.DeletedAt != nil {
		return nil
	}
	for _, c := range s.assetConnectors {
		for _, ref := range []*string{c.TariffRef, c.AdhocTariffRef} {
			if ref != nil && *ref == id {
				return fmt.Errorf("delete tariff %s: connector %s: %w", tariffId, c.Id, store.ErrInUse)
			}
		}
	}
	for _, loc := range s.assetLocations {
		if loc.DefaultTariffRef != nil && *loc.DefaultTariffRef == id {
			return fmt.Errorf("delete tariff %s: location %s: %w", tariffId, loc.Id, store.ErrInUse)
		}
	}
	for _, g := range s.tokenGroups {
		if g.OverrideTariffRef != nil && *g.OverrideTariffRef == id {
			return fmt.Errorf("delete tariff %s: token group %s: %w", tariffId, g.Id, store.ErrInUse)
		}
	}
	now := tariffTime(s.clock.Now())
	t.DeletedAt = &now
	t.UpdatedAt = now
	return nil
}
