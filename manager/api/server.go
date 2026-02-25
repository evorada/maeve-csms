// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"net/http"
	"time"

	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/ocpi"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"k8s.io/utils/clock"
)

type Server struct {
	store   store.Engine
	clock   clock.PassiveClock
	swagger *openapi3.T
	ocpi    ocpi.Api
}

func NewServer(engine store.Engine, clock clock.PassiveClock, ocpi ocpi.Api) (*Server, error) {
	swagger, err := GetSwagger()
	if err != nil {
		return nil, err
	}
	return &Server{
		store:   engine,
		clock:   clock,
		ocpi:    ocpi,
		swagger: swagger,
	}, nil
}

func (s *Server) RegisterChargeStation(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(ChargeStationAuth)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	var pwd string
	if req.Base64SHA256Password != nil {
		pwd = *req.Base64SHA256Password
	}
	invalidUsernameAllowed := false
	if req.InvalidUsernameAllowed != nil {
		invalidUsernameAllowed = *req.InvalidUsernameAllowed
	}

	// Validate SecurityProfile is within valid range for int8
	if req.SecurityProfile < 0 || req.SecurityProfile > 127 {
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("security profile value %d is out of valid range", req.SecurityProfile)))
		return
	}

	err := s.store.SetChargeStationAuth(r.Context(), csId, &store.ChargeStationAuth{
		SecurityProfile:        store.SecurityProfile(req.SecurityProfile),
		Base64SHA256Password:   pwd,
		InvalidUsernameAllowed: invalidUsernameAllowed,
	})
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (s *Server) ReconfigureChargeStation(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(ChargeStationSettings)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	chargeStationSettings := make(map[string]*store.ChargeStationSetting)
	for k, v := range *req {
		chargeStationSettings[k] = &store.ChargeStationSetting{
			Value:  v,
			Status: store.ChargeStationSettingStatusPending,
		}
	}

	err := s.store.UpdateChargeStationSettings(r.Context(), csId, &store.ChargeStationSettings{
		Settings: chargeStationSettings,
	})
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
}

func (s *Server) InstallChargeStationCertificates(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(ChargeStationInstallCertificates)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	var certs []*store.ChargeStationInstallCertificate
	for _, cert := range req.Certificates {
		certId, err := handlers.GetCertificateId(cert.Certificate)
		if err != nil {
			_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("invalid certificate: %w", err)))
			return
		}

		status := store.CertificateInstallationPending
		if cert.Status != nil {
			status = store.CertificateInstallationStatus(*cert.Status)
		}

		certs = append(certs, &store.ChargeStationInstallCertificate{
			CertificateType:               store.CertificateType(cert.Type),
			CertificateId:                 certId,
			CertificateData:               cert.Certificate,
			CertificateInstallationStatus: status,
		})
	}

	err := s.store.UpdateChargeStationInstallCertificates(r.Context(), csId, &store.ChargeStationInstallCertificates{
		ChargeStationId: csId,
		Certificates:    certs,
	})
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
}

func (s *Server) LookupChargeStationAuth(w http.ResponseWriter, r *http.Request, csId string) {
	auth, err := s.store.LookupChargeStationAuth(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	if auth == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	resp := &ChargeStationAuth{
		SecurityProfile: int(auth.SecurityProfile),
	}
	if auth.Base64SHA256Password != "" {
		resp.Base64SHA256Password = &auth.Base64SHA256Password
	}
	resp.InvalidUsernameAllowed = &auth.InvalidUsernameAllowed

	_ = render.Render(w, r, resp)
}

func (s *Server) TriggerChargeStation(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(ChargeStationTrigger)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	err := s.store.SetChargeStationTriggerMessage(r.Context(), csId, &store.ChargeStationTriggerMessage{
		TriggerMessage: store.TriggerMessage(req.Trigger),
		ConnectorId:    req.ConnectorId,
		TriggerStatus:  store.TriggerStatusPending,
	})
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (s *Server) SetToken(w http.ResponseWriter, r *http.Request) {
	req := new(Token)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	normContractId, err := ocpp.NormalizeEmaid(req.ContractId)
	if err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	err = s.store.SetToken(r.Context(), &store.Token{
		CountryCode:  req.CountryCode,
		PartyId:      req.PartyId,
		Type:         string(req.Type),
		Uid:          req.Uid,
		ContractId:   normContractId,
		VisualNumber: req.VisualNumber,
		Issuer:       req.Issuer,
		GroupId:      req.GroupId,
		Valid:        req.Valid,
		LanguageCode: req.LanguageCode,
		CacheMode:    string(req.CacheMode),
		LastUpdated:  s.clock.Now().Format(time.RFC3339),
	})
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func newToken(tok *store.Token) (*Token, error) {
	lastUpdated, err := time.Parse(time.RFC3339, tok.LastUpdated)
	if err != nil {
		return nil, err
	}

	return &Token{
		CountryCode:  tok.CountryCode,
		PartyId:      tok.PartyId,
		Type:         TokenType(tok.Type),
		Uid:          tok.Uid,
		ContractId:   tok.ContractId,
		VisualNumber: tok.VisualNumber,
		Issuer:       tok.Issuer,
		GroupId:      tok.GroupId,
		Valid:        tok.Valid,
		LanguageCode: tok.LanguageCode,
		CacheMode:    TokenCacheMode(tok.CacheMode),
		LastUpdated:  &lastUpdated,
	}, nil
}

func (s *Server) LookupToken(w http.ResponseWriter, r *http.Request, tokenUid string) {
	tok, err := s.store.LookupToken(r.Context(), tokenUid)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	if tok == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	resp, err := newToken(tok)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	_ = render.Render(w, r, resp)
}

func (s *Server) ListTokens(w http.ResponseWriter, r *http.Request, params ListTokensParams) {
	offset := 0
	limit := 20

	if params.Offset != nil {
		offset = *params.Offset
	}
	if params.Limit != nil {
		limit = *params.Limit
	}
	if limit > 100 {
		limit = 100
	}

	tokens, err := s.store.ListTokens(r.Context(), offset, limit)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	var resp = make([]render.Renderer, len(tokens))
	for i, tok := range tokens {
		resp[i], err = newToken(tok)
		if err != nil {
			_ = render.Render(w, r, ErrInternalError(err))
			return
		}
	}
	_ = render.RenderList(w, r, resp)
}

func (s *Server) UploadCertificate(w http.ResponseWriter, r *http.Request) {
	req := new(Certificate)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	err := s.store.SetCertificate(r.Context(), req.Certificate)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (s *Server) DeleteCertificate(w http.ResponseWriter, r *http.Request, certificateHash string) {
	err := s.store.DeleteCertificate(r.Context(), certificateHash)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) LookupCertificate(w http.ResponseWriter, r *http.Request, certificateHash string) {
	cert, err := s.store.LookupCertificate(r.Context(), certificateHash)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	if cert == "" {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	resp := &Certificate{
		Certificate: cert,
	}
	_ = render.Render(w, r, resp)
}

func (s *Server) RegisterParty(w http.ResponseWriter, r *http.Request) {
	if s.ocpi == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	req := new(Registration)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	if req.Url != nil {
		err := s.ocpi.RegisterNewParty(r.Context(), *req.Url, req.Token)
		if err != nil {
			_ = render.Render(w, r, ErrInternalError(err))
			return
		}
	} else {
		// store credentials in database
		status := store.OcpiRegistrationStatusPending
		if req.Status != nil && *req.Status == "REGISTERED" {
			status = store.OcpiRegistrationStatusRegistered
		}

		err := s.store.SetRegistrationDetails(r.Context(), req.Token, &store.OcpiRegistration{
			Status: status,
		})
		if err != nil {
			_ = render.Render(w, r, ErrInternalError(err))
			return
		}
	}

	w.WriteHeader(http.StatusCreated)
}

func (s *Server) RegisterLocation(w http.ResponseWriter, r *http.Request, locationId string) {
	if s.ocpi == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	req := new(Location)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	now := s.clock.Now()

	var numEvses int
	if req.Evses != nil {
		numEvses = len(*req.Evses)
	}
	storeEvses := make([]store.Evse, numEvses)
	if numEvses != 0 {
		for i, reqEvse := range *req.Evses {
			storeConnectors := make([]store.Connector, len(reqEvse.Connectors))
			for j, reqConnector := range reqEvse.Connectors {
				storeConnectors[j] = store.Connector{
					Id:          reqConnector.Id,
					Format:      string(reqConnector.Format),
					PowerType:   string(reqConnector.PowerType),
					Standard:    string(reqConnector.Standard),
					MaxVoltage:  reqConnector.MaxVoltage,
					MaxAmperage: reqConnector.MaxAmperage,
					LastUpdated: now.Format(time.RFC3339),
				}
				storeEvses[i] = store.Evse{
					Connectors:  storeConnectors,
					EvseId:      reqEvse.EvseId,
					Status:      string(ocpi.EvseStatusUNKNOWN),
					Uid:         reqEvse.Uid,
					LastUpdated: now.Format(time.RFC3339),
				}
			}
		}
	}
	err := s.store.SetLocation(r.Context(), &store.Location{
		Address: req.Address,
		City:    req.City,
		Coordinates: store.GeoLocation{
			Latitude:  req.Coordinates.Latitude,
			Longitude: req.Coordinates.Longitude,
		},
		Country:     req.Country,
		Evses:       &storeEvses,
		Id:          locationId,
		Name:        *req.Name,
		ParkingType: string(*req.ParkingType),
		PostalCode:  *req.PostalCode,
	})
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	ocpiEvses := make([]ocpi.Evse, numEvses)
	if numEvses != 0 {
		for i, reqEvse := range *req.Evses {
			ocpiConnectors := make([]ocpi.Connector, len(reqEvse.Connectors))
			for j, reqConnector := range reqEvse.Connectors {
				ocpiConnectors[j] = ocpi.Connector{
					Id:          reqConnector.Id,
					Format:      ocpi.ConnectorFormat(reqConnector.Format),
					PowerType:   ocpi.ConnectorPowerType(reqConnector.PowerType),
					Standard:    ocpi.ConnectorStandard(reqConnector.Standard),
					MaxVoltage:  reqConnector.MaxVoltage,
					MaxAmperage: reqConnector.MaxAmperage,
					LastUpdated: now.Format(time.RFC3339),
				}
				ocpiEvses[i] = ocpi.Evse{
					Connectors:  ocpiConnectors,
					EvseId:      reqEvse.EvseId,
					Status:      ocpi.EvseStatusUNKNOWN,
					Uid:         reqEvse.Uid,
					LastUpdated: now.Format(time.RFC3339),
				}
			}
		}
	}
	err = s.ocpi.PushLocation(r.Context(), ocpi.Location{
		Address: req.Address,
		City:    req.City,
		Coordinates: ocpi.GeoLocation{
			Latitude:  req.Coordinates.Latitude,
			Longitude: req.Coordinates.Longitude,
		},
		Country:     req.Country,
		CountryCode: req.CountryCode,
		Evses:       &ocpiEvses,
		Id:          locationId,
		LastUpdated: now.Format(time.RFC3339),
		Name:        req.Name,
		ParkingType: (*ocpi.LocationParkingType)(req.ParkingType),
		PartyId:     req.PartyId,
		PostalCode:  req.PostalCode,
		Publish:     true,
	})
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusCreated)
}

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

func (s *Server) CreateReservation(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(ReservationRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Build reservation
	reservation := &store.Reservation{
		ReservationId:   int(req.ReservationId),
		ChargeStationId: csId,
		ConnectorId:     int(req.ConnectorId),
		IdTag:           req.IdTag,
		ExpiryDate:      req.ExpiryDate,
		Status:          store.ReservationStatusAccepted, // Initial status
		CreatedAt:       s.clock.Now(),
	}

	if req.ParentIdTag != nil {
		reservation.ParentIdTag = req.ParentIdTag
	}

	// Store the reservation
	err := s.store.CreateReservation(r.Context(), reservation)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	// Return 202 Accepted - actual OCPP command will be sent asynchronously
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) CancelReservation(w http.ResponseWriter, r *http.Request, csId string, reservationId int32) {
	// Check if the reservation exists
	reservation, err := s.store.GetReservation(r.Context(), int(reservationId))
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	if reservation == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	// Verify the reservation belongs to the specified charge station
	if reservation.ChargeStationId != csId {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	// Cancel the reservation
	err = s.store.CancelReservation(r.Context(), int(reservationId))
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	// Return 202 Accepted - actual OCPP command will be sent asynchronously
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) ListReservations(w http.ResponseWriter, r *http.Request, csId string, params ListReservationsParams) {
	// Default status filter is "active"
	statusFilter := "active"
	if params.Status != nil {
		statusFilter = string(*params.Status)
	}

	var reservations []*store.Reservation
	var err error

	switch statusFilter {
	case "active":
		// Get active reservations (accepted status, not expired)
		reservations, err = s.store.GetActiveReservations(r.Context(), csId)
		if err != nil {
			_ = render.Render(w, r, ErrInternalError(err))
			return
		}
	case "all":
		// For "all", we still use GetActiveReservations as the primary method
		// In a real implementation, you might want a separate store method
		reservations, err = s.store.GetActiveReservations(r.Context(), csId)
		if err != nil {
			_ = render.Render(w, r, ErrInternalError(err))
			return
		}
		// Note: This is a simplified implementation
		// A full implementation might need a GetAllReservations method
	case "expired":
		// For expired, we'd need a separate query
		// For now, return empty list as a placeholder
		reservations = []*store.Reservation{}
	default:
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("invalid status filter: %s", statusFilter)))
		return
	}

	// Convert to API response format
	response := &ReservationList{
		Reservations: make([]ReservationResponse, 0, len(reservations)),
	}

	for _, res := range reservations {
		createdAt := res.CreatedAt
		apiRes := ReservationResponse{
			ReservationId: int32(res.ReservationId),
			ConnectorId:   int32(res.ConnectorId),
			ExpiryDate:    res.ExpiryDate,
			IdTag:         res.IdTag,
			Status:        ReservationResponseStatus(res.Status),
			CreatedAt:     &createdAt,
		}
		if res.ParentIdTag != nil {
			apiRes.ParentIdTag = res.ParentIdTag
		}
		response.Reservations = append(response.Reservations, apiRes)
	}

	_ = render.Render(w, r, response)
}
