// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// SetLocation creates or updates a location
func (s *Store) SetLocation(ctx context.Context, location *store.Location) error {
	// Serialize location data to JSON
	locationData, err := json.Marshal(location)
	if err != nil {
		return fmt.Errorf("failed to marshal location data: %w", err)
	}

	// Extract country code and party ID from location
	// These are stored separately for indexing purposes
	countryCode := location.Country
	if countryCode == "" {
		countryCode = "XX" // Default if not provided
	}

	// PartyId is not directly in Location struct, so we'll use a default
	// In a real implementation, this might come from context or be part of the location
	partyId := "XXX"

	params := SetLocationParams{
		ID:           location.Id,
		CountryCode:  countryCode,
		PartyID:      partyId,
		LocationData: locationData,
	}

	_, err = s.q.SetLocation(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to set location: %w", err)
	}

	return nil
}

// LookupLocation retrieves a location by its ID
func (s *Store) LookupLocation(ctx context.Context, locationId string) (*store.Location, error) {
	loc, err := s.q.GetLocation(ctx, locationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup location: %w", err)
	}

	// Deserialize location data from JSON
	var location store.Location
	if err := json.Unmarshal(loc.LocationData, &location); err != nil {
		return nil, fmt.Errorf("failed to unmarshal location data: %w", err)
	}

	return &location, nil
}

// ListLocations retrieves a paginated list of locations
func (s *Store) ListLocations(ctx context.Context, offset int, limit int) ([]*store.Location, error) {
	params := ListAllLocationsParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	}

	locations, err := s.q.ListAllLocations(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list locations: %w", err)
	}

	result := make([]*store.Location, len(locations))
	for i, loc := range locations {
		var location store.Location
		if err := json.Unmarshal(loc.LocationData, &location); err != nil {
			return nil, fmt.Errorf("failed to unmarshal location data: %w", err)
		}
		result[i] = &location
	}

	return result, nil
}
