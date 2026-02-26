// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Server) RequestChargeStationDiagnostics(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(DiagnosticsRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	diagStatus := &store.DiagnosticsStatus{
		ChargeStationId: csId,
		Status:          store.DiagnosticsStatusUploading,
		Location:        req.Location,
		UpdatedAt:       s.clock.Now(),
	}

	if err := s.store.SetDiagnosticsStatus(r.Context(), csId, diagStatus); err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) RequestChargeStationLogs(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(LogRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	logStatus := &store.LogStatus{
		ChargeStationId: csId,
		Status:          store.LogStatusUploading,
		RequestId:       req.RequestId,
		UpdatedAt:       s.clock.Now(),
	}

	if err := s.store.SetLogStatus(r.Context(), csId, logStatus); err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) GetChargeStationLogStatus(w http.ResponseWriter, r *http.Request, csId string, params GetChargeStationLogStatusParams) {
	logStatus, err := s.store.GetLogStatus(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	if logStatus == nil {
		resp := LogStatus{
			Status: Idle,
		}
		_ = render.Render(w, r, &resp)
		return
	}

	lastUpdate := logStatus.UpdatedAt
	requestId := logStatus.RequestId
	resp := LogStatus{
		Status:     LogStatusStatus(logStatus.Status),
		LastUpdate: &lastUpdate,
		RequestId:  &requestId,
	}
	_ = render.Render(w, r, &resp)
}

// Render implementations

func (d DiagnosticsRequest) Bind(r *http.Request) error {
	return nil
}

// Render implementations

func (d DiagnosticsStatus) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

// Render implementations

func (l LogRequest) Bind(r *http.Request) error {
	return nil
}

// Render implementations

func (l LogStatus) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}
