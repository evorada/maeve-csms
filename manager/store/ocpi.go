// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
)

type OcpiRegistrationStatusType string

var (
	OcpiRegistrationStatusRegistered OcpiRegistrationStatusType = "registered"
	OcpiRegistrationStatusPending    OcpiRegistrationStatusType = "pending"
)

type OcpiRegistration struct {
	Status OcpiRegistrationStatusType
	// OrgId is the org (UUID) that owns the registration. PostgreSQL stores a
	// registration that is set without an org in LegacyOrgId. Firestore does not store
	// the org: it always reads as "".
	OrgId string `firestore:"-"`
}

type OcpiParty struct {
	CountryCode string
	PartyId     string
	Role        string
	Url         string
	Token       string
	// OrgId is the org (UUID) that owns the party details. PostgreSQL stores party
	// details that are set without an org in LegacyOrgId. Firestore does not store the
	// org: it always reads as "".
	OrgId string `firestore:"-"`
}

type OcpiStore interface {
	SetRegistrationDetails(ctx context.Context, token string, registration *OcpiRegistration) error
	GetRegistrationDetails(ctx context.Context, token string) (*OcpiRegistration, error)
	DeleteRegistrationDetails(ctx context.Context, token string) error

	SetPartyDetails(ctx context.Context, partyDetails *OcpiParty) error
	GetPartyDetails(ctx context.Context, role, countryCode, partyId string) (*OcpiParty, error)
	ListPartyDetailsForRole(ctx context.Context, role string) ([]*OcpiParty, error)
}
