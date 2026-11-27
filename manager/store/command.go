// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// CommandStatus is the lifecycle state of a CSMS→station command.
type CommandStatus string

const (
	CommandStatusQueued    CommandStatus = "queued"    // stored, not yet sent
	CommandStatusSent      CommandStatus = "sent"      // emitted, waiting for CallResult/CallError
	CommandStatusAccepted  CommandStatus = "accepted"  // CallResult with a positive status
	CommandStatusRejected  CommandStatus = "rejected"  // CallResult with a negative status
	CommandStatusFailed    CommandStatus = "failed"    // CallError, or invalid CallResult payload
	CommandStatusTimeout   CommandStatus = "timeout"   // expired in queue, or no response in time
	CommandStatusCancelled CommandStatus = "cancelled" // cancelled while queued
)

// IsTerminal reports whether no further status change is expected (a CallResult that
// arrives late can still move a command from timeout to accepted or rejected).
func (s CommandStatus) IsTerminal() bool { return s != CommandStatusQueued && s != CommandStatusSent }

// ErrCommandConflict is returned by TransitionCommand when the current status is not in
// `from`, or when the update would put a second command of a station in flight.
var ErrCommandConflict = errors.New("command status conflict")

// Command is one CSMS→station command: what was asked, by whom, and what came of it.
type Command struct {
	Id               string          // UUIDv7, also used as the OCPP MessageId
	OrgId            string          // tenant that owns the station
	StationKey       string          // namespaced station key {org_key}.{stationId}
	OcppVersion      string          // transport.OcppVersion value: "ocpp1.6" | "ocpp2.0.1"
	Action           string          // OCPP action, e.g. "Reset"
	Payload          json.RawMessage // OCPP request payload as accepted by the API
	Status           CommandStatus
	Result           json.RawMessage // CallResult payload, nil until a result arrives
	ErrorCode        *string         // OCPP CallError code or internal code (QueueExpired, ResponseTimeout, FormatViolation)
	ErrorDescription *string
	Progress         json.RawMessage // state of what follows the CallResult, e.g. the transaction a remote start began
	ProgressAt       *time.Time
	Actor            *string // "user:…", "svc:…", "system:…"; ";act=<sub>" appended when acting on behalf
	Jti              *string // jti of the internal token that created the command
	CreatedAt        time.Time
	SentAt           *time.Time
	CompletedAt      *time.Time
	ExpiresAt        time.Time // a queued command is not sent after this instant
}

// CommandUpdate is what TransitionCommand writes. Every field but Result and Event is
// written as given.
type CommandUpdate struct {
	Status           CommandStatus
	Result           json.RawMessage // nil = leave unchanged
	ErrorCode        *string
	ErrorDescription *string
	SentAt           *time.Time // written as given (nil clears it, used when reverting sent → queued)
	CompletedAt      *time.Time
	// Event, when set, is appended to the outbox in the same transaction as the update,
	// and only when the update applies.
	Event *OutboxEvent
}

// MaxCommandListLimit is the most commands that one ListCommands call returns.
const MaxCommandListLimit = 500

// DefaultCommandListLimit is the number of commands that ListCommands returns when the
// filter sets no limit.
const DefaultCommandListLimit = 50

// CommandFilter selects the commands that ListCommands returns.
type CommandFilter struct {
	OrgId      string // "" = any org (internal callers only; API always sets it)
	StationKey string // "" = any station
	Actions    []string
	Statuses   []CommandStatus
	// PayloadContains selects commands whose payload contains this JSON object, e.g.
	// {"transactionId":123}. Only the top-level keys are compared, and their values must
	// be equal.
	PayloadContains json.RawMessage
	// ProgressContains selects commands whose progress contains this JSON object, e.g.
	// {"state":"awaiting_transaction"}, compared like PayloadContains. A command without
	// progress never matches.
	ProgressContains json.RawMessage
	CreatedAfter     *time.Time // created strictly after this instant
	CreatedBefore    *time.Time // created strictly before this instant
	// Limit is 1..MaxCommandListLimit: 0 or less is DefaultCommandListLimit, more than
	// the maximum is the maximum.
	Limit  int
	Offset int // a negative offset is 0
}

// EffectiveLimit is the number of commands that a list with the filter returns at most.
func (f CommandFilter) EffectiveLimit() int {
	switch {
	case f.Limit <= 0:
		return DefaultCommandListLimit
	case f.Limit > MaxCommandListLimit:
		return MaxCommandListLimit
	default:
		return f.Limit
	}
}

// EffectiveOffset is the number of matching commands that a list with the filter skips.
func (f CommandFilter) EffectiveOffset() int {
	return max(f.Offset, 0)
}

// CommandStore holds the CSMS→station commands. At most one command per station is in
// status sent at any time.
//
// A command id that is not a UUID cannot name a stored command: GetCommand,
// TransitionCommand and SetCommandProgress treat it as not found.
type CommandStore interface {
	// CreateCommand inserts a new command; Status must be queued, Id and OrgId UUIDs.
	CreateCommand(ctx context.Context, cmd *Command) error
	// GetCommand returns nil, nil when not found.
	GetCommand(ctx context.Context, id string) (*Command, error)
	// ListCommands returns a page ordered by created_at DESC, id DESC, plus the total count.
	ListCommands(ctx context.Context, filter CommandFilter) ([]*Command, int, error)
	// ClaimNextCommand atomically moves the oldest non-expired queued command of the station
	// to sent (sent_at = now) and returns it. Returns nil, nil if there is none or if the
	// station already has a command in status sent (one in flight per station).
	ClaimNextCommand(ctx context.Context, stationKey string, now time.Time) (*Command, error)
	// TransitionCommand applies upd if the current status is in from (compare-and-set).
	// Returns nil, nil if not found and ErrCommandConflict if the status does not match.
	TransitionCommand(ctx context.Context, id string, from []CommandStatus, upd CommandUpdate) (*Command, error)
	// SetCommandProgress replaces progress/progress_at without changing status. Returns
	// nil, nil if not found; evt, when set, is appended to the outbox with the change.
	SetCommandProgress(ctx context.Context, id string, progress json.RawMessage, at time.Time, evt *OutboxEvent) (*Command, error)
	// ListQueuedStationKeys pages (by station_key) over stations with non-expired queued commands.
	ListQueuedStationKeys(ctx context.Context, now time.Time, afterStationKey string, limit int) ([]string, error)
	// ListExpiredQueuedCommands returns queued commands with expires_at <= now, oldest
	// expiry first.
	ListExpiredQueuedCommands(ctx context.Context, now time.Time, limit int) ([]*Command, error)
	// ListOverdueSentCommands returns sent commands with sent_at <= sentBefore, oldest
	// first.
	ListOverdueSentCommands(ctx context.Context, sentBefore time.Time, limit int) ([]*Command, error)
	// DeleteCompletedCommandsBefore deletes up to limit terminal commands with completed_at < before.
	DeleteCompletedCommandsBefore(ctx context.Context, before time.Time, limit int) (int, error)
}
