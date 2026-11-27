// SPDX-License-Identifier: Apache-2.0

package inmemory

import (
	"context"
	"slices"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The connectivity methods check and write under the store mutex, so that the events of
// one station handled concurrently cannot interleave. Each write stores a new status
// value: GetChargeStationStatus hands out copies that share the pointers of the old one.

func (s *Store) RecordStationConnected(_ context.Context, chargeStationId, connectionId string, at time.Time) (bool, error) {
	s.Lock()
	defer s.Unlock()

	status := store.ChargeStationStatus{ChargeStationId: chargeStationId}
	if stored, ok := s.chargeStationStatuses[chargeStationId]; ok {
		if stored.LastConnectedAt != nil && stored.LastConnectedAt.After(at) {
			return false, nil
		}
		status = *stored
	}

	status.Connected = true
	status.LastConnectedAt = &at
	if status.LastSeenAt == nil || status.LastSeenAt.Before(at) {
		status.LastSeenAt = &at
	}
	status.ConnectionId = &connectionId
	status.UpdatedAt = s.clock.Now()
	s.chargeStationStatuses[chargeStationId] = &status
	return true, nil
}

func (s *Store) RecordStationDisconnected(_ context.Context, chargeStationId, connectionId string, at time.Time) (bool, error) {
	s.Lock()
	defer s.Unlock()

	stored, ok := s.chargeStationStatuses[chargeStationId]
	if !ok || !stored.Connected ||
		stored.ConnectionId == nil || *stored.ConnectionId != connectionId ||
		stored.LastConnectedAt == nil || stored.LastConnectedAt.After(at) {
		return false, nil
	}

	status := *stored
	status.Connected = false
	status.LastDisconnectedAt = &at
	status.UpdatedAt = s.clock.Now()
	s.chargeStationStatuses[chargeStationId] = &status
	return true, nil
}

func (s *Store) TouchStationLastSeen(_ context.Context, chargeStationId string, at time.Time) error {
	s.Lock()
	defer s.Unlock()

	stored, ok := s.chargeStationStatuses[chargeStationId]
	if !ok || (stored.LastSeenAt != nil && !stored.LastSeenAt.Before(at)) {
		return nil
	}

	status := *stored
	status.LastSeenAt = &at
	status.UpdatedAt = s.clock.Now()
	s.chargeStationStatuses[chargeStationId] = &status
	return nil
}

func (s *Store) DisconnectStaleStations(_ context.Context, seenBefore, now time.Time) ([]string, error) {
	s.Lock()
	defer s.Unlock()

	ids := []string{}
	for chargeStationId, stored := range s.chargeStationStatuses {
		if !stored.Connected {
			continue
		}
		newest := latest(stored.LastSeenAt, stored.LastConnectedAt, stored.LastHeartbeat)
		if newest == nil || !newest.Before(seenBefore) {
			continue
		}

		status := *stored
		status.Connected = false
		status.LastDisconnectedAt = &now
		status.UpdatedAt = s.clock.Now()
		s.chargeStationStatuses[chargeStationId] = &status
		ids = append(ids, chargeStationId)
	}
	slices.Sort(ids)
	return ids, nil
}

// latest returns the latest of the times that are not nil, nil when all are.
func latest(times ...*time.Time) *time.Time {
	var newest *time.Time
	for _, t := range times {
		if t != nil && (newest == nil || t.After(*newest)) {
			newest = t
		}
	}
	return newest
}
