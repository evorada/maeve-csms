// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// UpdateChargeStationFirmware asks the charge station to update its firmware. For an
// OCPP 1.6 charge station it queues an UpdateFirmware command, or a SignedUpdateFirmware
// command for a firmware with a signature (see firmwareCommand), and answers it. Other
// charge stations get a firmware update request in the store.
func (s *Server) UpdateChargeStationFirmware(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(FirmwareUpdateRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	queue, ok := s.useQueue(w, r, csId)
	if !ok {
		return
	}
	if queue {
		action, payload, err := s.firmwareCommand(req)
		if err != nil {
			_ = render.Render(w, r, ErrInvalidRequest(err))
			return
		}
		if cmd := s.enqueueFor(w, r, csId, action, payload); cmd != nil {
			s.renderQueued(w, r, http.StatusAccepted, cmd)
		}
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

// firmwareCommand returns the OCPP 1.6 command that updates the firmware as the request
// asks: SignedUpdateFirmware when the request has a signature and the certificate that
// signed it, UpdateFirmware when it has neither. The firmware is retrieved at the
// retrieve date, or at once. A signed update has the request id of the request, or a
// random one; installDate and requestId are not used by an update without signature.
func (s *Server) firmwareCommand(req *FirmwareUpdateRequest) (action string, payload any, err error) {
	retrieveDate := s.clock.Now()
	if req.RetrieveDate != nil {
		retrieveDate = *req.RetrieveDate
	}

	switch {
	case req.Signature == nil && req.SigningCertificate == nil:
		return "UpdateFirmware", &ocpp16.UpdateFirmwareJson{
			Location:      req.Location,
			RetrieveDate:  ocppDateTime(retrieveDate),
			Retries:       req.Retries,
			RetryInterval: req.RetryInterval,
		}, nil
	case req.Signature == nil || req.SigningCertificate == nil:
		return "", nil, errors.New("a signed firmware update needs both signature and signingCertificate")
	}

	requestId := randomRequestId()
	if req.RequestId != nil {
		requestId = *req.RequestId
	}
	return "SignedUpdateFirmware", &ocpp16.SignedUpdateFirmwareJson{
		RequestId: requestId,
		Firmware: ocpp16.SignedUpdateFirmwareFirmwareType{
			Location:           req.Location,
			RetrieveDateTime:   ocppDateTime(retrieveDate),
			InstallDateTime:    optionalOcppDateTime(req.InstallDate),
			SigningCertificate: *req.SigningCertificate,
			Signature:          *req.Signature,
		},
		Retries:       req.Retries,
		RetryInterval: req.RetryInterval,
	}, nil
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
