// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"crypto/x509"
	"net"
	"strings"
)

type SecurityProfile int

const (
	UnsecuredTransportWithBasicAuth SecurityProfile = iota
	TLSWithBasicAuth
	TLSWithClientSideCertificates
)

type ChargeStation struct {
	// ClientId is the station id as the charge station presents it in the URL (no prefix).
	ClientId string
	// StationKey names the station inside the CSMS: it is used for the MQTT client id and
	// topics. "{OrgKey}.{ClientId}", or the ClientId in single-tenant mode.
	StationKey string
	// OrgId is the org that owns the station.
	OrgId string
	// OrgKey is the org key of the tenant of the host; "" in single-tenant mode.
	OrgKey                 string
	SecurityProfile        SecurityProfile
	Base64SHA256Password   string
	InvalidUsernameAllowed bool
}

type DeviceRegistry interface {
	// LookupChargeStation returns the charge station that connects to host with the id
	// clientId. It returns nil, nil when the host or the station is not known.
	LookupChargeStation(ctx context.Context, host, clientId string) (*ChargeStation, error)
	LookupCertificate(ctx context.Context, certHash string) (*x509.Certificate, error)
}

// NormalizeHost returns the host of a Host header in the form that the manager uses to
// find a tenant: lower case, without the port and without a trailing dot.
func NormalizeHost(host string) string {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.TrimSuffix(host, ".")
}
