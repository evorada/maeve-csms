// SPDX-License-Identifier: Apache-2.0

package jobs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	clocktesting "k8s.io/utils/clock/testing"
)

func TestReservationExpiryJob(t *testing.T) {
	now := time.Now()
	clk := clocktesting.NewFakeClock(now)
	engine := inmemory.NewStore(clk)
	ctx := context.Background()

	const (
		pastId = iota + 1
		futureId
		cancelledId
	)
	for id, expiryDate := range map[int]time.Time{
		pastId:      now.Add(-time.Second),
		futureId:    now.Add(time.Hour),
		cancelledId: now.Add(-time.Hour),
	} {
		require.NoError(t, engine.CreateReservation(ctx, &store.Reservation{
			ReservationId:   id,
			ChargeStationId: "cs001",
			ConnectorId:     id,
			IdTag:           "tag001",
			ExpiryDate:      expiryDate,
			Status:          store.ReservationStatusAccepted,
			CreatedAt:       now.Add(-2 * time.Hour),
		}))
	}
	require.NoError(t, engine.CancelReservation(ctx, cancelledId))

	requireStatus := func(reservationId int, want store.ReservationStatus) {
		t.Helper()
		got, err := engine.GetReservation(ctx, reservationId)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, want, got.Status)
	}

	job := jobs.ReservationExpiryJob(engine, time.Minute)
	assert.Equal(t, "reservation-expiry", job.Name)
	assert.Equal(t, time.Minute, job.Interval)
	require.NoError(t, jobs.Validate([]jobs.Job{job}))

	require.NoError(t, job.Run(ctx))
	requireStatus(pastId, store.ReservationStatusExpired)
	requireStatus(futureId, store.ReservationStatusAccepted)
	requireStatus(cancelledId, store.ReservationStatusCancelled)

	// the reservation that was still valid expires in the first run after its expiry date
	clk.Step(time.Hour + time.Second)
	require.NoError(t, job.Run(ctx))
	requireStatus(pastId, store.ReservationStatusExpired)
	requireStatus(futureId, store.ReservationStatusExpired)
	requireStatus(cancelledId, store.ReservationStatusCancelled)
}

// failingReservationStore is a reservation store that cannot expire reservations.
type failingReservationStore struct {
	store.ReservationStore
	err error
}

func (s failingReservationStore) ExpireReservations(context.Context) (int, error) {
	return 0, s.err
}

func TestReservationExpiryJobReturnsStoreError(t *testing.T) {
	storeErr := errors.New("connection refused")
	job := jobs.ReservationExpiryJob(failingReservationStore{err: storeErr}, time.Minute)

	err := job.Run(context.Background())

	assert.ErrorIs(t, err, storeErr)
}
