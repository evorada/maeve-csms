// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
)

type vendorPingResultHandler struct {
	called       bool
	requestValue string
	responseEcho string
}

func (h *vendorPingResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	h.called = true
	req := request.(*vendorPingRequest)
	resp := response.(*vendorPingResponse)
	h.requestValue = req.Value
	h.responseEcho = resp.Echo
	return nil
}

func TestDataTransferResultHandlerForKnownVendorAndMessage(t *testing.T) {
	schemaFS := fstest.MapFS{
		"request.json":  {Data: []byte(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)},
		"response.json": {Data: []byte(`{"type":"object","properties":{"echo":{"type":"string"}},"required":["echo"]}`)},
	}

	result := &vendorPingResultHandler{}
	handler := ocpp201.DataTransferResultHandler{
		SchemaFS: schemaFS,
		CallResultRoutes: map[string]map[string]handlers.CallResultRoute{
			"acme": {
				"Ping": {
					NewRequest:     func() ocpp.Request { return &vendorPingRequest{} },
					NewResponse:    func() ocpp.Response { return &vendorPingResponse{} },
					RequestSchema:  "request.json",
					ResponseSchema: "response.json",
					Handler:        result,
				},
			},
		},
	}

	messageID := "Ping"
	request := &types.DataTransferRequestJson{
		VendorId:  "acme",
		MessageId: &messageID,
		Data:      []byte(`{"value":"hello"}`),
	}
	response := &types.DataTransferResponseJson{
		Status: types.DataTransferStatusEnumTypeAccepted,
		Data:   []byte(`{"echo":"hello"}`),
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	require.NoError(t, err)
	require.True(t, result.called)
	require.Equal(t, "hello", result.requestValue)
	require.Equal(t, "hello", result.responseEcho)
}
