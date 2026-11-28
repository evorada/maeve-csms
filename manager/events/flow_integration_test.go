// SPDX-License-Identifier: Apache-2.0

package events_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	clocktest "k8s.io/utils/clock/testing"
)

// replies records the messages that the router sends to the charge station.
type replies struct {
	messages []*transport.Message
}

func (r *replies) Emit(_ context.Context, _ transport.OcppVersion, _ string, message *transport.Message) error {
	r.messages = append(r.messages, message)
	return nil
}

// The events of a charging session go from the OCPP 1.6 messages of a station through
// the outbox to the stream, in order, with the org of the station.
func TestOcpp16SessionEventsReachTheStream(t *testing.T) {
	const stationKey = "k7f2.CP001"
	ctx := context.Background()
	js := newJetStream(t, true)
	require.NoError(t, events.EnsureStream(ctx, js, testStreamConfig()))

	clk := clocktest.NewFakePassiveClock(testNow)
	engine := inmemory.NewStore(clk)
	require.NoError(t, engine.SetChargeStationAuth(ctx, stationKey, &store.ChargeStationAuth{OrgId: testOrgId}))
	require.NoError(t, engine.SetToken(ctx, &store.Token{
		OrgId: testOrgId, CountryCode: "GB", PartyId: "TWK", Type: "RFID", Uid: "MYRFIDTAG",
		ContractId: "GBTWK012345678V", Issuer: "Thoughtworks", Valid: true, CacheMode: "NEVER",
	}))
	orgs := &tenancy.Resolver{Stations: engine, Tenants: engine}
	emitter := &events.OutboxEmitter{Store: engine, Orgs: orgs, Clock: clk, Product: events.Product}

	sent := &replies{}
	router := ocpp16.NewRouter(sent, clk, engine, emitter, orgs, nil, nil, nil, nil, time.Minute, schemas.OcppSchemas, nil, nil, nil)
	call := func(action, payload string) json.RawMessage {
		t.Helper()
		msg := &transport.Message{
			MessageType:    transport.MessageTypeCall,
			Action:         action,
			MessageId:      fmt.Sprintf("message-%d", len(sent.messages)+1),
			RequestPayload: json.RawMessage(payload),
		}
		router.Handle(ctx, stationKey, msg)
		require.NotEmpty(t, sent.messages)
		reply := sent.messages[len(sent.messages)-1]
		require.Equalf(t, transport.MessageTypeCallResult, reply.MessageType, "%s: %s %s", action, reply.ErrorCode, reply.ErrorDescription)
		return reply.ResponsePayload
	}

	call("BootNotification", `{"chargePointVendor": "Acme", "chargePointModel": "AC22"}`)
	call("StatusNotification", `{"connectorId": 1, "errorCode": "NoError", "status": "Preparing"}`)
	start := call("StartTransaction", `{"connectorId": 1, "idTag": "MYRFIDTAG", "meterStart": 1000, "timestamp": "2026-11-27T10:00:00Z"}`)
	var started struct {
		TransactionId int `json:"transactionId"`
	}
	require.NoError(t, json.Unmarshal(start, &started))
	call("MeterValues", fmt.Sprintf(`{"connectorId": 1, "transactionId": %d, "meterValue": [
		{"timestamp": "2026-11-27T10:15:00Z", "sampledValue": [{"value": "1800"}]}
	]}`, started.TransactionId))
	call("StopTransaction", fmt.Sprintf(`{"transactionId": %d, "meterStop": 2500, "timestamp": "2026-11-27T10:30:00Z"}`,
		started.TransactionId))

	relay := newRelay(engine, &events.JetStreamPublisher{JS: js})
	require.NoError(t, relay.RunOnce(ctx))

	wantTypes := []string{"connector.status_changed", "session.started", "session.updated", "session.completed"}
	stream, err := js.Stream(ctx, events.DefaultStreamName)
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(len(wantTypes)), info.State.Msgs, "one event per change, none for the boot")
	msgs := receive(t, js, len(wantTypes))
	require.Len(t, msgs, len(wantTypes))
	transactionUuid := ocpp16.ConvertToUUID(started.TransactionId)
	for i, msg := range msgs {
		var ce events.CloudEvent
		require.NoError(t, json.Unmarshal(msg.Data(), &ce))
		assert.Equal(t, "cpms."+wantTypes[i], ce.Type)
		assert.Equal(t, "cpms."+wantTypes[i]+"."+testOrgId, msg.Subject())
		assert.Equal(t, testOrgId, ce.OrgId, "the org of the station")
		assert.Equal(t, "station:"+stationKey, ce.Actor)
		assert.Equal(t, ce.Id, msg.Headers().Get(jetstream.MsgIDHeader))
		if i == 0 {
			assert.Equal(t, "urn:cpms:"+testOrgId+":connector/"+stationKey+"#1", ce.Subject)
		} else {
			assert.Equal(t, "urn:cpms:"+testOrgId+":session/"+transactionUuid, ce.Subject)
		}
	}

	var completed events.SessionCompletedData
	var lastCe events.CloudEvent
	require.NoError(t, json.Unmarshal(msgs[3].Data(), &lastCe))
	require.NoError(t, json.Unmarshal(lastCe.Data, &completed))
	require.NotNil(t, completed.EnergyWh)
	assert.Equal(t, int64(1500), *completed.EnergyWh)
	assert.Equal(t, "Local", completed.StopReason)
	assert.Equal(t, "CP001", completed.StationId)
}
