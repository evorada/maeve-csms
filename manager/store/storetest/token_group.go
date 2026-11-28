// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// testTokenGroup is a group of orgA without an id: the store generates it.
func testTokenGroup(name string) *store.TokenGroup {
	return &store.TokenGroup{OrgId: orgA, Name: name}
}

func createTokenGroup(t *testing.T, s store.Engine, g *store.TokenGroup) *store.TokenGroup {
	t.Helper()
	require.NoError(t, s.CreateTokenGroup(context.Background(), g))
	return g
}

func lookupTokenGroup(t *testing.T, s store.Engine, id string) *store.TokenGroup {
	t.Helper()
	g, err := s.LookupTokenGroup(context.Background(), id)
	require.NoError(t, err)
	return g
}

// addGroupToken stores a token of the org of the group with the UID and puts it into the
// group.
func addGroupToken(t *testing.T, s store.Engine, g *store.TokenGroup, uid string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, s.SetToken(ctx, testToken(g.OrgId, "TWK", uid)))
	require.NoError(t, s.SetTokenGroup(ctx, g.OrgId, uid, &g.Id))
}

// tokenGroupOf returns the group id of the token of the org and the party with the UID,
// "" when it is in no group.
func tokenGroupOf(t *testing.T, s store.Engine, orgId, partyId, uid string) string {
	t.Helper()
	tokens, err := s.ListTokensForOrgs(context.Background(), []string{orgId}, 0, 1000)
	require.NoError(t, err)
	for _, tok := range tokens {
		if tok.PartyId == partyId && tok.Uid == uid {
			if tok.TokenGroupId == nil {
				return ""
			}
			return *tok.TokenGroupId
		}
	}
	require.Fail(t, "no such token", "%s/%s/%s", orgId, partyId, uid)
	return ""
}

func tokenGroupNames(groups []*store.TokenGroup) []string {
	names := make([]string, len(groups))
	for i, g := range groups {
		names[i] = g.Name
	}
	return names
}

func tokenUids(tokens []*store.Token) []string {
	uids := make([]string, len(tokens))
	for i, tok := range tokens {
		uids[i] = tok.PartyId + "/" + tok.Uid
	}
	return uids
}

// findTokenGroupName returns the name of the group that the presented UID belongs to in
// the org, "" when it belongs to none.
func findTokenGroupName(t *testing.T, s store.Engine, orgId, uid string) string {
	t.Helper()
	g, err := s.FindTokenGroupForUid(context.Background(), orgId, uid)
	require.NoError(t, err)
	if g == nil {
		return ""
	}
	return g.Name
}

// TestTokenGroupStore is the contract of store.TokenGroupStore. newStore returns an empty
// store.
func TestTokenGroupStore(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()

	t.Run("group round trip", func(t *testing.T) {
		s := newStore(t)
		tariff := createTariffWithId(t, s, testAssetId(50), orgA)
		before := time.Now().Add(-time.Minute)

		g := testTokenGroup("Staff")
		g.PrefixMatch = true
		g.OverrideTariffRef = ptr(strings.ToUpper(tariff.Id))
		createTokenGroup(t, s, g)

		assertUUIDv7(t, g.Id)
		assert.Equal(t, orgA, g.OrgId)
		assert.Equal(t, &tariff.Id, g.OverrideTariffRef, "canonical form")
		assert.True(t, g.CreatedAt.After(before))
		assert.Equal(t, g.CreatedAt, g.UpdatedAt)
		assert.Equal(t, time.UTC, g.CreatedAt.Location())
		assert.Equal(t, g, lookupTokenGroup(t, s, g.Id))
		assert.Equal(t, g, lookupTokenGroup(t, s, strings.ToUpper(g.Id)), "an id is a UUID in any case")

		// the store keeps its own copy
		got := lookupTokenGroup(t, s, g.Id)
		got.Name = "changed"
		*got.OverrideTariffRef = testAssetId(51)
		assert.Equal(t, g, lookupTokenGroup(t, s, g.Id))

		// a group with a given id and nothing else
		plain := &store.TokenGroup{Id: strings.ToUpper(testAssetId(1)), OrgId: strings.ToUpper(orgB), Name: "Plain"}
		createTokenGroup(t, s, plain)
		assert.Equal(t, testAssetId(1), plain.Id, "canonical form")
		assert.Equal(t, orgB, plain.OrgId, "canonical form")
		assert.False(t, plain.PrefixMatch)
		assert.Nil(t, plain.OverrideTariffRef)
		assert.Equal(t, plain, lookupTokenGroup(t, s, testAssetId(1)))

		assert.Nil(t, lookupTokenGroup(t, s, testAssetId(99)))
		assert.Nil(t, lookupTokenGroup(t, s, "not-a-uuid"))
	})

	t.Run("update writes the name, the prefix match and the override tariff", func(t *testing.T) {
		s := newStore(t)
		tariff := createTariffWithId(t, s, testAssetId(50), orgA)
		g := createTokenGroup(t, s, testTokenGroup("Staff"))
		created := *g

		g.Name = "Employees"
		g.PrefixMatch = true
		g.OverrideTariffRef = &tariff.Id
		// what an update does not write
		g.OrgId = orgB
		g.CreatedAt = time.Time{}
		require.NoError(t, s.UpdateTokenGroup(ctx, g))

		assert.Equal(t, "Employees", g.Name)
		assert.True(t, g.PrefixMatch)
		assert.Equal(t, &tariff.Id, g.OverrideTariffRef)
		assert.Equal(t, orgA, g.OrgId, "a group stays in its org")
		assert.Equal(t, created.CreatedAt, g.CreatedAt)
		assert.False(t, g.UpdatedAt.Before(created.UpdatedAt))
		assert.Equal(t, g, lookupTokenGroup(t, s, g.Id))

		g.PrefixMatch = false
		g.OverrideTariffRef = nil
		require.NoError(t, s.UpdateTokenGroup(ctx, g))
		got := lookupTokenGroup(t, s, g.Id)
		assert.False(t, got.PrefixMatch)
		assert.Nil(t, got.OverrideTariffRef)

		unknown := &store.TokenGroup{Id: testAssetId(99), OrgId: orgA, Name: "Unknown"}
		assert.ErrorIs(t, s.UpdateTokenGroup(ctx, unknown), store.ErrNotFound)
		unknown.Id = "not-a-uuid"
		assert.ErrorIs(t, s.UpdateTokenGroup(ctx, unknown), store.ErrNotFound)
	})

	t.Run("the name of a group is unique in its org", func(t *testing.T) {
		s := newStore(t)
		staff := createTokenGroup(t, s, testTokenGroup("Staff"))
		fleet := createTokenGroup(t, s, testTokenGroup("Fleet"))

		dup := testTokenGroup("Staff")
		dup.Id = testAssetId(2)
		assert.ErrorIs(t, s.CreateTokenGroup(ctx, dup), store.ErrAlreadyExists, "same org and name")
		assert.Nil(t, lookupTokenGroup(t, s, testAssetId(2)), "a group that is refused is not stored")

		sameId := testTokenGroup("Other")
		sameId.Id = staff.Id
		assert.ErrorIs(t, s.CreateTokenGroup(ctx, sameId), store.ErrAlreadyExists, "same id")

		otherOrg := testTokenGroup("Staff")
		otherOrg.OrgId = orgB
		assert.NoError(t, s.CreateTokenGroup(ctx, otherOrg), "same name, other org")

		renamed := *fleet
		renamed.Name = "Staff"
		assert.ErrorIs(t, s.UpdateTokenGroup(ctx, &renamed), store.ErrAlreadyExists)
		assert.Equal(t, fleet, lookupTokenGroup(t, s, fleet.Id), "a refused update changes nothing")

		// a group keeps its own name, and a name is free again after its group is deleted
		assert.NoError(t, s.UpdateTokenGroup(ctx, staff))
		require.NoError(t, s.DeleteTokenGroup(ctx, staff.Id))
		renamed = *fleet
		renamed.Name = "Staff"
		assert.NoError(t, s.UpdateTokenGroup(ctx, &renamed))
	})

	t.Run("the override tariff of a group must exist", func(t *testing.T) {
		s := newStore(t)
		tariff := createTariffWithId(t, s, testAssetId(50), orgA)

		unknown := testTokenGroup("Unknown tariff")
		unknown.Id = testAssetId(2)
		unknown.OverrideTariffRef = ptr(testAssetId(98))
		assert.ErrorIs(t, s.CreateTokenGroup(ctx, unknown), store.ErrNotFound)
		assert.Nil(t, lookupTokenGroup(t, s, testAssetId(2)), "a group that is refused is not stored")
		notAnId := testTokenGroup("Not an id")
		notAnId.OverrideTariffRef = ptr("not-a-uuid")
		assert.Error(t, s.CreateTokenGroup(ctx, notAnId))

		g := createTokenGroup(t, s, testTokenGroup("Staff"))
		changed := *g
		changed.OverrideTariffRef = ptr(testAssetId(98))
		assert.ErrorIs(t, s.UpdateTokenGroup(ctx, &changed), store.ErrNotFound)
		assert.Equal(t, g, lookupTokenGroup(t, s, g.Id), "a refused update changes nothing")

		// a deleted tariff is still there
		require.NoError(t, s.DeleteTariff(ctx, tariff.Id))
		g.OverrideTariffRef = &tariff.Id
		require.NoError(t, s.UpdateTokenGroup(ctx, g))
		assert.Equal(t, &tariff.Id, lookupTokenGroup(t, s, g.Id).OverrideTariffRef)
	})

	t.Run("list by org, in pages", func(t *testing.T) {
		s := newStore(t)
		for i := 1; i <= 5; i++ {
			g := testTokenGroup(fmt.Sprintf("A%d", i))
			g.Id = testAssetId(i)
			createTokenGroup(t, s, g)
		}
		ofB := testTokenGroup("B1")
		ofB.Id = testAssetId(3 + 100)
		ofB.OrgId = orgB
		createTokenGroup(t, s, ofB)

		all, next, err := s.ListTokenGroups(ctx, []string{orgA}, store.PageRequest{})
		require.NoError(t, err)
		assert.Equal(t, []string{"A1", "A2", "A3", "A4", "A5"}, tokenGroupNames(all))
		assert.Empty(t, next)
		assert.Equal(t, lookupTokenGroup(t, s, testAssetId(1)), all[0])

		both, _, err := s.ListTokenGroups(ctx, []string{strings.ToUpper(orgB), orgA}, store.PageRequest{})
		require.NoError(t, err)
		assert.Equal(t, []string{"A1", "A2", "A3", "A4", "A5", "B1"}, tokenGroupNames(both))

		var names []string
		cursor := ""
		for page := 1; ; page++ {
			groups, next, err := s.ListTokenGroups(ctx, []string{orgA}, store.PageRequest{Limit: 2, Cursor: cursor})
			require.NoError(t, err)
			names = append(names, tokenGroupNames(groups)...)
			if next == "" {
				assert.Equal(t, 3, page)
				break
			}
			require.Less(t, page, 5)
			cursor = next
		}
		assert.Equal(t, []string{"A1", "A2", "A3", "A4", "A5"}, names)

		none, next, err := s.ListTokenGroups(ctx, nil, store.PageRequest{})
		require.NoError(t, err)
		assert.Empty(t, none, "no org lists nothing")
		assert.Empty(t, next)
		none, _, err = s.ListTokenGroups(ctx, []string{orgC}, store.PageRequest{})
		require.NoError(t, err)
		assert.Empty(t, none)

		_, _, err = s.ListTokenGroups(ctx, []string{"not-a-uuid"}, store.PageRequest{})
		assert.Error(t, err)
		_, _, err = s.ListTokenGroups(ctx, []string{orgA}, store.PageRequest{Cursor: "%%%"})
		assert.ErrorIs(t, err, store.ErrInvalidCursor)
	})

	t.Run("tokens go into a group, to another group and out of it", func(t *testing.T) {
		s := newStore(t)
		staff := createTokenGroup(t, s, testTokenGroup("Staff"))
		fleet := createTokenGroup(t, s, testTokenGroup("Fleet"))
		ofB := testTokenGroup("Of B")
		ofB.OrgId = orgB
		createTokenGroup(t, s, ofB)
		// the UID is a token of two parties of orgA, and of orgB
		require.NoError(t, s.SetToken(ctx, testToken(orgA, "TWK", "TAG1")))
		require.NoError(t, s.SetToken(ctx, testToken(orgA, "EVO", "TAG1")))
		require.NoError(t, s.SetToken(ctx, testToken(orgA, "TWK", "TAG2")))
		require.NoError(t, s.SetToken(ctx, testToken(orgB, "TWK", "TAG1")))
		assert.Empty(t, tokenGroupOf(t, s, orgA, "TWK", "TAG1"), "a new token is in no group")

		require.NoError(t, s.SetTokenGroup(ctx, orgA, "TAG1", ptr(strings.ToUpper(staff.Id))))
		assert.Equal(t, staff.Id, tokenGroupOf(t, s, orgA, "TWK", "TAG1"))
		assert.Equal(t, staff.Id, tokenGroupOf(t, s, orgA, "EVO", "TAG1"), "the token of every party with the UID")
		assert.Empty(t, tokenGroupOf(t, s, orgA, "TWK", "TAG2"))
		assert.Empty(t, tokenGroupOf(t, s, orgB, "TWK", "TAG1"), "the token of another org is left alone")
		looked, err := s.LookupTokenForOrg(ctx, orgA, "TAG1")
		require.NoError(t, err)
		require.NotNil(t, looked)
		assert.Equal(t, &staff.Id, looked.TokenGroupId)

		// a token is in one group: it moves
		require.NoError(t, s.SetTokenGroup(ctx, orgA, "TAG1", &fleet.Id))
		assert.Equal(t, fleet.Id, tokenGroupOf(t, s, orgA, "TWK", "TAG1"))
		assert.Equal(t, fleet.Id, tokenGroupOf(t, s, orgA, "EVO", "TAG1"))

		// setting the token again keeps its group; the caller cannot write it
		replaced := testToken(orgA, "TWK", "TAG1")
		replaced.Valid = false
		replaced.TokenGroupId = &staff.Id
		require.NoError(t, s.SetToken(ctx, replaced))
		assert.Equal(t, fleet.Id, tokenGroupOf(t, s, orgA, "TWK", "TAG1"))
		fresh := testToken(orgA, "TWK", "TAG3")
		fresh.TokenGroupId = &staff.Id
		require.NoError(t, s.SetToken(ctx, fresh))
		assert.Empty(t, tokenGroupOf(t, s, orgA, "TWK", "TAG3"))

		// what is not there
		assert.ErrorIs(t, s.SetTokenGroup(ctx, orgA, "UNKNOWN", &staff.Id), store.ErrNotFound, "no such token")
		assert.ErrorIs(t, s.SetTokenGroup(ctx, orgA, "UNKNOWN", nil), store.ErrNotFound, "no such token")
		assert.ErrorIs(t, s.SetTokenGroup(ctx, orgC, "TAG1", &staff.Id), store.ErrNotFound, "no such token in the org")
		assert.ErrorIs(t, s.SetTokenGroup(ctx, orgA, "TAG2", ptr(testAssetId(99))), store.ErrNotFound, "no such group")
		assert.ErrorIs(t, s.SetTokenGroup(ctx, orgA, "TAG2", ptr("not-a-uuid")), store.ErrNotFound, "no such group")
		assert.ErrorIs(t, s.SetTokenGroup(ctx, orgA, "TAG2", &ofB.Id), store.ErrNotFound, "a group of another org")
		assert.ErrorIs(t, s.SetTokenGroup(ctx, orgB, "TAG1", &staff.Id), store.ErrNotFound, "a group of another org")
		assert.Error(t, s.SetTokenGroup(ctx, "not-a-uuid", "TAG1", &staff.Id))
		assert.Empty(t, tokenGroupOf(t, s, orgA, "TWK", "TAG2"), "a refused call changes nothing")
		assert.Empty(t, tokenGroupOf(t, s, orgB, "TWK", "TAG1"))
		assert.Equal(t, fleet.Id, tokenGroupOf(t, s, orgA, "TWK", "TAG1"))

		// out of the group
		require.NoError(t, s.SetTokenGroup(ctx, strings.ToUpper(orgA), "TAG1", nil))
		assert.Empty(t, tokenGroupOf(t, s, orgA, "TWK", "TAG1"))
		assert.Empty(t, tokenGroupOf(t, s, orgA, "EVO", "TAG1"))
		require.NoError(t, s.SetTokenGroup(ctx, orgA, "TAG1", nil), "a token that is in no group")

		// a token that is deleted and stored again is in no group
		require.NoError(t, s.SetTokenGroup(ctx, orgA, "TAG2", &staff.Id))
		require.NoError(t, s.DeleteToken(ctx, orgA, "TAG2"))
		require.NoError(t, s.SetToken(ctx, testToken(orgA, "TWK", "TAG2")))
		assert.Empty(t, tokenGroupOf(t, s, orgA, "TWK", "TAG2"))
	})

	t.Run("the tokens of a group that is deleted are in no group", func(t *testing.T) {
		s := newStore(t)
		staff := createTokenGroup(t, s, testTokenGroup("Staff"))
		fleet := createTokenGroup(t, s, testTokenGroup("Fleet"))
		addGroupToken(t, s, staff, "TAG1")
		addGroupToken(t, s, staff, "TAG2")
		addGroupToken(t, s, fleet, "TAG3")

		require.NoError(t, s.DeleteTokenGroup(ctx, strings.ToUpper(staff.Id)))

		assert.Nil(t, lookupTokenGroup(t, s, staff.Id))
		assert.Empty(t, tokenGroupOf(t, s, orgA, "TWK", "TAG1"))
		assert.Empty(t, tokenGroupOf(t, s, orgA, "TWK", "TAG2"))
		assert.Equal(t, fleet.Id, tokenGroupOf(t, s, orgA, "TWK", "TAG3"), "the tokens of another group stay in it")
		looked, err := s.LookupTokenForOrg(ctx, orgA, "TAG1")
		require.NoError(t, err)
		require.NotNil(t, looked, "the tokens are still there")
		members, _, err := s.ListTokenGroupMembers(ctx, staff.Id, store.PageRequest{})
		require.NoError(t, err)
		assert.Empty(t, members)
		assert.Empty(t, findTokenGroupName(t, s, orgA, "TAG1"))

		// deleting again, or what is not there, does nothing
		assert.NoError(t, s.DeleteTokenGroup(ctx, staff.Id))
		assert.NoError(t, s.DeleteTokenGroup(ctx, testAssetId(99)))
		assert.NoError(t, s.DeleteTokenGroup(ctx, "not-a-uuid"))
		assert.NotNil(t, lookupTokenGroup(t, s, fleet.Id))
	})

	t.Run("the tokens of a group are listed in the order in which they were stored", func(t *testing.T) {
		s := newStore(t)
		staff := createTokenGroup(t, s, testTokenGroup("Staff"))
		fleet := createTokenGroup(t, s, testTokenGroup("Fleet"))
		// stored in an order that is not the order of the UIDs
		for _, uid := range []string{"TAG5", "TAG1", "TAG4", "TAG2", "TAG3"} {
			require.NoError(t, s.SetToken(ctx, testToken(orgA, "TWK", uid)))
		}
		require.NoError(t, s.SetToken(ctx, testToken(orgA, "EVO", "TAG1")))
		for _, uid := range []string{"TAG1", "TAG2", "TAG4", "TAG5"} {
			require.NoError(t, s.SetTokenGroup(ctx, orgA, uid, &staff.Id))
		}
		require.NoError(t, s.SetTokenGroup(ctx, orgA, "TAG3", &fleet.Id))

		want := []string{"TWK/TAG5", "TWK/TAG1", "TWK/TAG4", "TWK/TAG2", "EVO/TAG1"}
		all, next, err := s.ListTokenGroupMembers(ctx, strings.ToUpper(staff.Id), store.PageRequest{})
		require.NoError(t, err)
		assert.Equal(t, want, tokenUids(all))
		assert.Empty(t, next)
		assert.Equal(t, orgA, all[0].OrgId)
		assert.Equal(t, &staff.Id, all[0].TokenGroupId)
		assert.Equal(t, "GBTWK012345678V", all[0].ContractId, "the tokens are listed with what they hold")

		var uids []string
		cursor := ""
		for page := 1; ; page++ {
			tokens, next, err := s.ListTokenGroupMembers(ctx, staff.Id, store.PageRequest{Limit: 2, Cursor: cursor})
			require.NoError(t, err)
			uids = append(uids, tokenUids(tokens)...)
			if next == "" {
				assert.Equal(t, 3, page)
				break
			}
			require.Less(t, page, 5)
			cursor = next
		}
		assert.Equal(t, want, uids)

		others, _, err := s.ListTokenGroupMembers(ctx, fleet.Id, store.PageRequest{})
		require.NoError(t, err)
		assert.Equal(t, []string{"TWK/TAG3"}, tokenUids(others))

		for _, id := range []string{testAssetId(99), "not-a-uuid"} {
			none, next, err := s.ListTokenGroupMembers(ctx, id, store.PageRequest{})
			require.NoError(t, err)
			assert.Empty(t, none)
			assert.Empty(t, next)
		}
		for _, cursor := range []string{"%%%", store.EncodeKeyCursor("not-a-number"), store.EncodeKeyCursor("-1")} {
			_, _, err = s.ListTokenGroupMembers(ctx, staff.Id, store.PageRequest{Cursor: cursor})
			assert.ErrorIs(t, err, store.ErrInvalidCursor, cursor)
		}
	})

	t.Run("the group of a presented UID", func(t *testing.T) {
		s := newStore(t)
		prefixGroup := func(orgId, name string, uids ...string) *store.TokenGroup {
			g := testTokenGroup(name)
			g.OrgId = orgId
			g.PrefixMatch = true
			createTokenGroup(t, s, g)
			for _, uid := range uids {
				addGroupToken(t, s, g, uid)
			}
			return g
		}
		staff := createTokenGroup(t, s, testTokenGroup("Staff")) // its UIDs are not prefixes
		addGroupToken(t, s, staff, "04A1B2C3D4")
		addGroupToken(t, s, staff, "STAFF")
		prefixGroup(orgA, "Fleet", "04A1", "fe")
		prefixGroup(orgA, "Fleet depot", "04A1B2")
		prefixGroup(orgA, "Signs", "0_", "%", `B\`)
		prefixGroup(orgA, "Upper", "QR")
		prefixGroup(orgA, "Lower", "qr")
		prefixGroup(orgB, "Of B", "04", "ZZ")
		// a token of orgA that is in no group
		require.NoError(t, s.SetToken(ctx, testToken(orgA, "TWK", "04A1B2EEEE")))

		for name, tc := range map[string]struct {
			orgId, uid, want string
		}{
			"the group of the token with the UID, before a prefix":       {orgA, "04A1B2C3D4", "Staff"},
			"the token of a group without prefix match":                  {orgA, "STAFF", "Staff"},
			"a UID that starts with a token of a group without prefixes": {orgA, "STAFFXX", ""},
			"the same, where a prefix of another group fits":             {orgA, "04A1B2C3D4FF", "Fleet depot"},
			"the longest prefix": {orgA, "04A1B2FFFF", "Fleet depot"},
			"a shorter prefix when the longer one does not fit": {orgA, "04A1FFFFFF", "Fleet"},
			"a prefix itself":                                        {orgA, "04A1", "Fleet"},
			"a presented UID in lower case":                          {orgA, "04a1ffffff", "Fleet"},
			"a prefix in lower case":                                 {orgA, "FE0102", "Fleet"},
			"a token that is not in a group, with a prefix":          {orgA, "04A1B2EEEE", "Fleet depot"},
			"a UID that is shorter than every prefix":                {orgA, "04", ""},
			"a UID that no prefix fits":                              {orgA, "FFFF", ""},
			"an empty UID":                                           {orgA, "", ""},
			"an underscore in a prefix is an underscore":             {orgA, "0_99", "Signs"},
			"and not any character":                                  {orgA, "0X99", ""},
			"a percent sign in a prefix is a percent sign":           {orgA, "%55", "Signs"},
			"a backslash in a prefix is a backslash":                 {orgA, `B\1`, "Signs"},
			"and does not escape what follows":                       {orgA, "B1", ""},
			"of two prefixes of one length the one that sorts first": {orgA, "qr1", "Upper"},
			"the same in upper case":                                 {orgA, "QR1", "Upper"},
			"but the token with exactly the UID first":               {orgA, "qr", "Lower"},
			"the group of another org is not seen":                   {orgA, "ZZ01", ""},
			"nor its shorter prefix":                                 {orgA, "0400", ""},
			"in its own org it is":                                   {orgB, "ZZ01", "Of B"},
			"an org sees only its prefixes":                          {orgB, "04A1B2C3D4", "Of B"},
			"an org without groups":                                  {orgC, "04A1B2C3D4", ""},
			"an org id in upper case":                                {strings.ToUpper(orgA), "04A1FF", "Fleet"},
		} {
			assert.Equal(t, tc.want, findTokenGroupName(t, s, tc.orgId, tc.uid), name)
		}

		got, err := s.FindTokenGroupForUid(ctx, orgA, "04A1B2C3D4")
		require.NoError(t, err)
		assert.Equal(t, lookupTokenGroup(t, s, staff.Id), got, "the group is returned with what it holds")

		_, err = s.FindTokenGroupForUid(ctx, "not-a-uuid", "04A1")
		assert.Error(t, err)

		// a group that no longer matches prefixes
		depot, err := s.FindTokenGroupForUid(ctx, orgA, "04A1B2FFFF")
		require.NoError(t, err)
		depot.PrefixMatch = false
		require.NoError(t, s.UpdateTokenGroup(ctx, depot))
		assert.Equal(t, "Fleet", findTokenGroupName(t, s, orgA, "04A1B2FFFF"))
		assert.Equal(t, "Fleet depot", findTokenGroupName(t, s, orgA, "04A1B2"), "its tokens are still in it")
	})
}

// TestTransactionTariff is the contract of TransactionStore.SetTransactionTariff. newStore
// returns an empty store.
func TestTransactionTariff(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()
	const station = "k7f2.CP001"
	find := func(t *testing.T, s store.Engine, transactionId string) *store.Transaction {
		t.Helper()
		transaction, err := s.FindTransaction(ctx, station, transactionId)
		require.NoError(t, err)
		require.NotNil(t, transaction)
		return transaction
	}

	t.Run("a transaction gets its tariff once", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, station, orgA)
		require.NoError(t, s.CreateTransaction(ctx, station, "tx-1", "TAG1", "ISO14443", nil, 0, false))
		created := find(t, s, "tx-1")
		assert.Nil(t, created.TariffId)
		assert.Nil(t, created.TariffVersion)
		assert.Nil(t, created.TariffSource, "the tariff of a new transaction is not chosen yet")

		require.NoError(t, s.SetTransactionTariff(ctx, station, "tx-1", ptr(strings.ToUpper(testAssetId(50))), ptr(3), "connector"))

		got := find(t, s, "tx-1")
		assert.Equal(t, ptr(testAssetId(50)), got.TariffId, "canonical form")
		assert.Equal(t, ptr(3), got.TariffVersion)
		assert.Equal(t, ptr("connector"), got.TariffSource)
		assert.Equal(t, orgA, got.OrgId)
		assert.Equal(t, "TAG1", got.IdToken)

		// the tariff is not chosen again
		err := s.SetTransactionTariff(ctx, station, "tx-1", ptr(testAssetId(51)), ptr(1), "location")
		assert.ErrorIs(t, err, store.ErrAlreadyExists)
		err = s.SetTransactionTariff(ctx, station, "tx-1", nil, nil, "none")
		assert.ErrorIs(t, err, store.ErrAlreadyExists)
		assert.Equal(t, got, find(t, s, "tx-1"), "a refused call changes nothing")

		// the rest of the transaction goes on as before, with its tariff
		require.NoError(t, s.UpdateTransactionDetails(ctx, station, "tx-1", &store.TransactionDetails{ConnectorId: ptr(2)}))
		require.NoError(t, s.EndTransaction(ctx, station, "tx-1", "TAG1", "ISO14443", nil, 1))
		ended := find(t, s, "tx-1")
		assert.Equal(t, ptr(testAssetId(50)), ended.TariffId)
		assert.Equal(t, ptr(3), ended.TariffVersion)
		assert.Equal(t, ptr("connector"), ended.TariffSource)
		assert.Equal(t, ptr(2), ended.ConnectorId)

		listed, total, err := s.ListTransactionsForChargeStation(ctx, station, "all", nil, nil, 10, 0, nil)
		require.NoError(t, err)
		require.Equal(t, int64(1), total)
		assert.Equal(t, ptr(testAssetId(50)), listed[0].TariffId)
		assert.Equal(t, ptr(3), listed[0].TariffVersion)
		assert.Equal(t, ptr("connector"), listed[0].TariffSource)
	})

	t.Run("a transaction without a tariff has only the source", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, station, orgA)
		require.NoError(t, s.CreateTransaction(ctx, station, "tx-1", "TAG1", "ISO14443", nil, 0, false))

		require.NoError(t, s.SetTransactionTariff(ctx, station, "tx-1", nil, nil, "none"))

		got := find(t, s, "tx-1")
		assert.Nil(t, got.TariffId)
		assert.Nil(t, got.TariffVersion)
		assert.Equal(t, ptr("none"), got.TariffSource)
		err := s.SetTransactionTariff(ctx, station, "tx-1", ptr(testAssetId(50)), ptr(1), "connector")
		assert.ErrorIs(t, err, store.ErrAlreadyExists, "no tariff is a choice too")
	})

	t.Run("what is refused", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, station, orgA)
		require.NoError(t, s.CreateTransaction(ctx, station, "tx-1", "TAG1", "ISO14443", nil, 0, false))
		require.NoError(t, s.CreateTransaction(ctx, "k7f2.CP002", "tx-2", "TAG1", "ISO14443", nil, 0, false))

		tariff := ptr(testAssetId(50))
		assert.ErrorIs(t, s.SetTransactionTariff(ctx, station, "tx-9", tariff, ptr(1), "connector"), store.ErrNotFound, "no such transaction")
		assert.ErrorIs(t, s.SetTransactionTariff(ctx, station, "tx-2", tariff, ptr(1), "connector"), store.ErrNotFound,
			"the transaction of another charge station")
		for name, call := range map[string]func() error{
			"no source":                 func() error { return s.SetTransactionTariff(ctx, station, "tx-1", tariff, ptr(1), "") },
			"a tariff without version":  func() error { return s.SetTransactionTariff(ctx, station, "tx-1", tariff, nil, "connector") },
			"a version without tariff":  func() error { return s.SetTransactionTariff(ctx, station, "tx-1", nil, ptr(1), "connector") },
			"version 0":                 func() error { return s.SetTransactionTariff(ctx, station, "tx-1", tariff, ptr(0), "connector") },
			"a version that is too big": func() error { return s.SetTransactionTariff(ctx, station, "tx-1", tariff, ptr(1<<40), "connector") },
			"a tariff id that is no id": func() error {
				return s.SetTransactionTariff(ctx, station, "tx-1", ptr("not-a-uuid"), ptr(1), "connector")
			},
		} {
			err := call()
			assert.Error(t, err, name)
			assert.NotErrorIs(t, err, store.ErrAlreadyExists, name)
			assert.NotErrorIs(t, err, store.ErrNotFound, name)
		}
		assert.Nil(t, find(t, s, "tx-1").TariffSource, "a refused call writes nothing")

		// the tariff does not have to exist: a transaction outlives its tariff
		require.NoError(t, s.SetTransactionTariff(ctx, station, "tx-1", tariff, ptr(1), "connector"))
	})
}
