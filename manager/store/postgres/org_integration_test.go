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

func TestTokenOrg(t *testing.T) {
	s := emptyTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.SetToken(ctx, orgTestToken("WITHORG", testOrgA)))
	require.NoError(t, s.SetToken(ctx, orgTestToken("NOORG", "")))

	got, err := s.LookupToken(ctx, "WITHORG")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, testOrgA, got.OrgId)

	got, err = s.LookupToken(ctx, "NOORG")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, store.LegacyOrgId, got.OrgId)

	// setting the token again writes its org again
	require.NoError(t, s.SetToken(ctx, orgTestToken("WITHORG", testOrgB)))
	listed, err := s.ListTokens(ctx, 0, 10)
	require.NoError(t, err)
	orgIds := map[string]string{}
	for _, tok := range listed {
		orgIds[tok.Uid] = tok.OrgId
	}
	assert.Equal(t, map[string]string{"WITHORG": testOrgB, "NOORG": store.LegacyOrgId}, orgIds)

	assert.Error(t, s.SetToken(ctx, orgTestToken("BADORG", "not-a-uuid")))
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
		locationOrgId := orgId
		if locationOrgId == "" {
			locationOrgId = store.LegacyOrgId
		}
		suffix := string(rune('a' + id))
		require.NoError(t, s.SetChargeStationAuth(ctx, chargeStationId, &store.ChargeStationAuth{OrgId: orgId}))
		require.NoError(t, s.SetLocation(ctx, &store.Location{Id: "loc-" + suffix, Name: "Location", OrgId: locationOrgId}))
		require.NoError(t, s.SetToken(ctx, orgTestToken("TOKEN-"+suffix, orgId)))
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
