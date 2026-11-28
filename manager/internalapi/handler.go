// SPDX-License-Identifier: Apache-2.0

// Package internalapi is the API that the gateway uses to look up charge stations and
// certificates. It is served on the internal listener of the manager and is not part
// of the public API.
package internalapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/thoughtworks/maeve-csms/manager/stations"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

const (
	// ErrorUnknownHost is the error of a 404 when no tenant has the host.
	ErrorUnknownHost = "unknown_host"
	// ErrorUnknownStation is the error of a 404 when the station is not registered and
	// its tenant does not onboard such stations, or the station is decommissioned.
	ErrorUnknownStation = "unknown_station"
	// ErrorOnboardingDisabled is the error of a 404 of an onboarding connection when the
	// tenant does not onboard stations that are not registered.
	ErrorOnboardingDisabled = "onboarding_disabled"
	// ErrorCredentialNotAllowed is the error of a 403 of an onboarding connection whose
	// credential the tenant does not accept (any more).
	ErrorCredentialNotAllowed = "credential_not_allowed"
	// ErrorStationNotPending is the error of a 409 of an onboarding connection of a
	// station that is registered, approved or decommissioned.
	ErrorStationNotPending = "station_not_pending"
	// ErrorPendingLimitReached is the error of a 429 of an onboarding connection when the
	// tenant has as many pending stations as it allows.
	ErrorPendingLimitReached = "pending_limit_reached"

	// maxCertificateHashLength is the limit of the certificate hash in the public API.
	maxCertificateHashLength = 64

	// maxOnboardingBodyBytes limits the body of an onboarding connection.
	maxOnboardingBodyBytes = 1 << 10
)

// StationAuthResponse is the body of a successful station-auth lookup.
type StationAuthResponse struct {
	// OrgId is the org that owns the station: the org of its auth details, or the org
	// of the tenant of the host (the default org in single mode) when it has none.
	OrgId string `json:"orgId"`
	// OrgKey is the org key of the tenant of the host; left out in single mode.
	OrgKey string `json:"orgKey,omitempty"`
	// StationKey names the station inside the manager and on the message transport.
	StationKey string `json:"stationKey"`
	// TenantStatus is the status of the tenant of the host; left out in single mode.
	// It is informational and never a reason to refuse a station.
	TenantStatus string `json:"tenantStatus,omitempty"`
	// SecurityProfile is the security profile of a station that exists; left out for a
	// station that is not known and may connect for its onboarding.
	SecurityProfile      *int   `json:"securityProfile,omitempty"`
	Base64SHA256Password string `json:"base64SHA256Password,omitempty"`
	// PendingBase64SHA256Password is the hash of the new password of the station while
	// it is being told to use it; the station may connect with either password.
	PendingBase64SHA256Password string `json:"pendingBase64SHA256Password,omitempty"`
	InvalidUsernameAllowed      bool   `json:"invalidUsernameAllowed"`
	// Onboarding is set for a station that may connect for its onboarding: a pending
	// station, or a station that is not known to a tenant that onboards such stations.
	// The gateway admits such a station with the rules of the onboarding instead of its
	// security profile, and records the connection (PUT /station-onboarding).
	Onboarding *OnboardingResponse `json:"onboarding,omitempty"`
	// AcceptedCertOrgNames are the organisation names that the tenant that owns the
	// station accepts in the client certificate of a station. An empty list means that
	// the organisation is not checked. It is left out when there is no such list, and
	// the gateway then checks against its own list: in single mode, for a station whose
	// org is not a tenant, and when the stored config of the tenant cannot be read.
	AcceptedCertOrgNames *[]string `json:"acceptedCertOrgNames,omitempty"`
}

// OnboardingResponse says how a station may connect for its onboarding.
type OnboardingResponse struct {
	// OpenSP1 lets the station connect without TLS and without a password.
	OpenSP1 bool `json:"openSp1"`
	// Base64SHA256Password is the hash of the onboarding password of the tenant; left out
	// when it has none.
	Base64SHA256Password string `json:"base64SHA256Password,omitempty"`
}

// OnboardingConnectionRequest is the body of PUT /station-onboarding.
type OnboardingConnectionRequest struct {
	// Transport is "ws" (no TLS) or "wss" (TLS).
	Transport string `json:"transport"`
	// Credential is what the station connected with: "open" (no TLS and no password),
	// "onboarding_password" or "pending_password".
	Credential string `json:"credential"`
}

// OnboardingConnectionResponse is the body of a successful PUT /station-onboarding.
type OnboardingConnectionResponse struct {
	// Created is true when the station was created as a pending station.
	Created bool `json:"created"`
}

// CertificateResponse is the body of a successful certificate lookup.
type CertificateResponse struct {
	// Certificate is the PEM encoded certificate.
	Certificate string `json:"certificate"`
}

// StatusResponse is the body of every response that is not successful.
type StatusResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type handler struct {
	resolver *tenancy.Resolver
	config   *tenancy.ConfigProvider
	engine   store.Engine
	clock    clock.PassiveClock
}

// NewHandler creates the handler of the internal API:
//
//	GET /station-auth?host={host}&stationId={id}
//	PUT /station-onboarding?host={host}&stationId={id}
//	GET /certificates/{certificateHash}
func NewHandler(resolver *tenancy.Resolver, engine store.Engine) http.Handler {
	h := &handler{
		resolver: resolver,
		config:   &tenancy.ConfigProvider{Resolver: resolver},
		engine:   engine,
		clock:    clock.RealClock{},
	}

	r := chi.NewRouter()
	r.Get("/station-auth", h.stationAuth)
	r.Put("/station-onboarding", h.stationOnboarding)
	r.Get("/certificates/{certificateHash}", h.certificate)
	return r
}

// stationAuth returns the auth details of the charge station that connects to host
// with the id stationId. The station is looked up by the key that the resolver builds
// from the tenant of the host, so a station of another tenant is never returned.
//
// A station that is not known is refused (404), unless the tenant of the host onboards
// such stations: then the answer has the station key and the onboarding rules, and no
// security profile. A pending station is answered with the onboarding rules of its
// tenant, so that a change of them applies to the stations that wait; it is refused
// while its tenant does not onboard stations. A decommissioned station is refused.
func (h *handler) stationAuth(w http.ResponseWriter, r *http.Request) {
	res, ok := h.resolveStation(w, r)
	if !ok {
		return
	}

	auth, err := h.engine.LookupChargeStationAuth(r.Context(), res.StationKey)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if auth == nil {
		h.unknownStationAuth(w, r, res)
		return
	}
	// a decommissioned station is kept for its history, but may no longer connect
	record, err := stations.Lookup(r.Context(), h.engine, res.StationKey)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if record.State == store.StationStateDecommissioned {
		writeError(w, http.StatusNotFound, ErrorUnknownStation)
		return
	}

	securityProfile := int(auth.SecurityProfile)
	resp := StationAuthResponse{
		OrgId:                       tenancy.StationOrgId(auth, res.OrgId),
		OrgKey:                      res.OrgKey,
		StationKey:                  res.StationKey,
		SecurityProfile:             &securityProfile,
		Base64SHA256Password:        auth.Base64SHA256Password,
		PendingBase64SHA256Password: auth.PendingBase64SHA256Password,
		InvalidUsernameAllowed:      auth.InvalidUsernameAllowed,
	}
	if res.Tenant != nil {
		resp.TenantStatus = string(res.Tenant.Status)
	}
	if record.State == store.StationStatePending {
		settings, err := stations.LoadOnboardingSettings(r.Context(), h.config, resp.OrgId)
		if err != nil {
			internalError(w, r, err)
			return
		}
		if !settings.Enabled {
			writeError(w, http.StatusNotFound, ErrorUnknownStation)
			return
		}
		resp.Onboarding = onboardingResponse(settings)
	}
	if !h.addCertOrgNames(w, r, &resp) {
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// unknownStationAuth answers the lookup of a station that is not known: with the
// onboarding rules when the tenant of the host onboards such stations, otherwise 404.
// Nothing is written: the connection is recorded once the gateway admitted it.
func (h *handler) unknownStationAuth(w http.ResponseWriter, r *http.Request, res *tenancy.StationResolution) {
	settings, err := stations.LoadOnboardingSettings(r.Context(), h.config, res.OrgId)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if !settings.Enabled {
		writeError(w, http.StatusNotFound, ErrorUnknownStation)
		return
	}
	resp := StationAuthResponse{
		OrgId:      res.OrgId,
		OrgKey:     res.OrgKey,
		StationKey: res.StationKey,
		Onboarding: onboardingResponse(settings),
	}
	if res.Tenant != nil {
		resp.TenantStatus = string(res.Tenant.Status)
	}
	if !h.addCertOrgNames(w, r, &resp) {
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// addCertOrgNames sets the organisation names that the tenant of the station accepts. A
// list that is not known must not be sent as an empty one, which would switch the check
// of the organisation off; a list that cannot be looked up fails the request (500, and
// false is returned).
func (h *handler) addCertOrgNames(w http.ResponseWriter, r *http.Request, resp *StationAuthResponse) bool {
	names, known, err := h.config.AcceptedCertOrgNames(r.Context(), resp.OrgId)
	if err != nil {
		internalError(w, r, err)
		return false
	}
	if known {
		resp.AcceptedCertOrgNames = &names
	}
	return true
}

func onboardingResponse(settings stations.OnboardingSettings) *OnboardingResponse {
	return &OnboardingResponse{OpenSP1: settings.OpenSP1, Base64SHA256Password: settings.PasswordSHA256}
}

// stationOnboarding records a connection that the gateway admitted with the onboarding
// rules (see stationAuth): a station that is not known becomes a pending station of the
// tenant of the host, a pending station gets the time, transport and credential of the
// connection. The rules of the tenant are checked again, so that a gateway that still
// keeps older rules cannot admit a station that the tenant no longer admits.
func (h *handler) stationOnboarding(w http.ResponseWriter, r *http.Request) {
	res, ok := h.resolveStation(w, r)
	if !ok {
		return
	}
	var req OnboardingConnectionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, ok := store.OnboardingSecurityProfile(req.Transport); !ok {
		writeError(w, http.StatusBadRequest, `transport: "ws" or "wss" is required`)
		return
	}
	if !store.ValidOnboardingCredential(req.Credential) {
		writeError(w, http.StatusBadRequest, `credential: "open", "onboarding_password" or "pending_password" is required`)
		return
	}
	if req.Credential == store.OnboardingCredentialOpen && req.Transport != store.OnboardingTransportWs {
		writeError(w, http.StatusBadRequest, `credential: "open" is only for the transport "ws"`)
		return
	}

	auth, err := h.engine.LookupChargeStationAuth(r.Context(), res.StationKey)
	if err != nil {
		internalError(w, r, err)
		return
	}
	orgId := tenancy.StationOrgId(auth, res.OrgId)
	settings, err := stations.LoadOnboardingSettings(r.Context(), h.config, orgId)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if !settings.Enabled {
		writeError(w, http.StatusNotFound, ErrorOnboardingDisabled)
		return
	}
	if !credentialAllowed(settings, req.Credential) {
		writeError(w, http.StatusForbidden, ErrorCredentialNotAllowed)
		return
	}

	created, err := h.engine.RecordOnboardingConnection(r.Context(), res.StationKey, orgId, req.Transport, req.Credential,
		settings.MaxPending, h.clock.Now())
	switch {
	case errors.Is(err, store.ErrLimitExceeded):
		slog.Warn("a station that is not registered was refused: its tenant has as many pending stations as it allows",
			"stationKey", res.StationKey, "org", orgId, "maxPending", settings.MaxPending)
		writeError(w, http.StatusTooManyRequests, ErrorPendingLimitReached)
		return
	case errors.Is(err, store.ErrInvalidState):
		writeError(w, http.StatusConflict, ErrorStationNotPending)
		return
	case err != nil:
		internalError(w, r, err)
		return
	}
	if created {
		slog.Info("a station that is not registered connected and is pending", "stationKey", res.StationKey,
			"org", orgId, "transport", req.Transport, "credential", req.Credential)
	}
	writeJSON(w, http.StatusOK, OnboardingConnectionResponse{Created: created})
}

// credentialAllowed reports whether the tenant accepts the credential of an onboarding
// connection. The pending password belongs to the station: it is checked by the gateway
// against the auth details of the station.
func credentialAllowed(settings stations.OnboardingSettings, credential string) bool {
	switch credential {
	case store.OnboardingCredentialOpen:
		return settings.OpenSP1
	case store.OnboardingCredentialOnboardingPassword:
		return settings.PasswordSHA256 != ""
	}
	return true
}

// resolveStation reads the host and the station id of a request and resolves them to the
// station key. When it cannot, the request is answered and ok is false.
func (h *handler) resolveStation(w http.ResponseWriter, r *http.Request) (*tenancy.StationResolution, bool) {
	query := r.URL.Query()
	host, ok := singleValue(query["host"])
	if !ok {
		writeError(w, http.StatusBadRequest, "host: exactly one value is required")
		return nil, false
	}
	stationId, ok := singleValue(query["stationId"])
	if !ok {
		writeError(w, http.StatusBadRequest, "stationId: exactly one value is required")
		return nil, false
	}
	if err := tenancy.ValidateStationId(stationId); err != nil {
		writeError(w, http.StatusBadRequest, "stationId: "+err.Error())
		return nil, false
	}

	res, err := h.resolver.ResolveStation(r.Context(), host, stationId)
	if errors.Is(err, tenancy.ErrUnknownHost) {
		writeError(w, http.StatusNotFound, ErrorUnknownHost)
		return nil, false
	}
	if err != nil {
		internalError(w, r, err)
		return nil, false
	}
	return res, true
}

// decodeJSON reads a JSON object of at most maxOnboardingBodyBytes, without members that
// v does not have, into v.
func decodeJSON(r *http.Request, v any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxOnboardingBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("body: %w", err)
	}
	if decoder.More() {
		return errors.New("body: one JSON object is required")
	}
	return nil
}

// certificate returns the certificate with a hash, like the certificate lookup of the
// public API.
func (h *handler) certificate(w http.ResponseWriter, r *http.Request) {
	certificateHash := chi.URLParam(r, "certificateHash")
	if certificateHash == "" || len(certificateHash) > maxCertificateHashLength {
		writeError(w, http.StatusBadRequest, "certificateHash: 1 to 64 characters are required")
		return
	}

	cert, err := h.engine.LookupCertificate(r.Context(), certificateHash)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if cert == "" {
		writeError(w, http.StatusNotFound, "")
		return
	}
	writeJSON(w, http.StatusOK, CertificateResponse{Certificate: cert})
}

// singleValue returns the value of a query parameter that is given once and not empty.
func singleValue(values []string) (string, bool) {
	if len(values) != 1 || values[0] == "" {
		return "", false
	}
	return values[0], true
}

// internalError logs the error and answers 500 without the details.
func internalError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("internal api request failed", slog.String("path", r.URL.Path), slog.Any("err", err))
	writeError(w, http.StatusInternalServerError, "")
}

func writeError(w http.ResponseWriter, status int, errorText string) {
	writeJSON(w, status, StatusResponse{Status: http.StatusText(status), Error: errorText})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	// station credentials must not be kept by anything between the manager and the gateway
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
