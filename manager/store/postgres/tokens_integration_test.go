// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func TestToken_SetAndLookup(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	token := &store.Token{
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
