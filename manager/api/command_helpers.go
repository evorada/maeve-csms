// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/go-chi/render"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/thoughtworks/maeve-csms/manager/stations"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"golang.org/x/exp/slog"
)

// queueFor reports whether the commands to the charge station go through the command
// queue: the manager keeps commands (its storage engine is not Firestore) and the charge
// station booted as an OCPP 1.6 station. For any other charge station, including one
// that never booted, the operations write their requests to the stores as before.
func (s *Server) queueFor(ctx context.Context, stationKey string) (bool, error) {
	if s.commands == nil {
		return false, nil
	}
	details, err := s.store.LookupChargeStationRuntimeDetails(ctx, stationKey)
	if err != nil {
		return false, fmt.Errorf("looking up the OCPP version of %s: %w", stationKey, err)
	}
	return details != nil && details.OcppVersion == "1.6", nil
}

// useQueue is queueFor for an operation: when it cannot be found out the request is
// answered (500) and ok is false.
func (s *Server) useQueue(w http.ResponseWriter, r *http.Request, csId string) (queue, ok bool) {
	queue, err := s.queueFor(r.Context(), csId)
	if err != nil {
		renderInternalError(w, r, "finding out how to send a command", err)
		return false, false
	}
	return queue, true
}

// enqueueFor queues a command for the charge station on behalf of a caller with the
// permission cpms.connector.command. See enqueueWithPermission.
func (s *Server) enqueueFor(w http.ResponseWriter, r *http.Request, csId, action string, payload any) *store.Command {
	return s.enqueueWithPermission(w, r, csId, tenancy.PermConnectorCommand, action, payload)
}

// enqueueWithPermission queues the action with the payload (marshalled to JSON) for the
// charge station, in the org that owns it, on behalf of the caller, who needs the
// permission. The command records who queued it (actor and jti of the caller). It returns
// the command as it is after the attempt to send it. When the command cannot be queued
// the request is answered as the command API answers it (400 for a payload that does not
// match the OCPP schema of the action, 403 without the permission, 409 when the OCPP
// version of the charge station is not known) and nil is returned.
//
// Like the command API it never refuses a command because the tenant is suspended.
func (s *Server) enqueueWithPermission(w http.ResponseWriter, r *http.Request, csId, permission, action string, payload any) *store.Command {
	p, ok := s.caller(w, r)
	if !ok {
		return nil
	}
	if !p.Has(permission) {
		_ = render.Render(w, r, ErrForbidden("missing permission "+permission))
		return nil
	}
	orgId, ok := s.stationOrg(w, r, csId)
	if !ok {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		renderInternalError(w, r, "building the payload of a command", err)
		return nil
	}

	cmd := &store.Command{
		OrgId:      orgId,
		StationKey: csId,
		Action:     action,
		Payload:    raw,
		Actor:      commandActor(p),
	}
	if p.Jti != "" {
		jti := p.Jti
		cmd.Jti = &jti
	}
	queued, err := s.commands.Enqueue(r.Context(), cmd)
	if err != nil {
		renderEnqueueError(w, r, err)
		return nil
	}
	return queued
}

// checkCommandPayload checks the payload (marshalled to JSON) of an action as the command
// queue does before it stores a command. Its errors are answered by renderEnqueueError.
func (s *Server) checkCommandPayload(version transport.OcppVersion, action string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("building the payload of a command: %w", err)
	}
	return s.commands.CheckPayload(version, action, raw)
}

// answerThroughQueue answers an operation that sends one command to the charge station
// when the commands to the charge station go through the queue (queueFor): it queues
// the action with the payload on behalf of a caller with the permission
// cpms.connector.command and answers the command with the status code. It reports
// whether it answered the request; when it did not, the operation answers in its own
// way.
func (s *Server) answerThroughQueue(w http.ResponseWriter, r *http.Request, csId string, code int, action string, payload any) bool {
	return s.answerThroughQueueWithPermission(w, r, csId, tenancy.PermConnectorCommand, code, action, payload)
}

// answerThroughQueueWithPermission is answerThroughQueue for an operation whose caller
// needs another permission (see enqueueWithPermission).
func (s *Server) answerThroughQueueWithPermission(w http.ResponseWriter, r *http.Request, csId, permission string, code int, action string, payload any) bool {
	queue, ok := s.useQueue(w, r, csId)
	if !ok {
		return true
	}
	if !queue {
		return false
	}
	if cmd := s.enqueueWithPermission(w, r, csId, permission, action, payload); cmd != nil {
		s.renderQueued(w, r, code, cmd)
	}
	return true
}

// refusedUnlessChargingAllowed answers an operation that starts charging, or reserves a
// connector for it, without the command queue (see queueFor), when the charge station
// may not charge: 409 station_not_active, as the command queue answers such a command.
// It reports whether it answered the request.
func (s *Server) refusedUnlessChargingAllowed(w http.ResponseWriter, r *http.Request, csId string) bool {
	allowed, err := stations.ChargingAllowed(r.Context(), s.store, csId)
	if err != nil {
		renderInternalError(w, r, "looking up the state of a charge station", err)
		return true
	}
	if !allowed {
		_ = render.Render(w, r, ErrConflict(errorStationNotActive+": the charge station is pending or decommissioned"))
		return true
	}
	return false
}

// renderQueuedOperation answers a command that an operation answering an
// OperationResponse queued: the operation id is the id of the command, the status is
// Pending (the outcome is the result of the command), and Location names the command.
func renderQueuedOperation(w http.ResponseWriter, r *http.Request, cmd *store.Command) {
	setCommandLocation(w, cmd)
	id := cmd.Id
	status := OperationResponseStatusPending
	render.Status(r, http.StatusAccepted)
	render.JSON(w, r, &OperationResponse{OperationId: &id, Status: &status})
}

// randomRequestId returns a request id for a command whose caller gave none: a
// non-negative 31-bit number, like the transaction ids that the manager gives OCPP 1.6
// charge stations.
func randomRequestId() int {
	//#nosec G404 - a request id does not require a secure random number generator
	return int(rand.Int31())
}

// ocppDateTime is a time of a request as OCPP sends it: RFC 3339, in the offset that
// the caller gave, to the second.
func ocppDateTime(t time.Time) string {
	return t.Format(time.RFC3339)
}

// optionalOcppDateTime is ocppDateTime of an optional time.
func optionalOcppDateTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	formatted := ocppDateTime(*t)
	return &formatted
}

// renderQueued answers a command that an operation queued: Location names the command
// and the body is the command.
func (s *Server) renderQueued(w http.ResponseWriter, r *http.Request, code int, cmd *store.Command) {
	setCommandLocation(w, cmd)
	s.renderCommand(w, r, code, cmd)
}

// commandUUID returns the id of the command as the API shows it. When the id is not a
// UUID the request is answered (500) and false is reported.
func commandUUID(w http.ResponseWriter, r *http.Request, cmd *store.Command) (openapi_types.UUID, bool) {
	id, err := uuid.Parse(cmd.Id)
	if err != nil {
		renderInternalError(w, r, "answering with a command", fmt.Errorf("command id %q is not a UUID: %w", cmd.Id, err))
		return openapi_types.UUID{}, false
	}
	return id, true
}

// setCommandLocation names where the command can be read.
func setCommandLocation(w http.ResponseWriter, cmd *store.Command) {
	w.Header().Set("Location", BasePath+"/commands/"+cmd.Id)
}

// renderInternalError answers a failure whose reason is not for the caller (500): the
// reason goes to the log.
func renderInternalError(w http.ResponseWriter, r *http.Request, doing string, err error) {
	slog.Error(doing, "method", r.Method, "path", r.URL.Path, "err", err)
	_ = render.Render(w, r, errInternal)
}
