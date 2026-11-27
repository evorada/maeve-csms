// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/storetest"
)

func TestAssetStore(t *testing.T) {
	storetest.TestAssetStore(t, emptyTestStore)
}

func assetTestLocation(ocpiId string) *store.AssetLocation {
	return &store.AssetLocation{
		OrgId:     "0193b2c4-0000-7000-8000-0000000000a1",
		PartyRef:  "party-a",
		OcpiId:    ocpiId,
		Address:   "Calle Mayor 1",
		City:      "Madrid",
		Country:   "ESP",
		Latitude:  "40.416775",
		Longitude: "-3.703790",
		TimeZone:  "Europe/Madrid",
		Publish:   true,
	}
}

func assetTestEvse(locationId string, ocppEvseId int) *store.AssetEvse {
	return &store.AssetEvse{
		OrgId:      "0193b2c4-0000-7000-8000-0000000000a1",
		LocationId: locationId,
		StationKey: "k7f2.cs001",
		OcppEvseId: ocppEvseId,
		Connectors: []*store.AssetConnector{{
			OcppConnectorId: 1,
			Standard:        "IEC_62196_T2",
			Format:          "SOCKET",
			PowerType:       "AC_3_PHASE",
			MaxVoltage:      400,
			MaxAmperage:     32,
		}},
	}
}

// The writes of the asset store join the transaction of the caller. The methods that
// need several statements run in a savepoint there, so that their failure leaves the
// transaction of the caller usable.
func TestAssetStore_JoinsTheTransaction(t *testing.T) {
	ctx := context.Background()
	emptyTestStore(t)
	require.NoError(t, testStore.SetChargeStationAuth(ctx, "k7f2.cs001",
		&store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: "0193b2c4-0000-7000-8000-0000000000a1"}))

	write := func(ctx context.Context, ocpiId string) (*store.AssetLocation, *store.AssetEvse) {
		loc := assetTestLocation(ocpiId)
		require.NoError(t, testStore.CreateAssetLocation(ctx, loc))
		evse := assetTestEvse(loc.Id, 1)
		require.NoError(t, testStore.CreateAssetEvse(ctx, evse))

		// an EVSE whose second connector clashes fails in its savepoint
		clash := assetTestEvse(loc.Id, 9)
		clash.Connectors = append(clash.Connectors, clash.Connectors[0])
		require.ErrorIs(t, testStore.CreateAssetEvse(ctx, clash), store.ErrAlreadyExists)
		// a location in use is refused before a statement fails
		require.ErrorIs(t, testStore.DeleteAssetLocation(ctx, loc.Id), store.ErrInUse)

		got, err := testStore.LookupAssetEvse(ctx, evse.Id)
		require.NoError(t, err)
		require.NotNil(t, got, "the transaction is still usable")
		return loc, evse
	}

	var rolledBack *store.AssetLocation
	err := testStore.RunInTx(ctx, func(ctx context.Context) error {
		rolledBack, _ = write(ctx, "LOC-1")
		return errStopTx
	})
	require.ErrorIs(t, err, errStopTx)
	got, err := testStore.LookupAssetLocation(ctx, rolledBack.Id)
	require.NoError(t, err)
	assert.Nil(t, got, "rolled back with the transaction")

	var committed *store.AssetEvse
	require.NoError(t, testStore.RunInTx(ctx, func(ctx context.Context) error {
		_, committed = write(ctx, "LOC-2")
		return nil
	}))
	evses, err := testStore.ListAssetEvsesForStation(ctx, "k7f2.cs001")
	require.NoError(t, err)
	if assert.Len(t, evses, 1, "only the EVSE that did not fail") {
		assert.Equal(t, committed.Id, evses[0].Id)
		assert.Len(t, evses[0].Connectors, 1)
	}
	requireNoOpenTransaction(t)
}

// Migration 000041 creates the asset tables; down drops them again.
func TestMigration_CreateAssetTables(t *testing.T) {
	ctx := context.Background()

	const (
		versionBefore = 40
		versionAfter  = 41
	)

	migrationConnString, pool := migrationTestDatabase(t, "maeve_migration_000041")

	assetIndexes := func() []string {
		rows, err := pool.Query(ctx, `
			SELECT indexname FROM pg_indexes
			WHERE tablename IN ('asset_locations', 'asset_evses', 'asset_connectors')
			ORDER BY indexname`)
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
	wantIndexes := []string{
		"asset_connectors_ocpi_unique",
		"asset_connectors_ocpp_unique",
		"asset_connectors_pkey",
		"asset_evses_evse_id_unique",
		"asset_evses_ocpp_unique",
		"asset_evses_pkey",
		"asset_locations_ocpi_id_unique",
		"asset_locations_pkey",
		"idx_asset_connectors_org",
		"idx_asset_evses_location",
		"idx_asset_evses_org",
		"idx_asset_locations_org",
	}

	migrateTo(t, migrationConnString, versionBefore)
	assert.Empty(t, assetIndexes())

	migrateTo(t, migrationConnString, versionAfter)
	assert.Equal(t, wantIndexes, assetIndexes())
	_, err := pool.Exec(ctx, `
		INSERT INTO charge_stations (charge_station_id, security_profile, org_id) VALUES ('k7f2.cs001', 1, gen_random_uuid());
		INSERT INTO asset_locations (id, org_id, party_ref, ocpi_id, address, city, country, latitude, longitude, time_zone)
		VALUES ('0193b2c5-0000-7000-8000-000000000001', gen_random_uuid(), 'party-a', 'LOC-1', 'Calle Mayor 1', 'Madrid', 'ESP', '40.416775', '-3.703790', 'Europe/Madrid');
		INSERT INTO asset_evses (id, org_id, location_id, station_key, ocpp_evse_id)
		VALUES ('0193b2c5-0000-7000-8000-000000000002', gen_random_uuid(), '0193b2c5-0000-7000-8000-000000000001', 'k7f2.cs001', 1)`)
	require.NoError(t, err)
	var status string
	var publish bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT e.status, l.publish FROM asset_evses e JOIN asset_locations l ON l.id = e.location_id`).Scan(&status, &publish))
	assert.Equal(t, "ACTIVE", status)
	assert.True(t, publish)
	for name, statement := range map[string]string{
		"EVSE status that is not known": `
			INSERT INTO asset_evses (id, org_id, location_id, station_key, ocpp_evse_id, status)
			VALUES (gen_random_uuid(), gen_random_uuid(), '0193b2c5-0000-7000-8000-000000000001', 'k7f2.cs001', 2, 'BROKEN')`,
		"OCPP EVSE id 0": `
			INSERT INTO asset_evses (id, org_id, location_id, station_key, ocpp_evse_id)
			VALUES (gen_random_uuid(), gen_random_uuid(), '0193b2c5-0000-7000-8000-000000000001', 'k7f2.cs001', 0)`,
		"connector format that is not known": `
			INSERT INTO asset_connectors (id, org_id, evse_uid, ocpp_connector_id, ocpi_id, standard, format, power_type, max_voltage, max_amperage)
			VALUES (gen_random_uuid(), gen_random_uuid(), '0193b2c5-0000-7000-8000-000000000002', 1, '1', 'IEC_62196_T2', 'PLUG', 'AC_3_PHASE', 400, 32)`,
		"OCPP connector id 0": `
			INSERT INTO asset_connectors (id, org_id, evse_uid, ocpp_connector_id, ocpi_id, standard, format, power_type, max_voltage, max_amperage)
			VALUES (gen_random_uuid(), gen_random_uuid(), '0193b2c5-0000-7000-8000-000000000002', 0, '1', 'IEC_62196_T2', 'SOCKET', 'AC_3_PHASE', 400, 32)`,
		"charge station in use": `DELETE FROM charge_stations WHERE charge_station_id = 'k7f2.cs001'`,
	} {
		_, err := pool.Exec(ctx, statement)
		assert.Error(t, err, name)
	}

	migrateTo(t, migrationConnString, versionBefore)
	assert.Empty(t, assetIndexes())
	var tables int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM pg_tables WHERE tablename LIKE 'asset\_%'`).Scan(&tables))
	assert.Zero(t, tables)

	migrateTo(t, migrationConnString, versionAfter)
	assert.Equal(t, wantIndexes, assetIndexes())
}
