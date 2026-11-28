// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/storetest"
)

func TestTokenGroupStore(t *testing.T) {
	storetest.TestTokenGroupStore(t, emptyTestStore)
}

func TestTransactionTariff(t *testing.T) {
	storetest.TestTransactionTariff(t, emptyTestStore)
}

// The tariff of a transaction is written in the database transaction that creates the
// transaction. A call that is refused there (the tariff is chosen already, or what it is
// given is not a tariff) sends no statement that fails, so the database transaction of
// the caller stays usable and is committed.
func TestSetTransactionTariff_JoinsTheTransaction(t *testing.T) {
	ctx := context.Background()
	emptyTestStore(t)
	const station = "k7f2.cs001"
	tariffId := "0193b2c5-0000-7000-8000-000000000050"
	version := 2

	start := func(ctx context.Context, transactionId string) {
		require.NoError(t, testStore.CreateTransaction(ctx, station, transactionId, "TAG1", "ISO14443", nil, 0, false))
		require.NoError(t, testStore.SetTransactionTariff(ctx, station, transactionId, &tariffId, &version, "connector"))

		require.ErrorIs(t, testStore.SetTransactionTariff(ctx, station, transactionId, nil, nil, "none"), store.ErrAlreadyExists)
		require.ErrorIs(t, testStore.SetTransactionTariff(ctx, station, "tx-unknown", nil, nil, "none"), store.ErrNotFound)
		notAnId := "not-a-uuid"
		require.Error(t, testStore.SetTransactionTariff(ctx, station, transactionId, &notAnId, &version, "connector"))

		got, err := testStore.FindTransaction(ctx, station, transactionId)
		require.NoError(t, err)
		require.NotNil(t, got, "the transaction is still usable, and reads what it wrote")
		require.Equal(t, &tariffId, got.TariffId)
		require.Equal(t, &version, got.TariffVersion)
	}

	err := testStore.RunInTx(ctx, func(ctx context.Context) error {
		start(ctx, "tx-rolled-back")
		return errStopTx
	})
	require.ErrorIs(t, err, errStopTx)
	got, err := testStore.FindTransaction(ctx, station, "tx-rolled-back")
	require.NoError(t, err)
	assert.Nil(t, got, "rolled back with the transaction")

	require.NoError(t, testStore.RunInTx(ctx, func(ctx context.Context) error {
		start(ctx, "tx-committed")
		return nil
	}))
	got, err = testStore.FindTransaction(ctx, station, "tx-committed")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, &tariffId, got.TariffId)
	assert.Equal(t, &version, got.TariffVersion)
	source := "connector"
	assert.Equal(t, &source, got.TariffSource)
	requireNoOpenTransaction(t)
}

// Of the callers that choose the tariff of one transaction at the same time, one wins:
// the others are told that the transaction has its tariff, and the tariff of the winner
// stays.
func TestSetTransactionTariff_Concurrent(t *testing.T) {
	ctx := context.Background()
	emptyTestStore(t)
	const (
		station = "k7f2.cs001"
		callers = 8
	)
	require.NoError(t, testStore.CreateTransaction(ctx, station, "tx-1", "TAG1", "ISO14443", nil, 0, false))

	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tariffId := fmt.Sprintf("0193b2c5-0000-7000-8000-%012d", i+1)
			version := i + 1
			errs[i] = testStore.SetTransactionTariff(ctx, station, "tx-1", &tariffId, &version, "connector")
		}()
	}
	wg.Wait()

	winner := -1
	for i, err := range errs {
		if err == nil {
			require.Equal(t, -1, winner, "two callers set the tariff")
			winner = i
			continue
		}
		assert.ErrorIs(t, err, store.ErrAlreadyExists)
	}
	require.NotEqual(t, -1, winner, "no caller set the tariff")
	got, err := testStore.FindTransaction(ctx, station, "tx-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	wantId, wantVersion := fmt.Sprintf("0193b2c5-0000-7000-8000-%012d", winner+1), winner+1
	assert.Equal(t, &wantId, got.TariffId)
	assert.Equal(t, &wantVersion, got.TariffVersion)
}

// The group of a token is a column of its own: writing the token again (an update from
// its issuer) leaves it as it is, in the same row.
func TestTokenGroup_StoredWithTheToken(t *testing.T) {
	ctx := context.Background()
	emptyTestStore(t)

	group := &store.TokenGroup{OrgId: testOrgA, Name: "Staff", PrefixMatch: true}
	require.NoError(t, testStore.CreateTokenGroup(ctx, group))
	require.NoError(t, testStore.SetToken(ctx, orgTestToken("TAG1", testOrgA)))
	require.NoError(t, testStore.SetTokenGroup(ctx, testOrgA, "TAG1", &group.Id))

	read := func() (groupId *string, contractId string, rows int) {
		t.Helper()
		require.NoError(t, testPool.QueryRow(ctx,
			`SELECT (SELECT count(*) FROM tokens), token_group_id::text, contract_id FROM tokens WHERE uid = 'TAG1'`).
			Scan(&rows, &groupId, &contractId))
		return groupId, contractId, rows
	}
	groupId, _, rows := read()
	assert.Equal(t, &group.Id, groupId)
	assert.Equal(t, 1, rows)

	updated := orgTestToken("TAG1", testOrgA)
	updated.ContractId = "GBTWK999999999X"
	require.NoError(t, testStore.SetToken(ctx, updated))
	groupId, contractId, rows := read()
	assert.Equal(t, &group.Id, groupId, "the token stays in its group")
	assert.Equal(t, "GBTWK999999999X", contractId)
	assert.Equal(t, 1, rows)

	var prefixMatch bool
	var overrideTariff *string
	require.NoError(t, testPool.QueryRow(ctx, `SELECT prefix_match, override_tariff_ref::text FROM token_groups WHERE id = $1`, group.Id).
		Scan(&prefixMatch, &overrideTariff))
	assert.True(t, prefixMatch)
	assert.Nil(t, overrideTariff)
}

// Migration 000047 adds the token groups, the group of a token, the default tariff of a
// location and the tariff of a transaction. The rows that are there keep what they have;
// going down loses only what the migration added.
func TestMigration_CreateTokenGroups(t *testing.T) {
	ctx := context.Background()

	const (
		versionBefore = 46
		versionAfter  = 47
		orgId         = "0193b2c4-0000-7000-8000-0000000000a1"
		otherOrgId    = "0193b2c4-0000-7000-8000-0000000000b2"
		locationId    = "0193b2c5-0000-7000-8000-000000000001"
		tariffId      = "0193b2c5-0000-7000-8000-000000000021"
		otherTariffId = "0193b2c5-0000-7000-8000-000000000022"
		unknownTariff = "0193b2c5-0000-7000-8000-000000000099"
		groupId       = "0193b2c5-0000-7000-8000-000000000031"
		otherGroupId  = "0193b2c5-0000-7000-8000-000000000032"
		unknownGroup  = "0193b2c5-0000-7000-8000-000000000098"
	)

	migrationConnString, pool := migrationTestDatabase(t, "maeve_migration_000047")

	exec := func(statement string, args ...any) {
		t.Helper()
		_, err := pool.Exec(ctx, statement, args...)
		require.NoError(t, err, statement)
	}
	added := func() []string {
		// the C collation orders the names the same on every server
		rows, err := pool.Query(ctx, `
			SELECT name FROM (
				SELECT 'table ' || tablename FROM pg_tables WHERE tablename = 'token_groups'
				UNION ALL SELECT 'index ' || indexname FROM pg_indexes
				  WHERE tablename = 'token_groups' OR indexname IN ('idx_tokens_token_group', 'idx_asset_locations_default_tariff')
				UNION ALL SELECT 'column ' || table_name || '.' || column_name FROM information_schema.columns
				  WHERE (table_name = 'tokens' AND column_name = 'token_group_id')
				     OR (table_name = 'asset_locations' AND column_name = 'default_tariff_ref')
				     OR (table_name = 'transactions' AND column_name LIKE 'tariff%')
				UNION ALL SELECT 'constraint ' || conname FROM pg_constraint
				  WHERE contype = 'f' AND (conname LIKE '%token_group%' OR conname LIKE '%default_tariff%')
			) objects(name)
			ORDER BY name COLLATE "C"`)
		require.NoError(t, err)
		defer rows.Close()
		names := []string{}
		for rows.Next() {
			var name string
			require.NoError(t, rows.Scan(&name))
			names = append(names, name)
		}
		require.NoError(t, rows.Err())
		return names
	}
	wantAdded := []string{
		"column asset_locations.default_tariff_ref",
		"column tokens.token_group_id",
		"column transactions.tariff_id",
		"column transactions.tariff_source",
		"column transactions.tariff_version",
		"constraint asset_locations_default_tariff_ref_fkey",
		"constraint token_groups_override_tariff_ref_fkey",
		"constraint tokens_token_group_id_fkey",
		"index idx_asset_locations_default_tariff",
		"index idx_token_groups_org",
		"index idx_token_groups_override_tariff",
		"index idx_tokens_token_group",
		"index token_groups_name_unique",
		"index token_groups_pkey",
		"table token_groups",
	}
	type row struct {
		tokenGroup, defaultTariff, tariff, tariffSource *string
		tariffVersion                                   *int
	}
	read := func() row {
		t.Helper()
		var r row
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT (SELECT token_group_id::text FROM tokens WHERE uid = 'TAG1'),
			       (SELECT default_tariff_ref::text FROM asset_locations WHERE id = $1),
			       tariff_id::text, tariff_version, tariff_source
			FROM transactions WHERE charge_station_id = 'k7f2.cs001' AND id = 'tx-1'`, locationId).
			Scan(&r.tokenGroup, &r.defaultTariff, &r.tariff, &r.tariffVersion, &r.tariffSource))
		return r
	}
	counts := func() (tokens, locations, transactions int) {
		t.Helper()
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT (SELECT count(*) FROM tokens), (SELECT count(*) FROM asset_locations), (SELECT count(*) FROM transactions)`).
			Scan(&tokens, &locations, &transactions))
		return tokens, locations, transactions
	}
	ref := func(id string) *string { return &id }

	// 1. before the migration: a token, a location and a transaction, and two tariffs
	migrateTo(t, migrationConnString, versionBefore)
	assert.Empty(t, added())
	exec(`INSERT INTO charge_stations (charge_station_id, security_profile, org_id) VALUES ('k7f2.cs001', 1, $1)`, orgId)
	exec(`INSERT INTO asset_locations (id, org_id, party_ref, ocpi_id, address, city, country, latitude, longitude, time_zone)
	      VALUES ($1, $2, 'party-a', 'LOC-1', 'Calle Mayor 1', 'Madrid', 'ESP', '40.416775', '-3.703790', 'Europe/Madrid')`, locationId, orgId)
	exec(`INSERT INTO tariffs (id, org_id, party_ref, ocpi_id, name, current_version)
	      VALUES ($1, $3, 'party-a', 'TARIFF-1', 'Standard', 1), ($2, $3, 'party-a', 'TARIFF-2', 'Staff', 1)`, tariffId, otherTariffId, orgId)
	exec(`INSERT INTO tariff_versions (tariff_id, version, currency, elements, created_by)
	      VALUES ($1, 1, 'EUR', '[{"price_components": [{"type": "ENERGY", "price": 0.3, "step_size": 1}]}]', 'user:alice'),
	             ($2, 1, 'EUR', '[{"price_components": [{"type": "ENERGY", "price": 0.2, "step_size": 1}]}]', 'user:alice')`, tariffId, otherTariffId)
	exec(`INSERT INTO tokens (org_id, country_code, party_id, type, uid, contract_id, issuer, valid, cache_mode, last_updated)
	      VALUES ($1, 'GB', 'TWK', 'RFID', 'TAG1', 'GBTWK012345678V', 'Issuer', true, 'ALWAYS', NOW()),
	             ($1, 'GB', 'TWK', 'RFID', 'TAG2', 'GBTWK012345679V', 'Issuer', true, 'ALWAYS', NOW())`, orgId)
	exec(`INSERT INTO transactions (id, charge_station_id, token_uid, token_type, meter_start, start_timestamp, org_id)
	      VALUES ('tx-1', 'k7f2.cs001', 'TAG1', 'ISO14443', 100, '2026-11-28 09:00:00', $1)`, orgId)

	// 2. up: the rows that were there have no group and no tariff
	migrateTo(t, migrationConnString, versionAfter)
	assert.Equal(t, wantAdded, added())
	assert.Equal(t, row{}, read())

	// 3. the new columns and the table take values
	exec(`INSERT INTO token_groups (id, org_id, name, prefix_match, override_tariff_ref)
	      VALUES ($1, $3, 'Staff', true, $4), ($2, $3, 'Fleet', false, NULL)`, groupId, otherGroupId, orgId, otherTariffId)
	exec(`UPDATE tokens SET token_group_id = $1 WHERE uid = 'TAG1'`, groupId)
	exec(`UPDATE tokens SET token_group_id = $1 WHERE uid = 'TAG2'`, otherGroupId)
	exec(`UPDATE asset_locations SET default_tariff_ref = $2 WHERE id = $1`, locationId, tariffId)
	exec(`UPDATE transactions SET tariff_id = $1, tariff_version = 1, tariff_source = 'token_group' WHERE id = 'tx-1'`, otherTariffId)
	exec(`INSERT INTO token_groups (id, org_id, name) VALUES (gen_random_uuid(), $1, 'Staff')`, otherOrgId) // the name of a group of another org
	version, source := 1, "token_group"
	assert.Equal(t, row{tokenGroup: ref(groupId), defaultTariff: ref(tariffId), tariff: ref(otherTariffId), tariffVersion: &version, tariffSource: &source}, read())
	var prefixMatch bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT prefix_match FROM token_groups WHERE id = $1`, otherGroupId).Scan(&prefixMatch))
	assert.False(t, prefixMatch)

	for name, statement := range map[string]string{
		"name of another group of the org": `
			INSERT INTO token_groups (id, org_id, name) VALUES (gen_random_uuid(), '` + orgId + `', 'Staff')`,
		"group without a name": `
			INSERT INTO token_groups (id, org_id) VALUES (gen_random_uuid(), '` + orgId + `')`,
		"override tariff that is not there": `
			INSERT INTO token_groups (id, org_id, name, override_tariff_ref) VALUES (gen_random_uuid(), '` + orgId + `', 'Guests', '` + unknownTariff + `')`,
		"group of a token that is not there":            `UPDATE tokens SET token_group_id = '` + unknownGroup + `' WHERE uid = 'TAG2'`,
		"default tariff that is not there":              `UPDATE asset_locations SET default_tariff_ref = '` + unknownTariff + `'`,
		"removal of a tariff that a group uses":         `DELETE FROM tariffs WHERE id = '` + otherTariffId + `'`,
		"removal of a tariff that a location uses":      `DELETE FROM tariffs WHERE id = '` + tariffId + `'`,
		"tariff of a transaction that is not a UUID":    `UPDATE transactions SET tariff_id = 'standard'`,
		"version of a transaction that is not a number": `UPDATE transactions SET tariff_version = 'one'`,
	} {
		_, err := pool.Exec(ctx, statement)
		assert.Error(t, err, name)
	}

	// a tariff of a transaction does not have to be there, and a group that goes leaves
	// its tokens behind
	exec(`UPDATE transactions SET tariff_id = $1 WHERE id = 'tx-1'`, unknownTariff)
	exec(`UPDATE transactions SET tariff_id = $1 WHERE id = 'tx-1'`, otherTariffId)
	exec(`DELETE FROM token_groups WHERE id = $1`, otherGroupId)
	var groupOfSecond *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT token_group_id::text FROM tokens WHERE uid = 'TAG2'`).Scan(&groupOfSecond))
	assert.Nil(t, groupOfSecond)

	// 4. down, with groups, members and tariffs there: only what the migration added goes
	migrateTo(t, migrationConnString, versionBefore)
	assert.Empty(t, added())
	tokens, locations, transactions := counts()
	assert.Equal(t, []int{2, 1, 1}, []int{tokens, locations, transactions}, "the rows stay")
	var tariffs, connectorKeys int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM tariffs),
		       (SELECT count(*) FROM pg_constraint WHERE conname IN ('asset_connectors_tariff_fk', 'asset_connectors_adhoc_tariff_fk'))`).
		Scan(&tariffs, &connectorKeys))
	assert.Equal(t, 2, tariffs, "the tariffs stay")
	assert.Equal(t, 2, connectorKeys, "what the migration before it made stays")
	// nothing refers to the tariffs any more
	exec(`DELETE FROM tariffs WHERE id = $1`, otherTariffId)

	// 5. up again: nothing is left of the groups and the tariffs of the rows
	migrateTo(t, migrationConnString, versionAfter)
	assert.Equal(t, wantAdded, added())
	assert.Equal(t, row{}, read())
	var groups int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM token_groups`).Scan(&groups))
	assert.Zero(t, groups)
	tokens, locations, transactions = counts()
	assert.Equal(t, []int{2, 1, 1}, []int{tokens, locations, transactions})
}
