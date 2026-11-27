// SPDX-License-Identifier: Apache-2.0

package inmemory

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// commandKey returns the canonical form of a command id, under which the command is
// stored; false when the id is not a UUID, which no stored command has.
func commandKey(id string) (string, bool) {
	u, err := uuid.Parse(id)
	if err != nil {
		return "", false
	}
	return u.String(), true
}

func (s *Store) CreateCommand(_ context.Context, cmd *store.Command) error {
	if cmd.Status != store.CommandStatusQueued {
		return fmt.Errorf("create command %s: status must be %s, not %q", cmd.Id, store.CommandStatusQueued, cmd.Status)
	}
	id, ok := commandKey(cmd.Id)
	if !ok {
		return fmt.Errorf("create command: invalid id %q", cmd.Id)
	}
	orgId, err := uuid.Parse(cmd.OrgId)
	if err != nil {
		return fmt.Errorf("create command %s: invalid org id %q: %w", cmd.Id, cmd.OrgId, err)
	}

	s.Lock()
	defer s.Unlock()
	if _, found := s.commands[id]; found {
		return fmt.Errorf("create command %s: %w", cmd.Id, store.ErrAlreadyExists)
	}
	stored := copyCommand(cmd)
	stored.Id = id
	stored.OrgId = orgId.String()
	s.commands[id] = stored
	return nil
}

func (s *Store) GetCommand(_ context.Context, id string) (*store.Command, error) {
	key, ok := commandKey(id)
	if !ok {
		return nil, nil
	}
	s.Lock()
	defer s.Unlock()
	cmd := s.commands[key]
	if cmd == nil {
		return nil, nil
	}
	return copyCommand(cmd), nil
}

func (s *Store) ListCommands(_ context.Context, filter store.CommandFilter) ([]*store.Command, int, error) {
	orgId := ""
	if filter.OrgId != "" {
		u, err := uuid.Parse(filter.OrgId)
		if err != nil {
			return nil, 0, fmt.Errorf("list commands: invalid org id %q: %w", filter.OrgId, err)
		}
		orgId = u.String()
	}
	payloadFilter, ok := containmentFilter(filter.PayloadContains)
	if !ok {
		return nil, 0, fmt.Errorf("list commands: payload filter is not a JSON object: %s", filter.PayloadContains)
	}
	progressFilter, ok := containmentFilter(filter.ProgressContains)
	if !ok {
		return nil, 0, fmt.Errorf("list commands: progress filter is not a JSON object: %s", filter.ProgressContains)
	}

	s.Lock()
	defer s.Unlock()

	var matching []*store.Command
	for _, cmd := range s.commands {
		switch {
		case orgId != "" && cmd.OrgId != orgId,
			filter.StationKey != "" && cmd.StationKey != filter.StationKey,
			len(filter.Actions) > 0 && !slices.Contains(filter.Actions, cmd.Action),
			len(filter.Statuses) > 0 && !slices.Contains(filter.Statuses, cmd.Status),
			payloadFilter != nil && !payloadContains(cmd.Payload, payloadFilter),
			progressFilter != nil && !payloadContains(cmd.Progress, progressFilter),
			filter.CreatedAfter != nil && !cmd.CreatedAt.After(*filter.CreatedAfter),
			filter.CreatedBefore != nil && !cmd.CreatedAt.Before(*filter.CreatedBefore):
			continue
		}
		matching = append(matching, cmd)
	}
	// newest first, like PostgreSQL: created_at DESC, id DESC (the canonical text of a
	// UUID sorts like its bytes)
	slices.SortFunc(matching, func(a, b *store.Command) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), strings.Compare(b.Id, a.Id))
	})

	total := len(matching)
	start := min(filter.EffectiveOffset(), total)
	end := min(start+filter.EffectiveLimit(), total)
	page := make([]*store.Command, 0, end-start)
	for _, cmd := range matching[start:end] {
		page = append(page, copyCommand(cmd))
	}
	return page, total, nil
}

// containmentFilter decodes a containment filter of ListCommands: nil when it is not
// set, false when it is not a JSON object.
func containmentFilter(filter json.RawMessage) (map[string]any, bool) {
	if len(filter) == 0 {
		return nil, true
	}
	var want map[string]any
	if err := json.Unmarshal(filter, &want); err != nil || want == nil {
		return nil, false
	}
	return want, true
}

// payloadContains reports whether every top-level key of want is in the JSON object
// payload (or progress) with an equal value. Unlike the containment of PostgreSQL,
// nested values must be equal, not contained.
func payloadContains(payload json.RawMessage, want map[string]any) bool {
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		return false
	}
	for key, value := range want {
		gotValue, found := got[key]
		if !found || !reflect.DeepEqual(gotValue, value) {
			return false
		}
	}
	return true
}

func (s *Store) ClaimNextCommand(_ context.Context, stationKey string, now time.Time) (*store.Command, error) {
	s.Lock()
	defer s.Unlock()

	var next *store.Command
	for _, cmd := range s.commands {
		if cmd.StationKey != stationKey {
			continue
		}
		switch cmd.Status {
		case store.CommandStatusSent:
			return nil, nil
		case store.CommandStatusQueued:
			if cmd.ExpiresAt.After(now) && (next == nil || compareQueueOrder(cmd, next) < 0) {
				next = cmd
			}
		}
	}
	if next == nil {
		return nil, nil
	}
	next.Status = store.CommandStatusSent
	sentAt := now
	next.SentAt = &sentAt
	return copyCommand(next), nil
}

// compareQueueOrder orders queued commands as they are sent: oldest first, then by id.
func compareQueueOrder(a, b *store.Command) int {
	return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), strings.Compare(a.Id, b.Id))
}

func (s *Store) TransitionCommand(ctx context.Context, id string, from []store.CommandStatus, upd store.CommandUpdate) (*store.Command, error) {
	key, ok := commandKey(id)
	if !ok {
		return nil, nil
	}
	s.Lock()
	defer s.Unlock()

	cmd := s.commands[key]
	if cmd == nil {
		return nil, nil
	}
	if !slices.Contains(from, cmd.Status) {
		return nil, fmt.Errorf("transition command %s to %s: status is %s, not one of %v: %w",
			id, upd.Status, cmd.Status, from, store.ErrCommandConflict)
	}
	// what the unique index on the sent commands of a station does in PostgreSQL
	if upd.Status == store.CommandStatusSent && s.otherCommandSent(cmd) {
		return nil, fmt.Errorf("transition command %s to %s: another command of the station is in flight: %w",
			id, upd.Status, store.ErrCommandConflict)
	}

	cmd.Status = upd.Status
	if upd.Result != nil {
		cmd.Result = slices.Clone(upd.Result)
	}
	cmd.ErrorCode = copyOrNil(upd.ErrorCode)
	cmd.ErrorDescription = copyOrNil(upd.ErrorDescription)
	cmd.SentAt = copyOrNil(upd.SentAt)
	cmd.CompletedAt = copyOrNil(upd.CompletedAt)
	if upd.Event != nil {
		s.appendOutboxEventsLocked(ctx, upd.Event)
	}
	return copyCommand(cmd), nil
}

// otherCommandSent reports whether a command of the station of cmd, other than cmd, is
// in status sent. The caller holds the store mutex.
func (s *Store) otherCommandSent(cmd *store.Command) bool {
	for _, other := range s.commands {
		if other != cmd && other.StationKey == cmd.StationKey && other.Status == store.CommandStatusSent {
			return true
		}
	}
	return false
}

func (s *Store) SetCommandProgress(ctx context.Context, id string, progress json.RawMessage, at time.Time, evt *store.OutboxEvent) (*store.Command, error) {
	key, ok := commandKey(id)
	if !ok {
		return nil, nil
	}
	s.Lock()
	defer s.Unlock()

	cmd := s.commands[key]
	if cmd == nil {
		return nil, nil
	}
	cmd.Progress = slices.Clone(progress)
	cmd.ProgressAt = &at
	if evt != nil {
		s.appendOutboxEventsLocked(ctx, evt)
	}
	return copyCommand(cmd), nil
}

func (s *Store) ListQueuedStationKeys(_ context.Context, now time.Time, afterStationKey string, limit int) ([]string, error) {
	if limit < 1 {
		return nil, fmt.Errorf("list queued station keys: limit must be positive: %d", limit)
	}
	s.Lock()
	defer s.Unlock()

	keys := []string{}
	for _, cmd := range s.commands {
		if cmd.Status == store.CommandStatusQueued && cmd.ExpiresAt.After(now) && cmd.StationKey > afterStationKey &&
			!slices.Contains(keys, cmd.StationKey) {
			keys = append(keys, cmd.StationKey)
		}
	}
	slices.Sort(keys)
	return keys[:min(limit, len(keys))], nil
}

func (s *Store) ListExpiredQueuedCommands(_ context.Context, now time.Time, limit int) ([]*store.Command, error) {
	if limit < 1 {
		return nil, fmt.Errorf("list expired queued commands: limit must be positive: %d", limit)
	}
	s.Lock()
	defer s.Unlock()

	return s.firstCommands(limit,
		func(cmd *store.Command) bool {
			return cmd.Status == store.CommandStatusQueued && !cmd.ExpiresAt.After(now)
		},
		func(a, b *store.Command) int {
			return cmp.Or(a.ExpiresAt.Compare(b.ExpiresAt), strings.Compare(a.Id, b.Id))
		}), nil
}

func (s *Store) ListOverdueSentCommands(_ context.Context, sentBefore time.Time, limit int) ([]*store.Command, error) {
	if limit < 1 {
		return nil, fmt.Errorf("list overdue sent commands: limit must be positive: %d", limit)
	}
	s.Lock()
	defer s.Unlock()

	return s.firstCommands(limit,
		func(cmd *store.Command) bool {
			return cmd.Status == store.CommandStatusSent && cmd.SentAt != nil && !cmd.SentAt.After(sentBefore)
		},
		func(a, b *store.Command) int {
			return cmp.Or(a.SentAt.Compare(*b.SentAt), strings.Compare(a.Id, b.Id))
		}), nil
}

func (s *Store) DeleteCompletedCommandsBefore(_ context.Context, before time.Time, limit int) (int, error) {
	if limit < 1 {
		return 0, fmt.Errorf("delete completed commands: limit must be positive: %d", limit)
	}
	s.Lock()
	defer s.Unlock()

	completed := s.firstCommands(limit,
		func(cmd *store.Command) bool {
			return cmd.Status.IsTerminal() && cmd.CompletedAt != nil && cmd.CompletedAt.Before(before)
		},
		func(a, b *store.Command) int {
			return cmp.Or(a.CompletedAt.Compare(*b.CompletedAt), strings.Compare(a.Id, b.Id))
		})
	for _, cmd := range completed {
		delete(s.commands, cmd.Id)
	}
	return len(completed), nil
}

// firstCommands returns copies of the first limit commands that match, in the given
// order. The caller holds the store mutex.
func (s *Store) firstCommands(limit int, match func(*store.Command) bool, order func(a, b *store.Command) int) []*store.Command {
	var matching []*store.Command
	for _, cmd := range s.commands {
		if match(cmd) {
			matching = append(matching, cmd)
		}
	}
	slices.SortFunc(matching, order)
	commands := make([]*store.Command, 0, min(limit, len(matching)))
	for _, cmd := range matching[:min(limit, len(matching))] {
		commands = append(commands, copyCommand(cmd))
	}
	return commands
}

func copyCommand(cmd *store.Command) *store.Command {
	c := *cmd
	c.Payload = slices.Clone(cmd.Payload)
	c.Result = slices.Clone(cmd.Result)
	c.Progress = slices.Clone(cmd.Progress)
	c.ErrorCode = copyOrNil(cmd.ErrorCode)
	c.ErrorDescription = copyOrNil(cmd.ErrorDescription)
	c.ProgressAt = copyOrNil(cmd.ProgressAt)
	c.Actor = copyOrNil(cmd.Actor)
	c.Jti = copyOrNil(cmd.Jti)
	c.SentAt = copyOrNil(cmd.SentAt)
	c.CompletedAt = copyOrNil(cmd.CompletedAt)
	return &c
}

// copyOrNil returns a pointer to a copy of the value that p points to, or nil.
func copyOrNil[T any](p *T) *T {
	if p == nil {
		return nil
	}
	return copyOf(p)
}
