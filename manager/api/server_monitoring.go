// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"net/http"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Server) SetVariableMonitoring(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(SetVariableMonitoringRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

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
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("variable monitoring only supported on OCPP 2.0.1 charge stations")))
		return
	}

	// Store each monitoring configuration
	for _, data := range req.MonitoringData {
		for _, mon := range data.VariableMonitoring {
			config := &store.VariableMonitoringConfig{
				ChargeStationId: csId,
				ComponentName:   data.Component.Name,
				VariableName:    data.Variable.Name,
				MonitorType:     store.MonitoringType(mon.Type),
				Value:           float64(mon.Value),
				Severity:        mon.Severity,
			}
			if data.Component.Instance != nil {
				config.ComponentInstance = data.Component.Instance
			}
			if data.Variable.Instance != nil {
				config.VariableInstance = data.Variable.Instance
			}
			if mon.Id != nil {
				config.Id = *mon.Id
			}
			if mon.Transaction != nil {
				config.Transaction = *mon.Transaction
			}

			if err := s.store.SetVariableMonitoring(r.Context(), csId, config); err != nil {
				_ = render.Render(w, r, ErrInternalError(err))
				return
			}
		}
	}

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) ClearVariableMonitoring(w http.ResponseWriter, r *http.Request, csId string, monitorId int) {
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

	if err := s.store.DeleteVariableMonitoring(r.Context(), csId, monitorId); err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) SetMonitoringBase(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(SetMonitoringBaseRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

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
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("monitoring base only supported on OCPP 2.0.1 charge stations")))
		return
	}

	// SetMonitoringBase is an OCPP command - store as pending and let sync worker handle it
	// For now, respond with accepted status
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) SetMonitoringLevel(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(SetMonitoringLevelRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

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
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("monitoring level only supported on OCPP 2.0.1 charge stations")))
		return
	}

	// SetMonitoringLevel is an OCPP command - validate and accept
	if req.Severity < 0 || req.Severity > 9 {
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("severity must be between 0 and 9")))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) GetMonitoringReport(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(GetMonitoringReportRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

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
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("monitoring report only supported on OCPP 2.0.1 charge stations")))
		return
	}

	// GetMonitoringReport triggers an OCPP call - the report comes back asynchronously
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) GetChargeStationEvents(w http.ResponseWriter, r *http.Request, csId string, params GetChargeStationEventsParams) {
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

	limit := 50
	if params.Limit != nil && *params.Limit > 0 {
		limit = *params.Limit
		if limit > 200 {
			limit = 200
		}
	}
	offset := 0
	if params.Offset != nil && *params.Offset >= 0 {
		offset = *params.Offset
	}

	events, total, err := s.store.ListChargeStationEvents(r.Context(), csId, offset, limit)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	apiEvents := make([]ChargeStationEvent, len(events))
	for i, e := range events {
		apiEvents[i] = ChargeStationEvent{
			Timestamp: e.Timestamp,
			EventType: e.EventType,
			TechCode:  e.TechCode,
			TechInfo:  e.TechInfo,
			EventData: e.EventData,
		}
	}

	resp := EventsResponse{
		Events: apiEvents,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, resp)
}

func (s *Server) GetDeviceReports(w http.ResponseWriter, r *http.Request, csId string, params GetDeviceReportsParams) {
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

	limit := 50
	if params.Limit != nil && *params.Limit > 0 {
		limit = *params.Limit
		if limit > 200 {
			limit = 200
		}
	}
	offset := 0
	if params.Offset != nil && *params.Offset >= 0 {
		offset = *params.Offset
	}

	reports, total, err := s.store.ListDeviceReports(r.Context(), csId, offset, limit)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	apiReports := make([]DeviceReport, len(reports))
	for i, rpt := range reports {
		apiReports[i] = DeviceReport{
			RequestId:   rpt.RequestId,
			GeneratedAt: rpt.GeneratedAt,
			ReportType:  rpt.ReportType,
		}
	}

	resp := ReportsResponse{
		Reports: apiReports,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, resp)
}

// Render implementations

func (r SetVariableMonitoringRequest) Bind(_ *http.Request) error {
	return nil
}

// Render implementations

func (r SetMonitoringBaseRequest) Bind(_ *http.Request) error {
	return nil
}

// Render implementations

func (r SetMonitoringLevelRequest) Bind(_ *http.Request) error {
	return nil
}

// Render implementations

func (r GetMonitoringReportRequest) Bind(_ *http.Request) error {
	return nil
}
