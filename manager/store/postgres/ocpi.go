// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// SetRegistrationDetails creates or updates an OCPI registration by token
func (s *Store) SetRegistrationDetails(ctx context.Context, token string, registration *store.OcpiRegistration) error {
	params := SetOcpiRegistrationParams{
		Token:  token,
		Status: string(registration.Status),
	}

	_, err := s.q.SetOcpiRegistration(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to set registration details: %w", err)
	}

	return nil
}

// GetRegistrationDetails retrieves an OCPI registration by token
func (s *Store) GetRegistrationDetails(ctx context.Context, token string) (*store.OcpiRegistration, error) {
	reg, err := s.q.GetOcpiRegistration(ctx, token)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get registration details: %w", err)
	}

	return &store.OcpiRegistration{
		Status: store.OcpiRegistrationStatusType(reg.Status),
	}, nil
}

// DeleteRegistrationDetails removes an OCPI registration by token
func (s *Store) DeleteRegistrationDetails(ctx context.Context, token string) error {
	err := s.q.DeleteOcpiRegistration(ctx, token)
	if err != nil {
		return fmt.Errorf("failed to delete registration details: %w", err)
	}

	return nil
}

// SetPartyDetails creates or updates OCPI party details
func (s *Store) SetPartyDetails(ctx context.Context, partyDetails *store.OcpiParty) error {
	params := SetOcpiPartyParams{
		Role:        partyDetails.Role,
		CountryCode: partyDetails.CountryCode,
		PartyID:     partyDetails.PartyId,
		Url:         partyDetails.Url,
		Token:       partyDetails.Token,
	}

	_, err := s.q.SetOcpiParty(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to set party details: %w", err)
	}

	return nil
}

// GetPartyDetails retrieves OCPI party details by role, country code, and party ID
func (s *Store) GetPartyDetails(ctx context.Context, role, countryCode, partyId string) (*store.OcpiParty, error) {
	params := GetOcpiPartyParams{
		Role:        role,
		CountryCode: countryCode,
		PartyID:     partyId,
	}

	party, err := s.q.GetOcpiParty(ctx, params)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get party details: %w", err)
	}

	return &store.OcpiParty{
		CountryCode: party.CountryCode,
		PartyId:     party.PartyID,
		Role:        party.Role,
		Url:         party.Url,
		Token:       party.Token,
	}, nil
}

// ListPartyDetailsForRole retrieves all OCPI party details for a specific role
func (s *Store) ListPartyDetailsForRole(ctx context.Context, role string) ([]*store.OcpiParty, error) {
	parties, err := s.q.ListOcpiPartiesForRole(ctx, role)
	if err != nil {
		return nil, fmt.Errorf("failed to list party details for role: %w", err)
	}

	result := make([]*store.OcpiParty, len(parties))
	for i, p := range parties {
		result[i] = &store.OcpiParty{
			CountryCode: p.CountryCode,
			PartyId:     p.PartyID,
			Role:        p.Role,
			Url:         p.Url,
			Token:       p.Token,
		}
	}

	return result, nil
}
