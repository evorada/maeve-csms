// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"

	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type DataTransferHandler struct {
	CallRoutes map[string]map[string]handlers.CallRoute
	SchemaFS   fs.FS
}

func (d DataTransferHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	req := request.(*types.DataTransferRequestJson)
	messageID := ""
	if req.MessageId != nil {
		messageID = *req.MessageId
	}

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("data_transfer.vendor_id", req.VendorId))
	if messageID != "" {
		span.SetAttributes(attribute.String("data_transfer.message_id", messageID))
	}

	vendorMap, ok := d.CallRoutes[req.VendorId]
	if !ok {
		span.SetAttributes(attribute.String("data_transfer.status", string(types.DataTransferStatusEnumTypeUnknownVendorId)))
		return &types.DataTransferResponseJson{Status: types.DataTransferStatusEnumTypeUnknownVendorId}, nil
	}

	route, ok := vendorMap[messageID]
	if !ok {
		span.SetAttributes(attribute.String("data_transfer.status", string(types.DataTransferStatusEnumTypeUnknownMessageId)))
		return &types.DataTransferResponseJson{Status: types.DataTransferStatusEnumTypeUnknownMessageId}, nil
	}

	var dataTransferRequest ocpp.Request
	if len(req.Data) > 0 {
		if err := schemas.Validate(req.Data, d.SchemaFS, route.RequestSchema); err != nil {
			return nil, fmt.Errorf("validating %s:%s data transfer request data: %w", req.VendorId, messageID, err)
		}
		dataTransferRequest = route.NewRequest()
		if err := json.Unmarshal(req.Data, dataTransferRequest); err != nil {
			return nil, fmt.Errorf("unmarshalling %s:%s data transfer request data: %w", req.VendorId, messageID, err)
		}
	}

	dataTransferResponse, err := route.Handler.HandleCall(ctx, chargeStationId, dataTransferRequest)
	if err != nil {
		return nil, err
	}

	var responseData json.RawMessage
	if dataTransferResponse != nil {
		b, err := json.Marshal(dataTransferResponse)
		if err != nil {
			return nil, fmt.Errorf("marshalling %s:%s data transfer response data: %w", req.VendorId, messageID, err)
		}
		if err := schemas.Validate(b, d.SchemaFS, route.ResponseSchema); err != nil {
			span.SetAttributes(attribute.String("data_transfer.invalid_response", err.Error()))
		}
		responseData = b
	}

	span.SetAttributes(attribute.String("data_transfer.status", string(types.DataTransferStatusEnumTypeAccepted)))
	return &types.DataTransferResponseJson{Status: types.DataTransferStatusEnumTypeAccepted, Data: responseData}, nil
}
