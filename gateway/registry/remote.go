// SPDX-License-Identifier: Apache-2.0

package registry

import (
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
	OrgId                  string `json:"orgId"`
	OrgKey                 string `json:"orgKey"`
	StationKey             string `json:"stationKey"`
	SecurityProfile        *int   `json:"securityProfile"`
	Base64SHA256Password   string `json:"base64SHA256Password"`
	InvalidUsernameAllowed bool   `json:"invalidUsernameAllowed"`
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
	if resp.SecurityProfile == nil {
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

	return &ChargeStation{
		ClientId:               clientId,
		StationKey:             resp.StationKey,
		OrgId:                  resp.OrgId,
		OrgKey:                 resp.OrgKey,
		SecurityProfile:        SecurityProfile(*resp.SecurityProfile),
		Base64SHA256Password:   resp.Base64SHA256Password,
		InvalidUsernameAllowed: resp.InvalidUsernameAllowed,
	}, nil
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(r.ManagerApiAddr, "/")+internalApiPath+path, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("creating http request: %w", err)
	}
	req.Header.Set("accept", "application/json")

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

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, fmt.Errorf("reading http body: %w", err)
	}
	return resp.StatusCode, body, nil
}
