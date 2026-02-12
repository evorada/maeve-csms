// SPDX-License-Identifier: Apache-2.0

package inmemory_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
)

func TestCreateAndGetReservation(t *testing.T) {
	now := time.Now()
	clk := clocktesting.NewFakeClock(now)
	s := inmemory.NewStore(clk)
	ctx := context.Background()

	parentTag := "parent001"
	reservation := &store.Reservation{
		ReservationId:   1,
		ChargeStationId: "cs001",
		ConnectorId:     1,
		IdTag:           "tag001",
		ParentIdTag:     &parentTag,
		ExpiryDate:      now.Add(1 * time.Hour),
		Status:          store.ReservationStatusAccepted,
		CreatedAt:       now,
	}

	err := s.CreateReservation(ctx, reservation)
	require.NoError(t, err)

	got, err := s.GetReservation(ctx, 1)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 1, got.ReservationId)
	assert.Equal(t, "cs001", got.ChargeStationId)
	assert.Equal(t, "tag001", got.IdTag)
	assert.Equal(t, &parentTag, got.ParentIdTag)
	assert.Equal(t, store.ReservationStatusAccepted, got.Status)
}

func TestGetReservation_NotFound(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	s := inmemory.NewStore(clk)
	ctx := context.Background()

	got, err := s.GetReservation(ctx, 999)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestCancelReservation(t *testing.T) {
	now := time.Now()
	clk := clocktesting.NewFakeClock(now)
	s := inmemory.NewStore(clk)
	ctx := context.Background()

	err := s.CreateReservation(ctx, &store.Reservation{
		ReservationId:   1,
		ChargeStationId: "cs001",
		ConnectorId:     1,
		IdTag:           "tag001",
		ExpiryDate:      now.Add(1 * time.Hour),
		Status:          store.ReservationStatusAccepted,
		CreatedAt:       now,
	})
	require.NoError(t, err)

	err = s.CancelReservation(ctx, 1)
	require.NoError(t, err)

	got, err := s.GetReservation(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, store.ReservationStatusCancelled, got.Status)
}

func TestCancelReservation_NotFound(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	s := inmemory.NewStore(clk)
	ctx := context.Background()

	err := s.CancelReservation(ctx, 999)
	require.Error(t, err)
}

func TestGetActiveReservations(t *testing.T) {
	now := time.Now()
	clk := clocktesting.NewFakeClock(now)
	s := inmemory.NewStore(clk)
	ctx := context.Background()

	// Create accepted reservation
	err := s.CreateReservation(ctx, &store.Reservation{
		ReservationId:   1,
		ChargeStationId: "cs001",
		ConnectorId:     1,
		IdTag:           "tag001",
		ExpiryDate:      now.Add(1 * time.Hour),
		Status:          store.ReservationStatusAccepted,
		CreatedAt:       now,
	})
	require.NoError(t, err)

	// Create cancelled reservation (should not appear)
	err = s.CreateReservation(ctx, &store.Reservation{
		ReservationId:   2,
		ChargeStationId: "cs001",
		ConnectorId:     2,
		IdTag:           "tag002",
		ExpiryDate:      now.Add(1 * time.Hour),
		Status:          store.ReservationStatusCancelled,
		CreatedAt:       now,
	})
	require.NoError(t, err)

	// Create reservation for different station
	err = s.CreateReservation(ctx, &store.Reservation{
		ReservationId:   3,
		ChargeStationId: "cs002",
		ConnectorId:     1,
		IdTag:           "tag003",
		ExpiryDate:      now.Add(1 * time.Hour),
		Status:          store.ReservationStatusAccepted,
		CreatedAt:       now,
	})
	require.NoError(t, err)

	active, err := s.GetActiveReservations(ctx, "cs001")
	require.NoError(t, err)
	assert.Len(t, active, 1)
	assert.Equal(t, 1, active[0].ReservationId)
}

func TestGetActiveReservations_Empty(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Now())
	s := inmemory.NewStore(clk)
	ctx := context.Background()

	active, err := s.GetActiveReservations(ctx, "cs001")
	require.NoError(t, err)
	assert.Empty(t, active)
}

func TestGetReservationByConnector(t *testing.T) {
	now := time.Now()
	clk := clocktesting.NewFakeClock(now)
	s := inmemory.NewStore(clk)
	ctx := context.Background()

	err := s.CreateReservation(ctx, &store.Reservation{
		ReservationId:   1,
		ChargeStationId: "cs001",
		ConnectorId:     2,
		IdTag:           "tag001",
		ExpiryDate:      now.Add(1 * time.Hour),
		Status:          store.ReservationStatusAccepted,
		CreatedAt:       now,
	})
	require.NoError(t, err)

	got, err := s.GetReservationByConnector(ctx, "cs001", 2)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 1, got.ReservationId)

	// Different connector
	got, err = s.GetReservationByConnector(ctx, "cs001", 3)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestExpireReservations(t *testing.T) {
	now := time.Now()
	clk := clocktesting.NewFakeClock(now)
	s := inmemory.NewStore(clk)
	ctx := context.Background()

	// Create reservation that should expire
	err := s.CreateReservation(ctx, &store.Reservation{
		ReservationId:   1,
		ChargeStationId: "cs001",
		ConnectorId:     1,
		IdTag:           "tag001",
		ExpiryDate:      now.Add(-1 * time.Hour), // already expired
		Status:          store.ReservationStatusAccepted,
		CreatedAt:       now.Add(-2 * time.Hour),
	})
	require.NoError(t, err)

	// Create reservation that should NOT expire
	err = s.CreateReservation(ctx, &store.Reservation{
		ReservationId:   2,
		ChargeStationId: "cs001",
		ConnectorId:     2,
		IdTag:           "tag002",
		ExpiryDate:      now.Add(1 * time.Hour), // future
		Status:          store.ReservationStatusAccepted,
		CreatedAt:       now,
	})
	require.NoError(t, err)

	count, err := s.ExpireReservations(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Verify first is expired
	r1, err := s.GetReservation(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, store.ReservationStatusExpired, r1.Status)

	// Verify second is still active
	r2, err := s.GetReservation(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, store.ReservationStatusAccepted, r2.Status)
}

func TestExpireReservations_NoneExpired(t *testing.T) {
	now := time.Now()
	clk := clocktesting.NewFakeClock(now)
	s := inmemory.NewStore(clk)
	ctx := context.Background()

	count, err := s.ExpireReservations(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}
