// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/go-chi/render"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
)

// BasePath is where the manager mounts the API.
const BasePath = "/api/v0"

// maxCommandWaitSeconds is the longest that a request that queues a command may wait
// for the final status of the command.
const maxCommandWaitSeconds = 30

// commandPollInterval is how often a request that waits for the final status of a
// command reads the command.
const commandPollInterval = 200 * time.Millisecond

// maxTTLSeconds is the largest time to live of a command, in seconds, that a
// time.Duration can hold. The command service refuses much less (its max_ttl): this
// only keeps the conversion from overflowing.
const maxTTLSeconds = math.MaxInt64 / int64(time.Second)

// The errors of the answers to a command that cannot be queued start with these codes.
const (
	errorUnknownAction         = "unknown_action"
	errorInvalidPayload        = "invalid_payload"
	errorInvalidExpiry         = "invalid_expiry"
	errorStationVersionUnknown = "station_version_unknown"
	errorStationNotActive      = "station_not_active"
)

// errCommandsNotAvailable answers a command operation of a manager whose storage engine
// does not keep commands.
var errCommandsNotAvailable = &ErrResponse{
	HTTPStatusCode: http.StatusNotImplemented,
	StatusText:     http.StatusText(http.StatusNotImplemented),
	ErrorText:      "the storage engine of the manager does not keep commands",
}

// Bind checks what a command request must have before anything is queued. The payload
// is checked against the OCPP schema of the action when the command is queued.
func (c *CommandRequest) Bind(*http.Request) error {
	switch {
	case c.Action == "":
		return errors.New("action is required")
	case !isJSONObject(c.Payload):
		return errors.New("payload must be a JSON object")
	case c.ExpiresAt != nil && c.TtlSeconds != nil:
		return errors.New("expiresAt and ttlSeconds cannot be given together")
	case c.TtlSeconds != nil && *c.TtlSeconds < 1:
		return errors.New("ttlSeconds must be at least 1")
	}
	return nil
}

// isJSONObject reports whether raw, which the JSON decoder already read as one value,
// is an object.
func isJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	return len(trimmed) > 0 && trimmed[0] == '{'
}

// EnqueueCommand queues a command for the charge station. AuthzMiddleware has checked
// the permission and that the caller may reach the charge station. The command is not
// refused while the tenant is suspended: commands to a charge station are not writes.
//
// With waitSeconds the request waits at most that long for the final status of the
// command: a command that has none by then is answered 202, as without a wait.
func (s *Server) EnqueueCommand(w http.ResponseWriter, r *http.Request, csId string, params EnqueueCommandParams) {
	if !s.commandsAvailable(w, r) {
		return
	}
	p, ok := s.caller(w, r)
	if !ok {
		return
	}
	orgId, ok := s.stationOrg(w, r, csId)
	if !ok {
		return
	}

	var wait time.Duration
	if params.WaitSeconds != nil {
		if *params.WaitSeconds < 0 || *params.WaitSeconds > maxCommandWaitSeconds {
			_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("waitSeconds must be between 0 and %d", maxCommandWaitSeconds)))
			return
		}
		wait = time.Duration(*params.WaitSeconds) * time.Second
	}

	req := new(CommandRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	cmd := &store.Command{
		OrgId:      orgId,
		StationKey: csId,
		Action:     req.Action,
		Payload:    req.Payload,
		Actor:      commandActor(p),
	}
	if p.Jti != "" {
		jti := p.Jti
		cmd.Jti = &jti
	}
	switch {
	case req.ExpiresAt != nil:
		cmd.ExpiresAt = *req.ExpiresAt
	case req.TtlSeconds != nil:
		if int64(*req.TtlSeconds) > maxTTLSeconds {
			_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("%s: ttlSeconds is too large", errorInvalidExpiry)))
			return
		}
		cmd.ExpiresAt = s.clock.Now().Add(time.Duration(*req.TtlSeconds) * time.Second)
	}

	queued, err := s.commands.Enqueue(r.Context(), cmd)
	if err != nil {
		renderEnqueueError(w, r, err)
		return
	}

	status := http.StatusAccepted
	if wait > 0 {
		queued = s.awaitCommand(r.Context(), queued, wait)
		if queued.Status.IsTerminal() {
			status = http.StatusOK
		}
	}
	s.renderQueued(w, r, status, queued)
}

// commandActor names who creates a command: the subject of the caller, followed by
// ";act=" and whoever acts as the subject when somebody does.
func commandActor(p *tenancy.Principal) *string {
	if p.Subject == "" {
		return nil
	}
	actor := p.Subject
	if p.Actor != "" {
		actor += ";act=" + p.Actor
	}
	return &actor
}

// awaitCommand reads the command until it has a final status, for at most wait, and
// returns it as it was read last. It never fails: when the command cannot be read the
// command is returned as it was before. It waits in real time, never longer than wait,
// so that a caller is never kept waiting on a charge station.
func (s *Server) awaitCommand(ctx context.Context, cmd *store.Command, wait time.Duration) *store.Command {
	if cmd.Status.IsTerminal() {
		return cmd
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	ticker := time.NewTicker(commandPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return cmd
		case <-ticker.C:
			current, err := s.commands.Get(ctx, cmd.Id)
			if err != nil {
				if ctx.Err() == nil {
					slog.Warn("reading a command while waiting for its final status", "commandId", cmd.Id, "err", err)
				}
				return cmd
			}
			if current == nil {
				return cmd
			}
			cmd = current
			if cmd.Status.IsTerminal() {
				return cmd
			}
		}
	}
}

// renderEnqueueError answers a command that could not be queued.
func renderEnqueueError(w http.ResponseWriter, r *http.Request, err error) {
	coded := func(code string) error { return fmt.Errorf("%s: %w", code, err) }
	switch {
	case errors.Is(err, commands.ErrUnknownAction):
		_ = render.Render(w, r, ErrInvalidRequest(coded(errorUnknownAction)))
	case errors.Is(err, commands.ErrInvalidPayload):
		_ = render.Render(w, r, ErrInvalidRequest(coded(errorInvalidPayload)))
	case errors.Is(err, commands.ErrInvalidExpiry):
		_ = render.Render(w, r, ErrInvalidRequest(coded(errorInvalidExpiry)))
	case errors.Is(err, commands.ErrMissingField):
		_ = render.Render(w, r, ErrInvalidRequest(err))
	case errors.Is(err, commands.ErrUnknownOcppVersion):
		_ = render.Render(w, r, ErrConflict(coded(errorStationVersionUnknown).Error()))
	case errors.Is(err, commands.ErrStationNotActive):
		_ = render.Render(w, r, ErrConflict(coded(errorStationNotActive).Error()))
	default:
		renderCommandStoreError(w, r, "queueing a command", err)
	}
}

// renderCommandStoreError answers a command operation whose store call failed: 501 when
// the storage engine does not keep commands, otherwise 500 with the reason in the log.
func renderCommandStoreError(w http.ResponseWriter, r *http.Request, doing string, err error) {
	if errors.Is(err, store.ErrNotImplemented) {
		_ = render.Render(w, r, errCommandsNotAvailable)
		return
	}
	slog.Error(doing, "method", r.Method, "path", r.URL.Path, "err", err)
	_ = render.Render(w, r, errInternal)
}

// GetCommand answers the command when the caller may reach the org of the command. A
// command of an org outside the scope of the caller is answered like a command that
// does not exist.
func (s *Server) GetCommand(w http.ResponseWriter, r *http.Request, commandId openapi_types.UUID) {
	if !s.commandsAvailable(w, r) {
		return
	}
	cmd, ok := s.visibleCommand(w, r, commandId.String())
	if !ok {
		return
	}
	s.renderCommand(w, r, http.StatusOK, cmd)
}

// ListChargeStationCommands answers a page of the commands of the charge station that
// belong to the org that owns it. AuthzMiddleware has checked that the caller may reach
// the charge station.
func (s *Server) ListChargeStationCommands(w http.ResponseWriter, r *http.Request, csId string, params ListChargeStationCommandsParams) {
	if !s.commandsAvailable(w, r) {
		return
	}
	orgId, ok := s.stationOrg(w, r, csId)
	if !ok {
		return
	}

	filter := store.CommandFilter{
		OrgId:         orgId,
		StationKey:    csId,
		Limit:         store.DefaultCommandListLimit,
		CreatedAfter:  params.CreatedAfter,
		CreatedBefore: params.CreatedBefore,
	}
	if params.Limit != nil {
		if *params.Limit < 1 || *params.Limit > store.MaxCommandListLimit {
			_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("limit must be between 1 and %d", store.MaxCommandListLimit)))
			return
		}
		filter.Limit = *params.Limit
	}
	if params.Offset != nil {
		if *params.Offset < 0 {
			_ = render.Render(w, r, ErrInvalidRequest(errors.New("offset must not be negative")))
			return
		}
		filter.Offset = *params.Offset
	}
	if params.Status != nil {
		for _, status := range *params.Status {
			if !validCommandStatus(status) {
				_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("unknown command status %q", status)))
				return
			}
			filter.Statuses = append(filter.Statuses, store.CommandStatus(status))
		}
	}
	if params.Action != nil {
		filter.Actions = *params.Action
	}

	found, total, err := s.commands.List(r.Context(), filter)
	if err != nil {
		renderCommandStoreError(w, r, "listing commands", err)
		return
	}
	list := CommandList{Commands: make([]Command, 0, len(found)), Total: total}
	for _, cmd := range found {
		c, err := s.apiCommand(cmd)
		if err != nil {
			renderCommandStoreError(w, r, "listing commands", err)
			return
		}
		list.Commands = append(list.Commands, c)
	}
	render.Status(r, http.StatusOK)
	render.JSON(w, r, list)
}

// validCommandStatus reports whether status is one of the statuses of a command.
func validCommandStatus(status CommandStatus) bool {
	switch status {
	case CommandStatusQueued, CommandStatusSent, CommandStatusAccepted, CommandStatusRejected,
		CommandStatusFailed, CommandStatusTimeout, CommandStatusCancelled:
		return true
	}
	return false
}

// CancelCommand cancels a queued command when the caller may reach the org of the
// command. A command that is not queued any more is answered 409 with the command as it
// is. Like EnqueueCommand it is not refused while the tenant is suspended.
func (s *Server) CancelCommand(w http.ResponseWriter, r *http.Request, commandId openapi_types.UUID) {
	if !s.commandsAvailable(w, r) {
		return
	}
	cmd, ok := s.visibleCommand(w, r, commandId.String())
	if !ok {
		return
	}

	cancelled, err := s.commands.Cancel(r.Context(), cmd.Id)
	switch {
	case errors.Is(err, commands.ErrNotCancellable):
		current, err := s.commands.Get(r.Context(), cmd.Id)
		if err != nil {
			renderCommandStoreError(w, r, "reading a command that cannot be cancelled", err)
			return
		}
		if current == nil {
			_ = render.Render(w, r, ErrNotFound)
			return
		}
		s.renderCommand(w, r, http.StatusConflict, current)
	case err != nil:
		renderCommandStoreError(w, r, "cancelling a command", err)
	case cancelled == nil:
		// deleted since it was read
		_ = render.Render(w, r, ErrNotFound)
	default:
		s.renderCommand(w, r, http.StatusOK, cancelled)
	}
}

// visibleCommand returns the command when it exists and the caller may reach its org.
// Otherwise the request is answered and false is reported: a command of an org outside
// the scope of the caller is answered exactly like a command that does not exist.
func (s *Server) visibleCommand(w http.ResponseWriter, r *http.Request, id string) (*store.Command, bool) {
	p, ok := s.caller(w, r)
	if !ok {
		return nil, false
	}
	cmd, err := s.commands.Get(r.Context(), id)
	if err != nil {
		renderCommandStoreError(w, r, "reading a command", err)
		return nil, false
	}
	if cmd == nil {
		_ = render.Render(w, r, ErrNotFound)
		return nil, false
	}
	inScope, err := s.scoper.InScope(r.Context(), p, cmd.OrgId)
	if err != nil {
		renderScopeError(w, r, err)
		return nil, false
	}
	if !inScope {
		_ = render.Render(w, r, ErrNotFound)
		return nil, false
	}
	return cmd, true
}

// commandsAvailable reports whether the manager keeps commands. When it does not the
// request is answered (501).
func (s *Server) commandsAvailable(w http.ResponseWriter, r *http.Request) bool {
	if s.commands != nil {
		return true
	}
	_ = render.Render(w, r, errCommandsNotAvailable)
	return false
}

// stationOrg returns the org that owns the charge station of an operation on a charge
// station: AuthzMiddleware resolved it, and checked it against the scope of the caller,
// before the operation runs.
func (s *Server) stationOrg(w http.ResponseWriter, r *http.Request, csId string) (string, bool) {
	orgId, ok := tenancy.StationOrgFromContext(r.Context())
	if !ok || orgId == "" {
		slog.Error("operation on a charge station without a resolved org", "chargeStationId", csId, "path", r.URL.Path)
		_ = render.Render(w, r, errInternal)
		return "", false
	}
	return orgId, true
}

// renderCommand answers with the command and the status code.
func (s *Server) renderCommand(w http.ResponseWriter, r *http.Request, code int, cmd *store.Command) {
	c, err := s.apiCommand(cmd)
	if err != nil {
		renderCommandStoreError(w, r, "answering with a command", err)
		return
	}
	render.Status(r, code)
	render.JSON(w, r, c)
}

// apiCommand is the command as the API shows it. Its jti is kept for the audit and not
// shown, and neither are the secrets of its payload, even before they are replaced in
// the stored payload.
func (s *Server) apiCommand(cmd *store.Command) (Command, error) {
	id, err := uuid.Parse(cmd.Id)
	if err != nil {
		return Command{}, fmt.Errorf("command id %q is not a UUID: %w", cmd.Id, err)
	}
	payload, _ := commands.RedactPayload(cmd.Payload, cmd.RedactPaths)
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	return Command{
		Id:               id,
		ChargeStationId:  s.chargeStationId(cmd.StationKey),
		OcppVersion:      CommandOcppVersion(cmd.OcppVersion),
		Action:           cmd.Action,
		Payload:          payload,
		Status:           CommandStatus(cmd.Status),
		Result:           optionalJSON(cmd.Result),
		ErrorCode:        cmd.ErrorCode,
		ErrorDescription: cmd.ErrorDescription,
		Progress:         optionalJSON(cmd.Progress),
		ProgressAt:       cmd.ProgressAt,
		Actor:            cmd.Actor,
		CreatedAt:        cmd.CreatedAt,
		SentAt:           cmd.SentAt,
		CompletedAt:      cmd.CompletedAt,
		ExpiresAt:        cmd.ExpiresAt,
	}, nil
}

// chargeStationId is the id of the charge station of a station key. In multi mode the
// key starts with the org key of the tenant, which is left out. In single mode the key
// is the id: an id with a "." keeps all of it.
func (s *Server) chargeStationId(stationKey string) string {
	if s.scoper.Mode != tenancy.ModeMulti {
		return stationKey
	}
	_, stationId, _ := tenancy.SplitStationKey(stationKey)
	return stationId
}

// optionalJSON returns nil for a JSON document that is not there.
func optionalJSON(raw json.RawMessage) *json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return &raw
}
