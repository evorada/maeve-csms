// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func TestToken_SetAndLookup(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	token := &store.Token{
		OrgId:       store.LegacyOrgId,
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         "TOKEN001",
		ContractId:  "GBTWK001",
		Issuer:      "TestIssuer",
		Valid:       true,
		CacheMode:   store.CacheModeAlways,
		LastUpdated: "2026-01-01T00:00:00Z",
	}

	err := testStore.SetToken(ctx, token)
	require.NoError(t, err)

	got, err := testStore.LookupToken(ctx, "TOKEN001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "GB", got.CountryCode)
	assert.Equal(t, "TWK", got.PartyId)
	assert.Equal(t, "TOKEN001", got.Uid)
	assert.True(t, got.Valid)
}

func TestToken_LookupNotFound(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	got, err := testStore.LookupToken(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestToken_List(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		err := testStore.SetToken(ctx, &store.Token{
			OrgId:       store.LegacyOrgId,
			CountryCode: "GB",
			PartyId:     "TWK",
			Type:        "RFID",
			Uid:         "TOKEN00" + string(rune('1'+i)),
			ContractId:  "GBTWK00" + string(rune('1'+i)),
			Issuer:      "TestIssuer",
			Valid:       true,
			CacheMode:   store.CacheModeAlways,
			LastUpdated: "2026-01-01T00:00:00Z",
		})
		require.NoError(t, err)
	}

	results, err := testStore.ListTokens(ctx, 0, 10)
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestToken_Update(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	token := &store.Token{
		OrgId:       store.LegacyOrgId,
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         "TOKEN001",
		ContractId:  "GBTWK001",
		Issuer:      "TestIssuer",
		Valid:       true,
		CacheMode:   store.CacheModeAlways,
		LastUpdated: "2026-01-01T00:00:00Z",
	}

	err := testStore.SetToken(ctx, token)
	require.NoError(t, err)

	// Update to invalid
	token.Valid = false
	err = testStore.SetToken(ctx, token)
	require.NoError(t, err)

	got, err := testStore.LookupToken(ctx, "TOKEN001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, got.Valid)
}

func TestToken_ExpiresAt(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	// an offset and microseconds: the instant is kept, the time comes back in UTC
	expiresAt := time.Date(2027, 1, 31, 9, 30, 15, 123_456_000, time.FixedZone("CET", 3600))
	token := &store.Token{
		OrgId:       store.LegacyOrgId,
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         "TOKEN001",
		ContractId:  "GBTWK001",
		Issuer:      "TestIssuer",
		Valid:       true,
		CacheMode:   store.CacheModeAlways,
		LastUpdated: "2026-01-01T00:00:00Z",
		ExpiresAt:   &expiresAt,
	}
	lookup := func() *store.Token {
		t.Helper()
		got, err := testStore.LookupToken(ctx, "TOKEN001")
		require.NoError(t, err)
		require.NotNil(t, got)
		return got
	}

	// create
	require.NoError(t, testStore.SetToken(ctx, token))
	got := lookup()
	require.NotNil(t, got.ExpiresAt)
	assert.True(t, expiresAt.Equal(*got.ExpiresAt), "expires at %s, want %s", got.ExpiresAt, expiresAt)
	assert.Equal(t, time.UTC, got.ExpiresAt.Location())

	// a token without an expiry time, to tell the two apart in the list
	other := *token
	other.Uid = "TOKEN002"
	other.ContractId = "GBTWK002"
	other.ExpiresAt = nil
	require.NoError(t, testStore.SetToken(ctx, &other))
	got, err := testStore.LookupToken(ctx, "TOKEN002")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Nil(t, got.ExpiresAt)

	// update
	later := time.Date(2028, 6, 30, 22, 0, 0, 0, time.UTC)
	token.ExpiresAt = &later
	require.NoError(t, testStore.SetToken(ctx, token))
	got = lookup()
	require.NotNil(t, got.ExpiresAt)
	assert.True(t, later.Equal(*got.ExpiresAt), "expires at %s, want %s", got.ExpiresAt, later)

	// list
	listed, err := testStore.ListTokens(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	for _, tok := range listed {
		if tok.Uid == "TOKEN001" {
			require.NotNil(t, tok.ExpiresAt)
			assert.True(t, later.Equal(*tok.ExpiresAt), "listed token expires at %s, want %s", tok.ExpiresAt, later)
		} else {
			assert.Nil(t, tok.ExpiresAt, "token %s does not expire", tok.Uid)
		}
	}

	// updating without an expiry time clears it
	token.ExpiresAt = nil
	require.NoError(t, testStore.SetToken(ctx, token))
	assert.Nil(t, lookup().ExpiresAt)
}
