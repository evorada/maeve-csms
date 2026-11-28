// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clockTest "k8s.io/utils/clock/testing"

	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/money"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	types201 "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/services"
	"github.com/thoughtworks/maeve-csms/manager/sessions"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tariffs"
)

// The tests below rate the transactions that the OCPP handlers record, with the tables of
// PostgreSQL: the session service reads the transaction, its meter values, its tariff
// version and the time zone of its location, and writes the cost.

func newTestSessions(clk *clockTest.FakePassiveClock) *sessions.Service {
	return &sessions.Service{
		Analyzer:     newTestAnalyzer(clk),
		Transactions: testStore, MeterValues: testStore, Tariffs: testStore, Assets: testStore, Registry: testStore, Tx: testStore,
		Clock: clk,
	}
}

func validTestVersion(t *testing.T, v *store.TariffVersion) *store.TariffVersion {
	t.Helper()
	require.Empty(t, tariffs.Validate(v))
	return v
}

// An OCPP 1.6 transaction is rated with its meter values and with its stop: the cost so
// far and the final cost are in its session events and in its row. The tariff charges
// parking between 08:00 and 20:00 from Monday to Friday, on the clock of the location of
// the EVSE, which is in Europe/Madrid.
func TestRating_StopTransaction(t *testing.T) {
	ctx := context.Background()
	f := newTariffSelectionFixture(t)
	require.NoError(t, testStore.SetChargeStationRuntimeDetails(ctx, tariffSelectionStation, &store.ChargeStationRuntimeDetails{OcppVersion: "1.6"}))
	// energy at 0.30 per kWh; parking at 0.20 per 10 minutes, the first 15 minutes free,
	// from Monday to Friday between 08:00 and 20:00; VAT 21%
	vat := money.Percent(2100)
	require.NoError(t, testStore.AddTariffVersion(ctx, f.regular.Id, 1, validTestVersion(t, &store.TariffVersion{
		Currency: "EUR",
		Elements: []store.TariffElement{
			{PriceComponents: []store.PriceComponent{{Type: store.PriceComponentEnergy, Price: 3000, Vat: &vat, StepSize: 1}}},
			{
				PriceComponents: []store.PriceComponent{{
					Type: store.PriceComponentParkingTime, Vat: &vat, StepSize: 600, GracePeriodS: ptr(int32(900)), PricePerStep: ptr(money.Amount(2000)),
				}},
				Restrictions: &store.TariffRestrictions{
					StartTime: ptr("08:00"), EndTime: ptr("20:00"),
					DayOfWeek: []store.DayOfWeek{store.Monday, store.Tuesday, store.Wednesday, store.Thursday, store.Friday},
				},
			},
		},
	})))

	// a Friday: the session starts at 18:30 in Madrid
	start := time.Date(2026, 10, 2, 16, 30, 0, 0, time.UTC)
	at := func(minute int) time.Time { return start.Add(time.Duration(minute) * time.Minute) }
	clk := clockTest.NewFakePassiveClock(start)
	fixture := eventstest.New(testStore, testOrgA, clk)
	fixture.Events.Emitter = &events.OutboxEmitter{Store: testStore, Orgs: f.orgs, Clock: clk, Product: events.Product}
	service := newTestSessions(clk)
	startHandler := ocpp16.StartTransactionHandler{
		Clock: clk, TokenStore: testStore, RegistryStore: testStore, OrgResolver: f.orgs, TransactionStore: testStore, ReservationStore: testStore,
		Tx: fixture.Tx, Events: fixture.Events, TariffSelector: f.selector,
	}
	meterValuesHandler := ocpp16.MeterValuesHandler{
		TransactionStore: testStore, MeterValuesStore: testStore, Tx: fixture.Tx, Events: fixture.Events, Sessions: service,
	}
	stopHandler := ocpp16.StopTransactionHandler{
		Clock: clk, TokenStore: testStore, RegistryStore: testStore, OrgResolver: f.orgs, TransactionStore: testStore,
		Tx: fixture.Tx, Events: fixture.Events, Analyzer: service.Analyzer, Sessions: service,
	}
	// the station sends each message at the time that the message names
	begin := func() int {
		t.Helper()
		clk.SetTime(at(0))
		resp, err := startHandler.HandleCall(ctx, tariffSelectionStation, &types.StartTransactionJson{
			ConnectorId: 1, IdTag: "RFID0001", MeterStart: 200_000, Timestamp: at(0).Format(time.RFC3339),
		})
		require.NoError(t, err)
		return resp.(*types.StartTransactionResponseJson).TransactionId
	}
	reading := func(transactionId, minute, registerWh int) *events.SessionCost {
		t.Helper()
		clk.SetTime(at(minute))
		fixture.Take(t)
		wh := types.MeterValuesJsonMeterValueElemSampledValueElemUnitWh
		_, err := meterValuesHandler.HandleCall(ctx, tariffSelectionStation, &types.MeterValuesJson{
			ConnectorId: 1, TransactionId: &transactionId,
			MeterValue: []types.MeterValuesJsonMeterValueElem{{
				Timestamp:    at(minute).Format(time.RFC3339),
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{{Value: strconv.Itoa(registerWh), Unit: &wh}},
			}},
		})
		require.NoError(t, err)
		evs := fixture.Take(t)
		require.Equal(t, []string{events.TypeSessionUpdated}, eventstest.Types(evs))
		require.Equal(t, testOrgA, evs[0].OrgId)
		return eventstest.Data[events.SessionUpdatedData](t, evs[0]).Cost
	}
	stop := func(transactionId, minute, meterStop int) *events.SessionCost {
		t.Helper()
		clk.SetTime(at(minute))
		fixture.Take(t)
		_, err := stopHandler.HandleCall(ctx, tariffSelectionStation, &types.StopTransactionJson{
			TransactionId: transactionId, MeterStop: meterStop, Timestamp: at(minute).Format(time.RFC3339),
		})
		require.NoError(t, err)
		evs := fixture.Take(t)
		require.Equal(t, []string{events.TypeSessionCompleted}, eventstest.Types(evs))
		require.Equal(t, testOrgA, evs[0].OrgId)
		return eventstest.Data[events.SessionCompletedData](t, evs[0]).Cost
	}
	cost := func(currency string, excl, incl money.Amount, final bool, energyWh, chargingS, parkingS int64) *events.SessionCost {
		return &events.SessionCost{
			Currency: currency, ExclVat: excl, InclVat: incl, Final: final, TariffId: &f.regular.Id, TariffVersion: ptr(2),
			EnergyWh: energyWh, ChargingS: chargingS, ParkingS: parkingS,
		}
	}

	transactionId := begin()
	id := ocpp16.ConvertToUUID(transactionId)
	assert.Equal(t, costColumns{}, readCostColumns(t, tariffSelectionStation, id), "a transaction has no cost when it starts")

	// 3.7 kWh after half an hour: 1.11, and 1.3431 with VAT
	assert.Equal(t, cost("EUR", 1_1100, 1_3400, false, 3700, 1800, 0), reading(transactionId, 30, 203_700))
	got := readCostColumns(t, tariffSelectionStation, id)
	assert.Equal(t, ptr("EUR"), got.currency)
	assert.Equal(t, ptr("1.1100"), got.exclVat)
	assert.Equal(t, ptr("1.3400"), got.inclVat)
	assert.Equal(t, ptr("1.3400"), got.lastCost)
	assert.Equal(t, ptr(false), got.final)
	require.NotNil(t, got.ratedAt)
	assert.True(t, at(30).Equal(*got.ratedAt), "%s", got.ratedAt)

	// 7.4 kWh after an hour, and nothing more after two: idle since 19:30 in Madrid. The
	// grace period ends at 19:45 and the window at 20:00: 15 minutes of parking, 2 steps
	assert.Equal(t, cost("EUR", 2_2200, 2_6900, false, 7400, 3600, 0), reading(transactionId, 60, 207_400))
	assert.Equal(t, cost("EUR", 2_6200, 3_1700, false, 7400, 3600, 3600), reading(transactionId, 120, 207_400))

	// the stop, half an hour later: the parking after 20:00 is free
	assert.Equal(t, cost("EUR", 2_6200, 3_1700, true, 7400, 3600, 5400), stop(transactionId, 150, 207_400))
	got = readCostColumns(t, tariffSelectionStation, id)
	assert.Equal(t, ptr("2.6200"), got.exclVat)
	assert.Equal(t, ptr("3.1700"), got.inclVat)
	assert.Equal(t, ptr("3.1700"), got.lastCost)
	assert.Equal(t, ptr(true), got.final)
	require.NotNil(t, got.ratedAt)
	assert.True(t, at(150).Equal(*got.ratedAt), "%s", got.ratedAt)
	stored, err := testStore.FindTransaction(ctx, tariffSelectionStation, id)
	require.NoError(t, err)
	assert.Equal(t, &store.TransactionCost{Currency: "EUR", Total: money.Price{ExclVat: 2_6200, InclVat: 3_1700}, Final: true, RatedAt: at(150)}, stored.Cost)
	assert.Equal(t, ptr(3.17), stored.LastCost)
	assert.Equal(t, testOrgA, stored.OrgId)
	require.NotNil(t, stored.Analysis)
	assert.Equal(t, int64(5400), stored.Analysis.IdleS)
	assert.Zero(t, fixture.Events.OutsideTx())
	requireNoOpenTransaction(t)

	// a meter value that comes after the stop does not touch the final cost
	assert.Nil(t, reading(transactionId, 151, 207_400))
	assert.Equal(t, got, readCostColumns(t, tariffSelectionStation, id))

	// a rating that fails leaves the meter values and the stop as they are stored
	second := begin()
	secondId := ocpp16.ConvertToUUID(second)
	assert.Equal(t, cost("EUR", 1_1100, 1_3400, false, 3700, 1800, 0), reading(second, 30, 203_700))
	service.Tariffs = failingTariffVersions{TariffStore: testStore, err: errors.New("the tariffs cannot be read")}
	assert.Nil(t, reading(second, 60, 207_400))
	assert.Nil(t, stop(second, 90, 207_400))
	stored, err = testStore.FindTransaction(ctx, tariffSelectionStation, secondId)
	require.NoError(t, err)
	assert.Equal(t, ptr(at(90)), stored.StoppedAt)
	assert.Equal(t, ptr(int64(7400)), stored.EnergyWh)
	require.NotNil(t, stored.Cost)
	assert.Equal(t, money.Price{ExclVat: 1_1100, InclVat: 1_3400}, stored.Cost.Total, "the last cost so far that could be worked out")
	assert.False(t, stored.Cost.Final)
	result, err := testStore.QueryMeterValues(ctx, store.MeterValuesFilter{ChargeStationId: tariffSelectionStation, TransactionId: &secondId, Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, 2, result.Total)
	requireNoOpenTransaction(t)

	// it can be rated later: 7.4 kWh, and idle from 19:30 to the stop at 20:00 in Madrid,
	// of which the 15 minutes after the grace period are charged
	service.Tariffs = testStore
	final, err := service.End(ctx, tariffSelectionStation, secondId)
	require.NoError(t, err)
	require.NotNil(t, final)
	assert.Equal(t, money.Price{ExclVat: 2_6200, InclVat: 3_1700}, final.Breakdown.Total)
	assert.Equal(t, ptr(true), readCostColumns(t, tariffSelectionStation, secondId).final)
	requireNoOpenTransaction(t)
}

// An OCPP 2.0.1 transaction is rated with its Updated events and with its Ended event,
// whose answer has the total cost with VAT.
func TestRating_TransactionEvent(t *testing.T) {
	ctx := context.Background()
	f := newTariffSelectionFixture(t)
	// energy at 0.30 per kWh; parking at 0.50 per 15 minutes, the first 30 minutes free;
	// VAT 21%
	vat := money.Percent(2100)
	require.NoError(t, testStore.AddTariffVersion(ctx, f.regular.Id, 1, validTestVersion(t, &store.TariffVersion{
		Currency: "EUR",
		Elements: []store.TariffElement{{PriceComponents: []store.PriceComponent{
			{Type: store.PriceComponentEnergy, Price: 3000, Vat: &vat, StepSize: 1},
			{Type: store.PriceComponentParkingTime, Vat: &vat, StepSize: 900, GracePeriodS: ptr(int32(1800)), PricePerStep: ptr(money.Amount(5000))},
		}}},
	})))

	clk := clockTest.NewFakePassiveClock(analysisTestStart)
	service := newTestSessions(clk)
	handler := ocpp201.TransactionEventHandler{
		Store:            testStore,
		TokenAuthService: &services.OcppTokenAuthService{Clock: clk, TokenStore: testStore, OrgResolver: f.orgs, Registry: testStore},
		TariffSelector:   f.selector,
		OrgResolver:      f.orgs,
		Clock:            clk,
		Analyzer:         service.Analyzer,
		Sessions:         service,
	}
	// event sends an event of a transaction at EVSE 1 or 2, with a reading of the register
	// in Wh at the minute of the transaction, at the time of the event
	event := func(eventType types201.TransactionEventEnumType, transactionId string, evse, seqNo, minute int, registerWh float64) *float64 {
		t.Helper()
		clk.SetTime(analysisTestStart.Add(time.Duration(minute) * time.Minute))
		connector := 1
		sampled := types201.SampledValueType{Value: registerWh}
		switch eventType {
		case types201.TransactionEventEnumTypeStarted:
			sampled.Context = ptr(types201.ReadingContextEnumTypeTransactionBegin)
			sampled.Measurand = ptr(types201.MeasurandEnumTypeEnergyActiveImportRegister)
		case types201.TransactionEventEnumTypeEnded:
			sampled.Context = ptr(types201.ReadingContextEnumTypeTransactionEnd)
			sampled.Measurand = ptr(types201.MeasurandEnumTypeEnergyActiveImportRegister)
			sampled.Location = ptr(types201.LocationEnumTypeOutlet)
		}
		resp, err := handler.HandleCall(ctx, tariffSelectionStation, &types201.TransactionEventRequestJson{
			EventType: eventType, TriggerReason: types201.TriggerReasonEnumTypeMeterValuePeriodic, Timestamp: analysisTestMinute(minute),
			SeqNo: seqNo, Evse: &types201.EVSEType{Id: evse, ConnectorId: &connector},
			TransactionInfo: types201.TransactionType{TransactionId: transactionId},
			MeterValue:      []types201.MeterValueType{{Timestamp: analysisTestMinute(minute), SampledValue: []types201.SampledValueType{sampled}}},
		})
		require.NoError(t, err)
		return resp.(*types201.TransactionEventResponseJson).TotalCost
	}

	// 11 kWh in the first hour, then idle for 100 minutes: 30 are free, 70 are charged as
	// 5 steps
	assert.Nil(t, event(types201.TransactionEventEnumTypeStarted, "tx-1", 1, 0, 0, 100_000))
	assert.Equal(t, costColumns{}, readCostColumns(t, tariffSelectionStation, "tx-1"), "a transaction has no cost when it starts")
	assert.Nil(t, event(types201.TransactionEventEnumTypeUpdated, "tx-1", 1, 1, 30, 105_500))
	got := readCostColumns(t, tariffSelectionStation, "tx-1")
	// 5.5 kWh: 1.65, and 1.9965 with VAT
	assert.Equal(t, ptr("1.6500"), got.exclVat)
	assert.Equal(t, ptr("2.0000"), got.inclVat)
	assert.Equal(t, ptr(false), got.final)
	assert.Nil(t, event(types201.TransactionEventEnumTypeUpdated, "tx-1", 1, 2, 60, 111_000))
	assert.Nil(t, event(types201.TransactionEventEnumTypeUpdated, "tx-1", 1, 3, 90, 111_000))
	assert.Nil(t, event(types201.TransactionEventEnumTypeUpdated, "tx-1", 1, 4, 105, 111_000))
	got = readCostColumns(t, tariffSelectionStation, "tx-1")
	// idle since 10:00, at 10:45: one step of parking after the grace period
	assert.Equal(t, ptr("3.8000"), got.exclVat)
	assert.Equal(t, ptr("4.6000"), got.inclVat)

	assert.Equal(t, ptr(7.02), event(types201.TransactionEventEnumTypeEnded, "tx-1", 1, 5, 160, 111_000))

	got = readCostColumns(t, tariffSelectionStation, "tx-1")
	assert.Equal(t, ptr("EUR"), got.currency)
	assert.Equal(t, ptr("5.8000"), got.exclVat)
	assert.Equal(t, ptr("7.0200"), got.inclVat)
	assert.Equal(t, ptr("7.0200"), got.lastCost)
	assert.Equal(t, ptr(true), got.final)
	tariffId, version, source := tariffColumns(t, "tx-1")
	assert.Equal(t, &f.regular.Id, tariffId)
	assert.Equal(t, ptr(2), version)
	assert.Equal(t, ptr("connector"), source)
	requireNoOpenTransaction(t)

	// a transaction at an EVSE without a tariff of its own has the default tariff of the
	// location, 0.30 per kWh with 21% VAT: 11 kWh are 3.30, and 3.993 with VAT
	assert.Nil(t, event(types201.TransactionEventEnumTypeStarted, "tx-2", 2, 0, 0, 100_000))
	assert.Equal(t, ptr(3.99), event(types201.TransactionEventEnumTypeEnded, "tx-2", 2, 1, 60, 111_000))
	_, _, source = tariffColumns(t, "tx-2")
	assert.Equal(t, ptr("location"), source)

	// a rating that fails leaves the end of the transaction as it is stored, and the
	// answer has no total cost
	assert.Nil(t, event(types201.TransactionEventEnumTypeStarted, "tx-3", 1, 0, 0, 100_000))
	service.Tariffs = failingTariffVersions{TariffStore: testStore, err: errors.New("the tariffs cannot be read")}
	assert.Nil(t, event(types201.TransactionEventEnumTypeUpdated, "tx-3", 1, 1, 30, 105_500))
	assert.Nil(t, event(types201.TransactionEventEnumTypeEnded, "tx-3", 1, 2, 60, 111_000))
	assert.Equal(t, costColumns{}, readCostColumns(t, tariffSelectionStation, "tx-3"))
	stored, err := testStore.FindTransaction(ctx, tariffSelectionStation, "tx-3")
	require.NoError(t, err)
	assert.Equal(t, ptr(analysisTestStart.Add(time.Hour)), stored.StoppedAt)
	assert.Len(t, stored.MeterValues, 3)
	requireNoOpenTransaction(t)
}
