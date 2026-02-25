// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"net/http"
	"sort"
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

	// Sort by reservation ID for deterministic ordering
	sort.Slice(reservations, func(i, j int) bool {
		return reservations[i].ReservationId < reservations[j].ReservationId
	})

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

func (s *Server) GetMeterValues(w http.ResponseWriter, r *http.Request, csId string, params GetMeterValuesParams) {
	// Build filter from query parameters
	filter := store.MeterValuesFilter{
		ChargeStationId: csId,
		ConnectorId:     params.ConnectorId,
		TransactionId:   params.TransactionId,
		Limit:           100, // default
		Offset:          0,
	}

	if params.StartTime != nil {
		startTimeStr := params.StartTime.Format(time.RFC3339)
		filter.StartTime = &startTimeStr
	}
	if params.EndTime != nil {
		endTimeStr := params.EndTime.Format(time.RFC3339)
		filter.EndTime = &endTimeStr
	}
	if params.Limit != nil {
		if *params.Limit > 0 && *params.Limit <= 1000 {
			filter.Limit = *params.Limit
		}
	}
	if params.Offset != nil && *params.Offset >= 0 {
		filter.Offset = *params.Offset
	}

	result, err := s.store.QueryMeterValues(r.Context(), filter)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	// Convert store types to API types
	apiMeterValues := make([]MeterValue, len(result.MeterValues))
	for i, mv := range result.MeterValues {
		apiSampledValues := make([]MeterValuesSampledValue, len(mv.MeterValue.SampledValues))
		for j, sv := range mv.MeterValue.SampledValues {
			var unit *string
			if sv.UnitOfMeasure != nil {
				unit = &sv.UnitOfMeasure.Unit
			}
			apiSampledValues[j] = MeterValuesSampledValue{
				Value:     fmt.Sprintf("%f", sv.Value),
				Context:   sv.Context,
				Format:    nil, // not stored in current schema
				Measurand: sv.Measurand,
				Phase:     sv.Phase,
				Location:  sv.Location,
				Unit:      unit,
			}
		}

		connectorId := &mv.EvseId
		var transactionId *string
		if mv.TransactionId != "" {
			transactionId = &mv.TransactionId
		}

		// Parse timestamp from string
		timestamp, err := time.Parse(time.RFC3339, mv.MeterValue.Timestamp)
		if err != nil {
			_ = render.Render(w, r, ErrInternalError(fmt.Errorf("invalid timestamp: %w", err)))
			return
		}

		apiMeterValues[i] = MeterValue{
			Timestamp:     timestamp,
			ConnectorId:   connectorId,
			EvseId:        &mv.EvseId,
			TransactionId: transactionId,
			SampledValue:  apiSampledValues,
		}
	}

	resp := &MeterValuesResponse{
		MeterValues: apiMeterValues,
		Total:       result.Total,
		Limit:       filter.Limit,
		Offset:      filter.Offset,
	}

	_ = render.Render(w, r, resp)
}

func (s *Server) GetLocalListVersion(w http.ResponseWriter, r *http.Request, csId string) {
	version, err := s.store.GetLocalListVersion(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	resp := &LocalListVersionResponse{
		ListVersion: int32(version),
	}

	_ = render.Render(w, r, resp)
}

func (s *Server) GetLocalAuthorizationList(w http.ResponseWriter, r *http.Request, csId string) {
	entries, err := s.store.GetLocalAuthList(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	resp := &LocalAuthorizationListResponse{
		ListVersion:            0, // Will be set below
		LocalAuthorizationList: make([]LocalAuthorizationEntry, len(entries)),
	}

	// Get the current version
	version, err := s.store.GetLocalListVersion(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	resp.ListVersion = int32(version)

	// Convert store entries to API entries
	for i, entry := range entries {
		apiEntry := LocalAuthorizationEntry{
			IdTag: entry.IdTag,
			IdTagInfo: IdTagInfo{
				Status: IdTagInfoStatus(entry.IdTagInfo.Status),
			},
		}

		if entry.IdTagInfo.ExpiryDate != nil {
			expiryDate, err := time.Parse(time.RFC3339, *entry.IdTagInfo.ExpiryDate)
			if err == nil {
				apiEntry.IdTagInfo.ExpiryDate = &expiryDate
			}
		}

		if entry.IdTagInfo.ParentIdTag != nil {
			apiEntry.IdTagInfo.ParentIdTag = entry.IdTagInfo.ParentIdTag
		}

		resp.LocalAuthorizationList[i] = apiEntry
	}

	_ = render.Render(w, r, resp)
}

func (s *Server) UpdateLocalAuthorizationList(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(UpdateLocalListRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Validate update type
	if req.UpdateType != Full &&
		req.UpdateType != Differential {
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("invalid update type: %s", req.UpdateType)))
		return
	}

	// Convert API entries to store entries
	var entries []*store.LocalAuthListEntry
	if req.LocalAuthorizationList != nil {
		entries = make([]*store.LocalAuthListEntry, len(*req.LocalAuthorizationList))
		for i, apiEntry := range *req.LocalAuthorizationList {
			storeEntry := &store.LocalAuthListEntry{
				IdTag: apiEntry.IdTag,
				IdTagInfo: &store.IdTagInfo{
					Status: string(apiEntry.IdTagInfo.Status),
				},
			}

			if apiEntry.IdTagInfo.ExpiryDate != nil {
				expiryDate := apiEntry.IdTagInfo.ExpiryDate.Format(time.RFC3339)
				storeEntry.IdTagInfo.ExpiryDate = &expiryDate
			}

			if apiEntry.IdTagInfo.ParentIdTag != nil {
				storeEntry.IdTagInfo.ParentIdTag = apiEntry.IdTagInfo.ParentIdTag
			}

			entries[i] = storeEntry
		}
	}

	// Update the local authorization list
	err := s.store.UpdateLocalAuthList(r.Context(), csId, int(req.ListVersion), string(req.UpdateType), entries)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	// Return 202 Accepted - in a real implementation, this would trigger an OCPP SendLocalList call
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) GetChargeStationConfiguration(w http.ResponseWriter, r *http.Request, csId string, params GetChargeStationConfigurationParams) {
	settings, err := s.store.LookupChargeStationSettings(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	if settings == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	// Build response
	resp := &ConfigurationResponse{
		UnknownKey: &[]string{},
	}

	// Filter by keys if specified
	var requestedKeys map[string]bool
	if params.Key != nil && *params.Key != "" {
		requestedKeys = make(map[string]bool)
		for _, key := range splitKeys(*params.Key) {
			requestedKeys[key] = true
		}
	}

	for key, setting := range settings.Settings {
		// Skip if filtering and key not requested
		if requestedKeys != nil && !requestedKeys[key] {
			continue
		}

		valueCopy := setting.Value
		resp.ConfigurationKey = append(resp.ConfigurationKey, struct {
			Key      string  `json:"key"`
			Readonly bool    `json:"readonly"`
			Value    *string `json:"value,omitempty"`
		}{
			Key:      key,
			Readonly: false, // TODO: Track readonly status in store
			Value:    &valueCopy,
		})
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, resp)
}

func (s *Server) ChangeChargeStationConfiguration(w http.ResponseWriter, r *http.Request, csId string) {
	var req ConfigurationChangeRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Convert request map to ChargeStationSettings
	settings := &store.ChargeStationSettings{
		ChargeStationId: csId,
		Settings:        make(map[string]*store.ChargeStationSetting),
	}

	for key, value := range req {
		settings.Settings[key] = &store.ChargeStationSetting{
			Value:     value,
			Status:    store.ChargeStationSettingStatusPending,
			SendAfter: s.clock.Now(),
		}
	}

	err := s.store.UpdateChargeStationSettings(r.Context(), csId, settings)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	// Build response with pending status for all keys
	var resp ConfigurationChangeResponse

	for key := range req {
		resp.Results = append(resp.Results, struct {
			Key    string                                   `json:"key"`
			Status ConfigurationChangeResponseResultsStatus `json:"status"`
		}{
			Key:    key,
			Status: "Accepted", // Will be updated when ChangeConfigurationResult is received
		})
	}

	render.Status(r, http.StatusAccepted)
	render.JSON(w, r, resp)
}

func (s *Server) GetChargeStationVariables(w http.ResponseWriter, r *http.Request, csId string, params GetChargeStationVariablesParams) {
	// OCPP 2.0.1 variables support - TODO: Implement full variable caching
	// For now, return empty response
	resp := VariablesResponse{
		Variables: []struct {
			Component struct {
				Evse *struct {
					ConnectorId *int `json:"connectorId,omitempty"`
					Id          *int `json:"id,omitempty"`
				} `json:"evse,omitempty"`
				Instance *string `json:"instance,omitempty"`
				Name     string  `json:"name"`
			} `json:"component"`
			Variable struct {
				Instance *string `json:"instance,omitempty"`
				Name     string  `json:"name"`
			} `json:"variable"`
			VariableAttribute []struct {
				Constant   *bool                                                  `json:"constant,omitempty"`
				Mutability *VariablesResponseVariablesVariableAttributeMutability `json:"mutability,omitempty"`
				Persistent *bool                                                  `json:"persistent,omitempty"`
				Value      *string                                                `json:"value,omitempty"`
			} `json:"variableAttribute"`
		}{},
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, resp)
}

func (s *Server) SetChargeStationVariables(w http.ResponseWriter, r *http.Request, csId string) {
	var req VariablesChangeRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// OCPP 2.0.1 variables support - TODO: Implement full variable store
	// For now, return accepted for all variables
	var resp VariablesChangeResponse

	for _, v := range req.Variables {
		resp.Results = append(resp.Results, struct {
			AttributeStatus VariablesChangeResponseResultsAttributeStatus `json:"attributeStatus"`
			Component       struct {
				Name string `json:"name"`
			} `json:"component"`
			Variable struct {
				Name string `json:"name"`
			} `json:"variable"`
		}{
			AttributeStatus: "Accepted",
			Component: struct {
				Name string `json:"name"`
			}{Name: v.Component.Name},
			Variable: struct {
				Name string `json:"name"`
			}{Name: v.Variable.Name},
		})
	}

	render.Status(r, http.StatusAccepted)
	render.JSON(w, r, resp)
}

// splitKeys splits a comma-separated list of keys
func splitKeys(keys string) []string {
	var result []string
	for _, k := range splitBy(keys, ',') {
		if trimmed := trimSpace(k); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func splitBy(s string, sep rune) []string {
	var result []string
	var current string
	for _, c := range s {
		if c == sep {
			result = append(result, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" || len(result) > 0 {
		result = append(result, current)
	}
	return result
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

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
