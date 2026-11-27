// SPDX-License-Identifier: Apache-2.0

package jobs

import (
	"context"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/store"
	"golang.org/x/exp/slog"
)

// ReservationExpiryJob marks expired reservations (status Accepted, expiry in the past) as Expired.
func ReservationExpiryJob(reservations store.ReservationStore, interval time.Duration) Job {
	return Job{
		Name:     "reservation-expiry",
		Interval: interval,
		Run: func(ctx context.Context) error {
			n, err := reservations.ExpireReservations(ctx)
			if n > 0 {
				slog.Info("expired reservations", "count", n)
			}
			return err
		},
	}
}
