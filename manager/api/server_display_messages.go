// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"net/http"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Server) SetDisplayMessage(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(SetDisplayMessageRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Check OCPP version - display messages only supported in OCPP 2.0.1
	details, err := s.store.LookupChargeStationRuntimeDetails(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	if details == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}
	if details.OcppVersion != "2.0.1" {
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("display messages only supported on OCPP 2.0.1 charge stations")))
		return
	}

	// Convert API types to store types
	msg := &store.DisplayMessage{
		ChargeStationId: csId,
		Id:              req.Message.Id,
		Priority:        store.MessagePriority(req.Message.Priority),
		Message: store.MessageContent{
			Content: req.Message.Message.Content,
			Format:  store.MessageFormat(req.Message.Message.Format),
		},
		CreatedAt: s.clock.Now(),
		UpdatedAt: s.clock.Now(),
	}

	if req.Message.Message.Language != nil {
		msg.Message.Language = req.Message.Message.Language
	}
	if req.Message.State != nil {
		state := store.MessageState(*req.Message.State)
		msg.State = &state
	}
	if req.Message.StartDateTime != nil {
		msg.StartDateTime = req.Message.StartDateTime
	}
	if req.Message.EndDateTime != nil {
		msg.EndDateTime = req.Message.EndDateTime
	}
	if req.Message.TransactionId != nil {
		msg.TransactionId = req.Message.TransactionId
	}

	// Store the message
	if err := s.store.SetDisplayMessage(r.Context(), msg); err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	// Queue the OCPP SetDisplayMessage call
	// This would be handled by a background worker that polls for pending messages
	// and sends them via the OCPP connection

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) GetDisplayMessages(w http.ResponseWriter, r *http.Request, csId string, params GetDisplayMessagesParams) {
	// Check OCPP version
	details, err := s.store.LookupChargeStationRuntimeDetails(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	if details == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}
	if details.OcppVersion != "2.0.1" {
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("display messages only supported on OCPP 2.0.1 charge stations")))
		return
	}

	// Convert filter parameters
	var state *store.MessageState
	var priority *store.MessagePriority

	if params.State != nil {
		s := store.MessageState(*params.State)
		state = &s
	}
	if params.Priority != nil {
		p := store.MessagePriority(*params.Priority)
		priority = &p
	}

	// Retrieve messages from store
	_, err = s.store.ListDisplayMessages(r.Context(), csId, state, priority)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	// For now, accept the request as async operation
	// In a full implementation, this would trigger an OCPP GetDisplayMessages
	// request to sync with the charge station's current state and return results

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) ClearDisplayMessage(w http.ResponseWriter, r *http.Request, csId string, messageId int) {
	// Check OCPP version
	details, err := s.store.LookupChargeStationRuntimeDetails(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	if details == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}
	if details.OcppVersion != "2.0.1" {
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("display messages only supported on OCPP 2.0.1 charge stations")))
		return
	}

	// Delete the message from the store
	if err := s.store.DeleteDisplayMessage(r.Context(), csId, messageId); err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	// Queue the OCPP ClearDisplayMessage call
	// This would be handled by a background worker

	w.WriteHeader(http.StatusAccepted)
}

// Render implementations

func (s SetDisplayMessageRequest) Bind(r *http.Request) error {
	return nil
}
