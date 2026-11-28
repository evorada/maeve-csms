// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clockTest "k8s.io/utils/clock/testing"

	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	msghandlers "github.com/thoughtworks/maeve-csms/manager/handlers"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/rating"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// The analysis of a transaction that ends: how long it charged and how long it was idle,
// stored with the transaction, and never a reason to fail the event.

// analysisSessionStart is when the transactions of these tests start. The events of a
// transaction are given by the minute after it.
var analysisSessionStart = time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)

// analysisEvent is an event of a transaction with a reading of the energy register in Wh
// at the minute of the session. evse is the EVSE id and, after it, the connector id: none
// says nothing about the EVSE.
func analysisEvent(eventType types.TransactionEventEnumType, transactionId string, seqNo int, minute int, registerWh float64, evse ...int) *types.TransactionEventRequestJson {
	req := transactionEvent(eventType, transactionId, "", evse...)
	at := analysisSessionStart.Add(time.Duration(minute) * time.Minute).Format(time.RFC3339)
	req.SeqNo = seqNo
	req.Timestamp = at
	// a charge station may send only the value: it is the energy register at the outlet
	sampled := types.SampledValueType{Value: registerWh}
	if eventType == types.TransactionEventEnumTypeEnded {
		sampled.Context = makePtr(types.ReadingContextEnumTypeTransactionEnd)
		sampled.Measurand = makePtr(types.MeasurandEnumTypeEnergyActiveImportRegister)
		sampled.Location = makePtr(types.LocationEnumTypeOutlet)
	}
	req.MeterValue = []types.MeterValueType{{Timestamp: at, SampledValue: []types.SampledValueType{sampled}}}
	return req
}

// newAnalysisFixture is the tariff fixture with an Analyzer on its store.
func newAnalysisFixture(t *testing.T) *tariffFixture {
	t.Helper()
	f := newTariffFixture(t)
	f.handler.Analyzer = &rating.Analyzer{
		Transactions: f.engine,
		MeterValues:  f.engine,
		Assets:       f.engine,
		Registry:     f.engine,
		Tariffs:      f.engine,
		Defaults:     rating.DefaultIdleSettings(),
		Clock:        f.handler.Clock,
	}
	return f
}

// chargeAndPark sends the events of a transaction that charges 11 kWh in its first hour
// and stays plugged in for an hour and a half, up to the Ended event, which it returns.
func chargeAndPark(t *testing.T, f *tariffFixture, transactionId string) *types.TransactionEventRequestJson {
	t.Helper()
	f.send(t, analysisEvent(types.TransactionEventEnumTypeStarted, transactionId, 0, 0, 100_000, 1, 1))
	f.send(t, analysisEvent(types.TransactionEventEnumTypeUpdated, transactionId, 1, 30, 105_500, 1, 1))
	f.send(t, analysisEvent(types.TransactionEventEnumTypeUpdated, transactionId, 2, 60, 111_000, 1, 1))
	f.send(t, analysisEvent(types.TransactionEventEnumTypeUpdated, transactionId, 3, 120, 111_000, 1, 1))
	return analysisEvent(types.TransactionEventEnumTypeEnded, transactionId, 4, 150, 111_000, 1, 1)
}

func TestEndedEventAnalysesTheTransaction(t *testing.T) {
	f := newAnalysisFixture(t)
	ended := chargeAndPark(t, f, "tx-1")
	running, err := f.engine.FindTransaction(context.Background(), tariffStation, "tx-1")
	require.NoError(t, err)
	assert.Nil(t, running.Analysis, "a transaction that runs has no analysis")

	transaction := f.send(t, ended)

	assert.Equal(t, &store.TransactionAnalysis{
		IdleStartedAt: makePtr(analysisSessionStart.Add(time.Hour)),
		IdleS:         5400,
		ChargingS:     3600,
		AveragePowerW: makePtr(11_000.0),
		AnalysedAt:    tariffClockTime,
	}, transaction.Analysis)
	assertTariff(t, f.regular, 1, "connector", transaction)
}

func TestTransactionEventWithoutAnAnalyzerAnalysesNothing(t *testing.T) {
	f := newTariffFixture(t)

	transaction := f.send(t, chargeAndPark(t, f, "tx-1"))

	assert.Nil(t, transaction.Analysis)
}

// The analysis uses the idle settings of the tariff of the transaction, also when the
// transaction gets its tariff with the event that ends it.
func TestTheAnalysisComesAfterTheTariff(t *testing.T) {
	ctx := context.Background()
	f := newAnalysisFixture(t)
	// the tariff of the connector says that a transaction charges from 1.5 kW
	v := tariffVersion(3000)
	v.IdleDetection = &store.IdleDetection{MinChargingPowerW: makePtr(1500.0)}
	require.NoError(t, f.engine.AddTariffVersion(ctx, f.regular.Id, 1, v))

	// an hour at 11 kW and an hour at 1 kW; only the Ended event names the EVSE
	f.send(t, analysisEvent(types.TransactionEventEnumTypeStarted, "tx-1", 0, 0, 100_000))
	f.send(t, analysisEvent(types.TransactionEventEnumTypeUpdated, "tx-1", 1, 60, 111_000))
	transaction := f.send(t, analysisEvent(types.TransactionEventEnumTypeEnded, "tx-1", 2, 120, 112_000, 1, 1))

	assertTariff(t, f.regular, 2, "connector", transaction)
	require.NotNil(t, transaction.Analysis)
	assert.Equal(t, makePtr(analysisSessionStart.Add(time.Hour)), transaction.Analysis.IdleStartedAt, "1 kW is below the 1.5 kW of the tariff")
	assert.Equal(t, int64(3600), transaction.Analysis.IdleS)

	// with the settings of the manager the same session charged until its end
	f.send(t, analysisEvent(types.TransactionEventEnumTypeStarted, "tx-2", 0, 0, 100_000))
	f.send(t, analysisEvent(types.TransactionEventEnumTypeUpdated, "tx-2", 1, 60, 111_000))
	transaction = f.send(t, analysisEvent(types.TransactionEventEnumTypeEnded, "tx-2", 2, 120, 112_000, 2, 1))
	assertTariff(t, nil, 0, "none", transaction)
	require.NotNil(t, transaction.Analysis)
	assert.Equal(t, int64(0), transaction.Analysis.IdleS)
}

// analysisBackend is the store of an Analyzer. It counts the reads of a transaction with
// its meter values, notes what the last of them saw, and fails what it is told to fail.
type analysisBackend struct {
	store.Engine
	findErr, setErr error
	finds, sets     int
	// of the last read: whether it was in a database transaction, the deadline of its
	// ctx, and whether the transaction that it read had ended
	inTx        bool
	hasDeadline bool
	deadlineIn  time.Duration
	ended       bool
}

func (s *analysisBackend) FindTransaction(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	s.finds++
	s.inTx = eventstest.InTx(ctx)
	var deadline time.Time
	if deadline, s.hasDeadline = ctx.Deadline(); s.hasDeadline {
		s.deadlineIn = time.Until(deadline)
	}
	if s.findErr != nil {
		return nil, s.findErr
	}
	transaction, err := s.Engine.FindTransaction(ctx, chargeStationId, transactionId)
	s.ended = transaction != nil && transaction.EndedSeqNo > 0
	return transaction, err
}

func (s *analysisBackend) SetTransactionAnalysis(ctx context.Context, chargeStationId, transactionId string, analysis store.TransactionAnalysis) error {
	s.sets++
	if s.setErr != nil {
		return s.setErr
	}
	return s.Engine.SetTransactionAnalysis(ctx, chargeStationId, transactionId, analysis)
}

// Only the event that ends a transaction analyses it: the events of a running transaction
// do not read its meter values. The analysis reads the ended transaction in a database
// transaction (on the primary, where the end is), with a deadline.
func TestOnlyTheEndedEventAnalyses(t *testing.T) {
	f := newAnalysisFixture(t)
	fixture := eventstest.New(f.engine, tariffOrgA, clockTest.NewFakePassiveClock(tariffClockTime))
	handlerStore := &primaryReads{Engine: fixture.Engine(f.engine)}
	f.handler.Store = handlerStore
	backend := &analysisBackend{Engine: f.engine}
	f.handler.Analyzer.Transactions = backend

	ended := chargeAndPark(t, f, "tx-1")
	assert.Zero(t, backend.finds, "no analysis while the transaction runs")
	assert.Zero(t, handlerStore.withMeterValues, "and no read of its meter values")

	f.send(t, ended)

	assert.Equal(t, 1, backend.finds)
	assert.Equal(t, 1, backend.sets)
	assert.True(t, backend.ended, "the analysis reads the transaction after its end is stored")
	assert.True(t, backend.inTx)
	require.True(t, backend.hasDeadline, "the answer does not wait long for the analysis")
	assert.LessOrEqual(t, backend.deadlineIn, rating.AnalysisTimeout)
	assert.Positive(t, backend.deadlineIn)
}

// An event that ends a transaction is answered as ever when its analysis fails.
func TestEndedEventIsAnsweredWhenTheAnalysisFails(t *testing.T) {
	errDown := errors.New("the store is down")

	// the answer of the handler without an Analyzer, with the total cost of the session
	plain := rated(newTariffFixture(t))
	want, err := plain.handler.HandleCall(context.Background(), tariffStation, chargeAndPark(t, plain, "tx-1"))
	require.NoError(t, err)
	require.Equal(t, makePtr(3.3), want.(*types.TransactionEventResponseJson).TotalCost)

	for name, backend := range map[string]*analysisBackend{
		"the transaction cannot be read": {findErr: errDown},
		"the analysis cannot be stored":  {setErr: errDown},
		"the backend keeps no analysis":  {setErr: fmt.Errorf("SetTransactionAnalysis: %w", store.ErrNotImplemented)},
	} {
		t.Run(name, func(t *testing.T) {
			f := newCostFixture(t)
			backend.Engine = f.engine
			f.handler.Analyzer.Transactions = backend
			ended := chargeAndPark(t, f, "tx-1")

			resp, err := f.handler.HandleCall(context.Background(), tariffStation, ended)

			require.NoError(t, err)
			assert.Equal(t, want, resp)
			assert.Equal(t, 1, backend.finds, "the analysis was tried")
			transaction, err := f.engine.FindTransaction(context.Background(), tariffStation, "tx-1")
			require.NoError(t, err)
			require.NotNil(t, transaction)
			assert.Equal(t, 4, transaction.EndedSeqNo, "the end of the transaction is stored")
			assert.Nil(t, transaction.Analysis)
		})
	}
}

// The router gives its Analyzer to the handler of the transaction events.
func TestNewRouterGivesTheAnalyzerToTheTransactionEventHandler(t *testing.T) {
	clk := clockTest.NewFakePassiveClock(tariffClockTime)
	engine := inmemory.NewStore(clk)
	analyzer := &rating.Analyzer{Transactions: engine, Defaults: rating.DefaultIdleSettings()}

	router, ok := handlers.NewRouter(&fakeEmitter{}, clk, engine, &tenancy.Resolver{Stations: engine}, nil,
		&fakeCertValidationService{}, &fakeChargeStationCertProvider{}, &fakeContractCertProvider{}, 5*time.Minute, schemas.OcppSchemas,
		nil, nil, analyzer, nil).(*msghandlers.Router)
	require.True(t, ok)

	handler, ok := router.CallRoutes["TransactionEvent"].Handler.(handlers.TransactionEventHandler)
	require.True(t, ok)
	assert.Same(t, analyzer, handler.Analyzer)
}
