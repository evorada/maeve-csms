// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clockTest "k8s.io/utils/clock/testing"

	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/money"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/services"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tariffs"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// The tariff of a transaction: chosen with the first event that says where the
// transaction runs, stored with it, and never a reason to fail an event.

const (
	tariffOrgA    = "0193b2c4-0000-7000-8000-0000000000a1"
	tariffStation = "aaaa.CP201"
	// the time that the station gives its events
	tariffEventTime = "2026-11-28T11:00:00Z"
)

// tariffClockTime is the time of the manager, an hour after the events of the station.
var tariffClockTime = time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)

// tariffFixture is a TransactionEvent handler with a tariff selector on one in-memory
// store. EVSE 1 of the charge station of tariffOrgA has a connector 1 with a regular and
// an ad hoc tariff and a connector 2 with a tariff of its own; EVSE 2 has a connector
// without tariffs. The org has an RFID token, an ad hoc token and a token in a group with
// an override tariff.
type tariffFixture struct {
	engine                           *inmemory.Store
	handler                          handlers.TransactionEventHandler
	regular, adhoc, second, employee *store.Tariff
}

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
	tariff := &store.Tariff{OrgId: tariffOrgA, PartyRef: "party-a", Name: name}
	require.NoError(t, f.engine.CreateTariff(context.Background(), tariff, v))
	return tariff
}

func newTariffFixture(t *testing.T) *tariffFixture {
	t.Helper()
	ctx := context.Background()
	clk := clockTest.NewFakePassiveClock(tariffClockTime)
	engine := inmemory.NewStore(clk)
	orgs := &tenancy.Resolver{Stations: engine, DefaultOrgId: tariffOrgA}
	f := &tariffFixture{engine: engine}

	f.regular = f.tariff(t, "Regular", tariffVersion(3000))
	f.adhoc = f.tariff(t, "Ad hoc", tariffVersion(4500))
	f.second = f.tariff(t, "Second connector", tariffVersion(3500))
	f.employee = f.tariff(t, "Employees", tariffVersion(0))

	require.NoError(t, engine.SetChargeStationAuth(ctx, tariffStation, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: tariffOrgA}))
	location := &store.AssetLocation{
		OrgId: tariffOrgA, PartyRef: "party-a", Address: "Calle Mayor 1", City: "Madrid", Country: "ESP",
		Latitude: "40.416775", Longitude: "-3.703790", TimeZone: "Europe/Madrid", Publish: true,
	}
	require.NoError(t, engine.CreateAssetLocation(ctx, location))
	connector := func(id int, tariffRef, adhocTariffRef *string) *store.AssetConnector {
		return &store.AssetConnector{
			OcppConnectorId: id, Standard: "IEC_62196_T2", Format: "SOCKET", PowerType: "AC_3_PHASE", MaxVoltage: 400, MaxAmperage: 32,
			TariffRef: tariffRef, AdhocTariffRef: adhocTariffRef,
		}
	}
	for ocppEvseId, connectors := range map[int][]*store.AssetConnector{
		1: {connector(1, &f.regular.Id, &f.adhoc.Id), connector(2, &f.second.Id, nil)},
		2: {connector(1, nil, nil)},
	} {
		require.NoError(t, engine.CreateAssetEvse(ctx, &store.AssetEvse{
			OrgId: tariffOrgA, LocationId: location.Id, StationKey: tariffStation, OcppEvseId: ocppEvseId, Connectors: connectors,
		}))
	}

	for uid, tokenType := range map[string]string{"RFID0001": "RFID", "ADHOC001": "AD_HOC_USER", "EMP00001": "RFID"} {
		require.NoError(t, engine.SetToken(ctx, &store.Token{
			OrgId: tariffOrgA, CountryCode: "ES", PartyId: "CPO", Type: tokenType, Uid: uid,
			ContractId: "ESCPOC" + uid, Issuer: "Issuer", Valid: true, CacheMode: store.CacheModeAlways,
		}))
	}
	group := &store.TokenGroup{OrgId: tariffOrgA, Name: "Employees", OverrideTariffRef: &f.employee.Id}
	require.NoError(t, engine.CreateTokenGroup(ctx, group))
	require.NoError(t, engine.SetTokenGroup(ctx, tariffOrgA, "EMP00001", &group.Id))

	f.handler = handlers.TransactionEventHandler{
		Store:            engine,
		TokenAuthService: &services.OcppTokenAuthService{Clock: clk, TokenStore: engine, OrgResolver: orgs, Registry: engine},
		TariffSelector:   &tariffs.Selector{Assets: engine, Registry: engine, Tariffs: engine, Groups: engine, Tokens: engine},
		OrgResolver:      orgs,
		Clock:            clk,
	}
	return f
}

// transactionEvent is an event of a transaction. evse is the EVSE id and, after it, the
// connector id: none says nothing about the EVSE.
func transactionEvent(eventType types.TransactionEventEnumType, transactionId, idToken string, evse ...int) *types.TransactionEventRequestJson {
	req := &types.TransactionEventRequestJson{
		EventType:       eventType,
		TriggerReason:   types.TriggerReasonEnumTypeAuthorized,
		Timestamp:       tariffEventTime,
		TransactionInfo: types.TransactionType{TransactionId: transactionId},
	}
	if idToken != "" {
		req.IdToken = &types.IdTokenType{Type: types.IdTokenEnumTypeISO14443, IdToken: idToken}
	}
	if len(evse) > 0 {
		req.Evse = &types.EVSEType{Id: evse[0]}
	}
	if len(evse) > 1 {
		req.Evse.ConnectorId = &evse[1]
	}
	return req
}

// send handles the event at the station and returns the stored transaction of the event.
func (f *tariffFixture) sendAt(t *testing.T, stationKey string, req *types.TransactionEventRequestJson) *store.Transaction {
	t.Helper()
	resp, err := f.handler.HandleCall(context.Background(), stationKey, req)
	require.NoError(t, err)
	require.IsType(t, &types.TransactionEventResponseJson{}, resp)
	transaction, err := f.engine.FindTransaction(context.Background(), stationKey, req.TransactionInfo.TransactionId)
	require.NoError(t, err)
	require.NotNil(t, transaction)
	return transaction
}

func (f *tariffFixture) send(t *testing.T, req *types.TransactionEventRequestJson) *store.Transaction {
	t.Helper()
	return f.sendAt(t, tariffStation, req)
}

// assertTariff checks the tariff of a stored transaction: the version of the tariff, or
// no tariff when want is nil.
func assertTariff(t *testing.T, want *store.Tariff, version int, source tariffs.Source, transaction *store.Transaction, msgAndArgs ...any) {
	t.Helper()
	wantSource := string(source)
	assert.Equal(t, &wantSource, transaction.TariffSource, msgAndArgs...)
	if want == nil {
		assert.Nil(t, transaction.TariffId, msgAndArgs...)
		assert.Nil(t, transaction.TariffVersion, msgAndArgs...)
		return
	}
	assert.Equal(t, &want.Id, transaction.TariffId, msgAndArgs...)
	assert.Equal(t, &version, transaction.TariffVersion, msgAndArgs...)
}

// assertNoTariff checks that the tariff of the transaction was not chosen.
func assertNoTariff(t *testing.T, transaction *store.Transaction, msgAndArgs ...any) {
	t.Helper()
	assert.Nil(t, transaction.TariffId, msgAndArgs...)
	assert.Nil(t, transaction.TariffVersion, msgAndArgs...)
	assert.Nil(t, transaction.TariffSource, msgAndArgs...)
}

func TestStartedEventPinsTheTariff(t *testing.T) {
	f := newTariffFixture(t)
	started := types.TransactionEventEnumTypeStarted

	for name, tc := range map[string]struct {
		req    *types.TransactionEventRequestJson
		want   *store.Tariff
		source tariffs.Source
	}{
		"the tariff of the connector":                       {transactionEvent(started, "tx-1", "RFID0001", 1, 1), f.regular, tariffs.SourceConnector},
		"the tariff of the second connector of the EVSE":    {transactionEvent(started, "tx-2", "RFID0001", 1, 2), f.second, tariffs.SourceConnector},
		"S6: the ad hoc tariff for an ad hoc token":         {transactionEvent(started, "tx-3", "ADHOC001", 1, 1), f.adhoc, tariffs.SourceConnectorAdhoc},
		"S6: an ad hoc token at a connector without one":    {transactionEvent(started, "tx-4", "ADHOC001", 1, 2), f.second, tariffs.SourceConnector},
		"S7: the override tariff of the group of the token": {transactionEvent(started, "tx-5", "EMP00001", 1, 1), f.employee, tariffs.SourceTokenGroup},
		"an EVSE without tariffs":                           {transactionEvent(started, "tx-6", "RFID0001", 2, 1), nil, tariffs.SourceNone},
		"an EVSE without its connector":                     {transactionEvent(started, "tx-7", "RFID0001", 1), nil, tariffs.SourceNone},
		"an EVSE without its connector, a token in a group": {transactionEvent(started, "tx-8", "EMP00001", 1), f.employee, tariffs.SourceTokenGroup},
		"a token that is refused":                           {transactionEvent(started, "tx-9", "UNKNOWN1", 1, 1), f.regular, tariffs.SourceConnector},
		"no token":                                          {transactionEvent(started, "tx-10", "", 1, 1), f.regular, tariffs.SourceConnector},
	} {
		transaction := f.send(t, tc.req)
		assertTariff(t, tc.want, 1, tc.source, transaction, name)
		assert.Equal(t, tariffOrgA, transaction.OrgId, name)
	}

	f.handler.TariffSelector.Fallback = true
	assertTariff(t, nil, 0, tariffs.SourceFallback, f.send(t, transactionEvent(started, "tx-11", "RFID0001", 2, 1)),
		"no tariff, where sessions without a tariff have a price")
}

func TestTransactionEventWithoutASelectorChoosesNoTariff(t *testing.T) {
	f := newTariffFixture(t)
	f.handler.TariffSelector = nil

	assertNoTariff(t, f.send(t, transactionEvent(types.TransactionEventEnumTypeStarted, "tx-1", "RFID0001", 1, 1)))
}

// A transaction keeps the tariff version that it started with: a new version of the
// tariff, and the later events of the transaction, change nothing.
func TestTheTariffVersionOfATransactionStays(t *testing.T) {
	ctx := context.Background()
	f := newTariffFixture(t)
	tariffStore := &downTariffStore{TariffStore: f.engine}
	f.handler.TariffSelector.Tariffs = tariffStore
	first := f.send(t, transactionEvent(types.TransactionEventEnumTypeStarted, "tx-1", "RFID0001", 1, 1))
	assertTariff(t, f.regular, 1, tariffs.SourceConnector, first)

	require.NoError(t, f.engine.AddTariffVersion(ctx, f.regular.Id, 1, tariffVersion(3500)))

	second := f.send(t, transactionEvent(types.TransactionEventEnumTypeStarted, "tx-2", "RFID0001", 1, 1))
	assertTariff(t, f.regular, 2, tariffs.SourceConnector, second, "a new transaction gets the new version")
	selections := tariffStore.calls

	// the later events of the first transaction say where it runs too, also with another
	// token
	for _, req := range []*types.TransactionEventRequestJson{
		transactionEvent(types.TransactionEventEnumTypeUpdated, "tx-1", "", 1, 1),
		transactionEvent(types.TransactionEventEnumTypeUpdated, "tx-1", "EMP00001", 1, 2),
		transactionEvent(types.TransactionEventEnumTypeEnded, "tx-1", "RFID0001", 1, 1),
	} {
		req.SeqNo = 1
		first = f.send(t, req)
		assertTariff(t, f.regular, 1, tariffs.SourceConnector, first, "%s", req.EventType)
	}
	assert.Equal(t, selections, tariffStore.calls, "no tariff is selected for a transaction that has one")
}

// A transaction whose first event does not say where it runs gets its tariff with the
// first event that does.
func TestTheTariffIsChosenWithTheFirstEventThatNamesTheEvse(t *testing.T) {
	f := newTariffFixture(t)

	transaction := f.send(t, transactionEvent(types.TransactionEventEnumTypeStarted, "tx-1", "EMP00001"))
	assertNoTariff(t, transaction, "the Started event does not name the EVSE")
	transaction = f.send(t, transactionEvent(types.TransactionEventEnumTypeUpdated, "tx-1", ""))
	assertNoTariff(t, transaction)

	// the token is the one that the transaction started with
	transaction = f.send(t, transactionEvent(types.TransactionEventEnumTypeUpdated, "tx-1", "", 1, 1))
	assertTariff(t, f.employee, 1, tariffs.SourceTokenGroup, transaction)

	// only the Ended event names the EVSE
	f.send(t, transactionEvent(types.TransactionEventEnumTypeStarted, "tx-2", ""))
	ended := transactionEvent(types.TransactionEventEnumTypeEnded, "tx-2", "ADHOC001", 1, 1)
	ended.SeqNo = 1
	assertTariff(t, f.adhoc, 1, tariffs.SourceConnectorAdhoc, f.send(t, ended), "the token of the event that names the EVSE")
}

// The tariff must be valid at the time that the station gives the event.
func TestTheTariffIsSelectedForTheTimeOfTheEvent(t *testing.T) {
	ctx := context.Background()
	f := newTariffFixture(t)
	// the events are of 11:00, the time of the manager is 12:00
	setTariff := func(start, end time.Time) *store.Tariff {
		v := tariffVersion(2000)
		v.StartDateTime, v.EndDateTime = &start, &end
		tariff := f.tariff(t, "For a time", v)
		connector, err := f.engine.LookupAssetConnectorByOcpp(ctx, tariffStation, 2, 1)
		require.NoError(t, err)
		connector.TariffRef = &tariff.Id
		require.NoError(t, f.engine.UpdateAssetConnector(ctx, connector))
		return tariff
	}
	started := types.TransactionEventEnumTypeStarted

	untilHalfPastEleven := setTariff(tariffClockTime.Add(-2*time.Hour), tariffClockTime.Add(-30*time.Minute))
	assertTariff(t, untilHalfPastEleven, 1, tariffs.SourceConnector, f.send(t, transactionEvent(started, "tx-1", "RFID0001", 2, 1)))

	setTariff(tariffClockTime.Add(-30*time.Minute), tariffClockTime.Add(time.Hour))
	assertTariff(t, nil, 0, tariffs.SourceNone, f.send(t, transactionEvent(started, "tx-2", "RFID0001", 2, 1)), "a tariff from 11:30")

	// an event without a time that can be read is of the time of the manager
	unreadable := transactionEvent(started, "tx-3", "RFID0001", 2, 1)
	unreadable.Timestamp = "yesterday"
	sinceHalfPastEleven := setTariff(tariffClockTime.Add(-30*time.Minute), tariffClockTime.Add(time.Hour))
	assertTariff(t, sinceHalfPastEleven, 1, tariffs.SourceConnector, f.send(t, unreadable))
}

// downTariffStore is a tariff store that counts its lookups, which fail when it has an
// error.
type downTariffStore struct {
	store.TariffStore
	err   error
	calls int
}

func (s *downTariffStore) LookupTariff(ctx context.Context, tariffId string) (*store.Tariff, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.TariffStore.LookupTariff(ctx, tariffId)
}

// faultyEngine is a store that fails to read a transaction or to store its tariff.
type faultyEngine struct {
	store.Engine
	findErr, setTariffErr error
	setTariffCalls        int
}

func (s *faultyEngine) FindTransactionSummary(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}
	return s.Engine.FindTransactionSummary(ctx, chargeStationId, transactionId)
}

func (s *faultyEngine) SetTransactionTariff(ctx context.Context, chargeStationId, transactionId string, tariffId *string, version *int, source string) error {
	s.setTariffCalls++
	if s.setTariffErr != nil {
		return s.setTariffErr
	}
	return s.Engine.SetTransactionTariff(ctx, chargeStationId, transactionId, tariffId, version, source)
}

// Charging does not depend on a tariff: an event of a transaction whose tariff cannot be
// selected or stored is answered as ever.
func TestTransactionEventIsAnsweredWithoutATariffThatCannotBeChosen(t *testing.T) {
	errDown := errors.New("the store is down")
	started := transactionEvent(types.TransactionEventEnumTypeStarted, "tx-1", "RFID0001", 1, 1)
	want := &types.TransactionEventResponseJson{IdTokenInfo: &types.IdTokenInfoType{Status: types.AuthorizationStatusEnumTypeAccepted}}

	t.Run("the tariffs cannot be read", func(t *testing.T) {
		f := newTariffFixture(t)
		tariffStore := &downTariffStore{TariffStore: f.engine, err: errDown}
		f.handler.TariffSelector.Tariffs = tariffStore

		resp, err := f.handler.HandleCall(context.Background(), tariffStation, started)

		require.NoError(t, err)
		assert.Equal(t, want, resp)
		assert.Equal(t, 1, tariffStore.calls)
		transaction, err := f.engine.FindTransaction(context.Background(), tariffStation, "tx-1")
		require.NoError(t, err)
		require.NotNil(t, transaction, "the transaction is stored")
		assertNoTariff(t, transaction)

		// the next event that names the EVSE chooses the tariff, once the tariffs can be read
		f.handler.TariffSelector.Tariffs = f.engine
		assertTariff(t, f.regular, 1, tariffs.SourceConnector, f.send(t, transactionEvent(types.TransactionEventEnumTypeUpdated, "tx-1", "", 1, 1)))
	})

	for name, engine := range map[string]*faultyEngine{
		"the tariff cannot be stored":    {setTariffErr: errDown},
		"the backend keeps no tariff":    {setTariffErr: store.ErrNotImplemented},
		"the transaction cannot be read": {findErr: errDown},
	} {
		t.Run(name, func(t *testing.T) {
			f := newTariffFixture(t)
			engine.Engine = f.engine
			f.handler.Store = engine

			resp, err := f.handler.HandleCall(context.Background(), tariffStation, started)

			require.NoError(t, err)
			assert.Equal(t, want, resp)
			transaction, err := f.engine.FindTransaction(context.Background(), tariffStation, "tx-1")
			require.NoError(t, err)
			require.NotNil(t, transaction, "the transaction is stored")
			assertNoTariff(t, transaction)
			if engine.findErr != nil {
				assert.Zero(t, engine.setTariffCalls)
			}
		})
	}
}

// primaryReads is a store that counts the reads of a transaction for its tariff: all of
// them, and those inside a database transaction. It also counts the reads of a
// transaction with its meter values.
type primaryReads struct {
	store.Engine
	finds, inTx, withMeterValues int
}

func (s *primaryReads) FindTransactionSummary(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	s.finds++
	if eventstest.InTx(ctx) {
		s.inTx++
	}
	return s.Engine.FindTransactionSummary(ctx, chargeStationId, transactionId)
}

func (s *primaryReads) FindTransaction(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	s.withMeterValues++
	return s.Engine.FindTransaction(ctx, chargeStationId, transactionId)
}

// The transaction of an event is read in a database transaction: that read is on the
// primary, where the transaction is, also while a read replica is behind. It is read
// without its meter values, which are many by the end of a long session.
func TestTheTransactionIsReadOnThePrimaryForItsTariff(t *testing.T) {
	f := newTariffFixture(t)
	fixture := eventstest.New(f.engine, tariffOrgA, f.handler.Clock)
	backend := &primaryReads{Engine: fixture.Engine(f.engine)}
	f.handler.Store = backend

	_, err := f.handler.HandleCall(context.Background(), tariffStation, transactionEvent(types.TransactionEventEnumTypeStarted, "tx-1", "RFID0001", 1, 1))

	require.NoError(t, err)
	assert.Equal(t, 1, backend.finds)
	assert.Equal(t, 1, backend.inTx)
	transaction, err := f.engine.FindTransaction(context.Background(), tariffStation, "tx-1")
	require.NoError(t, err)
	assertTariff(t, f.regular, 1, tariffs.SourceConnector, transaction)

	// every later event that names the EVSE looks whether the transaction has its tariff
	for i := 0; i < 3; i++ {
		_, err = f.handler.HandleCall(context.Background(), tariffStation, transactionEvent(types.TransactionEventEnumTypeUpdated, "tx-1", "", 1, 1))
		require.NoError(t, err)
	}
	assert.Equal(t, 4, backend.finds)
	assert.Equal(t, 4, backend.inTx)
	assert.Zero(t, backend.withMeterValues, "no event of a running transaction reads its meter values")
}

// orgless is a store that does not keep the org of a transaction.
type orgless struct {
	store.Engine
	setTariffCalls int
}

func (s *orgless) FindTransactionSummary(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	transaction, err := s.Engine.FindTransactionSummary(ctx, chargeStationId, transactionId)
	if transaction != nil {
		withoutOrg := *transaction
		withoutOrg.OrgId = ""
		transaction = &withoutOrg
	}
	return transaction, err
}

func (s *orgless) SetTransactionTariff(context.Context, string, string, *string, *int, string) error {
	s.setTariffCalls++
	return store.ErrNotImplemented
}

// The tariff is a tariff of the org of the transaction.
func TestTheTariffIsOfTheOrgOfTheTransaction(t *testing.T) {
	started := types.TransactionEventEnumTypeStarted

	t.Run("a transaction of the legacy org belongs to the org of its charge station", func(t *testing.T) {
		f := newTariffFixture(t)
		// a charge station that is not registered: the store gives its transactions the
		// legacy org, and the resolver gives the station the default org
		transaction := f.sendAt(t, "LEGACY01", transactionEvent(started, "tx-1", "EMP00001", 1, 1))
		assert.Equal(t, store.LegacyOrgId, transaction.OrgId)
		assertTariff(t, f.employee, 1, tariffs.SourceTokenGroup, transaction)
	})

	t.Run("a transaction stays with its org when its charge station moves", func(t *testing.T) {
		ctx := context.Background()
		f := newTariffFixture(t)
		const otherOrg = "0193b2c4-0000-7000-8000-0000000000b2"
		// the transaction starts without its EVSE, then the charge station moves to
		// another org, where the token is in a group with another tariff
		f.send(t, transactionEvent(started, "tx-1", "EMP00001"))
		ofOther := &store.Tariff{OrgId: otherOrg, PartyRef: "party-b", Name: "Of the other org"}
		require.NoError(t, f.engine.CreateTariff(ctx, ofOther, tariffVersion(9900)))
		group := &store.TokenGroup{OrgId: otherOrg, Name: "Employees", OverrideTariffRef: &ofOther.Id}
		require.NoError(t, f.engine.CreateTokenGroup(ctx, group))
		require.NoError(t, f.engine.SetToken(ctx, &store.Token{
			OrgId: otherOrg, CountryCode: "ES", PartyId: "OTH", Type: "RFID", Uid: "EMP00001", ContractId: "ESOTHC1", Issuer: "Issuer", Valid: true,
		}))
		require.NoError(t, f.engine.SetTokenGroup(ctx, otherOrg, "EMP00001", &group.Id))
		require.NoError(t, f.engine.SetChargeStationAuth(ctx, tariffStation, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: otherOrg}))

		transaction := f.send(t, transactionEvent(types.TransactionEventEnumTypeUpdated, "tx-1", "", 1, 1))

		assert.Equal(t, tariffOrgA, transaction.OrgId)
		assertTariff(t, f.employee, 1, tariffs.SourceTokenGroup, transaction, "the tariff of the org that the transaction started in")
	})

	t.Run("a backend that keeps no org with a transaction has no tariff for it", func(t *testing.T) {
		f := newTariffFixture(t)
		backend := &orgless{Engine: f.engine}
		f.handler.Store = backend
		tariffStore := &downTariffStore{TariffStore: f.engine, err: errors.New("not asked")}
		f.handler.TariffSelector.Tariffs = tariffStore

		_, err := f.handler.HandleCall(context.Background(), tariffStation, transactionEvent(started, "tx-1", "RFID0001", 1, 1))

		require.NoError(t, err)
		assert.Zero(t, tariffStore.calls, "nothing is selected")
		assert.Zero(t, backend.setTariffCalls, "and nothing stored")
	})
}
