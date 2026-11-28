// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// RequestChargeStationDiagnostics asks the charge station to upload its diagnostics.
// For an OCPP 1.6 charge station it queues a GetDiagnostics command and answers it: the
// result handler of GetDiagnostics records that the upload started when the charge
// station answers. For other charge stations the diagnostics status says at once that
// the upload started.
func (s *Server) RequestChargeStationDiagnostics(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(DiagnosticsRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	if s.answerThroughQueue(w, r, csId, http.StatusAccepted, "GetDiagnostics", &ocpp16.GetDiagnosticsJson{
		Location:      req.Location,
		StartTime:     optionalOcppDateTime(req.StartTime),
		StopTime:      optionalOcppDateTime(req.StopTime),
		Retries:       req.Retries,
		RetryInterval: req.RetryInterval,
	}) {
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

// RequestChargeStationLogs asks the charge station to upload a log. For an OCPP 1.6
// charge station it queues a GetLog command and answers it: the log status changes when
// the charge station reports it (LogStatusNotification). For other charge stations the
// log status says at once that the upload started.
func (s *Server) RequestChargeStationLogs(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(LogRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	if s.answerThroughQueue(w, r, csId, http.StatusAccepted, "GetLog", &ocpp16.GetLogJson{
		LogType:   ocpp16.LogEnumType(req.LogType),
		RequestId: req.RequestId,
		Log: ocpp16.LogParametersType{
			RemoteLocation:  req.Log.RemoteLocation,
			OldestTimestamp: optionalOcppDateTime(req.Log.OldestTimestamp),
			LatestTimestamp: optionalOcppDateTime(req.Log.LatestTimestamp),
		},
		Retries:       req.Retries,
		RetryInterval: req.RetryInterval,
	}) {
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
			Status: LogStatusStatusIdle,
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
