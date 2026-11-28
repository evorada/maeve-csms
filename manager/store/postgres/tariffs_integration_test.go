// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/money"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/storetest"
)

const tariffTestOrg = "0193b2c4-0000-7000-8000-0000000000a1"

func TestTariffStore(t *testing.T) {
	storetest.TestTariffStore(t, emptyTestStore)
}

func tariffTestTariff(ocpiId string) *store.Tariff {
	return &store.Tariff{OrgId: tariffTestOrg, PartyRef: "party-a", OcpiId: ocpiId, Name: "Tariff " + ocpiId}
}

// tariffTestVersion is a version with one price: energy at the price per kWh (in
// 1/10000), with 21% VAT.
func tariffTestVersion(pricePerKwh money.Amount) *store.TariffVersion {
	vat := money.Percent(2100)
	return &store.TariffVersion{
		Currency: "EUR",
		Elements: []store.TariffElement{{PriceComponents: []store.PriceComponent{
			{Type: store.PriceComponentEnergy, Price: pricePerKwh, Vat: &vat, StepSize: 1},
		}}},
		CreatedBy: "user:alice",
	}
}

// A version is stored in the OCPI shape: the amounts of the limits as NUMERIC(14,4), the
// elements as a JSON document with the OCPI names and the amounts as numbers.
func TestTariffStore_StoredShape(t *testing.T) {
	ctx := context.Background()
	emptyTestStore(t)

	grace := int32(1800)
	perStep := money.Amount(5000)
	v := tariffTestVersion(3450)
	v.MinPrice = &money.Price{ExclVat: 5000, InclVat: 6050}
	v.MaxPrice = &money.Price{ExclVat: 1234567890, InclVat: 1493827147}
	v.Elements[0].PriceComponents = append(v.Elements[0].PriceComponents, store.PriceComponent{
		Type: store.PriceComponentParkingTime, Price: 20000, StepSize: 900, GracePeriodS: &grace, PricePerStep: &perStep,
	})
	tariff := tariffTestTariff("TARIFF-1")
	require.NoError(t, testStore.CreateTariff(ctx, tariff, v))

	var minExcl, minIncl, maxExcl, maxIncl, currency, altText string
	var energyPrice, energyVat, parkingPrice, parkingPerStep, parkingGrace string
	var hasParkingVat bool
	require.NoError(t, testPool.QueryRow(ctx, `
		SELECT min_price_excl_vat::text, min_price_incl_vat::text, max_price_excl_vat::text, max_price_incl_vat::text,
		       currency::text, tariff_alt_text::text,
		       elements #>> '{0,price_components,0,price}', elements #>> '{0,price_components,0,vat}',
		       elements #>> '{0,price_components,1,price}', elements #>> '{0,price_components,1,price_per_step}',
		       elements #>> '{0,price_components,1,grace_period_s}', elements #> '{0,price_components,1}' ? 'vat'
		FROM tariff_versions WHERE tariff_id = $1 AND version = 1`, tariff.Id).
		Scan(&minExcl, &minIncl, &maxExcl, &maxIncl, &currency, &altText,
			&energyPrice, &energyVat, &parkingPrice, &parkingPerStep, &parkingGrace, &hasParkingVat))
	assert.Equal(t, "0.5000", minExcl)
	assert.Equal(t, "0.6050", minIncl)
	assert.Equal(t, "123456.7890", maxExcl)
	assert.Equal(t, "149382.7147", maxIncl)
	assert.Equal(t, "EUR", currency)
	assert.Equal(t, "[]", altText)
	assert.Equal(t, "0.345", energyPrice)
	assert.Equal(t, "21", energyVat)
	assert.Equal(t, "2", parkingPrice)
	assert.Equal(t, "0.5", parkingPerStep)
	assert.Equal(t, "1800", parkingGrace)
	assert.False(t, hasParkingVat, "a component without VAT has no vat")

	got, err := testStore.LookupTariffVersion(ctx, tariff.Id, 1)
	require.NoError(t, err)
	assert.Equal(t, v, got)
}

// The rows of tariff_versions cannot be changed, whoever tries: a trigger refuses the
// update. They only go with their tariff, when the row of the tariff is removed.
func TestTariffVersionsAreImmutable(t *testing.T) {
	ctx := context.Background()
	emptyTestStore(t)

	v := tariffTestVersion(3000)
	tariff := tariffTestTariff("TARIFF-1")
	require.NoError(t, testStore.CreateTariff(ctx, tariff, v))
	second := tariffTestVersion(3500)
	require.NoError(t, testStore.AddTariffVersion(ctx, tariff.Id, 1, second))

	for name, statement := range map[string]string{
		"the content":     `UPDATE tariff_versions SET elements = '[]' WHERE tariff_id = $1`,
		"the currency":    `UPDATE tariff_versions SET currency = 'USD' WHERE tariff_id = $1 AND version = 1`,
		"the number":      `UPDATE tariff_versions SET version = 9 WHERE tariff_id = $1 AND version = 2`,
		"the same values": `UPDATE tariff_versions SET currency = currency WHERE tariff_id = $1`,
	} {
		_, err := testPool.Exec(ctx, statement, tariff.Id)
		if assert.Error(t, err, name) {
			assert.Contains(t, err.Error(), "tariff_versions rows are immutable", name)
		}
	}
	got, err := testStore.LookupTariffVersion(ctx, tariff.Id, 1)
	require.NoError(t, err)
	assert.Equal(t, v, got)
	got, err = testStore.LookupTariffVersion(ctx, tariff.Id, 2)
	require.NoError(t, err)
	assert.Equal(t, second, got)

	// the row of a tariff takes its versions with it
	_, err = testPool.Exec(ctx, `DELETE FROM tariffs WHERE id = $1`, tariff.Id)
	require.NoError(t, err)
	var versions int
	require.NoError(t, testPool.QueryRow(ctx, `SELECT count(*) FROM tariff_versions`).Scan(&versions))
	assert.Zero(t, versions)
}

// Of the callers that add a version to a tariff with the same current version, one wins:
// the others are told that the tariff moved on, and no version is stored twice.
func TestTariffStore_ConcurrentVersions(t *testing.T) {
	ctx := context.Background()
	emptyTestStore(t)

	tariff := tariffTestTariff("TARIFF-1")
	require.NoError(t, testStore.CreateTariff(ctx, tariff, tariffTestVersion(3000)))

	const callers = 8
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = testStore.AddTariffVersion(ctx, tariff.Id, 1, tariffTestVersion(money.Amount(3100+i)))
		}()
	}
	wg.Wait()

	added := 0
	for _, err := range errs {
		if err == nil {
			added++
			continue
		}
		assert.ErrorIs(t, err, store.ErrVersionConflict)
	}
	assert.Equal(t, 1, added)
	versions, err := testStore.ListTariffVersions(ctx, tariff.Id)
	require.NoError(t, err)
	assert.Len(t, versions, 2)
	got, err := testStore.LookupTariff(ctx, tariff.Id)
	require.NoError(t, err)
	assert.Equal(t, 2, got.CurrentVersion)
	requireNoOpenTransaction(t)
}

// The writes of the tariff store join the transaction of the caller. The methods that
// need several statements run in a savepoint there, so that their failure leaves the
// transaction of the caller usable.
func TestTariffStore_JoinsTheTransaction(t *testing.T) {
	ctx := context.Background()
	emptyTestStore(t)

	write := func(ctx context.Context, ocpiId string) *store.Tariff {
		tariff := tariffTestTariff(ocpiId)
		require.NoError(t, testStore.CreateTariff(ctx, tariff, tariffTestVersion(3000)))
		require.NoError(t, testStore.AddTariffVersion(ctx, tariff.Id, 1, tariffTestVersion(3500)))
		require.NoError(t, testStore.RenameTariff(ctx, tariff.Id, "Renamed "+ocpiId))

		// a tariff with a taken OCPI id fails in its savepoint, as does a stale version
		require.ErrorIs(t, testStore.CreateTariff(ctx, tariffTestTariff(ocpiId), tariffTestVersion(3000)), store.ErrAlreadyExists)
		require.ErrorIs(t, testStore.AddTariffVersion(ctx, tariff.Id, 1, tariffTestVersion(9900)), store.ErrVersionConflict)

		got, err := testStore.LookupTariff(ctx, tariff.Id)
		require.NoError(t, err)
		require.NotNil(t, got, "the transaction is still usable, and reads what it wrote")
		require.Equal(t, 2, got.CurrentVersion)
		require.Equal(t, "Renamed "+ocpiId, got.Name)
		return tariff
	}

	var rolledBack *store.Tariff
	err := testStore.RunInTx(ctx, func(ctx context.Context) error {
		rolledBack = write(ctx, "TARIFF-1")
		return errStopTx
	})
	require.ErrorIs(t, err, errStopTx)
	got, err := testStore.LookupTariff(ctx, rolledBack.Id)
	require.NoError(t, err)
	assert.Nil(t, got, "rolled back with the transaction")
	versions, err := testStore.ListTariffVersions(ctx, rolledBack.Id)
	require.NoError(t, err)
	assert.Empty(t, versions)

	var committed *store.Tariff
	require.NoError(t, testStore.RunInTx(ctx, func(ctx context.Context) error {
		committed = write(ctx, "TARIFF-2")
		return testStore.DeleteTariff(ctx, committed.Id)
	}))
	got, err = testStore.LookupTariff(ctx, committed.Id)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 2, got.CurrentVersion)
	assert.NotNil(t, got.DeletedAt)
	requireNoOpenTransaction(t)
}

// Migration 000046 creates the tariff tables and gives the tariff references of the
// connectors their foreign keys; down drops them again. Both ways work with rows there.
func TestMigration_CreateTariffs(t *testing.T) {
	ctx := context.Background()

	const (
		versionBefore = 45
		versionAfter  = 46
		orgId         = "0193b2c4-0000-7000-8000-0000000000a1"
		locationId    = "0193b2c5-0000-7000-8000-000000000001"
		evseUid       = "0193b2c5-0000-7000-8000-000000000002"
		plain         = "0193b2c5-0000-7000-8000-000000000011" // a connector without tariffs
		dangling      = "0193b2c5-0000-7000-8000-000000000012" // a connector with references to nothing
		tariffId      = "0193b2c5-0000-7000-8000-000000000021"
		otherTariffId = "0193b2c5-0000-7000-8000-000000000022"
		unknownTariff = "0193b2c5-0000-7000-8000-000000000099"
	)

	migrationConnString, pool := migrationTestDatabase(t, "maeve_migration_000046")

	exec := func(statement string, args ...any) {
		t.Helper()
		_, err := pool.Exec(ctx, statement, args...)
		require.NoError(t, err, statement)
	}
	tariffObjects := func() []string {
		// the C collation orders the names the same on every server
		rows, err := pool.Query(ctx, `
			SELECT name FROM (
				SELECT 'table ' || tablename FROM pg_tables WHERE tablename IN ('tariffs', 'tariff_versions')
				UNION ALL SELECT 'index ' || indexname FROM pg_indexes
				  WHERE tablename IN ('tariffs', 'tariff_versions') OR indexname LIKE 'idx_asset_connectors_%tariff'
				UNION ALL SELECT 'constraint ' || conname FROM pg_constraint
				  WHERE conrelid = 'asset_connectors'::regclass AND contype = 'f' AND conname LIKE '%tariff%'
				UNION ALL SELECT 'trigger ' || tgname FROM pg_trigger WHERE tgname = 'tariff_versions_no_update'
				UNION ALL SELECT 'function ' || proname FROM pg_proc WHERE proname = 'tariff_versions_immutable'
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
	wantObjects := []string{
		"constraint asset_connectors_adhoc_tariff_fk",
		"constraint asset_connectors_tariff_fk",
		"function tariff_versions_immutable",
		"index idx_asset_connectors_adhoc_tariff",
		"index idx_asset_connectors_tariff",
		"index idx_tariffs_org",
		"index tariff_versions_pkey",
		"index tariffs_ocpi_id_unique",
		"index tariffs_pkey",
		"table tariff_versions",
		"table tariffs",
		"trigger tariff_versions_no_update",
	}
	type connectorRefs struct {
		tariffRef, adhocTariffRef *string
		version                   int64
	}
	connector := func(id string) connectorRefs {
		t.Helper()
		var c connectorRefs
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT tariff_ref::text, adhoc_tariff_ref::text, version FROM asset_connectors WHERE id = $1`, id).
			Scan(&c.tariffRef, &c.adhocTariffRef, &c.version))
		return c
	}
	ref := func(id string) *string { return &id }

	// 1. before the migration a tariff reference of a connector can hold any UUID
	migrateTo(t, migrationConnString, versionBefore)
	assert.Empty(t, tariffObjects())
	exec(`INSERT INTO charge_stations (charge_station_id, security_profile, org_id) VALUES ('k7f2.cs001', 1, $1)`, orgId)
	exec(`INSERT INTO asset_locations (id, org_id, party_ref, ocpi_id, address, city, country, latitude, longitude, time_zone)
	      VALUES ($1, $2, 'party-a', 'LOC-1', 'Calle Mayor 1', 'Madrid', 'ESP', '40.416775', '-3.703790', 'Europe/Madrid')`, locationId, orgId)
	exec(`INSERT INTO asset_evses (id, org_id, location_id, station_key, ocpp_evse_id) VALUES ($1, $2, $3, 'k7f2.cs001', 1)`, evseUid, orgId, locationId)
	exec(`INSERT INTO asset_connectors (id, org_id, evse_uid, ocpp_connector_id, ocpi_id, standard, format, power_type, max_voltage, max_amperage, tariff_ref, adhoc_tariff_ref)
	      VALUES ($1, $3, $4, 1, '1', 'IEC_62196_T2', 'SOCKET', 'AC_3_PHASE', 400, 32, NULL, NULL),
	             ($2, $3, $4, 2, '2', 'IEC_62196_T2', 'SOCKET', 'AC_3_PHASE', 400, 32, $5, $6)`,
		plain, dangling, orgId, evseUid, unknownTariff, otherTariffId)

	// 2. up: the references to nothing are cleared, the connector gets a new version
	migrateTo(t, migrationConnString, versionAfter)
	assert.Equal(t, wantObjects, tariffObjects())
	assert.Equal(t, connectorRefs{version: 1}, connector(plain), "a connector without tariffs is left as it is")
	assert.Equal(t, connectorRefs{version: 2}, connector(dangling))

	// 3. the tables take tariffs with versions, and connectors can refer to them
	exec(`INSERT INTO tariffs (id, org_id, party_ref, ocpi_id, name, current_version)
	      VALUES ($1, $3, 'party-a', 'TARIFF-1', 'Standard', 2), ($2, $3, 'party-a', 'TARIFF-2', 'Ad hoc', 1)`, tariffId, otherTariffId, orgId)
	exec(`INSERT INTO tariff_versions (tariff_id, version, currency, type, min_price_excl_vat, min_price_incl_vat, elements, created_by)
	      VALUES ($1, 1, 'EUR', 'REGULAR', 0.5, 0.605, '[{"price_components": [{"type": "ENERGY", "price": 0.3, "step_size": 1}]}]', 'user:alice'),
	             ($1, 2, 'EUR', 'REGULAR', NULL, NULL, '[{"price_components": [{"type": "ENERGY", "price": 0.35, "step_size": 1}]}]', 'user:alice'),
	             ($2, 1, 'EUR', NULL, NULL, NULL, '[{"price_components": [{"type": "ENERGY", "price": 0.45, "step_size": 1}]}]', 'user:alice')`,
		tariffId, otherTariffId)
	exec(`UPDATE asset_connectors SET tariff_ref = $2, adhoc_tariff_ref = $3 WHERE id = $1`, dangling, tariffId, otherTariffId)
	var altText, minPrice string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT tariff_alt_text::text, min_price_excl_vat::text FROM tariff_versions WHERE tariff_id = $1 AND version = 1`, tariffId).
		Scan(&altText, &minPrice))
	assert.Equal(t, "[]", altText, "no alt text is an empty list")
	assert.Equal(t, "0.5000", minPrice, "an amount has 4 decimals")

	for name, statement := range map[string]string{
		"OCPI id of another tariff of the party": `
			INSERT INTO tariffs (id, org_id, party_ref, ocpi_id, name, current_version)
			VALUES (gen_random_uuid(), gen_random_uuid(), 'party-a', 'TARIFF-1', 'Again', 1)`,
		"current version 0": `
			INSERT INTO tariffs (id, org_id, party_ref, ocpi_id, name, current_version)
			VALUES (gen_random_uuid(), gen_random_uuid(), 'party-a', 'TARIFF-9', 'Zero', 0)`,
		"version of a tariff that is not there": `
			INSERT INTO tariff_versions (tariff_id, version, currency, elements, created_by)
			VALUES ('` + unknownTariff + `', 1, 'EUR', '[]', 'user:alice')`,
		"version number that is taken": `
			INSERT INTO tariff_versions (tariff_id, version, currency, elements, created_by)
			VALUES ('` + tariffId + `', 2, 'EUR', '[]', 'user:alice')`,
		"version 0": `
			INSERT INTO tariff_versions (tariff_id, version, currency, elements, created_by)
			VALUES ('` + tariffId + `', 0, 'EUR', '[]', 'user:alice')`,
		"type that is not known": `
			INSERT INTO tariff_versions (tariff_id, version, currency, type, elements, created_by)
			VALUES ('` + tariffId + `', 3, 'EUR', 'PROFILE_SLOW', '[]', 'user:alice')`,
		"half a price": `
			INSERT INTO tariff_versions (tariff_id, version, currency, max_price_excl_vat, elements, created_by)
			VALUES ('` + tariffId + `', 3, 'EUR', 10, '[]', 'user:alice')`,
		"version without elements": `
			INSERT INTO tariff_versions (tariff_id, version, currency, created_by)
			VALUES ('` + tariffId + `', 3, 'EUR', 'user:alice')`,
		"change of a version":                               `UPDATE tariff_versions SET currency = 'USD' WHERE tariff_id = '` + tariffId + `'`,
		"tariff of a connector that is not there":           `UPDATE asset_connectors SET tariff_ref = '` + unknownTariff + `' WHERE id = '` + plain + `'`,
		"ad hoc tariff of a connector that is not there":    `UPDATE asset_connectors SET adhoc_tariff_ref = '` + unknownTariff + `' WHERE id = '` + plain + `'`,
		"removal of a tariff that a connector uses":         `DELETE FROM tariffs WHERE id = '` + tariffId + `'`,
		"removal of an ad hoc tariff that a connector uses": `DELETE FROM tariffs WHERE id = '` + otherTariffId + `'`,
	} {
		_, err := pool.Exec(ctx, statement)
		assert.Error(t, err, name)
	}

	// 4. down, with tariffs and references there: the tables go, the connectors keep the
	// ids, which name nothing any more
	migrateTo(t, migrationConnString, versionBefore)
	assert.Empty(t, tariffObjects())
	assert.Equal(t, connectorRefs{version: 1}, connector(plain))
	assert.Equal(t, connectorRefs{tariffRef: ref(tariffId), adhocTariffRef: ref(otherTariffId), version: 2}, connector(dangling))

	// 5. up again: the references are cleared again, the tables are empty
	migrateTo(t, migrationConnString, versionAfter)
	assert.Equal(t, wantObjects, tariffObjects())
	assert.Equal(t, connectorRefs{version: 1}, connector(plain))
	assert.Equal(t, connectorRefs{version: 3}, connector(dangling))
	var tariffs int
	require.NoError(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM tariffs) + (SELECT count(*) FROM tariff_versions)`).Scan(&tariffs))
	assert.Zero(t, tariffs)
}
