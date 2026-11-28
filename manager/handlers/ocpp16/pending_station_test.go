// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	clockTest "k8s.io/utils/clock/testing"
)

// A charge station that connected without being registered is pending: it boots Pending
// and may not charge until it is approved. A decommissioned one boots Rejected and may
// not charge either. An active station is not affected.

const (
	pendingOrgId     = "0193b2c4-0000-7000-8000-0000000000a1"
	pendingStation   = "aaaa.CP050"
	activeStation    = "aaaa.CP001"
	retiredStation   = "aaaa.CP002"
	pendingHeartbeat = 300
)

var pendingBootTime = time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)

// bootFixture is a BootNotification handler on an in-memory store with tenant A (org
// key aaaa) that has a pending, an active and a decommissioned station.
type bootFixture struct {
	clock   *clockTest.FakePassiveClock
	engine  *inmemory.Store
	events  *eventstest.Fixture
	handler handlers.BootNotificationHandler
}

func newBootFixture(t *testing.T, tenantConfig string) *bootFixture {
	t.Helper()
	ctx := context.Background()
	clk := clockTest.NewFakePassiveClock(pendingBootTime)
	engine := inmemory.NewStore(clk)
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: pendingOrgId, Path: store.OrgPathLabel(pendingOrgId), Slug: "tenant-a", OrgKey: "aaaa",
		Hostnames: []string{"a.example"}, ConfigVersion: 1, Config: json.RawMessage(tenantConfig),
	}))
	created, err := engine.RecordOnboardingConnection(ctx, pendingStation, pendingOrgId, store.OnboardingTransportWs,
		store.OnboardingCredentialOpen, 50, pendingBootTime.Add(-time.Minute))
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, engine.SetChargeStationAuth(ctx, activeStation, &store.ChargeStationAuth{OrgId: pendingOrgId}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, retiredStation, &store.ChargeStationAuth{OrgId: pendingOrgId}))
	require.NoError(t, engine.DecommissionChargeStation(ctx, retiredStation, nil))

	domainEvents := eventstest.New(engine, pendingOrgId, clk)
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine}
	return &bootFixture{
		clock:  clk,
		engine: engine,
		events: domainEvents,
		handler: handlers.BootNotificationHandler{
			Clock:               clk,
			RuntimeDetailsStore: engine,
			SettingsStore:       engine,
			StatusStore:         engine,
			RegistryStore:       engine,
			Tx:                  domainEvents.Tx,
			Events:              domainEvents.Events,
			HeartbeatInterval:   pendingHeartbeat,
			TenantConfig:        &tenancy.ConfigProvider{Resolver: resolver},
		},
	}
}

func pendingBootRequest() *types.BootNotificationJson {
	serial, firmware, iccid, imsi := "SN-50", "1.2.3", "89340711000000000019", "214070000000001"
	return &types.BootNotificationJson{
		ChargePointVendor:       "Acme",
		ChargePointModel:        "Volt 22",
		ChargePointSerialNumber: &serial,
		FirmwareVersion:         &firmware,
		Iccid:                   &iccid,
		Imsi:                    &imsi,
	}
}

func (f *bootFixture) boot(t *testing.T, stationKey string) *types.BootNotificationResponseJson {
	t.Helper()
	resp, err := f.handler.HandleCall(context.Background(), stationKey, pendingBootRequest())
	require.NoError(t, err)
	got, ok := resp.(*types.BootNotificationResponseJson)
	require.True(t, ok, "unexpected response type %T", resp)
	return got
}

func TestBootNotificationOfAPendingStation(t *testing.T) {
	f := newBootFixture(t, `{"version":1,"onboarding_boot_interval_s":90}`)

	got := f.boot(t, pendingStation)
	assert.Equal(t, &types.BootNotificationResponseJson{
		CurrentTime: "2026-11-28T09:00:00Z",
		Interval:    90,
		Status:      types.BootNotificationResponseJsonStatusPending,
	}, got)

	evs := f.events.Take(t)
	require.Len(t, evs, 1)
	assert.Equal(t, events.TypeStationPending, evs[0].Type)
	assert.Equal(t, pendingOrgId, evs[0].OrgId)
	assert.Equal(t, "urn:cpms:"+pendingOrgId+":station/"+pendingStation, evs[0].Subject)
	assert.Equal(t, "station:"+pendingStation, evs[0].Actor)
	assert.Equal(t, events.StationPendingData{
		StationKey:      pendingStation,
		StationId:       "CP050",
		Vendor:          "Acme",
		Model:           "Volt 22",
		SerialNumber:    strPtr("SN-50"),
		FirmwareVersion: strPtr("1.2.3"),
		Iccid:           strPtr("89340711000000000019"),
		Imsi:            strPtr("214070000000001"),
		FirstSeenAt:     pendingBootTime.Add(-time.Minute),
		Transport:       "ws",
		Credential:      "open",
	}, eventstest.Data[events.StationPendingData](t, evs[0]))

	record, err := f.engine.LookupChargeStation(context.Background(), pendingStation)
	require.NoError(t, err)
	assert.Equal(t, store.StationStatePending, record.State)
	assert.Equal(t, pendingBootTime, record.Onboarding.LastSeenAt)
	assert.Equal(t, &pendingBootTime, record.Onboarding.NotifiedAt)
	// what the station reported is recorded as for any station
	require.NotNil(t, record.Runtime)
	assert.Equal(t, strPtr("Acme"), record.Runtime.Vendor)

	t.Run("one station.pending while the station is pending", func(t *testing.T) {
		f.clock.SetTime(pendingBootTime.Add(90 * time.Second))
		got := f.boot(t, pendingStation)
		assert.Equal(t, types.BootNotificationResponseJsonStatusPending, got.Status)
		assert.Equal(t, 90, got.Interval)
		assert.Empty(t, f.events.Take(t))

		record, err := f.engine.LookupChargeStation(context.Background(), pendingStation)
		require.NoError(t, err)
		assert.Equal(t, pendingBootTime.Add(90*time.Second), record.Onboarding.LastSeenAt)
		assert.Equal(t, &pendingBootTime, record.Onboarding.NotifiedAt)
	})
}

func TestBootNotificationOfAPendingStationHasTheDefaultInterval(t *testing.T) {
	for name, config := range map[string]string{
		"a tenant without a config":                   `{}`,
		"a tenant that has not set a boot interval":   `{"version":1,"heartbeat_interval_s":120}`,
		"a tenant that stopped onboarding":            `{"version":1,"onboarding_mode":"preregistered_only"}`,
		"a tenant whose config cannot be read at all": `{"version":1,"onboarding_mode":"open"}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newBootFixture(t, config)
			got := f.boot(t, pendingStation)
			assert.Equal(t, types.BootNotificationResponseJsonStatusPending, got.Status)
			assert.Equal(t, 60, got.Interval)
		})
	}
}

func TestBootNotificationOfADecommissionedStation(t *testing.T) {
	f := newBootFixture(t, `{"version":1,"heartbeat_interval_s":120}`)

	got := f.boot(t, retiredStation)
	assert.Equal(t, &types.BootNotificationResponseJson{
		CurrentTime: "2026-11-28T09:00:00Z",
		Interval:    120,
		Status:      types.BootNotificationResponseJsonStatusRejected,
	}, got)
	assert.Empty(t, f.events.Take(t))
}

func TestBootNotificationOfAnActiveStationIsAccepted(t *testing.T) {
	f := newBootFixture(t, `{"version":1,"heartbeat_interval_s":120,"onboarding_boot_interval_s":90}`)

	got := f.boot(t, activeStation)
	assert.Equal(t, &types.BootNotificationResponseJson{
		CurrentTime: "2026-11-28T09:00:00Z",
		Interval:    120,
		Status:      types.BootNotificationResponseJsonStatusAccepted,
	}, got)
	// so is a station without a record, as before there was a registry
	got = f.boot(t, "aaaa.CP099")
	assert.Equal(t, types.BootNotificationResponseJsonStatusAccepted, got.Status)
	assert.Equal(t, 120, got.Interval)
	assert.Empty(t, f.events.Take(t))
}

// registryWithoutRecords is a storage backend without a registry, like Firestore.
type registryWithoutRecords struct {
	store.ChargeStationRegistryStore
}

func (registryWithoutRecords) LookupChargeStation(context.Context, string) (*store.ChargeStation, error) {
	return nil, fmt.Errorf("LookupChargeStation: %w", store.ErrNotImplemented)
}

func TestBootNotificationWithoutARegistryIsAccepted(t *testing.T) {
	f := newBootFixture(t, `{}`)
	f.handler.RegistryStore = registryWithoutRecords{}

	got := f.boot(t, pendingStation)
	assert.Equal(t, types.BootNotificationResponseJsonStatusAccepted, got.Status)
	assert.Equal(t, pendingHeartbeat, got.Interval)

	f.handler.RegistryStore = nil
	got = f.boot(t, pendingStation)
	assert.Equal(t, types.BootNotificationResponseJsonStatusAccepted, got.Status)
}

// failingOnboardingBoot cannot record the boot of a pending station.
type failingOnboardingBoot struct {
	store.ChargeStationRegistryStore
}

func (failingOnboardingBoot) RecordOnboardingBoot(context.Context, string, time.Time) (*store.StationOnboarding, bool, error) {
	return nil, false, errors.New("database is down")
}

func TestBootNotificationOfAPendingStationIsPendingWhenTheBootCannotBeRecorded(t *testing.T) {
	f := newBootFixture(t, `{}`)
	f.handler.RegistryStore = failingOnboardingBoot{ChargeStationRegistryStore: f.engine}

	got := f.boot(t, pendingStation)
	assert.Equal(t, types.BootNotificationResponseJsonStatusPending, got.Status)
	assert.Empty(t, f.events.Take(t))

	t.Run("an event that cannot be stored", func(t *testing.T) {
		f := newBootFixture(t, `{}`)
		f.events.Events.Err = errOutbox
		got := f.boot(t, pendingStation)
		assert.Equal(t, types.BootNotificationResponseJsonStatusPending, got.Status)
	})
}

func TestBootNotificationFailsWhenTheStateCannotBeLookedUp(t *testing.T) {
	f := newBootFixture(t, `{}`)
	f.handler.RegistryStore = registryLookupStub{err: errors.New("database is down")}

	resp, err := f.handler.HandleCall(context.Background(), activeStation, pendingBootRequest())
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestBootNotificationSendsTheQueuedCommandsOfAPendingStation(t *testing.T) {
	for station, wantKick := range map[string]bool{pendingStation: true, retiredStation: false, activeStation: true} {
		t.Run(station, func(t *testing.T) {
			f := newBootFixture(t, `{}`)
			kicker := &recordingKicker{kicked: make(chan string, 1)}
			f.handler.CommandKicker = kicker
			f.boot(t, station)
			select {
			case kicked := <-kicker.kicked:
				assert.True(t, wantKick, "kicked %s", kicked)
			case <-time.After(200 * time.Millisecond):
				assert.False(t, wantKick, "not kicked")
			}
		})
	}
}

// chargingFixture has the Authorize and StartTransaction handlers on a store with a
// pending, an active and a decommissioned station, and a token that the org knows.
type chargingFixture struct {
	engine    *inmemory.Store
	events    *eventstest.Fixture
	authorize handlers.AuthorizeHandler
	start     handlers.StartTransactionHandler
}

const knownCard = "MYRFIDCARD"

func newChargingFixture(t *testing.T) *chargingFixture {
	t.Helper()
	ctx := context.Background()
	clk := clockTest.NewFakePassiveClock(pendingBootTime)
	engine := inmemory.NewStore(clk)
	tok := rfidToken(knownCard)
	tok.OrgId = pendingOrgId
	require.NoError(t, engine.SetToken(ctx, tok))
	_, err := engine.RecordOnboardingConnection(ctx, pendingStation, pendingOrgId, store.OnboardingTransportWs,
		store.OnboardingCredentialOpen, 50, pendingBootTime)
	require.NoError(t, err)
	require.NoError(t, engine.SetChargeStationAuth(ctx, activeStation, &store.ChargeStationAuth{OrgId: pendingOrgId}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, retiredStation, &store.ChargeStationAuth{OrgId: pendingOrgId}))
	require.NoError(t, engine.DecommissionChargeStation(ctx, retiredStation, nil))
	// every station accepts any token: the state comes first
	for _, key := range []string{pendingStation, activeStation, retiredStation} {
		record, err := engine.LookupChargeStation(ctx, key)
		require.NoError(t, err)
		record.AllowAnyToken = true
		require.NoError(t, engine.UpdateChargeStation(ctx, record))
	}

	domainEvents := eventstest.New(engine, pendingOrgId, clk)
	orgs := &tenancy.Resolver{Stations: engine}
	return &chargingFixture{
		engine: engine,
		events: domainEvents,
		authorize: handlers.AuthorizeHandler{
			TokenStore: engine, RegistryStore: engine, OrgResolver: orgs, Clock: clk,
		},
		start: handlers.StartTransactionHandler{
			Clock: clk, TokenStore: engine, RegistryStore: engine, OrgResolver: orgs, TransactionStore: engine,
			ReservationStore: engine, Tx: domainEvents.Tx, Events: domainEvents.Events,
		},
	}
}

func (f *chargingFixture) authorizeAt(t *testing.T, stationKey, idTag string) *types.AuthorizeResponseJson {
	t.Helper()
	resp, err := f.authorize.HandleCall(context.Background(), stationKey, &types.AuthorizeJson{IdTag: idTag})
	require.NoError(t, err)
	return resp.(*types.AuthorizeResponseJson)
}

func TestAuthorizeAtAStationThatMayNotCharge(t *testing.T) {
	f := newChargingFixture(t)
	invalid := &types.AuthorizeResponseJson{IdTagInfo: types.AuthorizeResponseJsonIdTagInfo{Status: types.AuthorizeResponseJsonIdTagInfoStatusInvalid}}

	for _, station := range []string{pendingStation, retiredStation} {
		for _, idTag := range []string{knownCard, "UNKNOWNCARD"} {
			assert.Equal(t, invalid, f.authorizeAt(t, station, idTag), "%s at %s", idTag, station)
		}
	}

	// an active station answers as before
	accepted := &types.AuthorizeResponseJson{IdTagInfo: types.AuthorizeResponseJsonIdTagInfo{Status: types.AuthorizeResponseJsonIdTagInfoStatusAccepted}}
	assert.Equal(t, accepted, f.authorizeAt(t, activeStation, knownCard))
	assert.Equal(t, accepted, f.authorizeAt(t, activeStation, "UNKNOWNCARD"), "allow_any_token")
}

func TestStartTransactionAtAStationThatMayNotCharge(t *testing.T) {
	ctx := context.Background()
	f := newChargingFixture(t)

	for _, station := range []string{pendingStation, retiredStation} {
		resp, err := f.start.HandleCall(ctx, station, startTransactionRequest(knownCard))
		require.NoError(t, err)
		assert.Equal(t, &types.StartTransactionResponseJson{
			IdTagInfo:     types.StartTransactionResponseJsonIdTagInfo{Status: types.StartTransactionResponseJsonIdTagInfoStatusInvalid},
			TransactionId: -1,
		}, resp, station)

		transactions, total, err := f.engine.ListTransactionsForChargeStation(ctx, station, "", nil, nil, 10, 0)
		require.NoError(t, err)
		assert.Zero(t, total, station)
		assert.Empty(t, transactions, "no transaction is recorded at %s", station)
	}
	assert.Empty(t, f.events.Take(t), "no session.started")

	// an active station starts its transaction as before
	got := startTransaction(t, f.start, activeStation, startTransactionRequest(knownCard))
	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusAccepted, got.IdTagInfo.Status)
	assert.Positive(t, got.TransactionId)
	found, err := f.engine.FindTransaction(ctx, activeStation, handlers.ConvertToUUID(got.TransactionId))
	require.NoError(t, err)
	assert.NotNil(t, found)
	assert.Equal(t, []string{events.TypeSessionStarted}, eventstest.Types(f.events.Take(t)))
}

func TestChargingGateFailsWhenTheStateCannotBeLookedUp(t *testing.T) {
	f := newChargingFixture(t)
	registryErr := errors.New("registry is down")
	f.start.RegistryStore = registryLookupStub{err: registryErr}

	resp, err := f.start.HandleCall(context.Background(), activeStation, startTransactionRequest(knownCard))
	assert.ErrorIs(t, err, registryErr)
	assert.Nil(t, resp)
}
