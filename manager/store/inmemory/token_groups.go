// SPDX-License-Identifier: Apache-2.0

package inmemory

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The token group store keeps the rules of the PostgreSQL tables: the name of a group is
// unique in its org, the override tariff of a group exists, a token is in one group at
// most, and the tokens of a group that is deleted are in no group.

func copyTokenGroup(g *store.TokenGroup) *store.TokenGroup {
	c := *g
	c.OverrideTariffRef = copyOrNil(g.OverrideTariffRef)
	return &c
}

// tokenGroupNameTaken reports whether another group of the org has the name of g. The
// caller holds the lock.
func (s *Store) tokenGroupNameTaken(g *store.TokenGroup) bool {
	for _, other := range s.tokenGroups {
		if other.Id != g.Id && other.OrgId == g.OrgId && other.Name == g.Name {
			return true
		}
	}
	return false
}

// tokenGroupTariff returns the canonical form of the override tariff of a group, after
// checking that the tariff exists (a deleted tariff is still there). The caller holds
// the lock.
func (s *Store) tokenGroupTariff(ref *string) (*string, error) {
	tariffRef, err := optionalAssetKey(ref)
	if err != nil {
		return nil, fmt.Errorf("invalid override tariff ref %q: %w", *ref, err)
	}
	if tariffRef != nil && s.tariffs[*tariffRef] == nil {
		return nil, fmt.Errorf("tariff %s: %w", *tariffRef, store.ErrNotFound)
	}
	return tariffRef, nil
}

func (s *Store) CreateTokenGroup(_ context.Context, g *store.TokenGroup) error {
	id, err := newAssetKey(g.Id)
	if err != nil {
		return fmt.Errorf("create token group: invalid id %q: %w", g.Id, err)
	}
	orgId, err := uuid.Parse(g.OrgId)
	if err != nil {
		return fmt.Errorf("create token group: invalid org id %q: %w", g.OrgId, err)
	}

	s.Lock()
	defer s.Unlock()
	stored := &store.TokenGroup{Id: id, OrgId: orgId.String(), Name: g.Name, PrefixMatch: g.PrefixMatch}
	if stored.OverrideTariffRef, err = s.tokenGroupTariff(g.OverrideTariffRef); err != nil {
		return fmt.Errorf("create token group: %w", err)
	}
	if _, found := s.tokenGroups[id]; found {
		return fmt.Errorf("create token group %s: %w", id, store.ErrAlreadyExists)
	}
	if s.tokenGroupNameTaken(stored) {
		return fmt.Errorf("create token group: name %q: %w", stored.Name, store.ErrAlreadyExists)
	}
	now := tariffTime(s.clock.Now())
	stored.CreatedAt = now
	stored.UpdatedAt = now
	s.tokenGroups[id] = stored
	*g = *copyTokenGroup(stored)
	return nil
}

func (s *Store) UpdateTokenGroup(_ context.Context, g *store.TokenGroup) error {
	s.Lock()
	defer s.Unlock()
	id, _ := assetKey(g.Id)
	current := s.tokenGroups[id]
	if current == nil {
		return fmt.Errorf("update token group %s: %w", g.Id, store.ErrNotFound)
	}
	updated := copyTokenGroup(current)
	updated.Name = g.Name
	updated.PrefixMatch = g.PrefixMatch
	var err error
	if updated.OverrideTariffRef, err = s.tokenGroupTariff(g.OverrideTariffRef); err != nil {
		return fmt.Errorf("update token group %s: %w", g.Id, err)
	}
	if s.tokenGroupNameTaken(updated) {
		return fmt.Errorf("update token group %s: name %q: %w", g.Id, updated.Name, store.ErrAlreadyExists)
	}
	updated.UpdatedAt = tariffTime(s.clock.Now())
	s.tokenGroups[id] = updated
	*g = *copyTokenGroup(updated)
	return nil
}

func (s *Store) LookupTokenGroup(_ context.Context, groupId string) (*store.TokenGroup, error) {
	id, ok := assetKey(groupId)
	if !ok {
		return nil, nil
	}
	s.Lock()
	defer s.Unlock()
	if g := s.tokenGroups[id]; g != nil {
		return copyTokenGroup(g), nil
	}
	return nil, nil
}

func (s *Store) ListTokenGroups(_ context.Context, orgIds []string, page store.PageRequest) ([]*store.TokenGroup, string, error) {
	afterId, err := store.DecodeCursor(page.Cursor)
	if err != nil {
		return nil, "", fmt.Errorf("list token groups: %w", err)
	}
	orgs := make(map[string]bool, len(orgIds))
	for _, orgId := range orgIds {
		u, err := uuid.Parse(orgId)
		if err != nil {
			return nil, "", fmt.Errorf("list token groups: invalid org id %q: %w", orgId, err)
		}
		orgs[u.String()] = true
	}

	s.Lock()
	defer s.Unlock()
	var matching []*store.TokenGroup
	for _, g := range s.tokenGroups {
		// the canonical text of a UUID sorts like its bytes
		if orgs[g.OrgId] && g.Id > afterId {
			matching = append(matching, g)
		}
	}
	slices.SortFunc(matching, func(a, b *store.TokenGroup) int { return strings.Compare(a.Id, b.Id) })

	limit := page.EffectiveLimit()
	nextCursor := ""
	if len(matching) > limit {
		matching = matching[:limit]
		nextCursor = store.EncodeCursor(matching[limit-1].Id)
	}
	groups := make([]*store.TokenGroup, len(matching))
	for i, g := range matching {
		groups[i] = copyTokenGroup(g)
	}
	return groups, nextCursor, nil
}

func (s *Store) DeleteTokenGroup(_ context.Context, groupId string) error {
	id, ok := assetKey(groupId)
	if !ok {
		return nil
	}
	s.Lock()
	defer s.Unlock()
	if s.tokenGroups[id] == nil {
		return nil
	}
	delete(s.tokenGroups, id)
	for _, entry := range s.tokens {
		if entry.token.TokenGroupId != nil && *entry.token.TokenGroupId == id {
			entry.setTokenGroup(nil)
		}
	}
	return nil
}

// setTokenGroup puts the stored token into the group (nil: into no group). The stored
// token may be the one that its caller set, or one that a lookup handed out: it is
// replaced, not changed.
func (e *tokenEntry) setTokenGroup(groupId *string) {
	token := *e.token
	token.TokenGroupId = copyOrNil(groupId)
	e.token = &token
}

func (s *Store) SetTokenGroup(_ context.Context, orgId, tokenUid string, groupId *string) error {
	what := fmt.Sprintf("set the group of token %s of org %s", tokenUid, orgId)
	org, err := uuid.Parse(orgId)
	if err != nil {
		return fmt.Errorf("%s: invalid org id: %w", what, err)
	}

	s.Lock()
	defer s.Unlock()
	var group *string
	if groupId != nil {
		id, _ := assetKey(*groupId)
		if g := s.tokenGroups[id]; g == nil || g.OrgId != org.String() {
			return fmt.Errorf("%s: group %s: %w", what, *groupId, store.ErrNotFound)
		}
		group = &id
	}
	found := false
	for _, entry := range s.tokens {
		if entry.token.Uid == tokenUid && stationOrgId(entry.token.OrgId) == org.String() {
			entry.setTokenGroup(group)
			found = true
		}
	}
	if !found {
		return fmt.Errorf("%s: no such token: %w", what, store.ErrNotFound)
	}
	return nil
}

func (s *Store) ListTokenGroupMembers(_ context.Context, groupId string, page store.PageRequest) ([]*store.Token, string, error) {
	afterSeq, err := decodeTokenCursor(page.Cursor)
	if err != nil {
		return nil, "", fmt.Errorf("list the tokens of token group %s: %w", groupId, err)
	}
	id, ok := assetKey(groupId)
	if !ok {
		return []*store.Token{}, "", nil
	}

	s.Lock()
	defer s.Unlock()
	var members []*tokenEntry
	for _, entry := range s.tokens {
		if entry.token.TokenGroupId != nil && *entry.token.TokenGroupId == id && entry.seq > afterSeq {
			members = append(members, entry)
		}
	}
	slices.SortFunc(members, func(a, b *tokenEntry) int { return cmp.Compare(a.seq, b.seq) })

	limit := page.EffectiveLimit()
	nextCursor := ""
	if len(members) > limit {
		members = members[:limit]
		nextCursor = store.EncodeKeyCursor(strconv.FormatUint(members[limit-1].seq, 10))
	}
	tokens := make([]*store.Token, len(members))
	for i, entry := range members {
		tokens[i] = entry.token
	}
	return tokens, nextCursor, nil
}

// decodeTokenCursor returns the place in the order of the tokens after which a page of
// tokens starts: 0, before every token, for the first page.
func decodeTokenCursor(cursor string) (uint64, error) {
	key, err := store.DecodeKeyCursor(cursor)
	if err != nil || key == "" {
		return 0, err
	}
	seq, err := strconv.ParseUint(key, 10, 63)
	if err != nil {
		return 0, fmt.Errorf("%w: not the cursor of a token", store.ErrInvalidCursor)
	}
	return seq, nil
}

func (s *Store) FindTokenGroupForUid(_ context.Context, orgId, uid string) (*store.TokenGroup, error) {
	org, err := uuid.Parse(orgId)
	if err != nil {
		return nil, fmt.Errorf("find the token group of %s: invalid org id %q: %w", uid, orgId, err)
	}

	s.Lock()
	defer s.Unlock()
	// the tokens of the org that are in a group of the org, with their group
	type member struct {
		uid   string
		group *store.TokenGroup
	}
	var exact, prefixes []member
	presented := strings.ToUpper(uid)
	for _, entry := range s.tokens {
		token := entry.token
		if token.TokenGroupId == nil || stationOrgId(token.OrgId) != org.String() {
			continue
		}
		group := s.tokenGroups[*token.TokenGroupId]
		if group == nil || group.OrgId != org.String() {
			continue
		}
		switch {
		case token.Uid == uid:
			exact = append(exact, member{token.Uid, group})
		case group.PrefixMatch && token.Uid != "" && strings.HasPrefix(presented, strings.ToUpper(token.Uid)):
			prefixes = append(prefixes, member{token.Uid, group})
		}
	}
	if len(exact) > 0 {
		first := slices.MinFunc(exact, func(a, b member) int { return strings.Compare(a.group.Id, b.group.Id) })
		return copyTokenGroup(first.group), nil
	}
	if len(prefixes) > 0 {
		// the longest UID first, then the UID that sorts first
		best := slices.MinFunc(prefixes, func(a, b member) int {
			return cmp.Or(
				cmp.Compare(utf8.RuneCountInString(b.uid), utf8.RuneCountInString(a.uid)),
				strings.Compare(a.uid, b.uid),
				strings.Compare(a.group.Id, b.group.Id))
		})
		return copyTokenGroup(best.group), nil
	}
	return nil, nil
}
