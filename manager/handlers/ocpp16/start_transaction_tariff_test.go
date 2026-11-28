// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/money"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tariffs"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	clockTest "k8s.io/utils/clock/testing"
)

// The tariff of a transaction: chosen when the transaction starts, stored with it, and
// never a reason to refuse or to fail the start.

// tariffFixture is a StartTransaction handler with a tariff selector on one in-memory
// store. The charge station of sessionOrgA has a connector 1 with a regular and an ad hoc
// tariff and a connector 2 without tariffs. The org has an RFID token, an ad hoc token
// and a token in a group with an override tariff.
type tariffFixture struct {
	engine                   *inmemory.Store
	events                   *eventstest.Fixture
	start                    handlers.StartTransactionHandler
	regular, adhoc, employee *store.Tariff
}

// tariffClockTime is the time of the handler's clock; the station reports the start of
// its transactions an hour earlier.
var tariffClockTime = time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)

func tariffVersion(pricePerKwh money.Amount) *store.TariffVersion {
	return &store.TariffVersion{
		Currency: "EUR",
		Elements: []store.TariffElement{{PriceComponents: []store.PriceComponent{
			{Type: store.PriceComponentEnergy, Price: pricePerKwh, StepSize: 1},
		}}},
		CreatedBy: "user:alice",
	}
}

func (f *tariffFixture) tariff(t *testing.T, name string, v *store.TariffVersion) *store.Tariff {
	t.Helper()
	tariff := &store.Tariff{OrgId: sessionOrgA, PartyRef: "party-a", Name: name}
	require.NoError(t, f.engine.CreateTariff(context.Background(), tariff, v))
	return tariff
}

func newTariffFixture(t *testing.T) *tariffFixture {
	t.Helper()
	ctx := context.Background()
	clk := clockTest.NewFakePassiveClock(tariffClockTime)
	engine := inmemory.NewStore(clk)
	orgs := &tenancy.Resolver{Stations: engine, DefaultOrgId: sessionOrgA}
	domainEvents := eventstest.New(engine, sessionOrgA, clk)
	domainEvents.Events.Emitter = &events.OutboxEmitter{Store: engine, Orgs: orgs, Clock: clk, Product: events.Product}
	f := &tariffFixture{engine: engine, events: domainEvents}

	f.regular = f.tariff(t, "Regular", tariffVersion(3000))
	f.adhoc = f.tariff(t, "Ad hoc", tariffVersion(4500))
	f.employee = f.tariff(t, "Employees", tariffVersion(0))

	require.NoError(t, engine.SetChargeStationAuth(ctx, sessionOrgStation, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: sessionOrgA}))
	location := &store.AssetLocation{
		OrgId: sessionOrgA, PartyRef: "party-a", Address: "Calle Mayor 1", City: "Madrid", Country: "ESP",
		Latitude: "40.416775", Longitude: "-3.703790", TimeZone: "Europe/Madrid", Publish: true,
	}
	require.NoError(t, engine.CreateAssetLocation(ctx, location))
	// an OCPP 1.6 connector is an EVSE with one connector
	for ocppEvseId, connector := range map[int]*store.AssetConnector{
		1: {TariffRef: &f.regular.Id, AdhocTariffRef: &f.adhoc.Id},
		2: {},
	} {
		connector.OcppConnectorId, connector.Standard, connector.Format, connector.PowerType = 1, "IEC_62196_T2", "SOCKET", "AC_3_PHASE"
		connector.MaxVoltage, connector.MaxAmperage = 400, 32
		require.NoError(t, engine.CreateAssetEvse(ctx, &store.AssetEvse{
			OrgId: sessionOrgA, LocationId: location.Id, StationKey: sessionOrgStation, OcppEvseId: ocppEvseId,
			Connectors: []*store.AssetConnector{connector},
		}))
	}

	for uid, tokenType := range map[string]string{"RFID0001": "RFID", "ADHOC001": "AD_HOC_USER", "EMP00001": "RFID"} {
		require.NoError(t, engine.SetToken(ctx, &store.Token{
			OrgId: sessionOrgA, CountryCode: "ES", PartyId: "CPO", Type: tokenType, Uid: uid,
			ContractId: "ESCPOC" + uid, Issuer: "Issuer", Valid: true, CacheMode: store.CacheModeAlways,
		}))
	}
	group := &store.TokenGroup{OrgId: sessionOrgA, Name: "Employees", OverrideTariffRef: &f.employee.Id}
	require.NoError(t, engine.CreateTokenGroup(ctx, group))
	require.NoError(t, engine.SetTokenGroup(ctx, sessionOrgA, "EMP00001", &group.Id))

	f.start = handlers.StartTransactionHandler{
		Clock: clk, TokenStore: engine, RegistryStore: engine, OrgResolver: orgs, TransactionStore: engine, ReservationStore: engine,
		Tx: domainEvents.Tx, Events: domainEvents.Events,
		TariffSelector: &tariffs.Selector{Assets: engine, Registry: engine, Tariffs: engine, Groups: engine, Tokens: engine},
	}
	return f
}

// startAt starts a transaction at the connector with the idTag and returns the answer,
// the stored transaction and the payload of its session.started event.
func (f *tariffFixture) startAt(t *testing.T, stationKey string, connectorId int, idTag string) (*types.StartTransactionResponseJson, *store.Transaction, events.SessionStartedData) {
	t.Helper()
	resp := startTransaction(t, f.start, stationKey, &types.StartTransactionJson{
		ConnectorId: connectorId, IdTag: idTag, MeterStart: 1000, Timestamp: "2026-11-28T11:00:00Z",
	})
	require.Positive(t, resp.TransactionId, "a transaction is recorded")
	transaction, err := f.engine.FindTransaction(context.Background(), stationKey, handlers.ConvertToUUID(resp.TransactionId))
	require.NoError(t, err)
	require.NotNil(t, transaction)
	evs := f.events.Take(t)
	require.Equal(t, []string{events.TypeSessionStarted}, eventstest.Types(evs))
	require.Zero(t, f.events.Events.OutsideTx(), "the event is stored with the transaction")
	return resp, transaction, eventstest.Data[events.SessionStartedData](t, evs[0])
}

func (f *tariffFixture) startTransaction(t *testing.T, connectorId int, idTag string) (*types.StartTransactionResponseJson, *store.Transaction, events.SessionStartedData) {
	t.Helper()
	return f.startAt(t, sessionOrgStation, connectorId, idTag)
}

// assertTariff checks the tariff of a stored transaction and of its session.started
// event: version 1 of the tariff, or no tariff when want is nil.
func assertTariff(t *testing.T, want *store.Tariff, source tariffs.Source, transaction *store.Transaction, started events.SessionStartedData, msgAndArgs ...any) {
	t.Helper()
	wantSource := string(source)
	assert.Equal(t, &wantSource, transaction.TariffSource, msgAndArgs...)
	assert.Equal(t, &wantSource, started.TariffSource, msgAndArgs...)
	if want == nil {
		assert.Nil(t, transaction.TariffId, msgAndArgs...)
		assert.Nil(t, transaction.TariffVersion, msgAndArgs...)
		assert.Nil(t, started.TariffId, msgAndArgs...)
		assert.Nil(t, started.TariffVersion, msgAndArgs...)
		return
	}
	version := 1
	assert.Equal(t, &want.Id, transaction.TariffId, msgAndArgs...)
	assert.Equal(t, &version, transaction.TariffVersion, msgAndArgs...)
	assert.Equal(t, &want.Id, started.TariffId, msgAndArgs...)
	assert.Equal(t, &version, started.TariffVersion, msgAndArgs...)
}

// assertNoTariff checks that the tariff of the transaction was not chosen, and that its
// session.started event says nothing about a tariff.
func assertNoTariff(t *testing.T, transaction *store.Transaction, started events.SessionStartedData) {
	t.Helper()
	assert.Nil(t, transaction.TariffId)
	assert.Nil(t, transaction.TariffVersion)
	assert.Nil(t, transaction.TariffSource)
	assert.Nil(t, started.TariffId)
	assert.Nil(t, started.TariffVersion)
	assert.Nil(t, started.TariffSource)
}

func TestStartTransactionPinsTheTariff(t *testing.T) {
	f := newTariffFixture(t)
	accepted, invalid := types.StartTransactionResponseJsonIdTagInfoStatusAccepted, types.StartTransactionResponseJsonIdTagInfoStatusInvalid

	for name, tc := range map[string]struct {
		connectorId int
		idTag       string
		status      types.StartTransactionResponseJsonIdTagInfoStatus
		want        *store.Tariff
		source      tariffs.Source
	}{
		"the tariff of the connector":                                        {1, "RFID0001", accepted, f.regular, tariffs.SourceConnector},
		"S6: the ad hoc tariff for an ad hoc token":                          {1, "ADHOC001", accepted, f.adhoc, tariffs.SourceConnectorAdhoc},
		"S7: the override tariff of the group of the token":                  {1, "EMP00001", accepted, f.employee, tariffs.SourceTokenGroup},
		"S7: also at a connector without tariffs":                            {2, "EMP00001", accepted, f.employee, tariffs.SourceTokenGroup},
		"a connector without tariffs":                                        {2, "RFID0001", accepted, nil, tariffs.SourceNone},
		"an idTag that is refused: the transaction is recorded all the same": {1, "UNKNOWN1", invalid, f.regular, tariffs.SourceConnector},
	} {
		t.Run(name, func(t *testing.T) {
			resp, transaction, started := f.startTransaction(t, tc.connectorId, tc.idTag)

			assert.Equal(t, tc.status, resp.IdTagInfo.Status)
			assertTariff(t, tc.want, tc.source, transaction, started)
			assert.Equal(t, sessionOrgA, transaction.OrgId)
		})
	}

	t.Run("no tariff, where sessions without a tariff have a price", func(t *testing.T) {
		f.start.TariffSelector.Fallback = true
		_, transaction, started := f.startTransaction(t, 2, "RFID0001")
		assertTariff(t, nil, tariffs.SourceFallback, transaction, started)
		_, transaction, started = f.startTransaction(t, 1, "RFID0001")
		assertTariff(t, f.regular, tariffs.SourceConnector, transaction, started, "a tariff comes first")
	})
}

func TestStartTransactionWithoutASelectorChoosesNoTariff(t *testing.T) {
	f := newTariffFixture(t)
	f.start.TariffSelector = nil

	_, transaction, started := f.startTransaction(t, 1, "RFID0001")

	assertNoTariff(t, transaction, started)
}

// A transaction keeps the tariff version that it started with when the tariff gets a new
// version.
func TestTheTariffVersionOfATransactionStays(t *testing.T) {
	ctx := context.Background()
	f := newTariffFixture(t)
	_, first, _ := f.startTransaction(t, 1, "RFID0001")
	require.Equal(t, ptr(1), first.TariffVersion)

	require.NoError(t, f.engine.AddTariffVersion(ctx, f.regular.Id, 1, tariffVersion(3500)))
	_, second, started := f.startTransaction(t, 1, "RFID0001")

	assert.Equal(t, &f.regular.Id, second.TariffId)
	assert.Equal(t, ptr(2), second.TariffVersion, "a new transaction gets the new version")
	assert.Equal(t, ptr(2), started.TariffVersion)
	first, err := f.engine.FindTransaction(ctx, first.ChargeStationId, first.TransactionId)
	require.NoError(t, err)
	assert.Equal(t, &f.regular.Id, first.TariffId)
	assert.Equal(t, ptr(1), first.TariffVersion, "the running transaction keeps its version")
	assert.Equal(t, ptr("connector"), first.TariffSource)

	// nor does it follow its connector to another tariff
	connector, err := f.engine.LookupAssetConnectorByOcpp(ctx, sessionOrgStation, 1, 1)
	require.NoError(t, err)
	connector.TariffRef = &f.adhoc.Id
	require.NoError(t, f.engine.UpdateAssetConnector(ctx, connector))
	first, err = f.engine.FindTransaction(ctx, first.ChargeStationId, first.TransactionId)
	require.NoError(t, err)
	assert.Equal(t, &f.regular.Id, first.TariffId)
	assert.Equal(t, ptr(1), first.TariffVersion)
}

// The tariff must be valid when the transaction is recorded: the time of the manager,
// not the time that the station gives for the start.
func TestStartTransactionSelectsTheTariffForTheTimeOfTheManager(t *testing.T) {
	ctx := context.Background()
	f := newTariffFixture(t)
	setTariff := func(start, end *time.Time) *store.Tariff {
		v := tariffVersion(2000)
		v.StartDateTime, v.EndDateTime = start, end
		tariff := f.tariff(t, "For a time", v)
		connector, err := f.engine.LookupAssetConnectorByOcpp(ctx, sessionOrgStation, 2, 1)
		require.NoError(t, err)
		connector.TariffRef = &tariff.Id
		require.NoError(t, f.engine.UpdateAssetConnector(ctx, connector))
		return tariff
	}
	// the station says that the transaction started at 11:00; it is recorded at 12:00

	sinceHalfPastEleven := setTariff(ptr(tariffClockTime.Add(-30*time.Minute)), nil)
	_, transaction, started := f.startTransaction(t, 2, "RFID0001")
	assertTariff(t, sinceHalfPastEleven, tariffs.SourceConnector, transaction, started)

	setTariff(nil, ptr(tariffClockTime.Add(-30*time.Minute)))
	_, transaction, started = f.startTransaction(t, 2, "RFID0001")
	assertTariff(t, nil, tariffs.SourceNone, transaction, started, "a tariff that ended at 11:30")
}

// downTariffStore is a tariff store whose lookups fail.
type downTariffStore struct {
	store.TariffStore
	err   error
	calls int
	inTx  int
}

func (s *downTariffStore) LookupTariff(ctx context.Context, tariffId string) (*store.Tariff, error) {
	s.calls++
	if eventstest.InTx(ctx) {
		s.inTx++
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.TariffStore.LookupTariff(ctx, tariffId)
}

// tariffTransactionStore is a transaction store that fails to store a tariff, and counts
// how a tariff was stored.
type tariffTransactionStore struct {
	store.TransactionStore
	err         error
	calls, inTx int
}

func (s *tariffTransactionStore) SetTransactionTariff(ctx context.Context, chargeStationId, transactionId string, tariffId *string, version *int, source string) error {
	s.calls++
	if eventstest.InTx(ctx) {
		s.inTx++
	}
	if s.err != nil {
		return s.err
	}
	return s.TransactionStore.SetTransactionTariff(ctx, chargeStationId, transactionId, tariffId, version, source)
}

// Charging does not depend on a tariff: a transaction whose tariff cannot be selected or
// stored starts as ever, without one.
func TestStartTransactionStartsWithoutATariffThatCannotBeChosen(t *testing.T) {
	errDown := errors.New("the store is down")

	t.Run("the tariffs cannot be read", func(t *testing.T) {
		f := newTariffFixture(t)
		tariffStore := &downTariffStore{TariffStore: f.engine, err: errDown}
		f.start.TariffSelector.Tariffs = tariffStore

		resp, transaction, started := f.startTransaction(t, 1, "RFID0001")

		assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusAccepted, resp.IdTagInfo.Status)
		assert.Equal(t, 1, tariffStore.calls)
		assertNoTariff(t, transaction, started)
		assert.Equal(t, ptr(1), transaction.ConnectorId, "the rest of the transaction is stored")
	})

	for name, err := range map[string]error{
		"the tariff cannot be stored":           errDown,
		"the backend keeps no tariff":           fmt.Errorf("SetTransactionTariff: %w", store.ErrNotImplemented),
		"the transaction has a tariff, somehow": fmt.Errorf("tx: %w", store.ErrAlreadyExists),
		"the transaction is not found, somehow": fmt.Errorf("tx: %w", store.ErrNotFound),
	} {
		t.Run(name, func(t *testing.T) {
			f := newTariffFixture(t)
			transactions := &tariffTransactionStore{TransactionStore: f.engine, err: err}
			f.start.TransactionStore = transactions

			resp, transaction, started := f.startTransaction(t, 1, "RFID0001")

			assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusAccepted, resp.IdTagInfo.Status)
			assert.Equal(t, 1, transactions.calls)
			assertNoTariff(t, transaction, started)
		})
	}
}

// The tariff is selected before the transaction is stored, outside the database
// transaction that stores it, and written inside it: reading the tariffs cannot hold up
// or break the storing, and the transaction and its tariff are stored together.
func TestStartTransactionSelectsTheTariffOutsideTheDatabaseTransaction(t *testing.T) {
	f := newTariffFixture(t)
	tariffStore := &downTariffStore{TariffStore: f.engine}
	f.start.TariffSelector.Tariffs = tariffStore
	transactions := &tariffTransactionStore{TransactionStore: f.engine}
	f.start.TransactionStore = transactions

	_, transaction, started := f.startTransaction(t, 1, "RFID0001")

	assertTariff(t, f.regular, tariffs.SourceConnector, transaction, started)
	assert.Equal(t, 1, tariffStore.calls)
	assert.Zero(t, tariffStore.inTx, "the tariffs are read outside the database transaction")
	assert.Equal(t, 1, transactions.calls)
	assert.Equal(t, 1, transactions.inTx, "the tariff is written inside it")
}

// fixedOrg says that every charge station belongs to one org.
type fixedOrg string

func (o fixedOrg) OrgIdForStation(context.Context, string) (string, error) {
	return string(o), nil
}

// The tariff is a tariff of the org of the transaction. A charge station that moves to
// another org while its transaction starts: the transaction belongs to the org that the
// store gives it, and so does its tariff, whatever was selected a moment before.
func TestStartTransactionSelectsTheTariffOfTheOrgOfTheTransaction(t *testing.T) {
	f := newTariffFixture(t)
	// the idTag is authorised, and the tariff selected, for an org that the charge
	// station no longer belongs to when the transaction is stored
	f.start.OrgResolver = fixedOrg(sessionOrgB)
	// a tariff of that other org is not used
	ofB := &store.Tariff{OrgId: sessionOrgB, PartyRef: "party-b", Name: "Of B"}
	require.NoError(t, f.engine.CreateTariff(context.Background(), ofB, tariffVersion(9900)))
	transactions := &tariffTransactionStore{TransactionStore: f.engine}
	f.start.TransactionStore = transactions

	resp, transaction, started := f.startTransaction(t, 1, "ADHOC001")

	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusInvalid, resp.IdTagInfo.Status, "not a token of the other org")
	assert.Equal(t, sessionOrgA, transaction.OrgId)
	assert.Equal(t, &f.adhoc.Id, transaction.TariffId, "the ad hoc tariff of the connector, for the ad hoc token of the org of the transaction")
	assert.Equal(t, ptr(1), transaction.TariffVersion)
	assert.Equal(t, ptr("connector_adhoc"), transaction.TariffSource)
	assert.Equal(t, 1, transactions.calls, "what was selected for the other org is not stored first")
	assert.Zero(t, transactions.inTx, "the tariff is stored after the transaction")
	// the event was stored with the transaction, before its tariff was known
	assert.Nil(t, started.TariffId)
	assert.Nil(t, started.TariffSource)
}

// A transaction of the legacy org belongs to the org of its charge station, like its
// events: its tariff is a tariff of that org.
func TestStartTransactionOfTheLegacyOrgSelectsTheTariffOfTheOrgOfTheStation(t *testing.T) {
	f := newTariffFixture(t)

	// a charge station that is not registered: the store gives its transactions the
	// legacy org, and the resolver gives the station the default org
	_, transaction, started := f.startAt(t, "LEGACY01", 1, "EMP00001")

	assert.Equal(t, store.LegacyOrgId, transaction.OrgId)
	assertTariff(t, f.employee, tariffs.SourceTokenGroup, transaction, started)

	_, transaction, started = f.startAt(t, "LEGACY01", 1, "RFID0001")
	assertTariff(t, nil, tariffs.SourceNone, transaction, started, "a station without connectors in the asset model")
}

func ptr[T any](v T) *T {
	return &v
}
