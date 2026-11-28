// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The Firestore backend has no command queue: the command service is not started with it.

func (s *Store) CreateCommand(context.Context, *store.Command) error {
	return fmt.Errorf("CreateCommand: %w", store.ErrNotImplemented)
}

func (s *Store) GetCommand(context.Context, string) (*store.Command, error) {
	return nil, fmt.Errorf("GetCommand: %w", store.ErrNotImplemented)
}

func (s *Store) ListCommands(context.Context, store.CommandFilter) ([]*store.Command, int, error) {
	return nil, 0, fmt.Errorf("ListCommands: %w", store.ErrNotImplemented)
}

func (s *Store) ClaimNextCommand(context.Context, string, time.Time) (*store.Command, error) {
	return nil, fmt.Errorf("ClaimNextCommand: %w", store.ErrNotImplemented)
}

func (s *Store) TransitionCommand(context.Context, string, []store.CommandStatus, store.CommandUpdate) (*store.Command, error) {
	return nil, fmt.Errorf("TransitionCommand: %w", store.ErrNotImplemented)
}

func (s *Store) SetCommandProgress(context.Context, string, json.RawMessage, time.Time, *store.OutboxEvent) (*store.Command, error) {
	return nil, fmt.Errorf("SetCommandProgress: %w", store.ErrNotImplemented)
}

func (s *Store) RedactCommandPayload(context.Context, string, json.RawMessage) error {
	return fmt.Errorf("RedactCommandPayload: %w", store.ErrNotImplemented)
}

func (s *Store) ListQueuedStationKeys(context.Context, time.Time, string, int) ([]string, error) {
	return nil, fmt.Errorf("ListQueuedStationKeys: %w", store.ErrNotImplemented)
}

func (s *Store) ListExpiredQueuedCommands(context.Context, time.Time, int) ([]*store.Command, error) {
	return nil, fmt.Errorf("ListExpiredQueuedCommands: %w", store.ErrNotImplemented)
}

func (s *Store) ListOverdueSentCommands(context.Context, time.Time, int) ([]*store.Command, error) {
	return nil, fmt.Errorf("ListOverdueSentCommands: %w", store.ErrNotImplemented)
}

func (s *Store) DeleteCompletedCommandsBefore(context.Context, time.Time, int) (int, error) {
	return 0, fmt.Errorf("DeleteCompletedCommandsBefore: %w", store.ErrNotImplemented)
}
