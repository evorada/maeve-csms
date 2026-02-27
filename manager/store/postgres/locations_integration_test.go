// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func TestLocation_SetAndLookup(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	loc := &store.Location{
		Id:      "LOC001",
		Name:    "Test Location",
		Address: "123 Test St",
		City:    "TestCity",
		Country: "GBR",
		Coordinates: store.GeoLocation{
			Latitude:  "51.5074",
			Longitude: "-0.1278",
		},
		PostalCode:  "EC1A 1BB",
		ParkingType: "ON_STREET",
		LastUpdated: "2026-01-01T00:00:00Z",
	}

	err := testStore.SetLocation(ctx, loc)
	require.NoError(t, err)

	got, err := testStore.LookupLocation(ctx, "LOC001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Test Location", got.Name)
	assert.Equal(t, "51.5074", got.Coordinates.Latitude)
}

func TestLocation_LookupNotFound(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	got, err := testStore.LookupLocation(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestLocation_List(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		err := testStore.SetLocation(ctx, &store.Location{
			Id:          "LOC00" + string(rune('1'+i)),
			Name:        "Location " + string(rune('1'+i)),
			Address:     "Addr",
			City:        "City",
			Country:     "GBR",
			Coordinates: store.GeoLocation{Latitude: "51.5", Longitude: "-0.1"},
			PostalCode:  "EC1",
			ParkingType: "ON_STREET",
			LastUpdated: "2026-01-01T00:00:00Z",
		})
		require.NoError(t, err)
	}

	results, err := testStore.ListLocations(ctx, 0, 10)
	require.NoError(t, err)
	assert.Len(t, results, 3)
}
