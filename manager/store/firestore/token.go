// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type token struct {
	CountryCode  string  `firestore:"country"`
	PartyId      string  `firestore:"partyId"`
	Type         string  `firestore:"type"`
	Uid          string  `firestore:"uid"`
	ContractId   string  `firestore:"contractId"`
	VisualNumber *string `firestore:"visual"`
	Issuer       string  `firestore:"issuer"`
	GroupId      *string `firestore:"group"`
	Valid        bool    `firestore:"valid"`
	LanguageCode *string `firestore:"lang"`
	CacheMode    string  `firestore:"cache"`
	// ExpiresAt is not written when the token does not expire
	ExpiresAt *time.Time `firestore:"expiresAt,omitempty"`
}

func (s *Store) SetToken(ctx context.Context, tok *store.Token) error {
	tokenRef := s.client.Doc(fmt.Sprintf("Token/%s", tok.Uid))
	tokenData := &token{
		CountryCode:  tok.CountryCode,
		PartyId:      tok.PartyId,
		Type:         tok.Type,
		Uid:          tok.Uid,
		ContractId:   tok.ContractId,
		VisualNumber: tok.VisualNumber,
		Issuer:       tok.Issuer,
		GroupId:      tok.GroupId,
		Valid:        tok.Valid,
		LanguageCode: tok.LanguageCode,
		CacheMode:    tok.CacheMode,
		ExpiresAt:    tok.ExpiresAt,
	}
	_, err := tokenRef.Set(ctx, tokenData)

	if err != nil {
		return fmt.Errorf("setting token: %s: %w", tok.Uid, err)
	}
	return nil
}

func (s *Store) LookupToken(ctx context.Context, tokenUid string) (*store.Token, error) {
	tokenRef := s.client.Doc(fmt.Sprintf("Token/%s", tokenUid))
	snap, err := tokenRef.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup token %s: %w", tokenUid, err)
	}
	return newToken(snap, tokenUid)
}

// LookupTokenForOrg ignores orgId: Firestore is single-tenant, it stores one token for
// each UID and no org.
func (s *Store) LookupTokenForOrg(ctx context.Context, _, tokenUid string) (*store.Token, error) {
	return s.LookupToken(ctx, tokenUid)
}

func newToken(snap *firestore.DocumentSnapshot, tokenUid string) (*store.Token, error) {
	var tok token
	if err := snap.DataTo(&tok); err != nil {
		return nil, fmt.Errorf("map token: %s: %w", tokenUid, err)
	}
	return &store.Token{
		CountryCode:  tok.CountryCode,
		PartyId:      tok.PartyId,
		Type:         tok.Type,
		Uid:          tok.Uid,
		ContractId:   tok.ContractId,
		VisualNumber: tok.VisualNumber,
		Issuer:       tok.Issuer,
		GroupId:      tok.GroupId,
		Valid:        tok.Valid,
		LanguageCode: tok.LanguageCode,
		CacheMode:    tok.CacheMode,
		LastUpdated:  snap.UpdateTime.Format(time.RFC3339),
		ExpiresAt:    tok.ExpiresAt,
	}, nil
}

func (s *Store) ListTokens(context context.Context, offset int, limit int) ([]*store.Token, error) {
	var tokens []*store.Token
	iter := s.client.Collection("Token").OrderBy("uid", firestore.Asc).Offset(offset).Limit(limit).Documents(context)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("next token: %w", err)
		}
		tok, err := newToken(doc, doc.Ref.ID)
		if err != nil {
			return nil, fmt.Errorf("map token: %w", err)
		}
		tokens = append(tokens, tok)
	}
	if tokens == nil {
		tokens = make([]*store.Token, 0)
	}
	return tokens, nil
}

// ListTokensForOrgs ignores orgIds: Firestore is single-tenant, it stores no org and
// lists every token.
func (s *Store) ListTokensForOrgs(ctx context.Context, _ []string, offset, limit int) ([]*store.Token, error) {
	return s.ListTokens(ctx, offset, limit)
}

// DeleteToken is not implemented: Firestore stores no org, so it cannot delete only the
// token of one org.
func (s *Store) DeleteToken(context.Context, string, string) error {
	return fmt.Errorf("DeleteToken: %w", store.ErrNotImplemented)
}
