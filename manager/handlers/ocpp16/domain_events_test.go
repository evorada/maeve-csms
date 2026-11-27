// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"errors"
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
	clockTest "k8s.io/utils/clock/testing"
)

// The domain events of the OCPP 1.6 handlers: what each one emits, that it emits it
// inside the transaction of its change, and that an event that cannot be stored fails
// the call.

const eventsOrgId = "0193b2c4-0000-7000-8000-0000000000a1"

var errOutbox = errors.New("outbox unavailable")

// statusNotificationFixture is a StatusNotification handler on an in-memory store
// whose events are recorded.
type statusNotificationFixture struct {
	clock   *clockTest.FakePassiveClock
	engine  *inmemory.Store
	events  *eventstest.Fixture
	handler handlers.StatusNotificationHandler
}

func newStatusNotificationFixture() *statusNotificationFixture {
	clk := clockTest.NewFakePassiveClock(time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC))
	engine := inmemory.NewStore(clk)
	domainEvents := eventstest.New(engine, eventsOrgId, clk)
	return &statusNotificationFixture{
		clock:  clk,
		engine: engine,
		events: domainEvents,
		handler: handlers.StatusNotificationHandler{
			Clock: clk, StatusStore: engine, Tx: domainEvents.Tx, Events: domainEvents.Events,
		},
	}
}

func (f *statusNotificationFixture) notify(t *testing.T, req *types.StatusNotificationJson) {
	t.Helper()
	resp, err := f.handler.HandleCall(context.Background(), "k7f2.CP001", req)
	require.NoError(t, err)
	assert.Equal(t, &types.StatusNotificationResponseJson{}, resp)
}

func statusNotification(connectorId int, status types.StatusNotificationJsonStatus, errorCode types.StatusNotificationJsonErrorCode) *types.StatusNotificationJson {
	return &types.StatusNotificationJson{ConnectorId: connectorId, Status: status, ErrorCode: errorCode}
}

func TestFirstStatusNotificationEmitsConnectorStatusChanged(t *testing.T) {
	f := newStatusNotificationFixture()
	info, vendorId, vendorErrorCode := "door open", "Acme", "E42"
	timestamp := "2026-11-27T12:59:00+01:00"

	f.notify(t, &types.StatusNotificationJson{
		ConnectorId:     2,
		Status:          types.StatusNotificationJsonStatusAvailable,
		ErrorCode:       types.StatusNotificationJsonErrorCodeNoError,
		Info:            &info,
		VendorId:        &vendorId,
		VendorErrorCode: &vendorErrorCode,
		Timestamp:       &timestamp,
	})

	evs := f.events.Take(t)
	require.Len(t, evs, 1)
	ev := evs[0]
	assert.Equal(t, "connector.status_changed", ev.Type)
	assert.Equal(t, eventsOrgId, ev.OrgId)
	assert.Equal(t, "urn:cpms:"+eventsOrgId+":connector/k7f2.CP001#2", ev.Subject)
	assert.Equal(t, "station:k7f2.CP001", ev.Actor)
	assert.Equal(t, f.clock.Now(), ev.Time)
	assert.Equal(t, events.ConnectorStatusChangedData{
		StationKey:      "k7f2.CP001",
		StationId:       "CP001",
		EvseId:          2,
		ConnectorId:     2,
		Status:          "Available",
		ErrorCode:       "NoError",
		Info:            &info,
		VendorId:        &vendorId,
		VendorErrorCode: &vendorErrorCode,
		Timestamp:       time.Date(2026, 11, 27, 11, 59, 0, 0, time.UTC),
	}, eventstest.Data[events.ConnectorStatusChangedData](t, ev))
	assert.NotContains(t, string(ev.Data), "previous_status")
}

func TestUnchangedStatusEmitsNothing(t *testing.T) {
	f := newStatusNotificationFixture()
	f.notify(t, statusNotification(1, types.StatusNotificationJsonStatusAvailable, types.StatusNotificationJsonErrorCodeNoError))
	require.Len(t, f.events.Take(t), 1)

	f.notify(t, statusNotification(1, types.StatusNotificationJsonStatusAvailable, types.StatusNotificationJsonErrorCodeNoError))

	assert.Empty(t, f.events.Take(t))
}

func TestStatusChangeEmitsThePreviousStatus(t *testing.T) {
	f := newStatusNotificationFixture()
	f.notify(t, statusNotification(1, types.StatusNotificationJsonStatusAvailable, types.StatusNotificationJsonErrorCodeNoError))
	// another connector and the station as a whole are not the previous status
	f.notify(t, statusNotification(2, types.StatusNotificationJsonStatusFaulted, types.StatusNotificationJsonErrorCodeGroundFailure))
	f.notify(t, statusNotification(0, types.StatusNotificationJsonStatusUnavailable, types.StatusNotificationJsonErrorCodeNoError))
	require.Len(t, f.events.Take(t), 3)

	// without a timestamp of the station the event has the time it was received
	f.clock.SetTime(f.clock.Now().Add(time.Minute))
	f.notify(t, statusNotification(1, types.StatusNotificationJsonStatusCharging, types.StatusNotificationJsonErrorCodeNoError))

	evs := f.events.Take(t)
	require.Len(t, evs, 1)
	data := eventstest.Data[events.ConnectorStatusChangedData](t, evs[0])
	assert.Equal(t, 1, data.ConnectorId)
	assert.Equal(t, "Charging", data.Status)
	assert.Equal(t, "Available", data.PreviousStatus)
	assert.Equal(t, f.clock.Now(), data.Timestamp)
}

func TestErrorCodeChangeEmitsAnEvent(t *testing.T) {
	f := newStatusNotificationFixture()
	f.notify(t, statusNotification(1, types.StatusNotificationJsonStatusFaulted, types.StatusNotificationJsonErrorCodeGroundFailure))
	f.events.Take(t)

	f.notify(t, statusNotification(1, types.StatusNotificationJsonStatusFaulted, types.StatusNotificationJsonErrorCodeOverVoltage))

	evs := f.events.Take(t)
	require.Len(t, evs, 1)
	data := eventstest.Data[events.ConnectorStatusChangedData](t, evs[0])
	assert.Equal(t, "Faulted", data.Status)
	assert.Equal(t, "Faulted", data.PreviousStatus)
	assert.Equal(t, "OverVoltage", data.ErrorCode)
}

// unlistableStatusStore cannot list the connector statuses.
type unlistableStatusStore struct {
	store.StatusStore
}

func (unlistableStatusStore) ListConnectorStatuses(context.Context, string) ([]*store.ConnectorStatus, error) {
	return nil, errors.New("list failed")
}

// The previous status is only read for the event: a store that cannot give it does not
// refuse the notification.
func TestStatusIsStoredWhenThePreviousStatusCannotBeRead(t *testing.T) {
	f := newStatusNotificationFixture()
	f.handler.StatusStore = unlistableStatusStore{StatusStore: f.engine}

	f.notify(t, statusNotification(1, types.StatusNotificationJsonStatusAvailable, types.StatusNotificationJsonErrorCodeNoError))

	status, err := f.engine.GetConnectorStatus(context.Background(), "k7f2.CP001", 1)
	require.NoError(t, err)
	assert.Equal(t, store.ConnectorStatusAvailable, status.Status)
	evs := f.events.Take(t)
	require.Len(t, evs, 1)
	assert.Empty(t, eventstest.Data[events.ConnectorStatusChangedData](t, evs[0]).PreviousStatus)
}

func TestStatusNotificationFailsWhenItsEventCannotBeStored(t *testing.T) {
	f := newStatusNotificationFixture()
	f.events.Events.Err = errOutbox

	_, err := f.handler.HandleCall(context.Background(), "k7f2.CP001",
		statusNotification(1, types.StatusNotificationJsonStatusAvailable, types.StatusNotificationJsonErrorCodeNoError))

	assert.ErrorIs(t, err, errOutbox)
	assert.Empty(t, f.events.Take(t))
}

// newStartTransactionEventsHandler is newStartTransactionHandler with recorded events.
func newStartTransactionEventsHandler(t *testing.T) (handlers.StartTransactionHandler, *inmemory.Store, *eventstest.Fixture) {
	t.Helper()
	handler, engine := newStartTransactionHandler(t)
	domainEvents := eventstest.New(engine, eventsOrgId, handler.Clock)
	handler.Tx, handler.Events = domainEvents.Tx, domainEvents.Events
	return handler, engine, domainEvents
}

func TestStartTransactionEmitsSessionStarted(t *testing.T) {
	handler, _, domainEvents := newStartTransactionEventsHandler(t)
	req := startTransactionRequest(startTransactionIdTag)
	reservationId := 7
	req.ReservationId = &reservationId

	started := startTransaction(t, handler, startTransactionChargeStationId, req)

	evs := domainEvents.Take(t)
	require.Len(t, evs, 1)
	ev := evs[0]
	transactionUuid := handlers.ConvertToUUID(started.TransactionId)
	assert.Equal(t, "session.started", ev.Type)
	assert.Equal(t, "urn:cpms:"+eventsOrgId+":session/"+transactionUuid, ev.Subject)
	assert.Equal(t, "station:"+startTransactionChargeStationId, ev.Actor)
	connectorId := 2
	groupId := startTransactionGroupId
	meterStart := int64(100)
	assert.Equal(t, events.SessionStartedData{
		StationKey:        startTransactionChargeStationId,
		StationId:         startTransactionChargeStationId,
		TransactionId:     transactionUuid,
		OcppTransactionId: "1",
		OcppVersion:       "ocpp1.6",
		EvseId:            &connectorId,
		ConnectorId:       &connectorId,
		IdToken:           startTransactionIdTag,
		TokenType:         "ISO14443",
		IdTokenGroup:      &groupId,
		AuthStatus:        "Accepted",
		StartedAt:         time.Date(2023, 6, 15, 14, 58, 30, 0, time.UTC),
		MeterStartWh:      &meterStart,
		ReservationId:     &reservationId,
	}, eventstest.Data[events.SessionStartedData](t, ev))
}

func TestStartTransactionOfAnInvalidTokenEmitsSessionStarted(t *testing.T) {
	handler, _, domainEvents := newStartTransactionEventsHandler(t)

	started := startTransaction(t, handler, startTransactionChargeStationId, startTransactionRequest("UNKNOWN"))

	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusInvalid, started.IdTagInfo.Status)
	evs := domainEvents.Take(t)
	require.Len(t, evs, 1)
	data := eventstest.Data[events.SessionStartedData](t, evs[0])
	assert.Equal(t, "Invalid", data.AuthStatus)
	assert.Equal(t, "UNKNOWN", data.IdToken)
	assert.Nil(t, data.IdTokenGroup)
	assert.NotContains(t, string(evs[0].Data), "id_token_group")
	assert.NotContains(t, string(evs[0].Data), "reservation_id")
}

func TestStartTransactionFailsWhenItsEventCannotBeStored(t *testing.T) {
	for name, fail := range map[string]func(*eventstest.Fixture){
		"emit fails":   func(f *eventstest.Fixture) { f.Events.Err = errOutbox },
		"commit fails": func(f *eventstest.Fixture) { f.Tx.CommitErr = errOutbox },
	} {
		t.Run(name, func(t *testing.T) {
			handler, _, domainEvents := newStartTransactionEventsHandler(t)
			fail(domainEvents)

			_, err := handler.HandleCall(context.Background(), startTransactionChargeStationId, startTransactionRequest(startTransactionIdTag))

			assert.ErrorIs(t, err, errOutbox)
			assert.Empty(t, domainEvents.Take(t))
		})
	}
}

// meterValuesEventsFixture is a MeterValues handler with recorded events, and a
// started transaction with a meter start of 1000 Wh on connector 1.
type meterValuesEventsFixture struct {
	engine        *inmemory.Store
	events        *eventstest.Fixture
	handler       handlers.MeterValuesHandler
	transactionId int
}

func newMeterValuesEventsFixture(t *testing.T) *meterValuesEventsFixture {
	t.Helper()
	handler, engine := newMeterValuesHandler(t)
	startHandler := handlers.StartTransactionHandler{
		Clock: clockTest.NewFakePassiveClock(time.Now()), TokenStore: engine, OrgResolver: eventsOrgResolver{},
		TransactionStore: engine, ReservationStore: engine, Tx: engine, Events: events.NopEmitter{},
	}
	started := startTransaction(t, startHandler, meterValuesChargeStationId, &types.StartTransactionJson{
		ConnectorId: 1, IdTag: "MYRFIDTAG", MeterStart: 1000, Timestamp: "2026-11-26T10:00:00Z",
	})

	domainEvents := eventstest.New(engine, eventsOrgId, clockTest.NewFakePassiveClock(time.Now()))
	handler.Tx, handler.Events = domainEvents.Tx, domainEvents.Events
	return &meterValuesEventsFixture{engine: engine, events: domainEvents, handler: handler, transactionId: started.TransactionId}
}

// eventsOrgResolver gives every station the legacy org.
type eventsOrgResolver struct{}

func (eventsOrgResolver) OrgIdForStation(context.Context, string) (string, error) {
	return store.LegacyOrgId, nil
}

func (f *meterValuesEventsFixture) send(t *testing.T, transactionId *int, meterValues ...types.MeterValuesJsonMeterValueElem) {
	t.Helper()
	resp, err := f.handler.HandleCall(context.Background(), meterValuesChargeStationId, &types.MeterValuesJson{
		ConnectorId: 1, TransactionId: transactionId, MeterValue: meterValues,
	})
	require.NoError(t, err)
	assert.Equal(t, &types.MeterValuesResponseJson{}, resp)
}

func meterValue(timestamp string, samples ...types.MeterValuesJsonMeterValueElemSampledValueElem) types.MeterValuesJsonMeterValueElem {
	return types.MeterValuesJsonMeterValueElem{Timestamp: timestamp, SampledValue: samples}
}

const (
	measurandEnergy = types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandEnergyActiveImportRegister
	measurandPower  = types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandPowerActiveImport
	measurandSoC    = types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandSoC
	unitWh          = types.MeterValuesJsonMeterValueElemSampledValueElemUnitWh
	unitKWh         = types.MeterValuesJsonMeterValueElemSampledValueElemUnitKWh
	unitW           = types.MeterValuesJsonMeterValueElemSampledValueElemUnitW
	unitKW          = types.MeterValuesJsonMeterValueElemSampledValueElemUnitKW
	unitPercent     = types.MeterValuesJsonMeterValueElemSampledValueElemUnitPercent
)

func TestMeterValuesOfATransactionEmitSessionUpdated(t *testing.T) {
	f := newMeterValuesEventsFixture(t)

	f.send(t, &f.transactionId, meterValue("2026-11-26T10:15:00+01:00",
		types.MeterValuesJsonMeterValueElemSampledValueElem{Value: "1800"}, // the default measurand and unit
		meterValuesSample("7.4", measurandPower, unitKW),
		meterValuesSample("63.5", measurandSoC, unitPercent),
	))

	evs := f.events.Take(t)
	require.Len(t, evs, 1)
	ev := evs[0]
	transactionUuid := handlers.ConvertToUUID(f.transactionId)
	assert.Equal(t, "session.updated", ev.Type)
	assert.Equal(t, "urn:cpms:"+eventsOrgId+":session/"+transactionUuid, ev.Subject)
	assert.Equal(t, "station:"+meterValuesChargeStationId, ev.Actor)
	connectorId := 1
	meterWh, energyWh := int64(1800), int64(800)
	powerW, soc := 7400.0, 63.5
	assert.Equal(t, events.SessionUpdatedData{
		StationKey:        meterValuesChargeStationId,
		StationId:         meterValuesChargeStationId,
		TransactionId:     transactionUuid,
		OcppTransactionId: "1",
		EvseId:            &connectorId,
		ConnectorId:       &connectorId,
		SampledAt:         time.Date(2026, 11, 26, 9, 15, 0, 0, time.UTC),
		MeterWh:           &meterWh,
		EnergyWh:          &energyWh,
		PowerW:            &powerW,
		SocPercent:        &soc,
	}, eventstest.Data[events.SessionUpdatedData](t, ev))
}

func TestSessionUpdatedHasTheNewestReadings(t *testing.T) {
	f := newMeterValuesEventsFixture(t)
	phase := types.MeterValuesJsonMeterValueElemSampledValueElemPhaseL1
	inlet := types.MeterValuesJsonMeterValueElemSampledValueElemLocationInlet
	perPhase := meterValuesSample("9999", measurandEnergy, unitWh)
	perPhase.Phase = &phase
	atTheInlet := meterValuesSample("8888", measurandPower, unitW)
	atTheInlet.Location = &inlet

	f.send(t, &f.transactionId,
		// the newest meter value comes first; only it has the power
		meterValue("2026-11-26T10:20:00Z",
			meterValuesSample("2.5", measurandEnergy, unitKWh),
			meterValuesSample("3600", measurandPower, unitW),
			perPhase, atTheInlet,
		),
		meterValue("2026-11-26T10:10:00Z",
			meterValuesSample("1500", measurandEnergy, unitWh),
			meterValuesSample("40", measurandSoC, unitPercent),
		),
		// a sample in a unit that cannot be converted is not a reading
		meterValue("2026-11-26T10:05:00Z", meterValuesSample("5", measurandPower, unitPercent)),
	)

	evs := f.events.Take(t)
	require.Len(t, evs, 1)
	data := eventstest.Data[events.SessionUpdatedData](t, evs[0])
	assert.Equal(t, time.Date(2026, 11, 26, 10, 20, 0, 0, time.UTC), data.SampledAt)
	require.NotNil(t, data.MeterWh)
	assert.Equal(t, int64(2500), *data.MeterWh, "kWh is converted, the phase sample is not the register")
	require.NotNil(t, data.EnergyWh)
	assert.Equal(t, int64(1500), *data.EnergyWh)
	require.NotNil(t, data.PowerW)
	assert.Equal(t, 3600.0, *data.PowerW, "the power at the inlet is not that of the connector")
	require.NotNil(t, data.SocPercent)
	assert.Equal(t, 40.0, *data.SocPercent, "the newest meter value with a state of charge")
}

func TestSessionUpdatedWithoutUsableReadings(t *testing.T) {
	f := newMeterValuesEventsFixture(t)

	// a register below the meter start (meter replaced) has no energy
	f.send(t, &f.transactionId, meterValue("2026-11-26T10:20:00Z", meterValuesSample("400", measurandEnergy, unitWh)))
	// a meter value without any of the readings still tells that the session is alive; a
	// power that is too large in W is not a reading (it could not be encoded)
	f.send(t, &f.transactionId, meterValue("2026-11-26T10:21:00Z",
		meterValuesSample("230", types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandVoltage, types.MeterValuesJsonMeterValueElemSampledValueElemUnitV),
		meterValuesSample("1e308", measurandPower, unitKW)))

	evs := f.events.Take(t)
	require.Len(t, evs, 2)
	first := eventstest.Data[events.SessionUpdatedData](t, evs[0])
	require.NotNil(t, first.MeterWh)
	assert.Equal(t, int64(400), *first.MeterWh)
	assert.Nil(t, first.EnergyWh)
	assert.NotContains(t, string(evs[1].Data), "_wh")
	assert.NotContains(t, string(evs[1].Data), "power_w")
	assert.NotContains(t, string(evs[1].Data), "soc_percent")
}

func TestMeterValuesWithoutAKnownTransactionEmitNothing(t *testing.T) {
	f := newMeterValuesEventsFixture(t)
	unknown := f.transactionId + 1
	sample := meterValue("2026-11-26T10:20:00Z", meterValuesSample("1500", measurandEnergy, unitWh))

	f.send(t, nil, sample)
	f.send(t, &unknown, sample)
	// nothing that can be stored: nothing changed
	f.send(t, &f.transactionId, meterValue("2026-11-26T10:20:00Z", signedMeterValuesSample()))

	assert.Empty(t, f.events.Take(t))
	assert.Equal(t, 2, storedMeterValues(t, f.engine).Total)
}

// unreadableTransactionStore cannot read transactions.
type unreadableTransactionStore struct {
	store.TransactionStore
}

func (unreadableTransactionStore) FindTransaction(context.Context, string, string) (*store.Transaction, error) {
	return nil, errors.New("read failed")
}

// The transaction is only read for the event: when it cannot be read, the meter values
// are stored all the same.
func TestMeterValuesAreStoredWhenTheTransactionCannotBeRead(t *testing.T) {
	f := newMeterValuesEventsFixture(t)
	f.handler.TransactionStore = unreadableTransactionStore{TransactionStore: f.engine}

	f.send(t, &f.transactionId, meterValue("2026-11-26T10:20:00Z", meterValuesSample("1500", measurandEnergy, unitWh)))

	assert.Empty(t, f.events.Take(t))
	assert.Equal(t, 1, storedMeterValues(t, f.engine).Total)
}

func TestMeterValuesFailWhenTheirEventCannotBeStored(t *testing.T) {
	f := newMeterValuesEventsFixture(t)
	f.events.Events.Err = errOutbox

	_, err := f.handler.HandleCall(context.Background(), meterValuesChargeStationId, &types.MeterValuesJson{
		ConnectorId: 1, TransactionId: &f.transactionId,
		MeterValue: []types.MeterValuesJsonMeterValueElem{meterValue("2026-11-26T10:20:00Z", meterValuesSample("1500", measurandEnergy, unitWh))},
	})

	assert.ErrorIs(t, err, errOutbox)
	assert.Empty(t, f.events.Take(t))
}

// newStopTransactionEventsHandler is newStopTransactionHandler with recorded events.
func newStopTransactionEventsHandler(t *testing.T) (handlers.StopTransactionHandler, *inmemory.Store, *eventstest.Fixture) {
	t.Helper()
	handler, engine := newStopTransactionHandler(t)
	domainEvents := eventstest.New(engine, eventsOrgId, handler.Clock)
	handler.Tx, handler.Events = domainEvents.Tx, domainEvents.Events
	return handler, engine, domainEvents
}

func TestStopTransactionEmitsSessionCompleted(t *testing.T) {
	handler, engine, domainEvents := newStopTransactionEventsHandler(t)
	transactionId := startedTransaction(t, engine)
	req := stopTransactionRequest(transactionId)
	idTag := stopTransactionIdTag
	req.IdTag = &idTag
	reason := types.StopTransactionJsonReasonEVDisconnected
	req.Reason = &reason

	stopTransaction(t, handler, engine, req)

	evs := domainEvents.Take(t)
	require.Len(t, evs, 1)
	ev := evs[0]
	transactionUuid := handlers.ConvertToUUID(transactionId)
	assert.Equal(t, "session.completed", ev.Type)
	assert.Equal(t, "urn:cpms:"+eventsOrgId+":session/"+transactionUuid, ev.Subject)
	assert.Equal(t, "station:"+stopTransactionChargeStationId, ev.Actor)
	connectorId := 1
	startedAt := time.Date(2023, 6, 15, 14, 58, 30, 0, time.UTC)
	duration, meterStart, meterStop, energy := int64(450), int64(50), int64(200), int64(150)
	assert.Equal(t, events.SessionCompletedData{
		StationKey:        stopTransactionChargeStationId,
		StationId:         stopTransactionChargeStationId,
		TransactionId:     transactionUuid,
		OcppTransactionId: "1",
		EvseId:            &connectorId,
		ConnectorId:       &connectorId,
		IdToken:           stopTransactionIdTag,
		TokenType:         "ISO14443",
		StartedAt:         &startedAt,
		StoppedAt:         time.Date(2023, 6, 15, 15, 6, 0, 0, time.UTC),
		DurationS:         &duration,
		MeterStartWh:      &meterStart,
		MeterStopWh:       &meterStop,
		EnergyWh:          &energy,
		StopReason:        "EVDisconnected",
	}, eventstest.Data[events.SessionCompletedData](t, ev))
}

func TestStopOfAnUnknownTransactionEmitsSessionCompleted(t *testing.T) {
	handler, engine, domainEvents := newStopTransactionEventsHandler(t)

	stopTransaction(t, handler, engine, stopTransactionRequest(42))

	evs := domainEvents.Take(t)
	require.Len(t, evs, 1)
	meterStop := int64(200)
	assert.Equal(t, events.SessionCompletedData{
		StationKey:        stopTransactionChargeStationId,
		StationId:         stopTransactionChargeStationId,
		TransactionId:     handlers.ConvertToUUID(42),
		OcppTransactionId: "42",
		StoppedAt:         time.Date(2023, 6, 15, 15, 6, 0, 0, time.UTC),
		MeterStopWh:       &meterStop,
		StopReason:        "Local",
	}, eventstest.Data[events.SessionCompletedData](t, evs[0]), "what is not known is left out")
}

// A station whose clock went backwards during the session gives no duration.
func TestSessionCompletedWithoutANegativeDuration(t *testing.T) {
	handler, engine, domainEvents := newStopTransactionEventsHandler(t)
	transactionId := startedTransaction(t, engine)
	req := stopTransactionRequest(transactionId)
	req.Timestamp = "2023-06-15T14:00:00Z"

	stopTransaction(t, handler, engine, req)

	evs := domainEvents.Take(t)
	require.Len(t, evs, 1)
	data := eventstest.Data[events.SessionCompletedData](t, evs[0])
	assert.NotNil(t, data.StartedAt)
	assert.Nil(t, data.DurationS)
}

func TestStopTransactionFailsWhenItsEventCannotBeStored(t *testing.T) {
	for name, fail := range map[string]func(*eventstest.Fixture){
		"emit fails":   func(f *eventstest.Fixture) { f.Events.Err = errOutbox },
		"commit fails": func(f *eventstest.Fixture) { f.Tx.CommitErr = errOutbox },
	} {
		t.Run(name, func(t *testing.T) {
			handler, engine, domainEvents := newStopTransactionEventsHandler(t)
			transactionId := startedTransaction(t, engine)
			fail(domainEvents)

			_, err := handler.HandleCall(context.Background(), stopTransactionChargeStationId, stopTransactionRequest(transactionId))

			assert.ErrorIs(t, err, errOutbox)
			assert.Empty(t, domainEvents.Take(t))
		})
	}
}
