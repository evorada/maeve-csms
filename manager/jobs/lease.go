// SPDX-License-Identifier: Apache-2.0

package jobs

import (
	"context"
	"errors"
)

// ErrLeaseLost is returned by Lease.Renew when the lease expired or another holder took it.
var ErrLeaseLost = errors.New("lease lost")

// Lease grants exclusive ownership of a named job across replicas.
type Lease interface {
	// Acquire takes the lease if nobody holds it. false = held by someone else.
	Acquire(ctx context.Context, name string) (bool, error)
	// Renew extends a held lease; ErrLeaseLost if it expired or was taken over.
	Renew(ctx context.Context, name string) error
	// Release gives the lease up early (best effort).
	Release(ctx context.Context, name string) error
}
