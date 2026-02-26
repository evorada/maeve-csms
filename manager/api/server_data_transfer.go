// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Server) SendDataTransfer(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(DataTransferRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	dataTransfer := &store.ChargeStationDataTransfer{
		ChargeStationId: csId,
		VendorId:        req.VendorId,
		MessageId:       req.MessageId,
		Data:            req.Data,
		Status:          store.DataTransferStatusPending,
		SendAfter:       s.clock.Now(),
	}

	err := s.store.SetChargeStationDataTransfer(r.Context(), csId, dataTransfer)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	// Return accepted immediately; the actual transfer happens asynchronously
	acceptedStatus := DataTransferResponseStatusAccepted
	resp := &DataTransferResponse{
		Status: &acceptedStatus,
	}

	w.WriteHeader(http.StatusAccepted)
	_ = render.Render(w, r, resp)
}

func (s *Server) ClearAuthorizationCache(w http.ResponseWriter, r *http.Request, csId string) {
	clearCache := &store.ChargeStationClearCache{
		ChargeStationId: csId,
		Status:          store.ClearCacheStatusPending,
		SendAfter:       s.clock.Now(),
	}

	err := s.store.SetChargeStationClearCache(r.Context(), csId, clearCache)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// Render implementations

func (d DataTransferRequest) Bind(r *http.Request) error {
	return nil
}

// Render implementations

func (d DataTransferResponse) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}
