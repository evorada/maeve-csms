// SPDX-License-Identifier: Apache-2.0

package commands_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	handlers16 "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/transport"
)

const flowIdTag = "DEADBEEF"

// addToken stores the idTag of the flow tests as a valid token of the station's org.
func (f *fixture) addToken() {
	f.t.Helper()
	require.NoError(f.t, f.store.SetToken(context.Background(), &store.Token{
		OrgId: store.LegacyOrgId, CountryCode: "GB", PartyId: "TWK", Type: "RFID", Uid: flowIdTag,
		ContractId: "GBTWK012345678V", Issuer: "Thoughtworks", Valid: true, CacheMode: "ALWAYS",
	}))
}

// call has the router handle a call of the station and returns the payload of the
// CallResult that the station gets.
func call(t *testing.T, f *fixture, router transport.MessageHandler, action, messageId, payload string) json.RawMessage {
	t.Helper()
	router.Handle(context.Background(), testStation, &transport.Message{MessageType: transport.MessageTypeCall,
		Action: action, MessageId: messageId, RequestPayload: json.RawMessage(payload)})
	for _, response := range f.responses() {
		if response.MessageId == messageId {
			require.Equal(t, transport.MessageTypeCallResult, response.MessageType, "%s: %s", action, response.ErrorDescription)
			return response.ResponsePayload
		}
	}
	require.Failf(t, "no answer", "%s %s", action, messageId)
	return nil
}

// answerThroughRouter has the station answer a sent command through the router.
func answerThroughRouter(t *testing.T, f *fixture, router transport.MessageHandler, cmd *store.Command, response string) {
	t.Helper()
	sent := f.waitStatus(cmd.Id, store.CommandStatusSent)
	router.Handle(context.Background(), testStation, result(sent, response))
	f.waitStatus(cmd.Id, store.CommandStatusAccepted)
}

func TestARemoteStartAndStopAreFollowedThroughTheRouter(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.addToken()
	router := f.router()

	start := f.enqueue("RemoteStartTransaction", `{"idTag":"DEADBEEF","connectorId":1}`)
	answerThroughRouter(t, f, router, start, `{"status":"Accepted"}`)
	assert.Equal(t, commands.ProgressAwaitingTransaction, f.progress(start.Id).State)

	call(t, f, router, "StatusNotification", "sn-1", `{"connectorId":1,"errorCode":"NoError","status":"Preparing"}`)
	answer := call(t, f, router, "StartTransaction", "st-1",
		`{"connectorId":1,"idTag":"DEADBEEF","meterStart":100,"timestamp":"2026-11-27T12:00:05Z"}`)

	var started struct {
		TransactionId int `json:"transactionId"`
		IdTagInfo     struct {
			Status string `json:"status"`
		} `json:"idTagInfo"`
	}
	require.NoError(t, json.Unmarshal(answer, &started))
	assert.Equal(t, "Accepted", started.IdTagInfo.Status)
	transactionId := ocpp16.ConvertToUUID(started.TransactionId)
	transaction, err := f.store.FindTransaction(ctx, testStation, transactionId)
	require.NoError(t, err)
	require.NotNil(t, transaction, "the transaction is stored as before")

	assert.Equal(t, &commands.Progress{State: commands.ProgressTransactionStarted, TransactionId: transactionId,
		OcppTransactionId: ptr(started.TransactionId), ConnectorId: ptr(1)}, f.progress(start.Id),
		"the remote start shows the transaction it began")

	// the API calls the stop with the transaction id of the progress
	stopPayload := `{"transactionId":` + strconv.Itoa(*f.progress(start.Id).OcppTransactionId) + `}`
	stop := f.enqueue("RemoteStopTransaction", stopPayload)
	answerThroughRouter(t, f, router, stop, `{"status":"Accepted"}`)
	assert.Equal(t, commands.ProgressAwaitingStop, f.progress(stop.Id).State)

	call(t, f, router, "StopTransaction", "sp-1", `{"transactionId":`+strconv.Itoa(started.TransactionId)+
		`,"meterStop":2100,"timestamp":"2026-11-27T12:10:00Z","reason":"Remote"}`)

	assert.Equal(t, &commands.Progress{State: commands.ProgressTransactionStopped, OcppTransactionId: ptr(started.TransactionId),
		StopReason: "Remote"}, f.progress(stop.Id))
	transaction, err = f.store.FindTransaction(ctx, testStation, transactionId)
	require.NoError(t, err)
	assert.Greater(t, transaction.EndedSeqNo, 0, "the transaction is ended as before")
	assert.Equal(t, commands.ProgressTransactionStarted, f.progress(start.Id).State, "the remote start keeps its transaction")

	var progress []string
	for _, ev := range f.progressEvents() {
		data := eventstest.Data[events.CommandProgressData](t, ev)
		var p commands.Progress
		require.NoError(t, json.Unmarshal(data.Progress, &p))
		progress = append(progress, data.Action+" "+string(p.State))
	}
	assert.Equal(t, []string{
		"RemoteStartTransaction awaiting_transaction",
		"RemoteStartTransaction transaction_started",
		"RemoteStopTransaction awaiting_stop",
		"RemoteStopTransaction transaction_stopped",
	}, progress, "one command.progress event per step, in order")
}

func TestAChangeOfAvailabilityIsFollowedThroughTheRouter(t *testing.T) {
	f := newFixture(t)
	router := f.router()
	cmd := f.enqueue("ChangeAvailability", `{"connectorId":1,"type":"Inoperative"}`)
	answerThroughRouter(t, f, router, cmd, `{"status":"Accepted"}`)
	assert.Equal(t, commands.ProgressAwaitingStatus, f.progress(cmd.Id).State)

	call(t, f, router, "StatusNotification", "sn-1", `{"connectorId":1,"errorCode":"NoError","status":"Unavailable"}`)

	assert.Equal(t, &commands.Progress{State: commands.ProgressApplied, ConnectorId: ptr(1), ConnectorStatus: "Unavailable"},
		f.progress(cmd.Id))
	status, err := f.store.GetConnectorStatus(context.Background(), testStation, 1)
	require.NoError(t, err)
	assert.Equal(t, store.ConnectorStatusUnavailable, status.Status, "the status is stored as before")
}

// routerWithTracker is the OCPP 1.6 router of f with another command tracker.
func (f *fixture) routerWithTracker(tracker handlers16.CommandTracker) transport.MessageHandler {
	return handlers16.NewRouter(f.emitter, f.clock, f.store, events.NopEmitter{}, &tenancy.Resolver{Stations: f.store},
		nil, nil, nil, nil, time.Minute, schemas.OcppSchemas, f.dispatcher, tracker)
}

// stationAnswers sends a StartTransaction, a StatusNotification and a StopTransaction
// through a router of a new fixture and returns the answers of the router.
func stationAnswers(t *testing.T, tracker func(f *fixture) handlers16.CommandTracker) (f *fixture, answers []string) {
	t.Helper()
	f = newFixture(t)
	f.addToken()
	waiting := f.answered("RemoteStartTransaction", `{"idTag":"CAFEBABE","connectorId":1}`, `{"status":"Accepted"}`)
	f.progressEvents()
	router := f.routerWithTracker(tracker(f))

	for _, msg := range []struct{ action, payload string }{
		{"StartTransaction", `{"connectorId":1,"idTag":"DEADBEEF","meterStart":100,"timestamp":"2026-11-27T12:00:05Z"}`},
		{"StatusNotification", `{"connectorId":1,"errorCode":"NoError","status":"Charging"}`},
		{"StopTransaction", `{"transactionId":1,"meterStop":2100,"timestamp":"2026-11-27T12:10:00Z","reason":"Local","idTag":"DEADBEEF"}`},
	} {
		answers = append(answers, string(call(t, f, router, msg.action, msg.action+"-1", msg.payload)))
	}

	transaction, err := f.store.FindTransaction(context.Background(), testStation, ocpp16.ConvertToUUID(1))
	require.NoError(t, err)
	require.NotNil(t, transaction)
	assert.Greater(t, transaction.EndedSeqNo, 0)
	assert.Equal(t, commands.ProgressAwaitingTransaction, f.progress(waiting.Id).State,
		"the remote start of another idTag stays waiting")
	f.assertNoProgressEvent()
	return f, answers
}

func TestTheStationGetsTheSameAnswersWhateverTheTracker(t *testing.T) {
	_, without := stationAnswers(t, func(*fixture) handlers16.CommandTracker { return nil })
	require.Len(t, without, 3)
	assert.JSONEq(t, `{"idTagInfo":{"status":"Accepted"},"transactionId":1}`, without[0])

	t.Run("a start that matches no remote start", func(t *testing.T) {
		_, with := stationAnswers(t, func(f *fixture) handlers16.CommandTracker { return f.tracker })
		assert.Equal(t, without, with)
	})

	t.Run("a tracker whose store fails", func(t *testing.T) {
		_, broken := stationAnswers(t, func(f *fixture) handlers16.CommandTracker {
			return &commands.Tracker{Store: brokenProgressStore{f.store}, Tx: f.events.Tx, Events: f.events.Events,
				Clock: f.clock, Config: f.tracker.Config}
		})
		assert.Equal(t, without, broken)
	})

	t.Run("a tracker whose progress write fails", func(t *testing.T) {
		f := newFixture(t)
		f.addToken()
		start := f.answered("RemoteStartTransaction", `{"idTag":"DEADBEEF","connectorId":1}`, `{"status":"Accepted"}`)
		router := f.routerWithTracker(&commands.Tracker{Store: progressFailingStore{f.store}, Tx: f.events.Tx,
			Events: f.events.Events, Clock: f.clock, Config: f.tracker.Config})

		answer := call(t, f, router, "StartTransaction", "st-1",
			`{"connectorId":1,"idTag":"DEADBEEF","meterStart":100,"timestamp":"2026-11-27T12:00:05Z"}`)

		assert.JSONEq(t, without[0], string(answer))
		assert.Equal(t, commands.ProgressAwaitingTransaction, f.progress(start.Id).State)
	})
}

// progressFailingStore fails the progress writes of the commands.
type progressFailingStore struct {
	store.CommandStore
}

func (progressFailingStore) SetCommandProgress(context.Context, string, json.RawMessage, time.Time, *store.OutboxEvent) (*store.Command, error) {
	return nil, errCommandStore
}
