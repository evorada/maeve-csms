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

type DataTransferResultHandler struct {
	SchemaFS         fs.FS
	CallResultRoutes map[string]map[string]handlers.CallResultRoute
}

func (d DataTransferResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.DataTransferRequestJson)
	resp := response.(*types.DataTransferResponseJson)

	messageID := ""
	if req.MessageId != nil {
		messageID = *req.MessageId
	}

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("data_transfer.vendor_id", req.VendorId))
	if messageID != "" {
		span.SetAttributes(attribute.String("data_transfer.message_id", messageID))
	}
	span.SetAttributes(attribute.String("data_transfer.status", string(resp.Status)))

	vendorMap, ok := d.CallResultRoutes[req.VendorId]
	if !ok {
		return fmt.Errorf("unknown data transfer result vendor: %s", req.VendorId)
	}
	route, ok := vendorMap[messageID]
	if !ok {
		return fmt.Errorf("unknown data transfer result message id: %s", messageID)
	}

	var dataTransferRequest ocpp.Request
	if len(req.Data) > 0 {
		if err := schemas.Validate(req.Data, d.SchemaFS, route.RequestSchema); err != nil {
			return fmt.Errorf("validating %s:%s data transfer result request data: %w", req.VendorId, messageID, err)
		}
		dataTransferRequest = route.NewRequest()
		if err := json.Unmarshal(req.Data, dataTransferRequest); err != nil {
			return fmt.Errorf("unmarshalling %s:%s data transfer request data: %w", req.VendorId, messageID, err)
		}
	}

	var dataTransferResponse ocpp.Response
	if len(resp.Data) > 0 {
		if err := schemas.Validate(resp.Data, d.SchemaFS, route.ResponseSchema); err != nil {
			return fmt.Errorf("validating %s:%s data transfer result response data: %w", req.VendorId, messageID, err)
		}
		dataTransferResponse = route.NewResponse()
		if err := json.Unmarshal(resp.Data, dataTransferResponse); err != nil {
			return fmt.Errorf("unmarshalling %s:%s data transfer response data: %w", req.VendorId, messageID, err)
		}
	}

	return route.Handler.HandleCallResult(ctx, chargeStationId, dataTransferRequest, dataTransferResponse, state)
}
