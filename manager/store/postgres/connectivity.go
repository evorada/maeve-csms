// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Each method below is one statement that checks and writes in the database, so that the
// connection events of one station handled by several manager replicas at the same time
// cannot interleave. They join the transaction of the ctx when there is one.

func (s *Store) RecordStationConnected(ctx context.Context, chargeStationId, connectionId string, at time.Time) (bool, error) {
	rows, err := s.writeQueries(ctx).RecordStationConnected(ctx, RecordStationConnectedParams{
		ChargeStationID: chargeStationId,
		ConnectedAt:     pgtype.Timestamptz{Time: at, Valid: true},
		ConnectionID:    pgtype.Text{String: connectionId, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("failed to record station connected: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) RecordStationDisconnected(ctx context.Context, chargeStationId, connectionId string, at time.Time) (bool, error) {
	rows, err := s.writeQueries(ctx).RecordStationDisconnected(ctx, RecordStationDisconnectedParams{
		ChargeStationID: chargeStationId,
		DisconnectedAt:  pgtype.Timestamptz{Time: at, Valid: true},
		ConnectionID:    pgtype.Text{String: connectionId, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("failed to record station disconnected: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) TouchStationLastSeen(ctx context.Context, chargeStationId string, at time.Time) error {
	err := s.writeQueries(ctx).TouchStationLastSeen(ctx, TouchStationLastSeenParams{
		ChargeStationID: chargeStationId,
		SeenAt:          pgtype.Timestamptz{Time: at, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to touch station last seen: %w", err)
	}
	return nil
}

func (s *Store) DisconnectStaleStations(ctx context.Context, seenBefore, now time.Time) ([]string, error) {
	ids, err := s.writeQueries(ctx).DisconnectStaleStations(ctx, DisconnectStaleStationsParams{
		SeenBefore: pgtype.Timestamptz{Time: seenBefore, Valid: true},
		Now:        pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to disconnect stale stations: %w", err)
	}
	slices.Sort(ids)
	return ids, nil
}
