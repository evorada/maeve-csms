// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The Firestore backend has no outbox: domain events are disabled with it.

func (s *Store) AppendOutboxEvents(context.Context, ...*store.OutboxEvent) error {
	return fmt.Errorf("AppendOutboxEvents: %w", store.ErrNotImplemented)
}

func (s *Store) RelayOutboxEvents(context.Context, int, store.OutboxPublishFunc) (int, error) {
	return 0, fmt.Errorf("RelayOutboxEvents: %w", store.ErrNotImplemented)
}

func (s *Store) ListOutboxEvents(context.Context, store.OutboxEventFilter) ([]*store.OutboxEvent, error) {
	return nil, fmt.Errorf("ListOutboxEvents: %w", store.ErrNotImplemented)
}

func (s *Store) PurgePublishedOutboxEvents(context.Context, time.Time) (int64, error) {
	return 0, fmt.Errorf("PurgePublishedOutboxEvents: %w", store.ErrNotImplemented)
}
