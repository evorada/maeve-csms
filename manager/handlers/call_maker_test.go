// SPDX-License-Identifier: Apache-2.0

package handlers_test

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/transport"
)

type FakeEmitter struct {
	called          bool
	ocppVersion     transport.OcppVersion
	chargeStationId string
	msg             *transport.Message
}

func (e *FakeEmitter) Emit(_ context.Context, ocppVersion transport.OcppVersion, chargeStationId string, message *transport.Message) error {
	e.called = true
	e.ocppVersion = ocppVersion
	e.chargeStationId = chargeStationId
	e.msg = message
	return nil
}

func TestCallMaker(t *testing.T) {
	emitter := &FakeEmitter{}
	callMaker := &handlers.OcppCallMaker{
		Emitter:     emitter,
		OcppVersion: transport.OcppVersion201,
		Actions: map[reflect.Type]string{
			reflect.TypeOf(&ocpp201.CertificateSignedRequestJson{}): "CertificateSigned",
		},
	}

	certType := ocpp201.CertificateSigningUseEnumTypeV2GCertificate
	certChain := "pemData"
	err := callMaker.Send(context.Background(), "cs001", &ocpp201.CertificateSignedRequestJson{
		CertificateType:  &certType,
		CertificateChain: certChain,
	})
	assert.NoError(t, err)

	uuidPattern := regexp.MustCompile(`^[0-9a-fA-F]{8}\b-[0-9a-fA-F]{4}\b-[0-9a-fA-F]{4}\b-[0-9a-fA-F]{4}\b-[0-9a-fA-F]{12}$`)
	assert.Equal(t, transport.MessageTypeCall, emitter.msg.MessageType)
	assert.Regexp(t, uuidPattern, emitter.msg.MessageId)
	assert.Equal(t, "CertificateSigned", emitter.msg.Action)
	assert.JSONEq(t, `{"certificateType":"V2GCertificate","certificateChain":"pemData"}`, string(emitter.msg.RequestPayload))
}

func TestCallMakerWithUnknownMessageType(t *testing.T) {
	emitter := &FakeEmitter{}
	callMaker := &handlers.OcppCallMaker{
		Emitter:     emitter,
		OcppVersion: transport.OcppVersion201,
		Actions: map[reflect.Type]string{
			reflect.TypeOf(&ocpp201.CertificateSignedRequestJson{}): "CertificateSigned",
		},
	}

	err := callMaker.Send(context.Background(), "cs001", &ocpp201.AuthorizeRequestJson{})
	assert.ErrorContains(t, err, "unknown request type")
	assert.Nil(t, emitter.msg)
}

func TestCallMakerSendRawUsesTheGivenMessageId(t *testing.T) {
	emitter := &FakeEmitter{}
	callMaker := handlers.OcppCallMaker{
		Emitter:     emitter,
		OcppVersion: transport.OcppVersion16,
	}

	payload := json.RawMessage(`{"type":"Soft"}`)
	err := callMaker.SendRaw(context.Background(), "k7f2.CP001", "0193b2c4-0000-7000-8000-000000000001", "Reset", payload)
	require.NoError(t, err)

	assert.Equal(t, transport.OcppVersion16, emitter.ocppVersion)
	assert.Equal(t, "k7f2.CP001", emitter.chargeStationId)
	assert.Equal(t, &transport.Message{
		MessageType:    transport.MessageTypeCall,
		MessageId:      "0193b2c4-0000-7000-8000-000000000001",
		Action:         "Reset",
		RequestPayload: payload,
	}, emitter.msg)
}

func TestCallMakerSendGivesEveryCallANewMessageId(t *testing.T) {
	emitter := &FakeEmitter{}
	callMaker := &handlers.OcppCallMaker{
		Emitter:     emitter,
		OcppVersion: transport.OcppVersion201,
		Actions: map[reflect.Type]string{
			reflect.TypeOf(&ocpp201.CertificateSignedRequestJson{}): "CertificateSigned",
		},
	}

	ids := map[string]bool{}
	for i := 0; i < 3; i++ {
		require.NoError(t, callMaker.Send(context.Background(), "cs001", &ocpp201.CertificateSignedRequestJson{CertificateChain: "pem"}))
		_, err := uuid.Parse(emitter.msg.MessageId)
		require.NoError(t, err)
		ids[emitter.msg.MessageId] = true
	}
	assert.Len(t, ids, 3)
}
