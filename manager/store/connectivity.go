// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"time"
)

// ConnectivityStore records whether the websocket of a charge station is open, from the
// connection events of the gateways and the messages of the station. Events can arrive
// late and out of order (several manager replicas handle them), so every method decides
// from the stored connection id and timestamps, atomically, whether an event still
// applies.
type ConnectivityStore interface {
	// RecordStationConnected marks the station connected through connectionId at `at`
	// (Connected, LastConnectedAt, LastSeenAt, ConnectionId). Ignored (false) if a newer
	// connection is already recorded (LastConnectedAt > at).
	RecordStationConnected(ctx context.Context, chargeStationId, connectionId string, at time.Time) (applied bool, err error)
	// RecordStationDisconnected marks the station disconnected at `at` only if it is
	// connected, the stored ConnectionId equals connectionId, and LastConnectedAt <= at.
	RecordStationDisconnected(ctx context.Context, chargeStationId, connectionId string, at time.Time) (applied bool, err error)
	// TouchStationLastSeen sets LastSeenAt = max(LastSeenAt, at). No-op without a status row.
	TouchStationLastSeen(ctx context.Context, chargeStationId string, at time.Time) error
	// DisconnectStaleStations marks every connected station whose newest of LastSeenAt,
	// LastConnectedAt and LastHeartbeat is before seenBefore as disconnected at `now`
	// and returns their ids. A station with none of the three is left alone.
	DisconnectStaleStations(ctx context.Context, seenBefore, now time.Time) ([]string, error)
}
