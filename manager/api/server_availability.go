// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Server) ResetChargeStation(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(ResetRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Check if charge station exists
	auth, err := s.store.LookupChargeStationAuth(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	if auth == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	// Store the reset request
	resetReq := &store.ResetRequest{
		ChargeStationId: csId,
		Type:            store.ResetType(req.Type),
		Status:          store.ResetRequestStatusPending,
		CreatedAt:       s.clock.Now(),
	}

	err = s.store.SetResetRequest(r.Context(), csId, resetReq)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) UnlockConnector(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(UnlockConnectorRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Check if charge station exists
	auth, err := s.store.LookupChargeStationAuth(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	if auth == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	// Store the unlock request
	unlockReq := &store.UnlockConnectorRequest{
		ChargeStationId: csId,
		ConnectorId:     int(req.ConnectorId),
		Status:          store.UnlockConnectorRequestStatusPending,
		CreatedAt:       s.clock.Now(),
	}

	err = s.store.SetUnlockConnectorRequest(r.Context(), csId, unlockReq)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) ChangeAvailability(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(ChangeAvailabilityRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	changeAvailability := &store.ChargeStationChangeAvailability{
		ChargeStationId: csId,
		ConnectorId:     req.ConnectorId,
		EvseId:          req.EvseId,
		Type:            store.AvailabilityType(req.Type),
		Status:          store.AvailabilityStatusPending,
		SendAfter:       s.clock.Now(),
	}

	err := s.store.SetChargeStationChangeAvailability(r.Context(), csId, changeAvailability)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// Render implementations

func (r ResetRequest) Bind(_ *http.Request) error {
	return nil
}

// Render implementations

func (u UnlockConnectorRequest) Bind(_ *http.Request) error {
	return nil
}

// Render implementations

func (c ChangeAvailabilityRequest) Bind(r *http.Request) error {
	return nil
}
