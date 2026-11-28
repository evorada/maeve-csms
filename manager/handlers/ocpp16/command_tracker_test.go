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
	msghandlers "github.com/thoughtworks/maeve-csms/manager/handlers"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	clockTest "k8s.io/utils/clock/testing"
)

// clockAt is a fake clock at the RFC 3339 time.
func clockAt(t *testing.T, at string) *clockTest.FakePassiveClock {
	t.Helper()
	now, err := time.Parse(time.RFC3339, at)
	require.NoError(t, err)
	return clockTest.NewFakePassiveClock(now)
}

// newRouterWithTracker is the OCPP 1.6 router on the store with the command tracker.
func newRouterWithTracker(t *testing.T, engine *inmemory.Store, tracker handlers.CommandTracker) *msghandlers.Router {
	t.Helper()
	router, ok := handlers.NewRouter(&FakeEmitter{}, clockAt(t, "2026-11-27T10:00:00Z"), engine, events.NopEmitter{},
		&tenancy.Resolver{Stations: engine}, nil, nil, nil, nil, time.Minute, schemas.OcppSchemas, nil, tracker, nil, nil, nil).(*msghandlers.Router)
	require.True(t, ok)
	return router
}

// recordingTracker records what the handlers tell the command tracker, and what the
// store held at that moment. It panics on every call when panics is set.
type recordingTracker struct {
	store  *inmemory.Store
	calls  []string
	panics bool
	// stored is, for each call, whether the transaction or status it is about was
	// already stored
	stored []bool
}

func (r *recordingTracker) OnTransactionStarted(ctx context.Context, stationKey string, connectorId int, idTag, transactionId string, ocppTransactionId int) {
	r.calls = append(r.calls, fmt.Sprintf("started %s %d %s %s %d", stationKey, connectorId, idTag, transactionId, ocppTransactionId))
	found, err := r.store.FindTransaction(ctx, stationKey, transactionId)
	r.stored = append(r.stored, err == nil && found != nil)
	if r.panics {
		panic("tracker broke")
	}
}

func (r *recordingTracker) OnTransactionStopped(ctx context.Context, stationKey string, ocppTransactionId int, reason string) {
	r.calls = append(r.calls, fmt.Sprintf("stopped %s %d %s", stationKey, ocppTransactionId, reason))
	found, err := r.store.FindTransaction(ctx, stationKey, handlers.ConvertToUUID(ocppTransactionId))
	r.stored = append(r.stored, err == nil && found != nil && found.EndedSeqNo > 0)
	if r.panics {
		panic("tracker broke")
	}
}

func (r *recordingTracker) OnConnectorStatus(ctx context.Context, stationKey string, connectorId int, status string) {
	r.calls = append(r.calls, fmt.Sprintf("status %s %d %s", stationKey, connectorId, status))
	found, err := r.store.GetConnectorStatus(ctx, stationKey, connectorId)
	r.stored = append(r.stored, err == nil && found != nil && string(found.Status) == status)
	if r.panics {
		panic("tracker broke")
	}
}

var _ handlers.CommandTracker = (*recordingTracker)(nil)

func TestStartTransactionTellsTheTrackerAboutAnAcceptedTransaction(t *testing.T) {
	handler, engine := newStartTransactionHandler(t)
	tracker := &recordingTracker{store: engine}
	handler.CommandTracker = tracker

	resp := startTransaction(t, handler, startTransactionChargeStationId, startTransactionRequest(startTransactionIdTag))

	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusAccepted, resp.IdTagInfo.Status)
	assert.Equal(t, []string{fmt.Sprintf("started cs001 2 MYRFIDTAG %s %d",
		handlers.ConvertToUUID(resp.TransactionId), resp.TransactionId)}, tracker.calls)
	assert.Equal(t, []bool{true}, tracker.stored, "once the transaction is stored")
}

func TestStartTransactionDoesNotTellTheTrackerAboutATransactionThatIsNotAccepted(t *testing.T) {
	handler, engine := newStartTransactionHandler(t)
	tracker := &recordingTracker{store: engine}
	handler.CommandTracker = tracker
	require.NoError(t, engine.SetToken(context.Background(), &store.Token{OrgId: store.LegacyOrgId, CountryCode: "GB",
		PartyId: "TWK", Type: "RFID", Uid: "BLOCKED", ContractId: "GBTWK012345679V", Issuer: "Thoughtworks", Valid: false,
		CacheMode: "NEVER"}))

	invalid := startTransaction(t, handler, startTransactionChargeStationId, startTransactionRequest("BADRFIDTAG"))
	blocked := startTransaction(t, handler, startTransactionChargeStationId, startTransactionRequest("BLOCKED"))

	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusInvalid, invalid.IdTagInfo.Status)
	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusBlocked, blocked.IdTagInfo.Status)
	assert.Empty(t, tracker.calls)

	t.Run("nor about a transaction that could not be stored", func(t *testing.T) {
		handler.TransactionStore = &faultyTransactionStore{TransactionStore: engine, createErrs: []error{errors.New("store failed")}}
		_, err := handler.HandleCall(context.Background(), startTransactionChargeStationId, startTransactionRequest(startTransactionIdTag))
		require.Error(t, err)
		assert.Empty(t, tracker.calls)
	})
}

func TestStartTransactionAnswersTheSameWhateverTheTracker(t *testing.T) {
	without, _ := newStartTransactionHandler(t)
	want := startTransaction(t, without, startTransactionChargeStationId, startTransactionRequest(startTransactionIdTag))

	handler, engine := newStartTransactionHandler(t)
	handler.CommandTracker = &recordingTracker{store: engine, panics: true}
	got := startTransaction(t, handler, startTransactionChargeStationId, startTransactionRequest(startTransactionIdTag))

	assert.Equal(t, want, got, "a tracker that panics changes nothing")
	found, err := engine.FindTransaction(context.Background(), startTransactionChargeStationId, handlers.ConvertToUUID(got.TransactionId))
	require.NoError(t, err)
	assert.NotNil(t, found)
}

func TestStopTransactionTellsTheTrackerAboutTheStop(t *testing.T) {
	handler, engine := newStopTransactionHandler(t)
	tracker := &recordingTracker{store: engine}
	handler.CommandTracker = tracker
	first := startedTransaction(t, engine)
	second := startedTransaction(t, engine)

	remote := stopTransactionRequest(first)
	reason := types.StopTransactionJsonReasonRemote
	remote.Reason = &reason
	stopTransaction(t, handler, engine, remote)
	stopTransaction(t, handler, engine, stopTransactionRequest(second))

	assert.Equal(t, []string{
		fmt.Sprintf("stopped %s %d Remote", stopTransactionChargeStationId, first),
		fmt.Sprintf("stopped %s %d Local", stopTransactionChargeStationId, second),
	}, tracker.calls, "a stop without reason is Local")
	assert.Equal(t, []bool{true, true}, tracker.stored, "once the end is stored")
}

func TestStopTransactionAnswersTheSameWhateverTheTracker(t *testing.T) {
	ctx := context.Background()
	without, engine := newStopTransactionHandler(t)
	want, err := without.HandleCall(ctx, stopTransactionChargeStationId, stopTransactionRequest(startedTransaction(t, engine)))
	require.NoError(t, err)

	handler, engine := newStopTransactionHandler(t)
	handler.CommandTracker = &recordingTracker{store: engine, panics: true}
	transactionId := startedTransaction(t, engine)
	got, err := handler.HandleCall(ctx, stopTransactionChargeStationId, stopTransactionRequest(transactionId))

	require.NoError(t, err)
	assert.Equal(t, want, got, "a tracker that panics changes nothing")
	found, err := engine.FindTransaction(ctx, stopTransactionChargeStationId, handlers.ConvertToUUID(transactionId))
	require.NoError(t, err)
	assert.Greater(t, found.EndedSeqNo, 0)
}

func TestStatusNotificationTellsTheTrackerAboutTheStatus(t *testing.T) {
	for name, panics := range map[string]bool{"tracker": false, "tracker that panics": true} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			clk := clockAt(t, "2026-11-27T10:00:00Z")
			engine := inmemory.NewStore(clk)
			tracker := &recordingTracker{store: engine, panics: panics}
			handler := handlers.StatusNotificationHandler{Clock: clk, StatusStore: engine, Tx: engine,
				Events: events.NopEmitter{}, CommandTracker: tracker}

			resp, err := handler.HandleCall(ctx, "cs001", &types.StatusNotificationJson{
				ConnectorId: 2,
				ErrorCode:   types.StatusNotificationJsonErrorCodeNoError,
				Status:      types.StatusNotificationJsonStatusUnavailable,
			})

			require.NoError(t, err)
			assert.Equal(t, &types.StatusNotificationResponseJson{}, resp)
			assert.Equal(t, []string{"status cs001 2 Unavailable"}, tracker.calls)
			assert.Equal(t, []bool{true}, tracker.stored, "once the status is stored")
		})
	}
}

// The handlers work as before without a tracker (Firestore, or no command queue).
func TestTheHandlersWorkWithoutATracker(t *testing.T) {
	ctx := context.Background()
	start, engine := newStartTransactionHandler(t)
	require.Nil(t, start.CommandTracker)
	resp := startTransaction(t, start, startTransactionChargeStationId, startTransactionRequest(startTransactionIdTag))
	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusAccepted, resp.IdTagInfo.Status)

	stop := handlers.StopTransactionHandler{Clock: start.Clock, TokenStore: engine, OrgResolver: start.OrgResolver,
		TransactionStore: engine, Tx: engine, Events: events.NopEmitter{}}
	_, err := stop.HandleCall(ctx, startTransactionChargeStationId, stopTransactionRequest(resp.TransactionId))
	require.NoError(t, err)

	status := handlers.StatusNotificationHandler{Clock: start.Clock, StatusStore: engine, Tx: engine, Events: events.NopEmitter{}}
	_, err = status.HandleCall(ctx, startTransactionChargeStationId, &types.StatusNotificationJson{ConnectorId: 1,
		ErrorCode: types.StatusNotificationJsonErrorCodeNoError, Status: types.StatusNotificationJsonStatusAvailable})
	require.NoError(t, err)
}

// The router gives the tracker to the three handlers that tell it.
func TestNewRouterGivesTheTrackerToTheHandlers(t *testing.T) {
	engine := inmemory.NewStore(clockAt(t, "2026-11-27T10:00:00Z"))
	tracker := &recordingTracker{store: engine}
	router := newRouterWithTracker(t, engine, tracker)

	for action, handler := range map[string]any{
		"StartTransaction":   router.CallRoutes["StartTransaction"].Handler,
		"StopTransaction":    router.CallRoutes["StopTransaction"].Handler,
		"StatusNotification": router.CallRoutes["StatusNotification"].Handler,
	} {
		var got handlers.CommandTracker
		switch h := handler.(type) {
		case handlers.StartTransactionHandler:
			got = h.CommandTracker
		case handlers.StopTransactionHandler:
			got = h.CommandTracker
		case handlers.StatusNotificationHandler:
			got = h.CommandTracker
		}
		assert.Same(t, tracker, got, action)
	}
}
