// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/go-chi/render"
)

func (s *Server) GetChargeStationStatus(w http.ResponseWriter, r *http.Request, csId string) {
	status, err := s.store.GetChargeStationStatus(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	response := &ChargeStationStatusResponse{
		Id:        csId,
		Connected: status.Connected,
	}

	if status.LastHeartbeat != nil {
		response.LastHeartbeat = status.LastHeartbeat
	}

	if status.FirmwareVersion != nil {
		response.FirmwareVersion = status.FirmwareVersion
	}

	if status.Model != nil {
		response.Model = status.Model
	}

	if status.Vendor != nil {
		response.Vendor = status.Vendor
	}

	if status.SerialNumber != nil {
		response.SerialNumber = status.SerialNumber
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = render.Render(w, r, response)
}

func (s *Server) GetConnectorStatuses(w http.ResponseWriter, r *http.Request, csId string) {
	connectorStatuses, err := s.store.ListConnectorStatuses(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	response := make([]ConnectorStatusResponse, 0, len(connectorStatuses))
	for _, cs := range connectorStatuses {
		connectorResponse := ConnectorStatusResponse{
			ConnectorId: int32(cs.ConnectorId),
			Status:      ConnectorStatusResponseStatus(cs.Status),
			ErrorCode:   ConnectorStatusResponseErrorCode(cs.ErrorCode),
		}

		if cs.Info != nil {
			connectorResponse.Info = cs.Info
		}

		if cs.Timestamp != nil {
			connectorResponse.Timestamp = cs.Timestamp
		}

		if cs.CurrentTransactionId != nil {
			connectorResponse.CurrentTransaction = cs.CurrentTransactionId
		}

		response = append(response, connectorResponse)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = render.RenderList(w, r, toRendererList(response))
}

// Render implementations

func (s Status) Render(_ http.ResponseWriter, _ *http.Request) error {
	return nil
}

// Render implementations

func (c ChargeStationStatusResponse) Render(_ http.ResponseWriter, _ *http.Request) error {
	return nil
}

// Render implementations

func (c ConnectorStatusResponse) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}
