// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	clockTest "k8s.io/utils/clock/testing"
)

// The stations that are there before the onboarding columns were registered by an
// operator: they have no onboarding, no pending password and no shared credentials.
func TestMigration_AddStationOnboarding(t *testing.T) {
	ctx := context.Background()

	const (
		versionBefore = 42
		versionAfter  = 43
	)
	onboardingColumns := []string{"onboarding", "pending_base64_sha256_password", "shared_credentials"}

	migrationConnString, pool := migrationTestDatabase(t, "maeve_migration_000043")
	columns := func() []string {
		rows, err := pool.Query(ctx, `
			SELECT column_name FROM information_schema.columns
			WHERE table_name = 'charge_stations' ORDER BY column_name`)
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

	migrateTo(t, migrationConnString, versionBefore)
	for _, column := range onboardingColumns {
		assert.NotContains(t, columns(), column)
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO charge_stations (charge_station_id, security_profile, base64_sha256_password, org_id)
		VALUES ('k7f2.cs001', 1, 'DEADBEEF', '0193b2c4-0000-7000-8000-0000000000a1'),
		       ('k7f2.cs002', 0, NULL, '0193b2c4-0000-7000-8000-0000000000a1')`)
	require.NoError(t, err)

	migrateTo(t, migrationConnString, versionAfter)
	assert.Subset(t, columns(), onboardingColumns)
	var unset, stations int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE onboarding IS NULL AND pending_base64_sha256_password IS NULL AND NOT shared_credentials),
		       count(*)
		FROM charge_stations`).Scan(&unset, &stations))
	assert.Equal(t, 2, stations)
	assert.Equal(t, 2, unset)

	// a pending station keeps its onboarding as JSON
	_, err = pool.Exec(ctx, `
		INSERT INTO charge_stations (charge_station_id, security_profile, org_id, state, onboarding)
		VALUES ('k7f2.cs003', 0, '0193b2c4-0000-7000-8000-0000000000a1', 'pending', '{"status":"awaiting_approval"}')`)
	require.NoError(t, err)
	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT onboarding->>'status' FROM charge_stations WHERE charge_station_id = 'k7f2.cs003'`).Scan(&status))
	assert.Equal(t, "awaiting_approval", status)

	migrateTo(t, migrationConnString, versionBefore)
	for _, column := range onboardingColumns {
		assert.NotContains(t, columns(), column)
	}
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM charge_stations`).Scan(&stations))
	assert.Equal(t, 3, stations, "rolling back keeps the stations")

	migrateTo(t, migrationConnString, versionAfter)
	assert.Subset(t, columns(), onboardingColumns)
}

// The boot of a pending station and its station.pending event are stored in one
// transaction: when the event cannot be stored, the station is still answered Pending,
// nothing of the boot is kept, and the next boot emits the event.
func TestOcpp16BootOfAPendingStation(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()
	const orgId = "0193b2c4-0000-7000-8000-0000000000a1"
	const stationKey = "k7f2.PENDING"
	firstSeen := time.Date(2026, 11, 28, 8, 59, 0, 0, time.UTC)
	_, err := testStore.RecordOnboardingConnection(ctx, stationKey, orgId, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 50, firstSeen)
	require.NoError(t, err)

	clk := clockTest.NewFakePassiveClock(time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC))
	fixture := eventstest.New(testStore, orgId, clk)
	failing := eventstest.New(testStore, orgId, clk)
	failing.Events.Emitter = &events.OutboxEmitter{Store: failingOutbox{testStore}, Orgs: events.StaticOrgResolver{OrgId: orgId}, Clock: clk, Product: events.Product}
	boot := func(fixture *eventstest.Fixture) *types.BootNotificationResponseJson {
		handler := ocpp16.BootNotificationHandler{
			Clock: clk, RuntimeDetailsStore: testStore, SettingsStore: testStore, StatusStore: testStore,
			RegistryStore: testStore, Tx: fixture.Tx, Events: fixture.Events, HeartbeatInterval: 300,
		}
		resp, err := handler.HandleCall(ctx, stationKey, &types.BootNotificationJson{ChargePointVendor: "Acme", ChargePointModel: "Volt 22"})
		require.NoError(t, err)
		return resp.(*types.BootNotificationResponseJson)
	}

	got := boot(failing)
	assert.Equal(t, types.BootNotificationResponseJsonStatusPending, got.Status)
	assert.Equal(t, 60, got.Interval)
	record, err := testStore.LookupChargeStation(ctx, stationKey)
	require.NoError(t, err)
	assert.Nil(t, record.Onboarding.NotifiedAt, "rolled back with the event")
	assert.Equal(t, firstSeen, record.Onboarding.LastSeenAt)
	requireNoOpenTransaction(t)

	got = boot(fixture)
	assert.Equal(t, types.BootNotificationResponseJsonStatusPending, got.Status)
	evs := fixture.Take(t)
	require.Equal(t, []string{events.TypeStationPending}, eventstest.Types(evs))
	data := eventstest.Data[events.StationPendingData](t, evs[0])
	assert.Equal(t, "PENDING", data.StationId)
	assert.Equal(t, firstSeen, data.FirstSeenAt)
	record, err = testStore.LookupChargeStation(ctx, stationKey)
	require.NoError(t, err)
	require.NotNil(t, record.Onboarding.NotifiedAt)
	assert.Equal(t, clk.Now(), *record.Onboarding.NotifiedAt)

	boot(fixture)
	assert.Empty(t, fixture.Take(t), "one station.pending while the station is pending")
	requireNoOpenTransaction(t)
}
