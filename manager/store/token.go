// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"time"
)

const (
	CacheModeAlways         = "ALWAYS"
	CacheModeAllowed        = "ALLOWED"
	CacheModeAllowedOffline = "ALLOWED_OFFLINE"
	CacheModeNever          = "NEVER"
)

type Token struct {
	CountryCode  string
	PartyId      string
	Type         string
	Uid          string
	ContractId   string
	VisualNumber *string
	Issuer       string
	GroupId      *string
	Valid        bool
	LanguageCode *string
	CacheMode    string
	LastUpdated  string
	// ExpiresAt is the instant from which the token is no longer accepted:
	// authorisation returns Expired. nil means that the token does not expire.
	ExpiresAt *time.Time
	// OrgId is the org (UUID) that owns the token. A token is identified by its org,
	// country code, party id and UID: two orgs, or two parties of one org, can hold a
	// token with the same UID. PostgreSQL and the in-memory store need the org to set
	// a token. Firestore does not store the org (it has one token for each UID): it
	// always reads as "".
	OrgId string
}

type TokenStore interface {
	// SetToken creates the token, or replaces the token with the same OrgId,
	// CountryCode, PartyId and Uid.
	SetToken(ctx context.Context, token *Token) error
	// LookupToken finds a token by UID across all orgs (a valid token first, then the
	// most recently updated one). It returns nil when there is none.
	//
	// It is for single-tenant use only: where the org is known, use LookupTokenForOrg.
	LookupToken(ctx context.Context, tokenUid string) (*Token, error)
	// ListTokens lists the tokens of all orgs.
	ListTokens(context context.Context, offset int, limit int) ([]*Token, error)
	// LookupTokenForOrg finds a token by UID inside one org (a valid token first, then
	// the most recently updated one). It returns nil when the org has no such token.
	// Firestore stores no org: it ignores orgId.
	LookupTokenForOrg(ctx context.Context, orgId, tokenUid string) (*Token, error)
	// ListTokensForOrgs lists the tokens of the given orgs, the token that was created
	// last first. Firestore stores no org: it lists every token.
	ListTokensForOrgs(ctx context.Context, orgIds []string, offset, limit int) ([]*Token, error)
}
