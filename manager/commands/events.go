// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"golang.org/x/exp/slog"
)

// completer moves commands to a final status and emits command.completed for each move
// that applied, in one store transaction. It then tells the hooks.
type completer struct {
	store  store.CommandStore
	tx     store.Transactor
	events events.Emitter
	hooks  []CompletionHook
}

// complete applies upd to the command id when its status is in from, replaces the
// secrets of its payload (RedactPaths) and emits command.completed with the stored
// command in the same transaction, then tells the hooks. late marks a result that
// arrived after the command timed out. It returns nil, nil when the command does not
// exist and store.ErrCommandConflict when its status is not in from; neither emits nor
// tells the hooks.
func (c completer) complete(ctx context.Context, id string, from []store.CommandStatus, upd store.CommandUpdate, late bool) (*store.Command, error) {
	if upd.CompletedAt == nil {
		return nil, errors.New("a completed command needs its completion time")
	}
	var done *store.Command
	err := c.tx.RunInTx(ctx, func(ctx context.Context) error {
		cmd, err := c.store.TransitionCommand(ctx, id, from, upd)
		if err != nil || cmd == nil {
			return err
		}
		// a command that ends without being sent keeps no secret either
		if redacted, changed := RedactPayload(cmd.Payload, cmd.RedactPaths); changed {
			if err := c.store.RedactCommandPayload(ctx, cmd.Id, redacted); err != nil {
				return fmt.Errorf("redacting the payload of command %s: %w", cmd.Id, err)
			}
			cmd.Payload = redacted
		}
		if err := c.events.Emit(ctx, completedEvent(cmd, late)); err != nil {
			return fmt.Errorf("emitting %s: %w", events.TypeCommandCompleted, err)
		}
		done = cmd
		return nil
	})
	if err != nil {
		return nil, err
	}
	if done != nil {
		c.afterCompletion(ctx, done)
	}
	return done, nil
}

// afterCompletion tells the hooks about a command that has just completed. A hook that
// panics is logged and the next one is told all the same.
func (c completer) afterCompletion(ctx context.Context, cmd *store.Command) {
	for _, hook := range c.hooks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("a hook on the completion of a command panicked", "commandId", cmd.Id,
						"stationKey", cmd.StationKey, "action", cmd.Action, "panic", r)
				}
			}()
			hook.AfterCompletion(ctx, cmd)
		}()
	}
}

// completedEvent is the command.completed event of a command that has just reached its
// final status. Its org is the org of the command and its actor is whoever created the
// command (when the command names nobody, the actor of ctx or the manager).
func completedEvent(cmd *store.Command, late bool) events.Event {
	var actor string
	if cmd.Actor != nil {
		actor = *cmd.Actor
	}
	data := events.CommandCompletedData{
		CommandId:   cmd.Id,
		StationKey:  cmd.StationKey,
		StationId:   events.StationId(cmd.StationKey),
		Action:      cmd.Action,
		Status:      string(cmd.Status),
		Result:      cmd.Result,
		CompletedAt: cmd.CompletedAt.UTC(),
		OrgId:       cmd.OrgId,
		OcppVersion: cmd.OcppVersion,
		Actor:       cmd.Actor,
		Jti:         cmd.Jti,
		CreatedAt:   cmd.CreatedAt.UTC(),
		Late:        late,
	}
	if cmd.ErrorCode != nil {
		data.ErrorCode = *cmd.ErrorCode
	}
	if cmd.ErrorDescription != nil {
		data.ErrorDescription = *cmd.ErrorDescription
	}
	if cmd.SentAt != nil {
		sentAt := cmd.SentAt.UTC()
		data.SentAt = &sentAt
	}
	return events.Event{
		Type:         events.TypeCommandCompleted,
		OrgId:        cmd.OrgId,
		ResourceType: events.ResourceCommand,
		ResourceId:   cmd.Id,
		Actor:        actor,
		Time:         *cmd.CompletedAt,
		Data:         data,
	}
}
