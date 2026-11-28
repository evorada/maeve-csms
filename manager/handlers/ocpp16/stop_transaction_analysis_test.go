// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	msghandlers "github.com/thoughtworks/maeve-csms/manager/handlers"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/rating"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// The analysis of a transaction that stops: how long it charged and how long it was idle,
// stored with the transaction, and never a reason to fail the stop.

// analysisSessionStart is when the transactions of these tests start. What happens in a
// transaction is given by the minute after it.
var analysisSessionStart = time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)

func analysisMinute(m int) string {
	return analysisSessionStart.Add(time.Duration(m) * time.Minute).Format(time.RFC3339)
}

// analysisFixture is the tariff fixture with the handlers of the messages of a running
// transaction and of its stop, which has an Analyzer on the store.
type analysisFixture struct {
	*tariffFixture
	meterValues handlers.MeterValuesHandler
	stop        handlers.StopTransactionHandler
}

func newAnalysisFixture(t *testing.T) *analysisFixture {
	t.Helper()
	f := &analysisFixture{tariffFixture: newTariffFixture(t)}
	f.meterValues = handlers.MeterValuesHandler{
		TransactionStore: f.engine, MeterValuesStore: f.engine, Tx: f.events.Tx, Events: f.events.Events,
	}
	f.stop = handlers.StopTransactionHandler{
		Clock: f.start.Clock, TokenStore: f.engine, RegistryStore: f.engine, OrgResolver: f.start.OrgResolver,
		TransactionStore: f.engine, Tx: f.events.Tx, Events: f.events.Events,
		Analyzer: &rating.Analyzer{
			Transactions: f.engine,
			MeterValues:  f.engine,
			Assets:       f.engine,
			Registry:     f.engine,
			Tariffs:      f.engine,
			Defaults:     rating.DefaultIdleSettings(),
			Clock:        f.start.Clock,
		},
	}
	return f
}

// charge starts a transaction at the connector with a register of 100 kWh and sends its
// meter readings: the minute of the transaction and the register in Wh, in pairs. It
// returns the OCPP 1.6 id of the transaction.
func (f *analysisFixture) charge(t *testing.T, connectorId int, minutesAndWh ...int) int {
	t.Helper()
	resp := startTransaction(t, f.start, sessionOrgStation, &types.StartTransactionJson{
		ConnectorId: connectorId, IdTag: "RFID0001", MeterStart: 100_000, Timestamp: analysisMinute(0),
	})
	require.Positive(t, resp.TransactionId)
	for i := 0; i+1 < len(minutesAndWh); i += 2 {
		_, err := f.meterValues.HandleCall(context.Background(), sessionOrgStation, &types.MeterValuesJson{
			ConnectorId:   connectorId,
			TransactionId: &resp.TransactionId,
			MeterValue: []types.MeterValuesJsonMeterValueElem{{
				Timestamp: analysisMinute(minutesAndWh[i]),
				// only the value: the energy register at the outlet in Wh
				SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{{Value: strconv.Itoa(minutesAndWh[i+1])}},
			}},
		})
		require.NoError(t, err)
	}
	return resp.TransactionId
}

func analysisStopRequest(transactionId, minute, meterStop int) *types.StopTransactionJson {
	return &types.StopTransactionJson{TransactionId: transactionId, MeterStop: meterStop, Timestamp: analysisMinute(minute)}
}

// stopAt stops the transaction and returns the stored transaction.
func (f *analysisFixture) stopAt(t *testing.T, transactionId, minute, meterStop int) *store.Transaction {
	t.Helper()
	_, err := f.stop.HandleCall(context.Background(), sessionOrgStation, analysisStopRequest(transactionId, minute, meterStop))
	require.NoError(t, err)
	return f.stored(t, transactionId)
}

func (f *analysisFixture) stored(t *testing.T, transactionId int) *store.Transaction {
	t.Helper()
	transaction, err := f.engine.FindTransaction(context.Background(), sessionOrgStation, handlers.ConvertToUUID(transactionId))
	require.NoError(t, err)
	require.NotNil(t, transaction)
	return transaction
}

func TestStopTransactionAnalysesTheTransaction(t *testing.T) {
	f := newAnalysisFixture(t)
	// 11 kWh in the first hour, then plugged in for an hour and a half
	transactionId := f.charge(t, 1, 30, 105_500, 60, 111_000, 120, 111_000)
	assert.Nil(t, f.stored(t, transactionId).Analysis, "a transaction that runs has no analysis")

	transaction := f.stopAt(t, transactionId, 150, 111_000)

	assert.Equal(t, &store.TransactionAnalysis{
		IdleStartedAt: ptr(analysisSessionStart.Add(time.Hour)),
		IdleS:         5400,
		ChargingS:     3600,
		AveragePowerW: ptr(11_000.0),
		AnalysedAt:    tariffClockTime,
	}, transaction.Analysis)
	assert.Equal(t, ptr(int64(11_000)), transaction.EnergyWh)
	assert.Equal(t, []string{events.TypeSessionUpdated, events.TypeSessionUpdated, events.TypeSessionUpdated, events.TypeSessionCompleted},
		eventstest.Types(f.events.Take(t))[1:], "the events of the session are as ever")
}

// The reading of the stop is a reading of the register: the meter stop, not the energy of
// the transaction. A transaction without meter values in between is analysed from its
// meter start and its meter stop.
func TestStopTransactionAnalysesFromTheMeterStartAndTheMeterStop(t *testing.T) {
	f := newAnalysisFixture(t)

	transaction := f.stopAt(t, f.charge(t, 1), 180, 111_000)

	assert.Equal(t, []store.MeterValue{
		beginMeterValue(analysisMinute(0), 100_000),
		endMeterValue(analysisMinute(180), 111_000),
	}, transaction.MeterValues)
	assert.Equal(t, &store.TransactionAnalysis{
		IdleStartedAt: ptr(analysisSessionStart.Add(3 * time.Hour)),
		IdleS:         0,
		ChargingS:     10_800,
		AveragePowerW: ptr(3666.7),
		AnalysedAt:    tariffClockTime,
	}, transaction.Analysis, "3.7 kW on average: charging until the stop")

	// a transaction that drew nothing
	transaction = f.stopAt(t, f.charge(t, 1), 45, 100_000)
	assert.Equal(t, &store.TransactionAnalysis{
		IdleStartedAt: ptr(analysisSessionStart), IdleS: 2700, ChargingS: 0, AveragePowerW: ptr(0.0), AnalysedAt: tariffClockTime,
	}, transaction.Analysis)
}

// The thresholds of the analysis: the highest power of the connector of the transaction
// and the idle settings of its tariff.
func TestStopTransactionAnalysesWithTheConnectorAndTheTariff(t *testing.T) {
	ctx := context.Background()
	// an hour at 11 kW and an hour at 1 kW
	trickle := []int{60, 111_000}
	idleSince := func(transaction *store.Transaction) time.Time {
		require.NotNil(t, transaction.Analysis)
		require.NotNil(t, transaction.Analysis.IdleStartedAt)
		return *transaction.Analysis.IdleStartedAt
	}
	afterAnHour, afterTwoHours := analysisSessionStart.Add(time.Hour), analysisSessionStart.Add(2*time.Hour)

	t.Run("the settings of the manager", func(t *testing.T) {
		f := newAnalysisFixture(t)
		assert.Equal(t, afterTwoHours, idleSince(f.stopAt(t, f.charge(t, 1, trickle...), 120, 112_000)), "1 kW is charging")
	})

	t.Run("a connector of 150 kW", func(t *testing.T) {
		f := newAnalysisFixture(t)
		require.NoError(t, f.engine.SetChargeStationRuntimeDetails(ctx, sessionOrgStation, &store.ChargeStationRuntimeDetails{OcppVersion: "1.6"}))
		connector, err := f.engine.LookupAssetConnectorByOcpp(ctx, sessionOrgStation, 2, 1)
		require.NoError(t, err)
		require.NotNil(t, connector)
		connector.MaxElectricPower = ptr(int32(150_000))
		require.NoError(t, f.engine.UpdateAssetConnector(ctx, connector))

		assert.Equal(t, afterAnHour, idleSince(f.stopAt(t, f.charge(t, 2, trickle...), 120, 112_000)), "1 kW is below 1 % of 150 kW")
		assert.Equal(t, afterTwoHours, idleSince(f.stopAt(t, f.charge(t, 1, trickle...), 120, 112_000)), "the other connector")
	})

	t.Run("the idle settings of the tariff of the transaction", func(t *testing.T) {
		f := newAnalysisFixture(t)
		v := tariffVersion(3000)
		v.IdleDetection = &store.IdleDetection{MinChargingPowerW: ptr(1500.0)}
		require.NoError(t, f.engine.AddTariffVersion(ctx, f.regular.Id, 1, v))

		transaction := f.stopAt(t, f.charge(t, 1, trickle...), 120, 112_000)
		assert.Equal(t, ptr(2), transaction.TariffVersion)
		assert.Equal(t, afterAnHour, idleSince(transaction), "1 kW is below the 1.5 kW of the tariff")
		assert.Equal(t, afterTwoHours, idleSince(f.stopAt(t, f.charge(t, 2, trickle...), 120, 112_000)), "a connector without a tariff")
	})
}

func TestStopTransactionWithoutAnAnalyzerAnalysesNothing(t *testing.T) {
	f := newAnalysisFixture(t)
	f.stop.Analyzer = nil

	transaction := f.stopAt(t, f.charge(t, 1, 60, 111_000), 150, 111_000)

	assert.Positive(t, transaction.EndedSeqNo)
	assert.Nil(t, transaction.Analysis)
}

// A stop of a transaction that the manager never saw is recorded and analysed like any
// other. Nothing is known of it but its stop: it has no idle time and no charging time.
func TestStopTransactionAnalysesATransactionThatWasNotKnown(t *testing.T) {
	f := newAnalysisFixture(t)

	transaction := f.stopAt(t, 4711, 150, 111_000)

	assert.Positive(t, transaction.EndedSeqNo)
	assert.Nil(t, transaction.StartedAt)
	assert.Equal(t, &store.TransactionAnalysis{
		IdleStartedAt: ptr(analysisSessionStart.Add(150 * time.Minute)), AveragePowerW: ptr(0.0), AnalysedAt: tariffClockTime,
	}, transaction.Analysis)
}

// analysisBackend is the store of an Analyzer. It notes what the reads of a transaction
// saw, and fails what it is told to fail.
type analysisBackend struct {
	store.Engine
	findErr, setErr error
	finds, sets     int
	// of the last read: whether it was in a database transaction, whether its ctx has a
	// deadline and how far away, and whether the transaction that it read had ended
	inTx        bool
	hasDeadline bool
	deadlineIn  time.Duration
	ended       bool
	// log, when set, is told of every read
	log func(string)
}

func (s *analysisBackend) FindTransaction(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	s.finds++
	s.inTx = eventstest.InTx(ctx)
	var deadline time.Time
	if deadline, s.hasDeadline = ctx.Deadline(); s.hasDeadline {
		s.deadlineIn = time.Until(deadline)
	}
	if s.log != nil {
		s.log("analysis reads the transaction")
	}
	if s.findErr != nil {
		return nil, s.findErr
	}
	transaction, err := s.Engine.FindTransaction(ctx, chargeStationId, transactionId)
	s.ended = transaction != nil && transaction.StoppedAt != nil
	return transaction, err
}

func (s *analysisBackend) SetTransactionAnalysis(ctx context.Context, chargeStationId, transactionId string, analysis store.TransactionAnalysis) error {
	s.sets++
	if s.setErr != nil {
		return s.setErr
	}
	return s.Engine.SetTransactionAnalysis(ctx, chargeStationId, transactionId, analysis)
}

// loggingTx is a transactor that tells when its database transactions begin and end.
type loggingTx struct {
	store.Transactor
	log func(string)
}

func (l loggingTx) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	l.log("begin")
	defer l.log("end")
	return l.Transactor.RunInTx(ctx, fn)
}

// loggingTracker tells when it hears of the stop.
type loggingTracker struct {
	handlers.CommandTracker
	log func(string)
}

func (l loggingTracker) OnTransactionStopped(context.Context, string, int, string) {
	l.log("tracker hears of the stop")
}

// The transaction is analysed once its stop is stored, in a database transaction of its
// own: an analysis that fails cannot undo the stop, and it reads the stop on the primary.
// The command tracker hears of the stop first, and the answer does not wait long for the
// analysis.
func TestStopTransactionAnalysesAfterTheStopIsStored(t *testing.T) {
	f := newAnalysisFixture(t)
	transactionId := f.charge(t, 1, 60, 111_000)
	var steps []string
	log := func(step string) { steps = append(steps, step) }
	backend := &analysisBackend{Engine: f.engine, log: log}
	f.stop.Analyzer.Transactions = backend
	f.stop.Tx = loggingTx{Transactor: f.events.Tx, log: log}
	f.stop.CommandTracker = loggingTracker{log: log}

	transaction := f.stopAt(t, transactionId, 150, 111_000)

	assert.Equal(t, []string{"begin", "end", "tracker hears of the stop", "begin", "analysis reads the transaction", "end"}, steps)
	assert.True(t, backend.ended, "the analysis reads the transaction with its stop")
	assert.True(t, backend.inTx)
	require.True(t, backend.hasDeadline, "the answer does not wait long for the analysis")
	assert.LessOrEqual(t, backend.deadlineIn, rating.AnalysisTimeout)
	assert.Positive(t, backend.deadlineIn)
	assert.Equal(t, 1, backend.sets)
	require.NotNil(t, transaction.Analysis)
	assert.Equal(t, int64(5400), transaction.Analysis.IdleS)
}

// A stop is answered as ever when the analysis of its transaction fails, and the
// transaction is stopped all the same.
func TestStopTransactionIsAnsweredWhenTheAnalysisFails(t *testing.T) {
	ctx := context.Background()
	errDown := errors.New("the store is down")

	// the answer and the events of the handler without an Analyzer
	plain := newAnalysisFixture(t)
	plain.stop.Analyzer = nil
	plainId := plain.charge(t, 1, 60, 111_000)
	plain.events.Take(t)
	stop := analysisStopRequest(plainId, 150, 111_000)
	stop.IdTag = ptr("RFID0001")
	want, err := plain.stop.HandleCall(ctx, sessionOrgStation, stop)
	require.NoError(t, err)
	require.NotNil(t, want.(*types.StopTransactionResponseJson).IdTagInfo)
	wantEvents := plain.events.Take(t)
	require.Equal(t, []string{events.TypeSessionCompleted}, eventstest.Types(wantEvents))

	for name, backend := range map[string]*analysisBackend{
		"the transaction cannot be read": {findErr: errDown},
		"the analysis cannot be stored":  {setErr: errDown},
		"the backend keeps no analysis":  {setErr: fmt.Errorf("SetTransactionAnalysis: %w", store.ErrNotImplemented)},
	} {
		t.Run(name, func(t *testing.T) {
			f := newAnalysisFixture(t)
			backend.Engine = f.engine
			f.stop.Analyzer.Transactions = backend
			tracker := &recordingTracker{store: f.engine}
			f.stop.CommandTracker = tracker
			transactionId := f.charge(t, 1, 60, 111_000)
			require.Equal(t, plainId, transactionId)
			f.events.Take(t)

			got, err := f.stop.HandleCall(ctx, sessionOrgStation, stop)

			require.NoError(t, err)
			assert.Equal(t, want, got)
			assert.Equal(t, 1, backend.finds, "the analysis was tried")
			transaction := f.stored(t, transactionId)
			assert.Positive(t, transaction.EndedSeqNo)
			assert.Equal(t, ptr(analysisSessionStart.Add(150*time.Minute)), transaction.StoppedAt)
			assert.Equal(t, ptr(int64(111_000)), transaction.MeterStopWh)
			assert.Nil(t, transaction.Analysis)
			gotEvents := f.events.Take(t)
			require.Equal(t, []string{events.TypeSessionCompleted}, eventstest.Types(gotEvents))
			assert.JSONEq(t, string(wantEvents[0].Data), string(gotEvents[0].Data))
			assert.Equal(t, []string{fmt.Sprintf("stopped %s %d Local", sessionOrgStation, transactionId)}, tracker.calls)
		})
	}
}

// The router gives its Analyzer to the handler of the stop.
func TestNewRouterGivesTheAnalyzerToTheStopHandler(t *testing.T) {
	engine := inmemory.NewStore(clockAt(t, "2026-11-28T10:00:00Z"))
	analyzer := &rating.Analyzer{Transactions: engine, Defaults: rating.DefaultIdleSettings()}

	router, ok := handlers.NewRouter(&FakeEmitter{}, clockAt(t, "2026-11-28T10:00:00Z"), engine, events.NopEmitter{},
		&tenancy.Resolver{Stations: engine}, nil, nil, nil, nil, time.Minute, schemas.OcppSchemas, nil, nil, nil, analyzer).(*msghandlers.Router)
	require.True(t, ok)

	stop, ok := router.CallRoutes["StopTransaction"].Handler.(handlers.StopTransactionHandler)
	require.True(t, ok)
	assert.Same(t, analyzer, stop.Analyzer)
}
