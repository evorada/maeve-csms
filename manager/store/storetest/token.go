// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

const orgC = "0193b2c4-0000-7000-8000-0000000000c3"

func testToken(orgId, partyId, uid string) *store.Token {
	return &store.Token{
		OrgId:       orgId,
		CountryCode: "GB",
		PartyId:     partyId,
		Type:        "RFID",
		Uid:         uid,
		ContractId:  "GB" + partyId + "012345678V",
		Issuer:      "Issuer of " + partyId,
		Valid:       true,
		CacheMode:   store.CacheModeAlways,
		LastUpdated: "2026-11-27T08:30:00Z",
	}
}

// tokenKeys returns "org/party/uid" for each token, in the order of the list.
func tokenKeys(tokens []*store.Token) []string {
	keys := make([]string, len(tokens))
	for i, tok := range tokens {
		keys[i] = tok.OrgId + "/" + tok.PartyId + "/" + tok.Uid
	}
	return keys
}

// TestTokensPerOrg checks that a token belongs to an org: its identity is the org, the
// country code, the party id and the UID, and the lookups and listings for an org do
// not see the tokens of another org.
func TestTokensPerOrg(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()

	t.Run("two orgs can hold a token with the same UID", func(t *testing.T) {
		s := newStore(t)
		tokenA := testToken(orgA, "TWK", "SAMEUID")
		tokenA.ContractId = "GBTWK00000000A"
		tokenB := testToken(orgB, "TWK", "SAMEUID")
		tokenB.ContractId = "GBTWK00000000B"
		tokenB.Valid = false
		require.NoError(t, s.SetToken(ctx, tokenA))
		require.NoError(t, s.SetToken(ctx, tokenB))

		got, err := s.LookupTokenForOrg(ctx, orgA, "SAMEUID")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, orgA, got.OrgId)
		assert.Equal(t, "GBTWK00000000A", got.ContractId)
		assert.True(t, got.Valid)

		got, err = s.LookupTokenForOrg(ctx, orgB, "SAMEUID")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, orgB, got.OrgId)
		assert.Equal(t, "GBTWK00000000B", got.ContractId)
		assert.False(t, got.Valid)

		// an org that has no token with the UID sees none
		got, err = s.LookupTokenForOrg(ctx, orgC, "SAMEUID")
		require.NoError(t, err)
		assert.Nil(t, got)
		got, err = s.LookupTokenForOrg(ctx, orgA, "OTHERUID")
		require.NoError(t, err)
		assert.Nil(t, got)

		// the lookup across the orgs prefers the valid token
		got, err = s.LookupToken(ctx, "SAMEUID")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, orgA, got.OrgId)
	})

	t.Run("two parties of one org can hold a token with the same UID, the valid one is preferred", func(t *testing.T) {
		s := newStore(t)
		valid := testToken(orgA, "TWK", "SAMEUID")
		// the token that is not valid is the one that was created and updated last
		invalid := testToken(orgA, "EVO", "SAMEUID")
		invalid.Valid = false
		invalid.LastUpdated = "2026-11-27T09:30:00Z"
		require.NoError(t, s.SetToken(ctx, valid))
		require.NoError(t, s.SetToken(ctx, invalid))

		listed, err := s.ListTokensForOrgs(ctx, []string{orgA}, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{orgA + "/EVO/SAMEUID", orgA + "/TWK/SAMEUID"}, tokenKeys(listed))

		got, err := s.LookupTokenForOrg(ctx, orgA, "SAMEUID")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "TWK", got.PartyId)
		assert.True(t, got.Valid)

		// once the other token is valid too, it is the most recently updated one
		invalid.Valid = true
		require.NoError(t, s.SetToken(ctx, invalid))
		got, err = s.LookupTokenForOrg(ctx, orgA, "SAMEUID")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "EVO", got.PartyId)
	})

	t.Run("a token needs an org", func(t *testing.T) {
		s := newStore(t)
		err := s.SetToken(ctx, testToken("", "TWK", "NOORG"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "token org id is required")

		got, err := s.LookupToken(ctx, "NOORG")
		require.NoError(t, err)
		assert.Nil(t, got)
		listed, err := s.ListTokens(ctx, 0, 10)
		require.NoError(t, err)
		assert.Empty(t, listed)
	})

	t.Run("setting a token again updates it in place", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.SetToken(ctx, testToken(orgA, "TWK", "FIRST")))
		require.NoError(t, s.SetToken(ctx, testToken(orgA, "TWK", "SECOND")))

		group := "GROUP1"
		updated := testToken(orgA, "TWK", "FIRST")
		updated.ContractId = "GBTWK99999999X"
		updated.Issuer = "Other issuer"
		updated.Type = "APP_USER"
		updated.GroupId = &group
		updated.Valid = false
		updated.CacheMode = store.CacheModeNever
		require.NoError(t, s.SetToken(ctx, updated))

		// still two tokens, in the order in which they were created
		listed, err := s.ListTokensForOrgs(ctx, []string{orgA}, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{orgA + "/TWK/SECOND", orgA + "/TWK/FIRST"}, tokenKeys(listed))
		all, err := s.ListTokens(ctx, 0, 10)
		require.NoError(t, err)
		assert.Len(t, all, 2)

		got, err := s.LookupTokenForOrg(ctx, orgA, "FIRST")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "GBTWK99999999X", got.ContractId)
		assert.Equal(t, "Other issuer", got.Issuer)
		assert.Equal(t, "APP_USER", got.Type)
		assert.Equal(t, &group, got.GroupId)
		assert.False(t, got.Valid)
		assert.Equal(t, store.CacheModeNever, got.CacheMode)
	})

	t.Run("the tokens of some orgs are listed without the tokens of the other orgs", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.SetToken(ctx, testToken(orgA, "TWK", "A1")))
		require.NoError(t, s.SetToken(ctx, testToken(orgB, "TWK", "B1")))
		require.NoError(t, s.SetToken(ctx, testToken(orgC, "TWK", "C1")))
		require.NoError(t, s.SetToken(ctx, testToken(orgA, "TWK", "A2")))

		listed, err := s.ListTokensForOrgs(ctx, []string{orgA, orgB}, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{orgA + "/TWK/A2", orgB + "/TWK/B1", orgA + "/TWK/A1"}, tokenKeys(listed))

		// the pages follow the same order
		listed, err = s.ListTokensForOrgs(ctx, []string{orgA, orgB}, 1, 1)
		require.NoError(t, err)
		assert.Equal(t, []string{orgB + "/TWK/B1"}, tokenKeys(listed))
		listed, err = s.ListTokensForOrgs(ctx, []string{orgA, orgB}, 3, 10)
		require.NoError(t, err)
		assert.Empty(t, listed)

		listed, err = s.ListTokensForOrgs(ctx, []string{orgC}, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{orgC + "/TWK/C1"}, tokenKeys(listed))

		// no org, no token
		listed, err = s.ListTokensForOrgs(ctx, []string{}, 0, 10)
		require.NoError(t, err)
		assert.Empty(t, listed)

		all, err := s.ListTokens(ctx, 0, 10)
		require.NoError(t, err)
		assert.Len(t, all, 4)
	})
}
