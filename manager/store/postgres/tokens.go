// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// SetToken creates or updates a token in the database
func (s *Store) SetToken(ctx context.Context, token *store.Token) error {
	lastUpdated, err := time.Parse(time.RFC3339, token.LastUpdated)
	if err != nil {
		return fmt.Errorf("invalid last_updated timestamp: %w", err)
	}

	params := CreateTokenParams{
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
	}

	_, err = s.q.CreateToken(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to create token: %w", err)
	}

	return nil
}

// LookupToken retrieves a token by its UID
func (s *Store) LookupToken(ctx context.Context, tokenUid string) (*store.Token, error) {
	token, err := s.q.GetToken(ctx, tokenUid)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup token: %w", err)
	}

	return toStoreToken(&token), nil
}

// ListTokens retrieves a paginated list of tokens
func (s *Store) ListTokens(ctx context.Context, offset int, limit int) ([]*store.Token, error) {
	tokens, err := s.q.ListTokens(ctx, ListTokensParams{
		Limit:  int32(limit),
		Offset: int32(offset),
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
	}
}

// Helper functions for pgtype conversions

func textFromString(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func stringFromText(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func timestampFromTime(t time.Time) pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  t,
		Valid: true,
	}
}

func timeFromTimestamp(ts pgtype.Timestamp) time.Time {
	if !ts.Valid {
		return time.Time{}
	}
	return ts.Time
}
