// SPDX-License-Identifier: Apache-2.0

package inmemory_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/store/storetest"
	"k8s.io/utils/clock"
	clockTest "k8s.io/utils/clock/testing"
)

func TestOrgOwnership(t *testing.T) {
	storetest.TestOrgOwnership(t, func(t *testing.T) store.Engine {
		return inmemory.NewStore(clock.RealClock{})
	})
}

func TestStationKeys(t *testing.T) {
	storetest.TestStationKeys(t, func(t *testing.T) store.Engine {
		return inmemory.NewStore(clock.RealClock{})
	})
}

func TestTokensPerOrg(t *testing.T) {
	storetest.TestTokensPerOrg(t, func(t *testing.T) store.Engine {
		return inmemory.NewStore(clock.RealClock{})
	})
}

func TestLookupTokenPrefersTheMostRecentlyUpdatedToken(t *testing.T) {
	const orgId = "0193b2c4-0000-7000-8000-0000000000a1"
	ctx := context.Background()
	clk := clockTest.NewFakeClock(time.Date(2026, 11, 27, 8, 30, 0, 0, time.UTC))
	s := inmemory.NewStore(clk)

	token := func(partyId string) *store.Token {
		return &store.Token{OrgId: orgId, CountryCode: "GB", PartyId: partyId, Uid: "SAMEUID", Valid: true}
	}
	require.NoError(t, s.SetToken(ctx, token("TWK")))
	clk.Step(time.Minute)
	require.NoError(t, s.SetToken(ctx, token("EVO")))

	got, err := s.LookupTokenForOrg(ctx, orgId, "SAMEUID")
	require.NoError(t, err)
	assert.Equal(t, "EVO", got.PartyId)

	// the token that was created first is updated last
	clk.Step(time.Minute)
	require.NoError(t, s.SetToken(ctx, token("TWK")))

	got, err = s.LookupTokenForOrg(ctx, orgId, "SAMEUID")
	require.NoError(t, err)
	assert.Equal(t, "TWK", got.PartyId)
	got, err = s.LookupToken(ctx, "SAMEUID")
	require.NoError(t, err)
	assert.Equal(t, "TWK", got.PartyId)
}
