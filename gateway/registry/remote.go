// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	// internalApiPath is where the manager serves the lookups on its internal listener.
	internalApiPath = "/_internal/v1"

	// defaultRequestTimeout is the timeout of a request when no HTTP client is given.
	defaultRequestTimeout = 5 * time.Second

	// maxResponseBytes limits how much of a response of the manager is read.
	maxResponseBytes = 1 << 20

	// the reasons that the manager gives for a charge station that is not found
	reasonUnknownHost    = "unknown_host"
	reasonUnknownStation = "unknown_station"
)

var defaultHTTPClient = &http.Client{Timeout: defaultRequestTimeout}

// RemoteRegistry looks up charge stations and certificates with the internal API of the
// manager.
type RemoteRegistry struct {
	// ManagerApiAddr is the address of the internal listener of the manager,
	// e.g. http://127.0.0.1:9412
	ManagerApiAddr string
	// HTTPClient makes the requests. When it is nil a client with a timeout of 5 seconds
	// is used.
	HTTPClient *http.Client
}

type stationAuthResponse struct {
	OrgId      string `json:"orgId"`
	OrgKey     string `json:"orgKey"`
	StationKey string `json:"stationKey"`
	// SecurityProfile is left out for a station that is not known and may connect for
	// its onboarding.
	SecurityProfile             *int                `json:"securityProfile"`
	Base64SHA256Password        string              `json:"base64SHA256Password"`
	PendingBase64SHA256Password string              `json:"pendingBase64SHA256Password"`
	InvalidUsernameAllowed      bool                `json:"invalidUsernameAllowed"`
	Onboarding                  *onboardingResponse `json:"onboarding"`
	// AcceptedCertOrgNames is nil when the manager has no list of a tenant for the
	// station, which is not the same as an empty list.
	AcceptedCertOrgNames *[]string `json:"acceptedCertOrgNames"`
}

type onboardingResponse struct {
	OpenSP1              bool   `json:"openSp1"`
	Base64SHA256Password string `json:"base64SHA256Password"`
}

type onboardingConnectionRequest struct {
	Transport  string `json:"transport"`
	Credential string `json:"credential"`
}

type statusResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

func (r RemoteRegistry) LookupChargeStation(ctx context.Context, host, clientId string) (*ChargeStation, error) {
	query := url.Values{}
	query.Set("host", host)
	query.Set("stationId", clientId)

	status, body, err := r.get(ctx, "/station-auth?"+query.Encode())
	if err != nil {
		return nil, err
	}

	switch status {
	case http.StatusOK:
		return chargeStationFromResponse(clientId, body)
	case http.StatusNotFound:
		reason := errorOfResponse(body)
		if reason != reasonUnknownHost && reason != reasonUnknownStation {
			// not an answer of the internal API: refuse it instead of reporting (and
			// caching) every charge station as unknown
			return nil, fmt.Errorf("looking up charge station: http status %d without a known reason: is %q the internal address of the manager?",
				status, r.ManagerApiAddr)
		}
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("cs.lookup.error", reason))
		return nil, nil
	default:
		if reason := errorOfResponse(body); reason != "" {
			return nil, fmt.Errorf("looking up charge station: http status %d: %s", status, reason)
		}
		return nil, fmt.Errorf("looking up charge station: http status %d", status)
	}
}

// chargeStationFromResponse reads the answer of the manager. It refuses an answer whose
// station key does not belong to the station that was asked for, so that a charge station
// can never be given the identity of another one.
func chargeStationFromResponse(clientId string, body []byte) (*ChargeStation, error) {
	var resp stationAuthResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshaling data: %w", err)
	}
	// only a station that may connect for its onboarding has no security profile
	if resp.SecurityProfile == nil && resp.Onboarding == nil {
		return nil, errors.New("station auth has no security profile")
	}
	if resp.OrgKey != "" && !validOrgKey(resp.OrgKey) {
		return nil, fmt.Errorf("station auth has the invalid org key %q", resp.OrgKey)
	}
	wantKey := clientId
	if resp.OrgKey != "" {
		wantKey = resp.OrgKey + "." + clientId
	}
	if resp.StationKey != wantKey {
		return nil, fmt.Errorf("station auth has the station key %q, want %q", resp.StationKey, wantKey)
	}

	cs := &ChargeStation{
		ClientId:                    clientId,
		StationKey:                  resp.StationKey,
		OrgId:                       resp.OrgId,
		OrgKey:                      resp.OrgKey,
		Base64SHA256Password:        resp.Base64SHA256Password,
		PendingBase64SHA256Password: resp.PendingBase64SHA256Password,
		InvalidUsernameAllowed:      resp.InvalidUsernameAllowed,
	}
	if resp.SecurityProfile != nil {
		cs.SecurityProfile = SecurityProfile(*resp.SecurityProfile)
	}
	if resp.Onboarding != nil {
		cs.Onboarding = &Onboarding{
			OpenSP1:              resp.Onboarding.OpenSP1,
			Base64SHA256Password: resp.Onboarding.Base64SHA256Password,
		}
	}
	if resp.AcceptedCertOrgNames != nil {
		cs.HasTenantOrgNames = true
		cs.AcceptedCertOrgNames = append([]string{}, *resp.AcceptedCertOrgNames...)
	}
	return cs, nil
}

// validOrgKey reports whether the org key is 4 to 8 characters of a-z and 0-9.
func validOrgKey(orgKey string) bool {
	if len(orgKey) < 4 || len(orgKey) > 8 {
		return false
	}
	for _, c := range []byte(orgKey) {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// errorOfResponse returns the error of a response of the manager that is not successful,
// or "" when it has none.
func errorOfResponse(body []byte) string {
	var resp statusResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return ""
	}
	return resp.Error
}

// RecordOnboardingConnection records the connection with PUT /station-onboarding of the
// internal API. A 429 of the manager is ErrPendingLimitReached; any other answer than 200
// is an error.
func (r RemoteRegistry) RecordOnboardingConnection(ctx context.Context, host, clientId, transport, credential string) error {
	query := url.Values{}
	query.Set("host", host)
	query.Set("stationId", clientId)
	body, err := json.Marshal(onboardingConnectionRequest{Transport: transport, Credential: credential})
	if err != nil {
		return fmt.Errorf("recording onboarding connection: %w", err)
	}

	status, respBody, err := r.do(ctx, http.MethodPut, "/station-onboarding?"+query.Encode(), body)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusOK:
		return nil
	case http.StatusTooManyRequests:
		return ErrPendingLimitReached
	default:
		if reason := errorOfResponse(respBody); reason != "" {
			return fmt.Errorf("recording onboarding connection: http status %d: %s", status, reason)
		}
		return fmt.Errorf("recording onboarding connection: http status %d", status)
	}
}

type CertificateResponse struct {
	Certificate string `json:"certificate"`
}

func (r RemoteRegistry) LookupCertificate(ctx context.Context, certHash string) (*x509.Certificate, error) {
	certHash = strings.Replace(certHash, "/", "_", -1)
	certHash = strings.Replace(certHash, "+", "-", -1)

	status, body, err := r.get(ctx, "/certificates/"+url.PathEscape(certHash))
	if err != nil {
		return nil, err
	}

	switch status {
	case http.StatusOK:
		var certificateResponse CertificateResponse
		err = json.Unmarshal(body, &certificateResponse)
		if err != nil {
			return nil, fmt.Errorf("unmarshaling data: %w", err)
		}

		block, _ := pem.Decode([]byte(certificateResponse.Certificate))
		if block == nil {
			return nil, fmt.Errorf("no pem data found")
		}
		if block.Type == "CERTIFICATE" {
			return x509.ParseCertificate(block.Bytes)
		} else {
			return nil, fmt.Errorf("no certificate found in PEM data")
		}
	case http.StatusNotFound:
		return nil, nil
	default:
		return nil, fmt.Errorf("looking up certificate: http status %d", status)
	}
}

// get requests a path of the internal API and returns the status code and the body.
func (r RemoteRegistry) get(ctx context.Context, path string) (int, []byte, error) {
	return r.do(ctx, http.MethodGet, path, nil)
}

// do sends a request with a JSON body (none when body is nil) to a path of the internal
// API and returns the status code and the body of the response.
func (r RemoteRegistry) do(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(r.ManagerApiAddr, "/")+internalApiPath+path, reqBody)
	if err != nil {
		return 0, nil, fmt.Errorf("creating http request: %w", err)
	}
	req.Header.Set("accept", "application/json")
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}

	client := r.HTTPClient
	if client == nil {
		client = defaultHTTPClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("making http request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, fmt.Errorf("reading http body: %w", err)
	}
	return resp.StatusCode, respBody, nil
}
