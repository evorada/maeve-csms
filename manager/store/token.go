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
}

type TokenStore interface {
	SetToken(ctx context.Context, token *Token) error
	LookupToken(ctx context.Context, tokenUid string) (*Token, error)
	ListTokens(context context.Context, offset int, limit int) ([]*Token, error)
}
