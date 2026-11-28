// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"strings"
)

type SecurityProfile int

const (
	UnsecuredTransportWithBasicAuth SecurityProfile = iota
	TLSWithBasicAuth
	TLSWithClientSideCertificates
)

// The transports of an onboarding connection.
const (
	OnboardingTransportWs  = "ws"  // no TLS
	OnboardingTransportWss = "wss" // TLS
)

// The credentials with which the gateway admits a charge station with the onboarding
// rules.
const (
	// OnboardingCredentialOpen is a connection without TLS and without a password.
	OnboardingCredentialOpen = "open"
	// OnboardingCredentialOnboardingPassword is the onboarding password of the tenant.
	OnboardingCredentialOnboardingPassword = "onboarding_password"
	// OnboardingCredentialPendingPassword is the new password of a pending station whose
	// approval is in flight.
	OnboardingCredentialPendingPassword = "pending_password"
)

// ErrPendingLimitReached is the error of RecordOnboardingConnection when the tenant
// already has as many pending charge stations as it allows.
var ErrPendingLimitReached = errors.New("the tenant has as many pending charge stations as it allows")

// Onboarding says how a charge station that is not registered, or waits for approval,
// may connect.
type Onboarding struct {
	// OpenSP1 lets the station connect without TLS and without a password.
	OpenSP1 bool
	// Base64SHA256Password is the base64 encoded SHA-256 hash of the onboarding password
	// of the tenant; "" when it has none.
	Base64SHA256Password string
}

type ChargeStation struct {
	// ClientId is the station id as the charge station presents it in the URL (no prefix).
	ClientId string
	// StationKey names the station inside the CSMS: it is used for the MQTT client id and
	// topics. "{OrgKey}.{ClientId}", or the ClientId in single-tenant mode.
	StationKey string
	// OrgId is the org that owns the station.
	OrgId string
	// OrgKey is the org key of the tenant of the host; "" in single-tenant mode.
	OrgKey string
	// SecurityProfile is the security profile of a station that exists; it does not apply
	// to a station with Onboarding.
	SecurityProfile      SecurityProfile
	Base64SHA256Password string
	// PendingBase64SHA256Password is the hash of the new password of the station while it
	// is being told to use it; "" when there is none. The station may connect with
	// either password.
	PendingBase64SHA256Password string
	InvalidUsernameAllowed      bool
	// Onboarding is set for a charge station that is admitted with the onboarding rules
	// instead of its security profile: a station that is not registered, of a tenant
	// that onboards such stations, or a pending one. Such a connection is recorded with
	// RecordOnboardingConnection before it is accepted.
	Onboarding *Onboarding
	// HasTenantOrgNames says that AcceptedCertOrgNames is the list of the tenant of the
	// station. Without it the tenant has no list and the list of the gateway applies.
	HasTenantOrgNames bool
	// AcceptedCertOrgNames are the organisation names that the tenant of the station
	// accepts in a client certificate. An empty list of a tenant (HasTenantOrgNames)
	// means that the organisation of a certificate is not checked.
	AcceptedCertOrgNames []string
}

type DeviceRegistry interface {
	// LookupChargeStation returns the charge station that connects to host with the id
	// clientId. It returns nil, nil when the host or the station is not known.
	LookupChargeStation(ctx context.Context, host, clientId string) (*ChargeStation, error)
	LookupCertificate(ctx context.Context, certHash string) (*x509.Certificate, error)
	// RecordOnboardingConnection records that the gateway admitted the charge station that
	// connects to host with the id clientId with the onboarding rules, over the transport
	// (OnboardingTransport*) with the credential (OnboardingCredential*). The manager makes
	// a station that is not known a pending station. ErrPendingLimitReached when the
	// tenant already has as many pending stations as it allows; any other error means
	// that the station must not be admitted now.
	RecordOnboardingConnection(ctx context.Context, host, clientId, transport, credential string) error
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
