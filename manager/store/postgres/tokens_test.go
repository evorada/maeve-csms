package postgres_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

func TestTokenStore_SetAndLookup(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	token := &store.Token{
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         "DEADBEEF",
		ContractId:  "GBTWK012345678V",
		Issuer:      "Thoughtworks",
		Valid:       true,
		CacheMode:   "ALWAYS",
		LastUpdated: "2026-02-04T23:00:00Z",
	}

	// Test SetToken
	err := db.store.SetToken(ctx, token)
	require.NoError(t, err)

	// Test LookupToken - found
	foundToken, err := db.store.LookupToken(ctx, "DEADBEEF")
	require.NoError(t, err)
	require.NotNil(t, foundToken)
	assert.Equal(t, token.Uid, foundToken.Uid)
	assert.Equal(t, token.ContractId, foundToken.ContractId)
	assert.Equal(t, token.CountryCode, foundToken.CountryCode)
	assert.Equal(t, token.PartyId, foundToken.PartyId)
	assert.Equal(t, token.Type, foundToken.Type)
	assert.Equal(t, token.Issuer, foundToken.Issuer)
	assert.Equal(t, token.Valid, foundToken.Valid)
	assert.Equal(t, token.CacheMode, foundToken.CacheMode)

	// Test LookupToken - not found
	notFound, err := db.store.LookupToken(ctx, "NOTEXIST")
	require.NoError(t, err)
	assert.Nil(t, notFound)
}

func TestTokenStore_SetTokenWithNilFields(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Token with nil pointer fields
	token := &store.Token{
		CountryCode:  "GB",
		PartyId:      "TWK",
		Type:         "RFID",
		Uid:          "MINIMAL",
		ContractId:   "GBTWK000000001V",
		Issuer:       "Thoughtworks",
		Valid:        true,
		CacheMode:    "ALLOWED",
		LastUpdated:  "2026-02-04T23:00:00Z",
		VisualNumber: nil,
		GroupId:      nil,
		LanguageCode: nil,
	}

	err := db.store.SetToken(ctx, token)
	require.NoError(t, err)

	// Verify nil fields are handled correctly
	foundToken, err := db.store.LookupToken(ctx, "MINIMAL")
	require.NoError(t, err)
	require.NotNil(t, foundToken)
	assert.Nil(t, foundToken.VisualNumber)
	assert.Nil(t, foundToken.GroupId)
	assert.Nil(t, foundToken.LanguageCode)
}

func TestTokenStore_SetTokenWithOptionalFields(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	visualNumber := "1234-5678-9012"
	groupId := "GROUP123"
	languageCode := "en"

	token := &store.Token{
		CountryCode:  "GB",
		PartyId:      "TWK",
		Type:         "RFID",
		Uid:          "WITHFIELDS",
		ContractId:   "GBTWK000000002V",
		VisualNumber: &visualNumber,
		Issuer:       "Thoughtworks",
		GroupId:      &groupId,
		Valid:        true,
		LanguageCode: &languageCode,
		CacheMode:    "ALWAYS",
		LastUpdated:  "2026-02-04T23:00:00Z",
	}

	err := db.store.SetToken(ctx, token)
	require.NoError(t, err)

	// Verify optional fields are preserved
	foundToken, err := db.store.LookupToken(ctx, "WITHFIELDS")
	require.NoError(t, err)
	require.NotNil(t, foundToken)
	require.NotNil(t, foundToken.VisualNumber)
	assert.Equal(t, visualNumber, *foundToken.VisualNumber)
	require.NotNil(t, foundToken.GroupId)
	assert.Equal(t, groupId, *foundToken.GroupId)
	require.NotNil(t, foundToken.LanguageCode)
	assert.Equal(t, languageCode, *foundToken.LanguageCode)
}

func TestTokenStore_ListTokens(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Create multiple tokens
	for i := 0; i < 5; i++ {
		token := &store.Token{
			CountryCode: "GB",
			PartyId:     "TWK",
			Type:        "RFID",
			Uid:         fmt.Sprintf("TOKEN%03d", i),
			ContractId:  fmt.Sprintf("GBTWK%010dV", i),
			Issuer:      "Thoughtworks",
			Valid:       true,
			CacheMode:   "ALWAYS",
			LastUpdated: "2026-02-04T23:00:00Z",
		}
		require.NoError(t, db.store.SetToken(ctx, token))
	}

	// Test pagination - first page
	tokens, err := db.store.ListTokens(ctx, 0, 3)
	require.NoError(t, err)
	assert.Len(t, tokens, 3)

	// Test pagination - second page
	tokens, err = db.store.ListTokens(ctx, 3, 3)
	require.NoError(t, err)
	assert.Len(t, tokens, 2)

	// Test getting all tokens
	tokens, err = db.store.ListTokens(ctx, 0, 10)
	require.NoError(t, err)
	assert.Len(t, tokens, 5)
}

func TestTokenStore_ListTokensEmpty(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// List when no tokens exist
	tokens, err := db.store.ListTokens(ctx, 0, 10)
	require.NoError(t, err)
	assert.Empty(t, tokens)
}

func TestTokenStore_UpdateToken(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Create initial token
	token := &store.Token{
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         "UPDATEME",
		ContractId:  "GBTWK999999999V",
		Issuer:      "Thoughtworks",
		Valid:       true,
		CacheMode:   "ALWAYS",
		LastUpdated: "2026-02-04T23:00:00Z",
	}
	require.NoError(t, db.store.SetToken(ctx, token))

	// Update token (SetToken should handle upsert)
	updatedToken := &store.Token{
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         "UPDATEME",
		ContractId:  "GBTWK111111111V", // Changed
		Issuer:      "Thoughtworks UK", // Changed
		Valid:       false,             // Changed
		CacheMode:   "NEVER",           // Changed
		LastUpdated: "2026-02-05T12:00:00Z",
	}
	require.NoError(t, db.store.SetToken(ctx, updatedToken))

	// Verify update
	foundToken, err := db.store.LookupToken(ctx, "UPDATEME")
	require.NoError(t, err)
	require.NotNil(t, foundToken)
	assert.Equal(t, "GBTWK111111111V", foundToken.ContractId)
	assert.Equal(t, "Thoughtworks UK", foundToken.Issuer)
	assert.False(t, foundToken.Valid)
	assert.Equal(t, "NEVER", foundToken.CacheMode)
}

func TestTokenStore_DifferentTokenTypes(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	tokenTypes := []string{"RFID", "APP_USER", "AD_HOC_USER", "OTHER"}

	for i, tokenType := range tokenTypes {
		token := &store.Token{
			CountryCode: "GB",
			PartyId:     "TWK",
			Type:        tokenType,
			Uid:         fmt.Sprintf("TYPE%d", i),
			ContractId:  fmt.Sprintf("GBTWK%010dV", i),
			Issuer:      "Thoughtworks",
			Valid:       true,
			CacheMode:   "ALWAYS",
			LastUpdated: "2026-02-04T23:00:00Z",
		}
		require.NoError(t, db.store.SetToken(ctx, token))
	}

	// Verify all token types can be retrieved
	for i, tokenType := range tokenTypes {
		foundToken, err := db.store.LookupToken(ctx, fmt.Sprintf("TYPE%d", i))
		require.NoError(t, err)
		require.NotNil(t, foundToken)
		assert.Equal(t, tokenType, foundToken.Type)
	}
}
