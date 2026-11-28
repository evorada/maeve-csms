// SPDX-License-Identifier: Apache-2.0

// Package internalapi is the API that the gateway uses to look up charge stations and
// certificates. It is served on the internal listener of the manager and is not part
// of the public API.
package internalapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/thoughtworks/maeve-csms/manager/stations"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
)

const (
	// ErrorUnknownHost is the error of a 404 when no tenant has the host.
	ErrorUnknownHost = "unknown_host"
	// ErrorUnknownStation is the error of a 404 when the station is not registered, or is
	// decommissioned.
	ErrorUnknownStation = "unknown_station"

	// maxCertificateHashLength is the limit of the certificate hash in the public API.
	maxCertificateHashLength = 64
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
	TenantStatus           string `json:"tenantStatus,omitempty"`
	SecurityProfile        int    `json:"securityProfile"`
	Base64SHA256Password   string `json:"base64SHA256Password,omitempty"`
	InvalidUsernameAllowed bool   `json:"invalidUsernameAllowed"`
	// AcceptedCertOrgNames are the organisation names that the tenant that owns the
	// station accepts in the client certificate of a station. An empty list means that
	// the organisation is not checked. It is left out when there is no such list, and
	// the gateway then checks against its own list: in single mode, for a station whose
	// org is not a tenant, and when the stored config of the tenant cannot be read.
	AcceptedCertOrgNames *[]string `json:"acceptedCertOrgNames,omitempty"`
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
}

// NewHandler creates the handler of the internal API:
//
//	GET /station-auth?host={host}&stationId={id}
//	GET /certificates/{certificateHash}
func NewHandler(resolver *tenancy.Resolver, engine store.Engine) http.Handler {
	h := &handler{resolver: resolver, config: &tenancy.ConfigProvider{Resolver: resolver}, engine: engine}

	r := chi.NewRouter()
	r.Get("/station-auth", h.stationAuth)
	r.Get("/certificates/{certificateHash}", h.certificate)
	return r
}

// stationAuth returns the auth details of the charge station that connects to host
// with the id stationId. The station is looked up by the key that the resolver builds
// from the tenant of the host, so a station of another tenant is never returned.
func (h *handler) stationAuth(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	host, ok := singleValue(query["host"])
	if !ok {
		writeError(w, http.StatusBadRequest, "host: exactly one value is required")
		return
	}
	stationId, ok := singleValue(query["stationId"])
	if !ok {
		writeError(w, http.StatusBadRequest, "stationId: exactly one value is required")
		return
	}
	if err := tenancy.ValidateStationId(stationId); err != nil {
		writeError(w, http.StatusBadRequest, "stationId: "+err.Error())
		return
	}

	res, err := h.resolver.ResolveStation(r.Context(), host, stationId)
	if errors.Is(err, tenancy.ErrUnknownHost) {
		writeError(w, http.StatusNotFound, ErrorUnknownHost)
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}

	auth, err := h.engine.LookupChargeStationAuth(r.Context(), res.StationKey)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if auth == nil {
		writeError(w, http.StatusNotFound, ErrorUnknownStation)
		return
	}
	// a decommissioned station is kept for its history, but may no longer connect
	decommissioned, err := stations.Decommissioned(r.Context(), h.engine, res.StationKey)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if decommissioned {
		writeError(w, http.StatusNotFound, ErrorUnknownStation)
		return
	}

	resp := StationAuthResponse{
		OrgId:                  tenancy.StationOrgId(auth, res.OrgId),
		OrgKey:                 res.OrgKey,
		StationKey:             res.StationKey,
		SecurityProfile:        int(auth.SecurityProfile),
		Base64SHA256Password:   auth.Base64SHA256Password,
		InvalidUsernameAllowed: auth.InvalidUsernameAllowed,
	}
	if res.Tenant != nil {
		resp.TenantStatus = string(res.Tenant.Status)
	}
	// A list that is not known must not be sent as an empty one, which would switch the
	// check of the organisation off; a list that cannot be looked up fails the request.
	names, known, err := h.config.AcceptedCertOrgNames(r.Context(), resp.OrgId)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if known {
		resp.AcceptedCertOrgNames = &names
	}
	writeJSON(w, http.StatusOK, resp)
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
