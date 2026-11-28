// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Store) CreateTokenGroup(ctx context.Context, g *store.TokenGroup) error {
	id, err := newAssetId(g.Id)
	if err != nil {
		return fmt.Errorf("create token group: invalid id %q: %w", g.Id, err)
	}
	orgId, err := pgUUID(g.OrgId)
	if err != nil {
		return fmt.Errorf("create token group: invalid org id %q: %w", g.OrgId, err)
	}
	tariffRef, err := optionalUUID(g.OverrideTariffRef)
	if err != nil {
		return fmt.Errorf("create token group: invalid override tariff ref %q: %w", *g.OverrideTariffRef, err)
	}

	row, err := s.writeQueries(ctx).CreateTokenGroup(ctx, CreateTokenGroupParams{
		ID:                id,
		OrgID:             orgId,
		Name:              g.Name,
		PrefixMatch:       g.PrefixMatch,
		OverrideTariffRef: tariffRef,
	})
	if err != nil {
		// a name that is taken is ErrAlreadyExists, a tariff that does not exist ErrNotFound
		return assetWriteError("create token group", err)
	}
	*g = *tokenGroupFromRow(row)
	return nil
}

func (s *Store) UpdateTokenGroup(ctx context.Context, g *store.TokenGroup) error {
	id, ok := assetId(g.Id)
	if !ok {
		return fmt.Errorf("update token group %s: %w", g.Id, store.ErrNotFound)
	}
	tariffRef, err := optionalUUID(g.OverrideTariffRef)
	if err != nil {
		return fmt.Errorf("update token group %s: invalid override tariff ref %q: %w", g.Id, *g.OverrideTariffRef, err)
	}

	row, err := s.writeQueries(ctx).UpdateTokenGroup(ctx, UpdateTokenGroupParams{
		ID:                id,
		Name:              g.Name,
		PrefixMatch:       g.PrefixMatch,
		OverrideTariffRef: tariffRef,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("update token group %s: %w", g.Id, store.ErrNotFound)
	}
	if err != nil {
		return assetWriteError("update token group "+g.Id, err)
	}
	*g = *tokenGroupFromRow(row)
	return nil
}

func (s *Store) LookupTokenGroup(ctx context.Context, groupId string) (*store.TokenGroup, error) {
	id, ok := assetId(groupId)
	if !ok {
		return nil, nil
	}
	row, err := s.readQueries(ctx).GetTokenGroup(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup token group %s: %w", groupId, err)
	}
	return tokenGroupFromRow(row), nil
}

func (s *Store) ListTokenGroups(ctx context.Context, orgIds []string, page store.PageRequest) ([]*store.TokenGroup, string, error) {
	afterId, err := store.DecodeCursor(page.Cursor)
	if err != nil {
		return nil, "", fmt.Errorf("list token groups: %w", err)
	}
	pgAfterId, err := pgUUID(afterId)
	if err != nil {
		return nil, "", fmt.Errorf("list token groups: %w", err)
	}
	ids := make([]pgtype.UUID, len(orgIds))
	for i, orgId := range orgIds {
		if ids[i], err = pgUUID(orgId); err != nil {
			return nil, "", fmt.Errorf("list token groups: invalid org id %q: %w", orgId, err)
		}
	}
	if len(ids) == 0 {
		return []*store.TokenGroup{}, "", nil
	}
	limit := page.EffectiveLimit()
	// one more row than the page tells whether there is a next page
	rowLimit, err := safeIntToInt32(limit + 1)
	if err != nil {
		return nil, "", fmt.Errorf("list token groups: %w", err)
	}

	rows, err := s.readQueries(ctx).ListTokenGroups(ctx, ListTokenGroupsParams{OrgIds: ids, AfterID: pgAfterId, RowLimit: rowLimit})
	if err != nil {
		return nil, "", fmt.Errorf("list token groups: %w", err)
	}

	nextCursor := ""
	if len(rows) > limit {
		rows = rows[:limit]
		nextCursor = store.EncodeCursor(uuidString(rows[limit-1].ID))
	}
	groups := make([]*store.TokenGroup, len(rows))
	for i, row := range rows {
		groups[i] = tokenGroupFromRow(row)
	}
	return groups, nextCursor, nil
}

// DeleteTokenGroup deletes the group; its tokens leave it (ON DELETE SET NULL).
func (s *Store) DeleteTokenGroup(ctx context.Context, groupId string) error {
	id, ok := assetId(groupId)
	if !ok {
		return nil
	}
	if err := s.writeQueries(ctx).DeleteTokenGroup(ctx, id); err != nil {
		return fmt.Errorf("delete token group %s: %w", groupId, err)
	}
	return nil
}

func (s *Store) SetTokenGroup(ctx context.Context, orgId, tokenUid string, groupId *string) error {
	what := fmt.Sprintf("set the group of token %s of org %s", tokenUid, orgId)
	org, err := pgUUID(orgId)
	if err != nil {
		return fmt.Errorf("%s: invalid org id: %w", what, err)
	}
	group := pgtype.UUID{}
	if groupId != nil {
		var ok bool
		if group, ok = assetId(*groupId); !ok {
			// not a UUID: no group has this id
			return fmt.Errorf("%s: group %s: %w", what, *groupId, store.ErrNotFound)
		}
	}

	changed, err := s.writeQueries(ctx).SetTokenGroup(ctx, SetTokenGroupParams{TokenGroupID: group, OrgID: org, Uid: tokenUid})
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if changed == 0 {
		return fmt.Errorf("%s: no such token, or no such group of the org: %w", what, store.ErrNotFound)
	}
	return nil
}

func (s *Store) ListTokenGroupMembers(ctx context.Context, groupId string, page store.PageRequest) ([]*store.Token, string, error) {
	afterId, err := decodeTokenCursor(page.Cursor)
	if err != nil {
		return nil, "", fmt.Errorf("list the tokens of token group %s: %w", groupId, err)
	}
	id, ok := assetId(groupId)
	if !ok {
		return []*store.Token{}, "", nil
	}
	limit := page.EffectiveLimit()
	// one more row than the page tells whether there is a next page
	rowLimit, err := safeIntToInt32(limit + 1)
	if err != nil {
		return nil, "", fmt.Errorf("list the tokens of token group %s: %w", groupId, err)
	}

	rows, err := s.readQueries(ctx).ListTokenGroupMembers(ctx, ListTokenGroupMembersParams{TokenGroupID: id, AfterID: afterId, RowLimit: rowLimit})
	if err != nil {
		return nil, "", fmt.Errorf("list the tokens of token group %s: %w", groupId, err)
	}

	nextCursor := ""
	if len(rows) > limit {
		rows = rows[:limit]
		nextCursor = store.EncodeKeyCursor(strconv.FormatInt(rows[limit-1].ID, 10))
	}
	tokens := make([]*store.Token, len(rows))
	for i := range rows {
		tokens[i] = toStoreToken(&rows[i])
	}
	return tokens, nextCursor, nil
}

// decodeTokenCursor returns the row id of the token after which a page of tokens starts:
// 0, before every token, for the first page.
func decodeTokenCursor(cursor string) (int64, error) {
	key, err := store.DecodeKeyCursor(cursor)
	if err != nil || key == "" {
		return 0, err
	}
	id, err := strconv.ParseInt(key, 10, 64)
	if err != nil || id < 0 {
		return 0, fmt.Errorf("%w: not the cursor of a token", store.ErrInvalidCursor)
	}
	return id, nil
}

// FindTokenGroupForUid looks for the group of the token with the UID first, then for a
// group whose tokens are prefixes.
func (s *Store) FindTokenGroupForUid(ctx context.Context, orgId, uid string) (*store.TokenGroup, error) {
	org, err := pgUUID(orgId)
	if err != nil {
		return nil, fmt.Errorf("find the token group of %s: invalid org id %q: %w", uid, orgId, err)
	}
	q := s.readQueries(ctx)

	row, err := q.FindExactTokenGroup(ctx, FindExactTokenGroupParams{OrgID: org, Uid: uid})
	if err == nil {
		return tokenGroupFromRow(row), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("find the token group of %s in org %s: %w", uid, orgId, err)
	}

	row, err = q.FindPrefixTokenGroup(ctx, FindPrefixTokenGroupParams{OrgID: org, Uid: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find the token group of %s in org %s: %w", uid, orgId, err)
	}
	return tokenGroupFromRow(row), nil
}

func tokenGroupFromRow(row TokenGroup) *store.TokenGroup {
	return &store.TokenGroup{
		Id:                uuidString(row.ID),
		OrgId:             uuidString(row.OrgID),
		Name:              row.Name,
		PrefixMatch:       row.PrefixMatch,
		OverrideTariffRef: optionalUUIDString(row.OverrideTariffRef),
		CreatedAt:         fromPgTimestamptz(row.CreatedAt),
		UpdatedAt:         fromPgTimestamptz(row.UpdatedAt),
	}
}
