// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// SendDataTransfer sends vendor data to the charge station. For an OCPP 1.6 charge
// station it queues a DataTransfer command and answers its id without a status: the
// status is what the charge station answers, the result of the command. Other charge
// stations get a data transfer in the store, and the answer is Accepted.
func (s *Server) SendDataTransfer(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(DataTransferRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	queue, ok := s.useQueue(w, r, csId)
	if !ok {
		return
	}
	if queue {
		cmd := s.enqueueFor(w, r, csId, "DataTransfer", &ocpp16.DataTransferJson{
			VendorId:  req.VendorId,
			MessageId: req.MessageId,
			Data:      req.Data,
		})
		if cmd == nil {
			return
		}
		id, ok := commandUUID(w, r, cmd)
		if !ok {
			return
		}
		setCommandLocation(w, cmd)
		render.Status(r, http.StatusAccepted)
		render.JSON(w, r, &DataTransferResponse{CommandId: &id})
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

// ClearAuthorizationCache asks the charge station to clear its authorization cache. For
// an OCPP 1.6 charge station it queues a ClearCache command and answers it; other charge
// stations get a clear cache request in the store.
func (s *Server) ClearAuthorizationCache(w http.ResponseWriter, r *http.Request, csId string) {
	if s.answerThroughQueue(w, r, csId, http.StatusAccepted, "ClearCache", &ocpp16.ClearCacheJson{}) {
		return
	}

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
