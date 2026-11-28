// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	types201 "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/services"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tariffs"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	clockTest "k8s.io/utils/clock/testing"
)

// The tests in this file select and store the tariff of a transaction with the tables of
// PostgreSQL: the selection reads the assets, the tokens, the token groups, the tariffs
// and the config of the tenant, and the OCPP handlers store the tariff with the
// transaction.

const tariffSelectionStation = "k7f2.cs001"

// tariffSelectionFixture is a tenant with a charge station at a location. Connector 1 of
// the station has a regular and an ad hoc tariff, connector 2 has none; the location and
// the tenant have a default tariff. The tokens whose UID starts with 04A1 are in a group
// with an override tariff, and the party XX*PAY sells ad hoc sessions.
type tariffSelectionFixture struct {
	selector                                           *tariffs.Selector
	orgs                                               *tenancy.Resolver
	regular, adhoc, fleet, atLocation, ofTenant, other *store.Tariff
}

func newTariffSelectionFixture(t *testing.T) *tariffSelectionFixture {
	t.Helper()
	ctx := context.Background()
	emptyTestStore(t)
	f := &tariffSelectionFixture{}

	tariff := func(orgId, name string) *store.Tariff {
		created := &store.Tariff{OrgId: orgId, PartyRef: "party-of-" + orgId, Name: name}
		require.NoError(t, testStore.CreateTariff(ctx, created, tariffTestVersion(3000)))
		return created
	}
	f.regular, f.adhoc, f.fleet = tariff(testOrgA, "Regular"), tariff(testOrgA, "Ad hoc"), tariff(testOrgA, "Fleet")
	f.atLocation, f.ofTenant = tariff(testOrgA, "Location"), tariff(testOrgA, "Tenant")
	f.other = tariff(testOrgB, "Of another org")

	config, err := json.Marshal(map[string]any{"version": 1, "default_tariff_ref": f.ofTenant.Id, "adhoc_emsp_parties": []string{"XX*PAY"}})
	require.NoError(t, err)
	require.NoError(t, testStore.PutTenant(ctx, &store.Tenant{
		OrgId: testOrgA, Path: store.OrgPathLabel(testOrgA), Slug: "tenant-a", OrgKey: "k7f2", ConfigVersion: 1, Config: config,
	}))
	require.NoError(t, testStore.SetChargeStationAuth(ctx, tariffSelectionStation, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: testOrgA}))
	location := &store.AssetLocation{
		OrgId: testOrgA, PartyRef: "party-a", Address: "Calle Mayor 1", City: "Madrid", Country: "ESP",
		Latitude: "40.416775", Longitude: "-3.703790", TimeZone: "Europe/Madrid", Publish: true, DefaultTariffRef: &f.atLocation.Id,
	}
	require.NoError(t, testStore.CreateAssetLocation(ctx, location))
	connector := func(tariffRef, adhocTariffRef *string) []*store.AssetConnector {
		return []*store.AssetConnector{{
			OcppConnectorId: 1, Standard: "IEC_62196_T2", Format: "SOCKET", PowerType: "AC_3_PHASE", MaxVoltage: 400, MaxAmperage: 32,
			TariffRef: tariffRef, AdhocTariffRef: adhocTariffRef,
		}}
	}
	for ocppEvseId, connectors := range map[int][]*store.AssetConnector{1: connector(&f.regular.Id, &f.adhoc.Id), 2: connector(nil, nil)} {
		require.NoError(t, testStore.CreateAssetEvse(ctx, &store.AssetEvse{
			OrgId: testOrgA, LocationId: location.Id, StationKey: tariffSelectionStation, OcppEvseId: ocppEvseId, Connectors: connectors,
		}))
	}

	token := func(orgId, tokenType, countryCode, partyId, uid string) {
		require.NoError(t, testStore.SetToken(ctx, &store.Token{
			OrgId: orgId, CountryCode: countryCode, PartyId: partyId, Type: tokenType, Uid: uid, ContractId: countryCode + partyId + "C" + uid,
			Issuer: "Issuer", Valid: true, CacheMode: store.CacheModeAlways, LastUpdated: "2026-11-28T08:00:00Z",
		}))
	}
	token(testOrgA, "RFID", "ES", "CPO", "RFID0001")
	token(testOrgA, "AD_HOC_USER", "ES", "CPO", "ADHOC001")
	token(testOrgA, "APP_USER", "XX", "PAY", "PAY00001")
	token(testOrgA, "RFID", "ES", "CPO", "04A1")
	group := &store.TokenGroup{OrgId: testOrgA, Name: "Fleet", PrefixMatch: true, OverrideTariffRef: &f.fleet.Id}
	require.NoError(t, testStore.CreateTokenGroup(ctx, group))
	require.NoError(t, testStore.SetTokenGroup(ctx, testOrgA, "04A1", &group.Id))
	// the same UIDs in another org, in a group with a tariff of that org
	token(testOrgB, "RFID", "ES", "OTH", "RFID0001")
	ofB := &store.TokenGroup{OrgId: testOrgB, Name: "Fleet", OverrideTariffRef: &f.other.Id}
	require.NoError(t, testStore.CreateTokenGroup(ctx, ofB))
	require.NoError(t, testStore.SetTokenGroup(ctx, testOrgB, "RFID0001", &ofB.Id))

	f.orgs = &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: testStore, Stations: testStore}
	f.selector = &tariffs.Selector{
		Assets: testStore, Registry: testStore, Tariffs: testStore, Groups: testStore, Tokens: testStore,
		Tenants: &tenancy.ConfigProvider{Resolver: f.orgs},
	}
	return f
}

func TestTariffSelection_Order(t *testing.T) {
	ctx := context.Background()
	f := newTariffSelectionFixture(t)
	at := time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)

	for name, tc := range map[string]struct {
		connectorId int
		tokenUid    string
		want        *store.Tariff
		source      tariffs.Source
	}{
		"S7: a UID with the prefix of a token of the group": {1, "04a1b2c3d4", f.fleet, tariffs.SourceTokenGroup},
		"S6: an ad hoc token":                               {1, "ADHOC001", f.adhoc, tariffs.SourceConnectorAdhoc},
		"S6: a token of the party of ad hoc payment":        {1, "PAY00001", f.adhoc, tariffs.SourceConnectorAdhoc},
		"S6: an RFID token":                                 {1, "RFID0001", f.regular, tariffs.SourceConnector},
		"a connector without tariffs":                       {2, "ADHOC001", f.atLocation, tariffs.SourceLocation},
		"a connector that is not in the asset model":        {7, "RFID0001", f.ofTenant, tariffs.SourceTenantDefault},
	} {
		choice, err := f.selector.Select(ctx, tariffs.SelectInput{
			OrgId: testOrgA, StationKey: tariffSelectionStation, OcppVersion: "ocpp1.6",
			EvseId: tc.connectorId, ConnectorId: tc.connectorId, TokenUid: tc.tokenUid, At: at,
		})
		require.NoError(t, err, name)
		assert.Equal(t, tariffs.Choice{TariffId: tc.want.Id, Version: 1, Source: tc.source}, choice, name)
	}

	// in a database transaction the selection reads what the transaction wrote
	err := testStore.RunInTx(ctx, func(ctx context.Context) error {
		require.NoError(t, testStore.AddTariffVersion(ctx, f.regular.Id, 1, tariffTestVersion(3500)))
		choice, err := f.selector.Select(ctx, tariffs.SelectInput{
			OrgId: testOrgA, StationKey: tariffSelectionStation, OcppVersion: "ocpp1.6", EvseId: 1, ConnectorId: 1, TokenUid: "RFID0001", At: at,
		})
		require.NoError(t, err)
		assert.Equal(t, tariffs.Choice{TariffId: f.regular.Id, Version: 2, Source: tariffs.SourceConnector}, choice)
		return errStopTx
	})
	require.ErrorIs(t, err, errStopTx)
	requireNoOpenTransaction(t)
}

// tariffColumns reads the tariff of a transaction as it is stored.
func tariffColumns(t *testing.T, transactionId string) (tariffId *string, version *int, source *string) {
	t.Helper()
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT tariff_id::text, tariff_version, tariff_source FROM transactions WHERE charge_station_id = $1 AND id = $2`,
		tariffSelectionStation, transactionId).Scan(&tariffId, &version, &source))
	return tariffId, version, source
}

// An OCPP 1.6 transaction gets its tariff in the database transaction that stores it,
// and its session.started event names it.
func TestTariffSelection_StartTransaction(t *testing.T) {
	ctx := context.Background()
	f := newTariffSelectionFixture(t)
	clk := clockTest.NewFakePassiveClock(time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC))
	fixture := eventstest.New(testStore, testOrgA, clk)
	fixture.Events.Emitter = &events.OutboxEmitter{Store: testStore, Orgs: f.orgs, Clock: clk, Product: events.Product}
	handler := ocpp16.StartTransactionHandler{
		Clock: clk, TokenStore: testStore, RegistryStore: testStore, OrgResolver: f.orgs, TransactionStore: testStore, ReservationStore: testStore,
		Tx: fixture.Tx, Events: fixture.Events, TariffSelector: f.selector,
	}
	start := func(connectorId int, idTag string) (string, events.SessionStartedData) {
		t.Helper()
		resp, err := handler.HandleCall(ctx, tariffSelectionStation, &types.StartTransactionJson{
			ConnectorId: connectorId, IdTag: idTag, MeterStart: 1000, Timestamp: "2026-11-28T11:00:00Z",
		})
		require.NoError(t, err)
		evs := fixture.Take(t)
		require.Equal(t, []string{events.TypeSessionStarted}, eventstest.Types(evs))
		assert.Equal(t, testOrgA, evs[0].OrgId)
		return ocpp16.ConvertToUUID(resp.(*types.StartTransactionResponseJson).TransactionId), eventstest.Data[events.SessionStartedData](t, evs[0])
	}

	first, started := start(1, "RFID0001")
	tariffId, version, source := tariffColumns(t, first)
	assert.Equal(t, &f.regular.Id, tariffId)
	assert.Equal(t, ptr(1), version)
	assert.Equal(t, ptr("connector"), source)
	assert.Equal(t, &f.regular.Id, started.TariffId)
	assert.Equal(t, ptr(1), started.TariffVersion)
	assert.Equal(t, ptr("connector"), started.TariffSource)

	// a new version of the tariff is for the transactions that start after it
	require.NoError(t, testStore.AddTariffVersion(ctx, f.regular.Id, 1, tariffTestVersion(3500)))
	second, started := start(1, "RFID0001")
	_, version, _ = tariffColumns(t, second)
	assert.Equal(t, ptr(2), version)
	assert.Equal(t, ptr(2), started.TariffVersion)
	_, version, _ = tariffColumns(t, first)
	assert.Equal(t, ptr(1), version, "the running transaction keeps its version")

	third, started := start(2, "04A1FFFF")
	tariffId, _, source = tariffColumns(t, third)
	assert.Equal(t, &f.fleet.Id, tariffId)
	assert.Equal(t, ptr("token_group"), source)
	assert.Equal(t, ptr("token_group"), started.TariffSource)
	stored, err := testStore.FindTransaction(ctx, tariffSelectionStation, third)
	require.NoError(t, err)
	assert.Equal(t, &f.fleet.Id, stored.TariffId)
	assert.Equal(t, testOrgA, stored.OrgId)
	assert.Zero(t, fixture.Events.OutsideTx())
	requireNoOpenTransaction(t)

	// a transaction whose event cannot be stored is not stored, and neither is its tariff
	fixture.Events.Emitter = &events.OutboxEmitter{Store: failingOutbox{OutboxStore: testStore}, Orgs: f.orgs, Product: events.Product}
	_, err = handler.HandleCall(ctx, tariffSelectionStation, &types.StartTransactionJson{
		ConnectorId: 1, IdTag: "RFID0001", MeterStart: 1000, Timestamp: "2026-11-28T11:00:00Z",
	})
	require.ErrorIs(t, err, errAppendFailed)
	var transactions int
	require.NoError(t, testPool.QueryRow(ctx, `SELECT count(*) FROM transactions`).Scan(&transactions))
	assert.Equal(t, 3, transactions)
	requireNoOpenTransaction(t)
}

// An OCPP 2.0.1 transaction gets its tariff with the event that names its EVSE.
func TestTariffSelection_TransactionEvent(t *testing.T) {
	ctx := context.Background()
	f := newTariffSelectionFixture(t)
	clk := clockTest.NewFakePassiveClock(time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC))
	handler := ocpp201.TransactionEventHandler{
		Store:            testStore,
		TokenAuthService: &services.OcppTokenAuthService{Clock: clk, TokenStore: testStore, OrgResolver: f.orgs, Registry: testStore},
		TariffService:    services.BasicKwhTariffService{},
		TariffSelector:   f.selector,
		OrgResolver:      f.orgs,
		Clock:            clk,
	}
	event := func(eventType types201.TransactionEventEnumType, transactionId, idToken string, seqNo int, evse *types201.EVSEType) {
		t.Helper()
		req := &types201.TransactionEventRequestJson{
			EventType: eventType, TriggerReason: types201.TriggerReasonEnumTypeAuthorized, Timestamp: "2026-11-28T11:00:00Z",
			SeqNo: seqNo, Evse: evse, TransactionInfo: types201.TransactionType{TransactionId: transactionId},
		}
		if idToken != "" {
			req.IdToken = &types201.IdTokenType{Type: types201.IdTokenEnumTypeISO14443, IdToken: idToken}
		}
		_, err := handler.HandleCall(ctx, tariffSelectionStation, req)
		require.NoError(t, err)
	}
	connector := 1

	event(types201.TransactionEventEnumTypeStarted, "tx-1", "ADHOC001", 0, &types201.EVSEType{Id: 1, ConnectorId: &connector})
	tariffId, version, source := tariffColumns(t, "tx-1")
	assert.Equal(t, &f.adhoc.Id, tariffId)
	assert.Equal(t, ptr(1), version)
	assert.Equal(t, ptr("connector_adhoc"), source)

	// the later events of the transaction leave its tariff as it is
	require.NoError(t, testStore.AddTariffVersion(ctx, f.adhoc.Id, 1, tariffTestVersion(5000)))
	event(types201.TransactionEventEnumTypeUpdated, "tx-1", "", 1, &types201.EVSEType{Id: 1, ConnectorId: &connector})
	event(types201.TransactionEventEnumTypeEnded, "tx-1", "RFID0001", 2, &types201.EVSEType{Id: 2, ConnectorId: &connector})
	tariffId, version, source = tariffColumns(t, "tx-1")
	assert.Equal(t, &f.adhoc.Id, tariffId)
	assert.Equal(t, ptr(1), version)
	assert.Equal(t, ptr("connector_adhoc"), source)

	// a transaction that starts without its EVSE gets its tariff when the EVSE is named
	event(types201.TransactionEventEnumTypeStarted, "tx-2", "04A1B2C3", 0, nil)
	tariffId, version, source = tariffColumns(t, "tx-2")
	assert.Nil(t, tariffId)
	assert.Nil(t, version)
	assert.Nil(t, source)
	event(types201.TransactionEventEnumTypeUpdated, "tx-2", "", 1, &types201.EVSEType{Id: 2})
	tariffId, version, source = tariffColumns(t, "tx-2")
	assert.Equal(t, &f.fleet.Id, tariffId)
	assert.Equal(t, ptr(1), version)
	assert.Equal(t, ptr("token_group"), source)
	requireNoOpenTransaction(t)
}
