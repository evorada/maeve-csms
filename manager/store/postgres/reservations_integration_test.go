// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// TestReservation_UsedIsNoLongerActive checks that a reservation that was consumed by a
// transaction is kept, but is not listed as active and is not expired later.
func TestReservation_UsedIsNoLongerActive(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	for id, connectorId := range map[int]int{1: 1, 2: 2} {
		err := testStore.CreateReservation(ctx, &store.Reservation{
			ReservationId:   id,
			ChargeStationId: "cs001",
			ConnectorId:     connectorId,
			IdTag:           "tag001",
			ParentIdTag:     ptr("group001"),
			ExpiryDate:      now.Add(-1 * time.Hour), // past its expiry date
			Status:          store.ReservationStatusAccepted,
			CreatedAt:       now.Add(-2 * time.Hour),
		})
		require.NoError(t, err)
	}

	// reservation 1 is consumed, reservation 2 is the control
	err := testStore.UpdateReservationStatus(ctx, 1, store.ReservationStatusUsed)
	require.NoError(t, err)

	got, err := testStore.GetReservation(ctx, 1)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, store.ReservationStatusUsed, got.Status)
	assert.Equal(t, "tag001", got.IdTag)
	assert.Equal(t, ptr("group001"), got.ParentIdTag)

	active, err := testStore.GetActiveReservations(ctx, "cs001")
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, 2, active[0].ReservationId)

	byConnector, err := testStore.GetReservationByConnector(ctx, "cs001", 1)
	require.NoError(t, err)
	assert.Nil(t, byConnector)
	byConnector, err = testStore.GetReservationByConnector(ctx, "cs001", 2)
	require.NoError(t, err)
	require.NotNil(t, byConnector)

	// only the reservation that is still accepted expires
	count, err := testStore.ExpireReservations(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	got, err = testStore.GetReservation(ctx, 1)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, store.ReservationStatusUsed, got.Status)
	got, err = testStore.GetReservation(ctx, 2)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, store.ReservationStatusExpired, got.Status)
}

// TestExpireReservations checks that only accepted reservations whose expiry date has passed
// are expired, and that the number of reservations that were expired is returned.
func TestExpireReservations(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	const (
		pastId = iota + 1
		alsoPastId
		futureId
		cancelledId
	)
	for id, expiryDate := range map[int]time.Time{
		pastId:      now.Add(-1 * time.Hour),
		alsoPastId:  now.Add(-1 * time.Minute),
		futureId:    now.Add(1 * time.Hour),
		cancelledId: now.Add(-1 * time.Hour),
	} {
		err := testStore.CreateReservation(ctx, &store.Reservation{
			ReservationId:   id,
			ChargeStationId: "cs001",
			ConnectorId:     id,
			IdTag:           "tag001",
			ExpiryDate:      expiryDate,
			Status:          store.ReservationStatusAccepted,
			CreatedAt:       now.Add(-2 * time.Hour),
		})
		require.NoError(t, err)
	}
	err := testStore.CancelReservation(ctx, cancelledId)
	require.NoError(t, err)

	requireStatus := func(reservationId int, want store.ReservationStatus) {
		t.Helper()
		got, err := testStore.GetReservation(ctx, reservationId)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, want, got.Status)
	}

	count, err := testStore.ExpireReservations(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	requireStatus(pastId, store.ReservationStatusExpired)
	requireStatus(alsoPastId, store.ReservationStatusExpired)
	requireStatus(futureId, store.ReservationStatusAccepted)
	requireStatus(cancelledId, store.ReservationStatusCancelled)

	// expiring again changes nothing: every replica may run the job
	count, err = testStore.ExpireReservations(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	requireStatus(futureId, store.ReservationStatusAccepted)
	requireStatus(cancelledId, store.ReservationStatusCancelled)

	active, err := testStore.GetActiveReservations(ctx, "cs001")
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, futureId, active[0].ReservationId)
}
