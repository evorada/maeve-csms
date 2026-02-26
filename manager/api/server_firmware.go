// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Server) UpdateChargeStationFirmware(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(FirmwareUpdateRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Build firmware update request
	firmwareReq := &store.FirmwareUpdateRequest{
		ChargeStationId: csId,
		Location:        req.Location,
		Status:          store.FirmwareUpdateRequestStatusPending,
		SendAfter:       s.clock.Now(),
	}

	if req.RetrieveDate != nil {
		firmwareReq.RetrieveDate = req.RetrieveDate
	}
	if req.Retries != nil {
		firmwareReq.Retries = req.Retries
	}
	if req.RetryInterval != nil {
		firmwareReq.RetryInterval = req.RetryInterval
	}
	if req.Signature != nil {
		firmwareReq.Signature = req.Signature
	}
	if req.SigningCertificate != nil {
		firmwareReq.SigningCertificate = req.SigningCertificate
	}

	err := s.store.SetFirmwareUpdateRequest(r.Context(), csId, firmwareReq)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) GetChargeStationFirmwareStatus(w http.ResponseWriter, r *http.Request, csId string) {
	status, err := s.store.GetFirmwareUpdateStatus(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	if status == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	resp := &FirmwareStatus{
		Status: FirmwareStatusStatus(status.Status),
	}

	if !status.UpdatedAt.IsZero() {
		resp.LastUpdate = &status.UpdatedAt
	}

	// Note: CurrentVersion and PendingVersion would need to come from additional
	// charge station runtime details. For now, we only return status based on
	// FirmwareUpdateStatus notifications from the charge station.

	_ = render.Render(w, r, resp)
}

func (s *Server) GetChargeStationDiagnosticsStatus(w http.ResponseWriter, r *http.Request, csId string) {
	diagStatus, err := s.store.GetDiagnosticsStatus(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	if diagStatus == nil {
		resp := DiagnosticsStatus{
			Status: DiagnosticsStatusStatusIdle,
		}
		_ = render.Render(w, r, &resp)
		return
	}

	lastUpdate := diagStatus.UpdatedAt
	resp := DiagnosticsStatus{
		Status:     DiagnosticsStatusStatus(diagStatus.Status),
		LastUpdate: &lastUpdate,
	}
	_ = render.Render(w, r, &resp)
}

// Render implementations

func (f FirmwareUpdateRequest) Bind(r *http.Request) error {
	return nil
}

// Render implementations

func (f FirmwareStatus) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}
