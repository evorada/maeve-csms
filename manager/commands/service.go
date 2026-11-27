// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/google/uuid"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

var (
	ErrMissingField       = errors.New("command is missing a required field")
	ErrUnknownAction      = errors.New("unknown action for OCPP version")
	ErrInvalidPayload     = errors.New("payload does not match OCPP schema")
	ErrUnknownOcppVersion = errors.New("station OCPP version unknown")
	ErrInvalidExpiry      = errors.New("expiry out of range")
	ErrNotCancellable     = errors.New("command is not queued")
)

// Preprocessor changes a command before it is checked and stored: it may rewrite its
// payload or set its progress. The command already has its id and creation time.
type Preprocessor interface {
	Preprocess(ctx context.Context, cmd *store.Command) error
}

// Service is what the rest of the manager uses to send commands to the stations: it
// checks and stores them and has the dispatcher send them.
type Service struct {
	Store store.CommandStore
	// Tx runs a cancellation and its event in one transaction.
	Tx store.Transactor
	// Events receives the command.completed event of a cancellation.
	Events events.Emitter
	// RuntimeDetails gives the OCPP version of a station when the command names none.
	RuntimeDetails store.ChargeStationRuntimeDetailsStore
	Dispatcher     *Dispatcher
	// SchemaFS holds the schemas of the requests.
	SchemaFS fs.FS
	Clock    clock.PassiveClock
	Config   Config
	// NewId returns the id of a new command; nil is a new UUIDv7.
	NewId func() string
	// Preprocessors run in order before the payload is checked.
	Preprocessors []Preprocessor
}

// Enqueue checks and stores a command, then has the dispatcher send it when its station
// is connected. The caller sets OrgId, StationKey, Action and Payload and may set
// OcppVersion, Actor, Jti and ExpiresAt; the rest is set here. cmd is not changed. It
// returns the command as stored after the attempt to send it.
//
// Enqueue must not be called inside a store transaction: the command is sent before
// that transaction would commit.
func (s *Service) Enqueue(ctx context.Context, cmd *store.Command) (*store.Command, error) {
	if cmd == nil {
		return nil, fmt.Errorf("%w: no command", ErrMissingField)
	}
	c := *cmd
	switch {
	case c.OrgId == "":
		return nil, fmt.Errorf("%w: org id", ErrMissingField)
	case c.StationKey == "":
		return nil, fmt.Errorf("%w: station key", ErrMissingField)
	case c.Action == "":
		return nil, fmt.Errorf("%w: action", ErrMissingField)
	}
	if len(c.Payload) == 0 {
		c.Payload = json.RawMessage(`{}`)
	}

	now := s.Clock.Now()
	if c.Id == "" {
		c.Id = s.newId()
	}
	c.CreatedAt = now

	if c.OcppVersion == "" {
		version, err := s.stationVersion(ctx, c.StationKey)
		if err != nil {
			return nil, err
		}
		c.OcppVersion = string(version)
	}

	spec, ok := Lookup(transport.OcppVersion(c.OcppVersion), c.Action)
	if !ok {
		return nil, fmt.Errorf("%w: %s for %s", ErrUnknownAction, c.Action, c.OcppVersion)
	}

	for _, preprocessor := range s.Preprocessors {
		if err := preprocessor.Preprocess(ctx, &c); err != nil {
			return nil, fmt.Errorf("preprocessing %s command: %w", c.Action, err)
		}
	}

	if err := s.validate(spec, c.Payload); err != nil {
		return nil, err
	}

	c.Status = store.CommandStatusQueued
	c.Result, c.ErrorCode, c.ErrorDescription = nil, nil, nil
	c.SentAt, c.CompletedAt = nil, nil
	latest := now.Add(s.Config.maxTTL())
	if c.ExpiresAt.IsZero() {
		c.ExpiresAt = now.Add(s.Config.TTLFor(spec))
	}
	if !c.ExpiresAt.After(now) || c.ExpiresAt.After(latest) {
		return nil, fmt.Errorf("%w: %s is not after now and within %s", ErrInvalidExpiry,
			c.ExpiresAt.UTC().Format(time.RFC3339), s.Config.maxTTL())
	}

	if err := s.Store.CreateCommand(ctx, &c); err != nil {
		return nil, fmt.Errorf("storing %s command: %w", c.Action, err)
	}

	if s.Dispatcher != nil {
		if err := s.Dispatcher.Kick(context.WithoutCancel(ctx), c.StationKey); err != nil {
			slog.Warn("sending a new command; the sweep tries again", "commandId", c.Id,
				"stationKey", c.StationKey, "action", c.Action, "err", err)
		}
	}

	stored, err := s.Store.GetCommand(ctx, c.Id)
	if err != nil || stored == nil {
		// the command is stored: what it looked like when it was stored will do
		return &c, nil
	}
	return stored, nil
}

// CheckPayload checks a payload of an action of an OCPP version as Enqueue does before
// it stores a command, without the preprocessors: it returns ErrUnknownAction or
// ErrInvalidPayload as Enqueue would. A caller that queues several commands together
// checks them all first, so that it queues all of them or none.
func (s *Service) CheckPayload(version transport.OcppVersion, action string, payload json.RawMessage) error {
	spec, ok := Lookup(version, action)
	if !ok {
		return fmt.Errorf("%w: %s for %s", ErrUnknownAction, action, version)
	}
	return s.validate(spec, payload)
}

// validate checks a payload against the request schema of the action.
func (s *Service) validate(spec ActionSpec, payload json.RawMessage) error {
	if err := schemas.Validate(payload, s.SchemaFS, spec.RequestSchema); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}
	return nil
}

// stationVersion returns the OCPP version of a station from its runtime details.
func (s *Service) stationVersion(ctx context.Context, stationKey string) (transport.OcppVersion, error) {
	details, err := s.RuntimeDetails.LookupChargeStationRuntimeDetails(ctx, stationKey)
	if err != nil {
		return "", fmt.Errorf("looking up the OCPP version of %s: %w", stationKey, err)
	}
	if details == nil {
		return "", fmt.Errorf("%w: %s has not booted", ErrUnknownOcppVersion, stationKey)
	}
	version, ok := VersionFromRuntime(details.OcppVersion)
	if !ok {
		return "", fmt.Errorf("%w: %s has version %q", ErrUnknownOcppVersion, stationKey, details.OcppVersion)
	}
	return version, nil
}

func (s *Service) newId() string {
	if s.NewId != nil {
		return s.NewId()
	}
	return uuid.Must(uuid.NewV7()).String()
}

// Cancel moves a queued command to cancelled and emits its command.completed event. It
// returns ErrNotCancellable when the command is not queued, and nil, nil when there is
// no such command.
func (s *Service) Cancel(ctx context.Context, id string) (*store.Command, error) {
	now := s.Clock.Now()
	c := completer{store: s.Store, tx: s.Tx, events: s.Events}
	cmd, err := c.complete(ctx, id, []store.CommandStatus{store.CommandStatusQueued}, store.CommandUpdate{
		Status:      store.CommandStatusCancelled,
		CompletedAt: &now,
	}, false)
	if errors.Is(err, store.ErrCommandConflict) {
		return nil, ErrNotCancellable
	}
	if err != nil {
		return nil, fmt.Errorf("cancelling command %s: %w", id, err)
	}
	return cmd, nil
}

// Get returns a command, or nil when there is none with that id.
func (s *Service) Get(ctx context.Context, id string) (*store.Command, error) {
	return s.Store.GetCommand(ctx, id)
}

// List returns a page of the commands that match the filter and their total count.
func (s *Service) List(ctx context.Context, f store.CommandFilter) ([]*store.Command, int, error) {
	return s.Store.ListCommands(ctx, f)
}
