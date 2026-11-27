// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	clockTest "k8s.io/utils/clock/testing"
)

// capturingEmitter records every message that the router sends to the charge station.
type capturingEmitter struct {
	messages []*transport.Message
}

func (e *capturingEmitter) Emit(_ context.Context, _ transport.OcppVersion, _ string, message *transport.Message) error {
	e.messages = append(e.messages, message)
	return nil
}

// TestOcpp16TransactionFlow is the OCPP 1.6 end-to-end check of a charging session:
// the messages of a station go through the router that NewRouter builds (schema
// validation, routing and store wiring included) and the test then reads what the
// store holds. The Everest E2E test runs OCPP 2.0.1.
func TestOcpp16TransactionFlow(t *testing.T) {
	const chargeStationId = "cs001"
	const idTag = "MYRFIDTAG"
	const reservationId = 7
	ctx := context.Background()

	// the clock of the CSMS is not the clock of the station
	csmsTime := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	clk := clockTest.NewFakePassiveClock(csmsTime)
	engine := inmemory.NewStore(clk)

	groupId := "FLEET1"
	require.NoError(t, engine.SetToken(ctx, &store.Token{
		OrgId:       store.LegacyOrgId,
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         idTag,
		ContractId:  "GBTWK012345678V",
		Issuer:      "Thoughtworks",
		GroupId:     &groupId,
		Valid:       true,
		CacheMode:   "NEVER",
	}))
	require.NoError(t, engine.CreateReservation(ctx, &store.Reservation{
		ReservationId:   reservationId,
		ChargeStationId: chargeStationId,
		ConnectorId:     1,
		IdTag:           idTag,
		ExpiryDate:      time.Date(2026, 11, 26, 11, 0, 0, 0, time.UTC),
		Status:          store.ReservationStatusAccepted,
		CreatedAt:       time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC),
	}))

	emitter := &capturingEmitter{}
	router := ocpp16.NewRouter(emitter,
		clk,
		engine,
		&tenancy.Resolver{Stations: engine},
		nil,
		nil,
		nil,
		nil,
		time.Minute,
		schemas.OcppSchemas)

	// call sends a call of the station and returns the payload of the CallResult
	messageNo := 0
	call := func(action, payload string) json.RawMessage {
		t.Helper()

		messageNo++
		msg := &transport.Message{
			MessageType:    transport.MessageTypeCall,
			Action:         action,
			MessageId:      fmt.Sprintf("message-%d", messageNo),
			RequestPayload: json.RawMessage(payload),
		}
		router.Handle(ctx, chargeStationId, msg)

		require.Len(t, emitter.messages, messageNo, "%s: expected one reply", action)
		reply := emitter.messages[messageNo-1]
		require.Equalf(t, transport.MessageTypeCallResult, reply.MessageType,
			"%s: reply is not a CallResult: %s %s", action, reply.ErrorCode, reply.ErrorDescription)
		assert.Equal(t, action, reply.Action)
		assert.Equal(t, msg.MessageId, reply.MessageId)
		// the router only logs a reply that does not match its schema
		require.NoErrorf(t, schemas.Validate(reply.ResponsePayload, schemas.OcppSchemas, "ocpp16/"+action+"Response.json"),
			"%s: reply does not match its schema: %s", action, reply.ResponsePayload)
		return reply.ResponsePayload
	}

	boot := call("BootNotification", `{"chargePointVendor": "Evorada", "chargePointModel": "AC22", "firmwareVersion": "1.4.2"}`)
	assert.JSONEq(t, `{"currentTime": "2026-11-26T12:00:00Z", "interval": 60, "status": "Accepted"}`, string(boot))

	authorize := call("Authorize", `{"idTag": "MYRFIDTAG"}`)
	// the token must not be cached: it expires at the time of the CSMS
	const idTagInfo = `{"status": "Accepted", "expiryDate": "2026-11-26T12:00:00Z", "parentIdTag": "FLEET1"}`
	assert.JSONEq(t, `{"idTagInfo": `+idTagInfo+`}`, string(authorize))

	start := call("StartTransaction", `{
		"connectorId": 1,
		"idTag": "MYRFIDTAG",
		"meterStart": 1000,
		"reservationId": 7,
		"timestamp": "2026-11-26T10:00:00Z"
	}`)
	var startResponse struct {
		TransactionId *int            `json:"transactionId"`
		IdTagInfo     json.RawMessage `json:"idTagInfo"`
	}
	require.NoError(t, json.Unmarshal(start, &startResponse))
	assert.JSONEq(t, idTagInfo, string(startResponse.IdTagInfo))
	require.NotNil(t, startResponse.TransactionId)
	transactionId := *startResponse.TransactionId
	assert.Equal(t, 1, transactionId, "the first id of the sequence of a new store")

	meterValues := call("MeterValues", fmt.Sprintf(`{
		"connectorId": 1,
		"transactionId": %d,
		"meterValue": [
			{
				"timestamp": "2026-11-26T10:15:00Z",
				"sampledValue": [
					{"value": "1800"},
					{"value": "7.4", "measurand": "Power.Active.Import", "unit": "kW"}
				]
			}
		]
	}`, transactionId))
	assert.JSONEq(t, `{}`, string(meterValues))

	stop := call("StopTransaction", fmt.Sprintf(`{
		"transactionId": %d,
		"idTag": "MYRFIDTAG",
		"meterStop": 2500,
		"reason": "EVDisconnected",
		"timestamp": "2026-11-26T10:30:00Z",
		"transactionData": [
			{
				"timestamp": "2026-11-26T10:29:00Z",
				"sampledValue": [
					{"value": "2450", "context": "Sample.Periodic", "measurand": "Energy.Active.Import.Register", "unit": "Wh"}
				]
			}
		]
	}`, transactionId))
	assert.JSONEq(t, `{"idTagInfo": `+idTagInfo+`}`, string(stop))

	// BootNotification
	runtimeDetails, err := engine.LookupChargeStationRuntimeDetails(ctx, chargeStationId)
	require.NoError(t, err)
	require.NotNil(t, runtimeDetails)
	assert.Equal(t, "1.6", runtimeDetails.OcppVersion)

	// StartTransaction and StopTransaction: the complete transaction
	transactionUuid := ocpp16.ConvertToUUID(transactionId)
	transaction, err := engine.FindTransaction(ctx, chargeStationId, transactionUuid)
	require.NoError(t, err)

	startedAt := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	stoppedAt := time.Date(2026, 11, 26, 10, 30, 0, 0, time.UTC)
	connectorId := 1
	meterStart := int64(1000)
	meterStop := int64(2500)
	energy := int64(1500)
	stopReason := "EVDisconnected"
	reservation := reservationId
	wantTransaction := &store.Transaction{
		OrgId:           store.LegacyOrgId, // the charge station is not registered
		ChargeStationId: chargeStationId,
		TransactionId:   transactionUuid,
		IdToken:         idTag,
		TokenType:       "ISO14443",
		MeterValues: []store.MeterValue{
			beginMeterValue("2026-11-26T10:00:00Z", 1000),
			{
				Timestamp:     "2026-11-26T10:29:00Z",
				SampledValues: []store.SampledValue{transactionDataSample("Sample.Periodic", 2450, "Wh")},
			},
			endMeterValue("2026-11-26T10:30:00Z", 2500),
		},
		EndedSeqNo:    1,
		ConnectorId:   &connectorId,
		EvseId:        &connectorId,
		StartedAt:     &startedAt,
		StoppedAt:     &stoppedAt,
		MeterStartWh:  &meterStart,
		MeterStopWh:   &meterStop,
		EnergyWh:      &energy,
		StopReason:    &stopReason,
		IdTokenGroup:  &groupId,
		ReservationId: &reservation,
	}
	assert.Equal(t, wantTransaction, transaction)

	// the reservation that the transaction consumed
	storedReservation, err := engine.GetReservation(ctx, reservationId)
	require.NoError(t, err)
	require.NotNil(t, storedReservation)
	assert.Equal(t, store.ReservationStatusUsed, storedReservation.Status)

	// MeterValues: the samples of the transaction
	result, err := engine.QueryMeterValues(ctx, store.MeterValuesFilter{
		ChargeStationId: chargeStationId,
		TransactionId:   &transactionUuid,
		Limit:           100,
	})
	require.NoError(t, err)

	powerSample := transactionDataSample("Sample.Periodic", 7.4, "kW")
	power := "Power.Active.Import"
	powerSample.Measurand = &power
	wantMeterValues := []store.StoredMeterValue{
		{
			ChargeStationId: chargeStationId,
			EvseId:          1,
			TransactionId:   transactionUuid,
			MeterValue: store.MeterValue{
				Timestamp: "2026-11-26T10:15:00Z",
				SampledValues: []store.SampledValue{
					transactionDataSample("Sample.Periodic", 1800, "Wh"),
					powerSample,
				},
			},
		},
	}
	assert.Equal(t, wantMeterValues, result.MeterValues)
	assert.Equal(t, 1, result.Total)
}
