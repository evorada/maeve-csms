// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The Firestore backend does not track connectivity: Connected is only set by
// BootNotification and Heartbeat there.

func (s *Store) RecordStationConnected(context.Context, string, string, time.Time) (bool, error) {
	return false, fmt.Errorf("RecordStationConnected: %w", store.ErrNotImplemented)
}

func (s *Store) RecordStationDisconnected(context.Context, string, string, time.Time) (bool, error) {
	return false, fmt.Errorf("RecordStationDisconnected: %w", store.ErrNotImplemented)
}

func (s *Store) TouchStationLastSeen(context.Context, string, time.Time) error {
	return fmt.Errorf("TouchStationLastSeen: %w", store.ErrNotImplemented)
}

func (s *Store) DisconnectStaleStations(context.Context, time.Time, time.Time) ([]string, error) {
	return nil, fmt.Errorf("DisconnectStaleStations: %w", store.ErrNotImplemented)
}
