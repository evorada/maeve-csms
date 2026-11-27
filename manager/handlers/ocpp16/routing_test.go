// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"encoding/json"
	"io/fs"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"k8s.io/utils/clock"
)

type FakeEmitter struct {
	got *transport.Message
}

func (e *FakeEmitter) Emit(ctx context.Context, ocppVersion transport.OcppVersion, chargeStationId string, message *transport.Message) error {
	e.got = message
	return nil
}

func TestDataTransferCallMaker(t *testing.T) {
	emitter := &FakeEmitter{}
	callMaker := ocpp16.NewDataTransferCallMaker(emitter)

	certType := ocpp201.CertificateSigningUseEnumTypeV2GCertificate
	certChain := "pemData"
	err := callMaker.Send(context.Background(), "cs001", &ocpp201.CertificateSignedRequestJson{
		CertificateType:  &certType,
		CertificateChain: certChain,
	})
	require.NoError(t, err)

	uuidPattern := regexp.MustCompile(`^[0-9a-fA-F]{8}\b-[0-9a-fA-F]{4}\b-[0-9a-fA-F]{4}\b-[0-9a-fA-F]{4}\b-[0-9a-fA-F]{12}$`)
	assert.Equal(t, transport.MessageTypeCall, emitter.got.MessageType)
	assert.Regexp(t, uuidPattern, emitter.got.MessageId)
	assert.Equal(t, "DataTransfer", emitter.got.Action)
	assert.JSONEq(t, `{"vendorId":"org.openchargealliance.iso15118pnc","messageId":"CertificateSigned","data":"{\"certificateChain\":\"pemData\",\"certificateType\":\"V2GCertificate\"}"}`, string(emitter.got.RequestPayload))
}

func TestDataTransferCallMakerWithUnknownMessageType(t *testing.T) {
	emitter := &FakeEmitter{}
	callMaker := ocpp16.NewDataTransferCallMaker(emitter)

	err := callMaker.Send(context.Background(), "cs001", &ocpp201.AuthorizeRequestJson{})
	assert.ErrorContains(t, err, "unknown request type")
	assert.Nil(t, emitter.got)
}

// TestAllRouteSchemasExist fails when a route refers to a schema file that is missing
// or does not compile. The router validates every message before calling the handler,
// so a wrong schema name silently breaks the whole action.
func TestAllRouteSchemasExist(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})

	router, ok := ocpp16.NewRouter(&FakeEmitter{},
		clock.RealClock{},
		engine,
		&tenancy.Resolver{Stations: engine},
		nil,
		nil,
		nil,
		time.Minute,
		schemas.OcppSchemas).(*handlers.Router)
	require.True(t, ok, "expected NewRouter to return a *handlers.Router")

	// schema path indexed by a description of the route that uses it
	routeSchemas := make(map[string]string)
	var dataTransferCallRoutes, dataTransferCallResultRoutes int

	for action, route := range router.CallRoutes {
		routeSchemas["call "+action+" request"] = route.RequestSchema
		routeSchemas["call "+action+" response"] = route.ResponseSchema

		if dataTransfer, ok := route.Handler.(ocpp16.DataTransferHandler); ok {
			for vendorId, vendorRoutes := range dataTransfer.CallRoutes {
				for messageId, vendorRoute := range vendorRoutes {
					dataTransferCallRoutes++
					routeSchemas["call "+action+" "+vendorId+":"+messageId+" request"] = vendorRoute.RequestSchema
					routeSchemas["call "+action+" "+vendorId+":"+messageId+" response"] = vendorRoute.ResponseSchema
				}
			}
		}
	}

	for action, route := range router.CallResultRoutes {
		routeSchemas["call result "+action+" request"] = route.RequestSchema
		routeSchemas["call result "+action+" response"] = route.ResponseSchema

		if dataTransfer, ok := route.Handler.(ocpp16.DataTransferResultHandler); ok {
			for vendorId, vendorRoutes := range dataTransfer.CallResultRoutes {
				for messageId, vendorRoute := range vendorRoutes {
					dataTransferCallResultRoutes++
					routeSchemas["call result "+action+" "+vendorId+":"+messageId+" request"] = vendorRoute.RequestSchema
					routeSchemas["call result "+action+" "+vendorId+":"+messageId+" response"] = vendorRoute.ResponseSchema
				}
			}
		}
	}

	require.NotEmpty(t, router.CallRoutes)
	require.NotEmpty(t, router.CallResultRoutes)
	require.NotZero(t, dataTransferCallRoutes, "nested DataTransfer call routes were not checked")
	require.NotZero(t, dataTransferCallResultRoutes, "nested DataTransfer call result routes were not checked")

	for route, schemaFile := range routeSchemas {
		_, err := fs.Stat(schemas.OcppSchemas, schemaFile)
		if !assert.NoErrorf(t, err, "%s: schema file %q is missing", route, schemaFile) {
			continue
		}
		_, err = schemas.Compile(schemas.OcppSchemas, schemaFile)
		assert.NoErrorf(t, err, "%s: schema file %q does not compile", route, schemaFile)
	}
}

// TestMeterValuesCallIsStoredThroughTheRouter sends a MeterValues call through the router
// that NewRouter builds, so it also covers the schema validation and the store wiring of the route.
func TestMeterValuesCallIsStoredThroughTheRouter(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	emitter := &FakeEmitter{}

	router := ocpp16.NewRouter(emitter,
		clock.RealClock{},
		engine,
		&tenancy.Resolver{Stations: engine},
		nil,
		nil,
		nil,
		time.Minute,
		schemas.OcppSchemas)

	msg := &transport.Message{
		MessageType: transport.MessageTypeCall,
		Action:      "MeterValues",
		MessageId:   "b5a0e9f4-6a1c-4d2e-9c51-0c2f4b9d7e11",
		RequestPayload: json.RawMessage(`{
			"connectorId": 1,
			"transactionId": 42,
			"meterValue": [
				{
					"timestamp": "2026-11-26T10:05:00Z",
					"sampledValue": [
						{"value": "1500"},
						{"value": "7.4", "context": "Sample.Clock", "measurand": "Power.Active.Import", "location": "Outlet", "unit": "kW", "phase": "L1"}
					]
				}
			]
		}`),
	}

	router.Handle(context.Background(), "cs001", msg)

	require.NotNil(t, emitter.got, "no response was emitted")
	assert.Equal(t, transport.MessageTypeCallResult, emitter.got.MessageType)
	assert.Equal(t, "MeterValues", emitter.got.Action)
	assert.Equal(t, msg.MessageId, emitter.got.MessageId)
	assert.JSONEq(t, `{}`, string(emitter.got.ResponsePayload))

	transactionId := ocpp16.ConvertToUUID(42)
	result, err := engine.QueryMeterValues(context.Background(), store.MeterValuesFilter{
		ChargeStationId: "cs001",
		TransactionId:   &transactionId,
		Limit:           100,
	})
	require.NoError(t, err)

	periodic := "Sample.Periodic"
	clockAligned := "Sample.Clock"
	outlet := "Outlet"
	energy := "Energy.Active.Import.Register"
	power := "Power.Active.Import"
	phase := "L1"
	want := []store.StoredMeterValue{
		{
			ChargeStationId: "cs001",
			EvseId:          1,
			TransactionId:   transactionId,
			MeterValue: store.MeterValue{
				Timestamp: "2026-11-26T10:05:00Z",
				SampledValues: []store.SampledValue{
					{
						Context:       &periodic,
						Location:      &outlet,
						Measurand:     &energy,
						UnitOfMeasure: &store.UnitOfMeasure{Unit: "Wh", Multipler: 0},
						Value:         1500,
					},
					{
						Context:       &clockAligned,
						Location:      &outlet,
						Measurand:     &power,
						Phase:         &phase,
						UnitOfMeasure: &store.UnitOfMeasure{Unit: "kW", Multipler: 0},
						Value:         7.4,
					},
				},
			},
		},
	}
	assert.Equal(t, want, result.MeterValues)
	assert.Equal(t, 1, result.Total)
}

// TestChangeConfigurationCallErrorRejectsSettingThroughTheRouter sends the CallError of a charge
// station through the router that NewRouter builds, so it also covers the wiring of the route's
// error handler.
func TestChangeConfigurationCallErrorRejectsSettingThroughTheRouter(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	emitter := &FakeEmitter{}

	router := ocpp16.NewRouter(emitter,
		clock.RealClock{},
		engine,
		&tenancy.Resolver{Stations: engine},
		nil,
		nil,
		nil,
		time.Minute,
		schemas.OcppSchemas)

	err := engine.UpdateChargeStationSettings(ctx, "cs001", &store.ChargeStationSettings{
		Settings: map[string]*store.ChargeStationSetting{
			"UnknownKey":        {Value: "value", Status: store.ChargeStationSettingStatusPending},
			"HeartbeatInterval": {Value: "300", Status: store.ChargeStationSettingStatusPending},
		},
	})
	require.NoError(t, err)

	router.Handle(ctx, "cs001", &transport.Message{
		MessageType:      transport.MessageTypeCallError,
		Action:           "ChangeConfiguration",
		MessageId:        "5d0f3a1e-7c1b-4f0e-8a44-2f6f6c7f9a10",
		RequestPayload:   json.RawMessage(`{"key":"UnknownKey","value":"value"}`),
		ErrorCode:        transport.ErrorNotSupported,
		ErrorDescription: "Requested Action is recognized but not supported by the receiver",
	})

	assert.Nil(t, emitter.got, "nothing must be sent in reply to a call error")

	settings, err := engine.LookupChargeStationSettings(ctx, "cs001")
	require.NoError(t, err)
	require.Len(t, settings.Settings, 2)
	assert.Equal(t, store.ChargeStationSettingStatusRejected, settings.Settings["UnknownKey"].Status)
	assert.Equal(t, store.ChargeStationSettingStatusPending, settings.Settings["HeartbeatInterval"].Status)
}

// TestTriggerMessageCallErrorStoresStatusThroughTheRouter covers the wiring of the store and
// of the error handler of the TriggerMessage route.
func TestTriggerMessageCallErrorStoresStatusThroughTheRouter(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	emitter := &FakeEmitter{}

	router := ocpp16.NewRouter(emitter,
		clock.RealClock{},
		engine,
		&tenancy.Resolver{Stations: engine},
		nil,
		nil,
		nil,
		time.Minute,
		schemas.OcppSchemas)

	connectorId := 1
	err := engine.SetChargeStationTriggerMessage(ctx, "cs001", &store.ChargeStationTriggerMessage{
		ChargeStationId: "cs001",
		TriggerMessage:  store.TriggerMessageStatusNotification,
		ConnectorId:     &connectorId,
		TriggerStatus:   store.TriggerStatusPending,
	})
	require.NoError(t, err)

	router.Handle(ctx, "cs001", &transport.Message{
		MessageType:      transport.MessageTypeCallError,
		Action:           "TriggerMessage",
		MessageId:        "0c6d5a52-0f0e-4a55-9f6b-0d6f1c3f6f2b",
		RequestPayload:   json.RawMessage(`{"requestedMessage":"StatusNotification","connectorId":1}`),
		ErrorCode:        transport.ErrorNotImplemented,
		ErrorDescription: "Requested Action is not known by receiver",
	})

	assert.Nil(t, emitter.got, "nothing must be sent in reply to a call error")

	got, err := engine.LookupChargeStationTriggerMessage(ctx, "cs001")
	require.NoError(t, err)
	assert.Equal(t, &store.ChargeStationTriggerMessage{
		ChargeStationId: "cs001",
		TriggerMessage:  store.TriggerMessageStatusNotification,
		ConnectorId:     &connectorId,
		TriggerStatus:   store.TriggerStatusNotImplemented,
	}, got)
}
