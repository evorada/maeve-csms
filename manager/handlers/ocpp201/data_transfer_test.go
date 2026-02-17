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

type vendorPingRequest struct {
	Value string `json:"value"`
}

func (*vendorPingRequest) IsRequest() {}

type vendorPingResponse struct {
	Echo string `json:"echo"`
}

func (*vendorPingResponse) IsResponse() {}

type vendorPingCallHandler struct{}

func (vendorPingCallHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	req := request.(*vendorPingRequest)
	return &vendorPingResponse{Echo: req.Value}, nil
}

func TestDataTransferHandlerForKnownVendorAndMessage(t *testing.T) {
	schemaFS := fstest.MapFS{
		"request.json":  {Data: []byte(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)},
		"response.json": {Data: []byte(`{"type":"object","properties":{"echo":{"type":"string"}},"required":["echo"]}`)},
	}

	handler := ocpp201.DataTransferHandler{
		SchemaFS: schemaFS,
		CallRoutes: map[string]map[string]handlers.CallRoute{
			"acme": {
				"Ping": {
					NewRequest:     func() ocpp.Request { return &vendorPingRequest{} },
					RequestSchema:  "request.json",
					ResponseSchema: "response.json",
					Handler:        vendorPingCallHandler{},
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

	response, err := handler.HandleCall(context.Background(), "cs001", request)
	require.NoError(t, err)

	resp := response.(*types.DataTransferResponseJson)
	require.Equal(t, types.DataTransferStatusEnumTypeAccepted, resp.Status)
	require.JSONEq(t, `{"echo":"hello"}`, string(resp.Data))
}

func TestDataTransferHandlerUnknownVendor(t *testing.T) {
	handler := ocpp201.DataTransferHandler{CallRoutes: map[string]map[string]handlers.CallRoute{}}

	request := &types.DataTransferRequestJson{VendorId: "unknown"}
	response, err := handler.HandleCall(context.Background(), "cs001", request)
	require.NoError(t, err)
	require.Equal(t, types.DataTransferStatusEnumTypeUnknownVendorId, response.(*types.DataTransferResponseJson).Status)
}

func TestDataTransferHandlerUnknownMessage(t *testing.T) {
	handler := ocpp201.DataTransferHandler{
		CallRoutes: map[string]map[string]handlers.CallRoute{
			"acme": {
				"Known": {
					NewRequest:     func() ocpp.Request { return &vendorPingRequest{} },
					RequestSchema:  "request.json",
					ResponseSchema: "response.json",
					Handler:        vendorPingCallHandler{},
				},
			},
		},
	}

	messageID := "Unknown"
	request := &types.DataTransferRequestJson{VendorId: "acme", MessageId: &messageID}
	response, err := handler.HandleCall(context.Background(), "cs001", request)
	require.NoError(t, err)
	require.Equal(t, types.DataTransferStatusEnumTypeUnknownMessageId, response.(*types.DataTransferResponseJson).Status)
}
