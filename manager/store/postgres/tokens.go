// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// SetToken creates the token, or updates the token with the same org, country code,
// party id and UID.
func (s *Store) SetToken(ctx context.Context, token *store.Token) error {
	slog.Debug("setting token", "uid", token.Uid, "contract_id", token.ContractId)

	if token.OrgId == "" {
		return fmt.Errorf("token org id is required")
	}
	orgId, err := pgUUID(token.OrgId)
	if err != nil {
		return fmt.Errorf("invalid token org id %q: %w", token.OrgId, err)
	}

	lastUpdated, err := time.Parse(time.RFC3339, token.LastUpdated)
	if err != nil {
		slog.Error("invalid timestamp in token", "uid", token.Uid, "error", err)
		return fmt.Errorf("invalid last_updated timestamp: %w", err)
	}

	_, err = s.writeQueries(ctx).UpsertToken(ctx, UpsertTokenParams{
		CountryCode:  token.CountryCode,
		PartyID:      token.PartyId,
		Type:         token.Type,
		Uid:          token.Uid,
		ContractID:   token.ContractId,
		VisualNumber: textFromString(token.VisualNumber),
		Issuer:       token.Issuer,
		GroupID:      textFromString(token.GroupId),
		Valid:        token.Valid,
		LanguageCode: textFromString(token.LanguageCode),
		CacheMode:    token.CacheMode,
		LastUpdated:  timestampFromTime(lastUpdated),
		ExpiresAt:    toNullTimestamptz(token.ExpiresAt),
		OrgID:        orgId,
	})
	if err != nil {
		slog.Error("failed to set token", "uid", token.Uid, "error", err)
		return fmt.Errorf("failed to set token: %w", err)
	}

	slog.Debug("token set successfully", "uid", token.Uid)
	return nil
}

// LookupToken retrieves a token by its UID, whatever its org: a valid token first,
// then the most recently updated one.
func (s *Store) LookupToken(ctx context.Context, tokenUid string) (*store.Token, error) {
	slog.Debug("looking up token", "uid", tokenUid)

	token, err := s.readQueries(ctx).GetTokenAnyOrg(ctx, tokenUid)
	if err != nil {
		if err == pgx.ErrNoRows {
			slog.Debug("token not found", "uid", tokenUid)
			return nil, nil
		}
		slog.Error("failed to lookup token", "uid", tokenUid, "error", err)
		return nil, fmt.Errorf("failed to lookup token: %w", err)
	}

	slog.Debug("token found", "uid", tokenUid, "contract_id", token.ContractID)
	return toStoreToken(&token), nil
}

// LookupTokenForOrg retrieves a token of one org by its UID: a valid token first, then
// the most recently updated one.
func (s *Store) LookupTokenForOrg(ctx context.Context, orgId, tokenUid string) (*store.Token, error) {
	slog.Debug("looking up token", "org_id", orgId, "uid", tokenUid)

	id, err := pgUUID(orgId)
	if err != nil {
		return nil, fmt.Errorf("invalid org id %q: %w", orgId, err)
	}

	token, err := s.readQueries(ctx).GetTokenForOrg(ctx, GetTokenForOrgParams{OrgID: id, Uid: tokenUid})
	if err != nil {
		if err == pgx.ErrNoRows {
			slog.Debug("token not found", "org_id", orgId, "uid", tokenUid)
			return nil, nil
		}
		slog.Error("failed to lookup token", "org_id", orgId, "uid", tokenUid, "error", err)
		return nil, fmt.Errorf("failed to lookup token: %w", err)
	}

	slog.Debug("token found", "org_id", orgId, "uid", tokenUid, "contract_id", token.ContractID)
	return toStoreToken(&token), nil
}

// ListTokensForOrgs retrieves a paginated list of the tokens of the given orgs, the
// token that was created last first.
func (s *Store) ListTokensForOrgs(ctx context.Context, orgIds []string, offset, limit int) ([]*store.Token, error) {
	limitInt32, err := safeIntToInt32(limit)
	if err != nil {
		return nil, fmt.Errorf("invalid limit value: %w", err)
	}
	offsetInt32, err := safeIntToInt32(offset)
	if err != nil {
		return nil, fmt.Errorf("invalid offset value: %w", err)
	}

	ids := make([]pgtype.UUID, len(orgIds))
	for i, orgId := range orgIds {
		ids[i], err = pgUUID(orgId)
		if err != nil {
			return nil, fmt.Errorf("invalid org id %q: %w", orgId, err)
		}
	}

	tokens, err := s.readQueries(ctx).ListTokensForOrgs(ctx, ListTokensForOrgsParams{
		OrgIds:    ids,
		RowLimit:  limitInt32,
		RowOffset: offsetInt32,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list tokens: %w", err)
	}

	result := make([]*store.Token, len(tokens))
	for i, t := range tokens {
		result[i] = toStoreToken(&t)
	}

	return result, nil
}

// DeleteToken deletes the tokens of the org with the UID, of every party.
func (s *Store) DeleteToken(ctx context.Context, orgId, tokenUid string) error {
	id, err := pgUUID(orgId)
	if err != nil {
		return fmt.Errorf("delete token: invalid org id %q: %w", orgId, err)
	}
	if err := s.writeQueries(ctx).DeleteToken(ctx, DeleteTokenParams{OrgID: id, Uid: tokenUid}); err != nil {
		return fmt.Errorf("delete token %s of org %s: %w", tokenUid, orgId, err)
	}
	return nil
}

// ListTokens retrieves a paginated list of tokens
func (s *Store) ListTokens(ctx context.Context, offset int, limit int) ([]*store.Token, error) {
	limitInt32, err := safeIntToInt32(limit)
	if err != nil {
		return nil, fmt.Errorf("invalid limit value: %w", err)
	}
	offsetInt32, err := safeIntToInt32(offset)
	if err != nil {
		return nil, fmt.Errorf("invalid offset value: %w", err)
	}

	tokens, err := s.readQueries(ctx).ListTokens(ctx, ListTokensParams{
		Limit:  limitInt32,
		Offset: offsetInt32,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list tokens: %w", err)
	}

	result := make([]*store.Token, len(tokens))
	for i, t := range tokens {
		result[i] = toStoreToken(&t)
	}

	return result, nil
}

// toStoreToken converts a PostgreSQL Token model to a store.Token
func toStoreToken(t *Token) *store.Token {
	return &store.Token{
		CountryCode:  t.CountryCode,
		PartyId:      t.PartyID,
		Type:         t.Type,
		Uid:          t.Uid,
		ContractId:   t.ContractID,
		VisualNumber: stringFromText(t.VisualNumber),
		Issuer:       t.Issuer,
		GroupId:      stringFromText(t.GroupID),
		Valid:        t.Valid,
		LanguageCode: stringFromText(t.LanguageCode),
		CacheMode:    t.CacheMode,
		LastUpdated:  timeFromTimestamp(t.LastUpdated).Format(time.RFC3339),
		ExpiresAt:    fromNullTimestamptz(t.ExpiresAt),
		OrgId:        uuidString(t.OrgID),
	}
}
