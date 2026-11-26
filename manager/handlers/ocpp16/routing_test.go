// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
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
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
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
