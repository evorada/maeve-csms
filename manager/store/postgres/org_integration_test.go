// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/storetest"
)

const (
	testOrgA = "0193b2c4-0000-7000-8000-0000000000a1"
	testOrgB = "0193b2c4-0000-7000-8000-0000000000b2"
)

func emptyTestStore(t *testing.T) store.Engine {
	truncateAll(t)
	t.Cleanup(func() { truncateAll(t) })
	return testStore
}

func TestOrgOwnership(t *testing.T) {
	storetest.TestOrgOwnership(t, emptyTestStore)
}

func TestStationKeys(t *testing.T) {
	storetest.TestStationKeys(t, emptyTestStore)
}

func orgTestToken(uid, orgId string) *store.Token {
	return &store.Token{
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         uid,
		ContractId:  "GBTWK012345678V",
		Issuer:      "Thoughtworks",
		Valid:       true,
		CacheMode:   "ALWAYS",
		LastUpdated: time.Now().UTC().Format(time.RFC3339),
		OrgId:       orgId,
	}
}

func TestChargeStationRegisteredWithoutOrgBelongsToLegacyOrg(t *testing.T) {
	s := emptyTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{}))

	got, err := s.LookupChargeStationAuth(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, store.LegacyOrgId, got.OrgId)

	err = s.SetChargeStationAuth(ctx, "cs002", &store.ChargeStationAuth{OrgId: "not-a-uuid"})
	assert.Error(t, err)
}

func TestTokensPerOrg(t *testing.T) {
	storetest.TestTokensPerOrg(t, emptyTestStore)
}

func TestTokenOrg(t *testing.T) {
	s := emptyTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.SetToken(ctx, orgTestToken("WITHORG", testOrgA)))

	got, err := s.LookupToken(ctx, "WITHORG")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, testOrgA, got.OrgId)

	listed, err := s.ListTokens(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, testOrgA, listed[0].OrgId)

	// an org id must be a UUID
	assert.Error(t, s.SetToken(ctx, orgTestToken("BADORG", "not-a-uuid")))
	_, err = s.LookupTokenForOrg(ctx, "not-a-uuid", "WITHORG")
	assert.Error(t, err)
	_, err = s.ListTokensForOrgs(ctx, []string{testOrgA, "not-a-uuid"}, 0, 10)
	assert.Error(t, err)
}

func TestLookupTokenPrefersTheMostRecentlyUpdatedToken(t *testing.T) {
	s := emptyTestStore(t)
	ctx := context.Background()

	// the token that is created first is the most recently updated one
	recent := orgTestToken("SAMEUID", testOrgA)
	recent.PartyId = "TWK"
	recent.LastUpdated = "2026-11-27T09:30:00Z"
	older := orgTestToken("SAMEUID", testOrgA)
	older.PartyId = "EVO"
	older.LastUpdated = "2026-11-27T08:30:00Z"
	require.NoError(t, s.SetToken(ctx, recent))
	require.NoError(t, s.SetToken(ctx, older))

	got, err := s.LookupTokenForOrg(ctx, testOrgA, "SAMEUID")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "TWK", got.PartyId)
	got, err = s.LookupToken(ctx, "SAMEUID")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "TWK", got.PartyId)

	// a valid token goes before a more recently updated token that is not valid
	recent.Valid = false
	require.NoError(t, s.SetToken(ctx, recent))
	got, err = s.LookupTokenForOrg(ctx, testOrgA, "SAMEUID")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "EVO", got.PartyId)
}

func TestOcpiOrg(t *testing.T) {
	s := emptyTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.SetRegistrationDetails(ctx, "token-a", &store.OcpiRegistration{
		Status: store.OcpiRegistrationStatusRegistered,
		OrgId:  testOrgA,
	}))
	require.NoError(t, s.SetRegistrationDetails(ctx, "token-none", &store.OcpiRegistration{
		Status: store.OcpiRegistrationStatusPending,
	}))

	registration, err := s.GetRegistrationDetails(ctx, "token-a")
	require.NoError(t, err)
	assert.Equal(t, &store.OcpiRegistration{Status: store.OcpiRegistrationStatusRegistered, OrgId: testOrgA}, registration)
	registration, err = s.GetRegistrationDetails(ctx, "token-none")
	require.NoError(t, err)
	assert.Equal(t, &store.OcpiRegistration{Status: store.OcpiRegistrationStatusPending, OrgId: store.LegacyOrgId}, registration)

	partyA := &store.OcpiParty{CountryCode: "GB", PartyId: "TWK", Role: "EMSP", Url: "https://a.example.com", Token: "a", OrgId: testOrgA}
	partyNone := &store.OcpiParty{CountryCode: "BE", PartyId: "EVO", Role: "EMSP", Url: "https://b.example.com", Token: "b"}
	require.NoError(t, s.SetPartyDetails(ctx, partyA))
	require.NoError(t, s.SetPartyDetails(ctx, partyNone))

	party, err := s.GetPartyDetails(ctx, "EMSP", "GB", "TWK")
	require.NoError(t, err)
	assert.Equal(t, partyA, party)

	parties, err := s.ListPartyDetailsForRole(ctx, "EMSP")
	require.NoError(t, err)
	orgIds := map[string]string{}
	for _, p := range parties {
		orgIds[p.PartyId] = p.OrgId
	}
	assert.Equal(t, map[string]string{"TWK": testOrgA, "EVO": store.LegacyOrgId}, orgIds)
}

// orgRowCounts returns, for every table whose rows belong to an org, how many rows
// each org owns.
func orgRowCounts(t *testing.T) map[string]map[string]int {
	t.Helper()
	ctx := context.Background()
	counts := map[string]map[string]int{}
	for _, table := range []string{
		"charge_stations", "locations", "tokens", "transactions",
		"reservations", "charging_profiles", "ocpi_registrations", "ocpi_parties",
	} {
		rows, err := testPool.Query(ctx, `SELECT org_id::text, COUNT(*) FROM `+table+` GROUP BY org_id`)
		require.NoError(t, err)
		counts[table] = map[string]int{}
		for rows.Next() {
			var orgId string
			var count int
			require.NoError(t, rows.Scan(&orgId, &count))
			counts[table][orgId] = count
		}
		require.NoError(t, rows.Err())
		rows.Close()
	}
	return counts
}

func TestAssignLegacyOrg(t *testing.T) {
	emptyTestStore(t)
	ctx := context.Background()
	s := testStore

	// one row of the legacy org and one row of org B in each of the eight tables
	seed := func(chargeStationId string, id int, orgId string) {
		t.Helper()
		// a location and a token must name their org
		locationOrgId := orgId
		if locationOrgId == "" {
			locationOrgId = store.LegacyOrgId
		}
		suffix := string(rune('a' + id))
		require.NoError(t, s.SetChargeStationAuth(ctx, chargeStationId, &store.ChargeStationAuth{OrgId: orgId}))
		require.NoError(t, s.SetLocation(ctx, &store.Location{Id: "loc-" + suffix, Name: "Location", OrgId: locationOrgId}))
		require.NoError(t, s.SetToken(ctx, orgTestToken("TOKEN-"+suffix, locationOrgId)))
		require.NoError(t, s.CreateTransaction(ctx, chargeStationId, "tx-"+suffix, "TOKEN", "ISO14443", nil, 0, false))
		require.NoError(t, s.CreateReservation(ctx, &store.Reservation{
			ReservationId:   id,
			ChargeStationId: chargeStationId,
			ConnectorId:     1,
			IdTag:           "TOKEN",
			ExpiryDate:      time.Now().Add(time.Hour),
			Status:          store.ReservationStatusAccepted,
			CreatedAt:       time.Now(),
		}))
		require.NoError(t, s.SetChargingProfile(ctx, &store.ChargingProfile{
			ChargeStationId:        chargeStationId,
			ChargingProfileId:      id,
			ChargingProfilePurpose: store.ChargingProfilePurposeTxDefaultProfile,
			ChargingProfileKind:    store.ChargingProfileKindAbsolute,
			ChargingSchedule: store.ChargingSchedule{
				ChargingRateUnit:       store.ChargingRateUnitA,
				ChargingSchedulePeriod: []store.ChargingSchedulePeriod{{StartPeriod: 0, Limit: 16}},
			},
		}))
		require.NoError(t, s.SetRegistrationDetails(ctx, "registration-"+suffix, &store.OcpiRegistration{
			Status: store.OcpiRegistrationStatusRegistered,
			OrgId:  orgId,
		}))
		require.NoError(t, s.SetPartyDetails(ctx, &store.OcpiParty{
			CountryCode: "GB", PartyId: "TW" + suffix, Role: "EMSP", Url: "https://example.com", Token: "token-" + suffix,
			OrgId: orgId,
		}))
	}
	seed("cs-legacy", 1, "")
	seed("cs-org-b", 2, testOrgB)

	before := orgRowCounts(t)
	require.Len(t, before, 8)
	for table, counts := range before {
		require.Equal(t, map[string]int{store.LegacyOrgId: 1, testOrgB: 1}, counts, table)
	}

	// nothing to do for the legacy org itself, and an org id must be a UUID
	rows, err := s.AssignLegacyOrg(ctx, store.LegacyOrgId)
	require.NoError(t, err)
	assert.Equal(t, int64(0), rows)
	_, err = s.AssignLegacyOrg(ctx, "not-a-uuid")
	require.Error(t, err)
	assert.Equal(t, before, orgRowCounts(t))

	// only the rows of the legacy org move
	rows, err = s.AssignLegacyOrg(ctx, testOrgA)
	require.NoError(t, err)
	assert.Equal(t, int64(8), rows)
	for table, counts := range orgRowCounts(t) {
		assert.Equal(t, map[string]int{testOrgA: 1, testOrgB: 1}, counts, table)
	}

	auth, err := s.LookupChargeStationAuth(ctx, "cs-legacy")
	require.NoError(t, err)
	assert.Equal(t, testOrgA, auth.OrgId)
	auth, err = s.LookupChargeStationAuth(ctx, "cs-org-b")
	require.NoError(t, err)
	assert.Equal(t, testOrgB, auth.OrgId)

	// a second run finds no legacy rows
	rows, err = s.AssignLegacyOrg(ctx, testOrgA)
	require.NoError(t, err)
	assert.Equal(t, int64(0), rows)
}

// A token of the legacy org cannot move to an org that already has a token with the
// same country code, party id and UID. Both tokens must be kept as they are, every
// other row must move, and the tokens that stay behind must be named in a warning.
func TestAssignLegacyOrgKeepsTokensThatTheOrgAlreadyHas(t *testing.T) {
	emptyTestStore(t)
	ctx := context.Background()
	s := testStore

	var logged bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	token := func(orgId, partyId, uid, contractId string, valid bool) *store.Token {
		tok := orgTestToken(uid, orgId)
		tok.PartyId = partyId
		tok.ContractId = contractId
		tok.Valid = valid
		return tok
	}
	// the default org and the legacy org both have GB-TWK-DUPLICATE
	require.NoError(t, s.SetToken(ctx, token(testOrgA, "TWK", "DUPLICATE", "GBTWK0000000DEF", true)))
	require.NoError(t, s.SetToken(ctx, token(store.LegacyOrgId, "TWK", "DUPLICATE", "GBTWK0000000LEG", false)))
	// the same UID for another party, and another UID, are not in the default org
	require.NoError(t, s.SetToken(ctx, token(store.LegacyOrgId, "EVO", "DUPLICATE", "GBEVO0000000LEG", true)))
	require.NoError(t, s.SetToken(ctx, token(store.LegacyOrgId, "TWK", "ONLYLEGACY", "GBTWK0000001LEG", true)))
	// another org has the UID too
	require.NoError(t, s.SetToken(ctx, token(testOrgB, "TWK", "DUPLICATE", "GBTWK0000000ORB", true)))
	// rows of the legacy org in tables that come before and after the tokens
	require.NoError(t, s.SetChargeStationAuth(ctx, "cs-legacy", &store.ChargeStationAuth{}))
	require.NoError(t, s.CreateTransaction(ctx, "cs-legacy", "tx-legacy", "DUPLICATE", "ISO14443", nil, 0, false))
	require.NoError(t, s.SetPartyDetails(ctx, &store.OcpiParty{
		CountryCode: "GB", PartyId: "TWK", Role: "EMSP", Url: "https://example.com", Token: "token",
	}))

	// tokensByOrg returns "org/party/uid/contract/valid" for every token.
	tokensByOrg := func() []string {
		rows, err := testPool.Query(ctx, `
			SELECT org_id::text || '/' || party_id || '/' || uid || '/' || contract_id || '/' || valid::text
			FROM tokens ORDER BY 1`)
		require.NoError(t, err)
		defer rows.Close()
		tokens := []string{}
		for rows.Next() {
			var tok string
			require.NoError(t, rows.Scan(&tok))
			tokens = append(tokens, tok)
		}
		require.NoError(t, rows.Err())
		return tokens
	}
	wantTokens := []string{
		store.LegacyOrgId + "/TWK/DUPLICATE/GBTWK0000000LEG/false",
		testOrgA + "/EVO/DUPLICATE/GBEVO0000000LEG/true",
		testOrgA + "/TWK/DUPLICATE/GBTWK0000000DEF/true",
		testOrgA + "/TWK/ONLYLEGACY/GBTWK0000001LEG/true",
		testOrgB + "/TWK/DUPLICATE/GBTWK0000000ORB/true",
	}

	// two tokens, the charge station, the transaction and the party move
	logged.Reset()
	rows, err := s.AssignLegacyOrg(ctx, testOrgA)
	require.NoError(t, err)
	assert.Equal(t, int64(5), rows)
	assert.Equal(t, wantTokens, tokensByOrg())
	counts := orgRowCounts(t)
	assert.Equal(t, map[string]int{testOrgA: 1}, counts["charge_stations"])
	assert.Equal(t, map[string]int{testOrgA: 1}, counts["transactions"])
	assert.Equal(t, map[string]int{testOrgA: 1}, counts["ocpi_parties"])

	// the warning names the token that stays in the legacy org, and only that one
	var warning struct {
		Level  string   `json:"level"`
		OrgId  string   `json:"org_id"`
		Count  int      `json:"count"`
		Tokens []string `json:"tokens"`
	}
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(logged.Bytes()), &warning), logged.String())
	assert.Equal(t, "WARN", warning.Level)
	assert.Equal(t, testOrgA, warning.OrgId)
	assert.Equal(t, 1, warning.Count)
	assert.Equal(t, []string{"GB-TWK-DUPLICATE"}, warning.Tokens)

	// a second run moves nothing, changes nothing and warns again
	logged.Reset()
	rows, err = s.AssignLegacyOrg(ctx, testOrgA)
	require.NoError(t, err)
	assert.Equal(t, int64(0), rows)
	assert.Equal(t, wantTokens, tokensByOrg())
	assert.Contains(t, logged.String(), "GB-TWK-DUPLICATE")

	// once one token of the pair is gone, the other one moves and the warning stops
	_, err = testPool.Exec(ctx, `DELETE FROM tokens WHERE org_id = $1 AND party_id = 'TWK' AND uid = 'DUPLICATE'`, testOrgA)
	require.NoError(t, err)
	logged.Reset()
	rows, err = s.AssignLegacyOrg(ctx, testOrgA)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)
	assert.Empty(t, logged.String())
	assert.Equal(t, []string{
		testOrgA + "/EVO/DUPLICATE/GBEVO0000000LEG/true",
		testOrgA + "/TWK/DUPLICATE/GBTWK0000000LEG/false",
		testOrgA + "/TWK/ONLYLEGACY/GBTWK0000001LEG/true",
		testOrgB + "/TWK/DUPLICATE/GBTWK0000000ORB/true",
	}, tokensByOrg())
}
