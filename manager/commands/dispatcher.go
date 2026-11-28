// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

// The error codes that the dispatcher records itself; the others are the OCPP error
// codes of the CallErrors of the stations.
const (
	ErrorCodeQueueExpired      = "QueueExpired"      // not sent before it expired
	ErrorCodeResponseTimeout   = "ResponseTimeout"   // sent, no answer in time
	ErrorCodeFormatViolation   = "FormatViolation"   // the CallResult does not match the schema
	ErrorCodePayloadResolution = "PayloadResolution" // the payload to send could not be built
	ErrorCodeInternalError     = "InternalError"     // the schema of the response could not be loaded
)

// The jobs of the command queue.
const (
	SweepJobName  = "commands-sweep"
	PurgeJobName  = "commands-purge"
	PurgeInterval = time.Hour
)

const (
	// sweepPageSize is the number of commands that the sweep moves per store call.
	sweepPageSize = 100
	// stationPageSize is the number of station keys that the sweep reads per store call.
	stationPageSize = 200
	// maxSweepPages bounds the store calls of each step of one sweep.
	maxSweepPages = 50
	// purgeBatchSize is the number of completed commands that one delete removes.
	purgeBatchSize = 1000
)

// Connectivity says whether a station has an open connection to a gateway.
type Connectivity interface {
	IsConnected(ctx context.Context, stationKey string) (bool, error)
}

// StatusStoreConnectivity reads ChargeStationStatus.Connected, which the connection
// events of the gateways and the BootNotifications and Heartbeats of the stations keep.
// A station without a status has never connected.
type StatusStoreConnectivity struct {
	Store store.StatusStore
}

func (c StatusStoreConnectivity) IsConnected(ctx context.Context, stationKey string) (bool, error) {
	status, err := c.Store.GetChargeStationStatus(ctx, stationKey)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return status != nil && status.Connected, nil
}

// SettledConnectivity is a Connectivity that also knows whether a connected station is
// ready for commands: it has been connected for at least the given delay, or it has
// already talked to the CSMS on its current connection (a BootNotification or a
// Heartbeat).
type SettledConnectivity interface {
	Connectivity
	IsSettled(ctx context.Context, stationKey string, now time.Time, delay time.Duration) (bool, error)
}

func (c StatusStoreConnectivity) IsSettled(ctx context.Context, stationKey string, now time.Time, delay time.Duration) (bool, error) {
	status, err := c.Store.GetChargeStationStatus(ctx, stationKey)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if status == nil || !status.Connected {
		return false, nil
	}
	// a station whose connection is not tracked (no connection events) is settled
	if status.LastConnectedAt == nil {
		return true, nil
	}
	if now.Sub(*status.LastConnectedAt) >= delay {
		return true, nil
	}
	return status.LastHeartbeat != nil && !status.LastHeartbeat.Before(*status.LastConnectedAt), nil
}

// PayloadResolver builds the payload that is sent for a command, at the moment it is
// sent, from the payload that was stored. It must not change the command.
type PayloadResolver interface {
	Resolve(ctx context.Context, cmd *store.Command) (json.RawMessage, error)
}

// ResultHook follows a command after its station answered it.
type ResultHook interface {
	// AfterResult is told about a command that has just moved to accepted or rejected
	// (also when the answer came after the command timed out). It must not fail the
	// handling of the answer: it logs its own errors.
	AfterResult(ctx context.Context, cmd *store.Command)
}

// CompletionHook follows a command to its final status, whatever it is.
type CompletionHook interface {
	// AfterCompletion is told about a command that has just reached a final status
	// (accepted, rejected, failed, timeout or cancelled), once that status is stored, and
	// again when an answer arrives after the command timed out. It runs in the handling
	// of the answer, of the sweep or of the cancellation, and must not fail it: it logs
	// its own errors.
	AfterCompletion(ctx context.Context, cmd *store.Command)
}

// Sweeper is a step of the sweep job of the queue that runs after Dispatcher.Sweep.
type Sweeper interface {
	Sweep(ctx context.Context) error
}

// Dispatcher sends the queued commands to the connected stations, one command per
// station at a time, with the command id as the OCPP MessageId, and records the answer
// of the station (CallResult or CallError), the commands that expire in the queue and
// those that get no answer in time. Each final status is stored in one transaction with
// its command.completed event.
//
// It never fails or holds up the handling of the messages of a station: its errors are
// logged, and a command that could not be sent stays queued for the next attempt.
type Dispatcher struct {
	Store store.CommandStore
	// Tx runs each final status change and its event in one transaction.
	Tx store.Transactor
	// Events receives the command.completed events.
	Events events.Emitter
	// Senders send the calls to the stations, by OCPP version.
	Senders      map[transport.OcppVersion]handlers.RawCallMaker
	Connectivity Connectivity
	// SchemaFS holds the schemas of the CallResults.
	SchemaFS fs.FS
	Clock    clock.WithDelayedExecution
	Config   Config
	// Resolvers build the payload sent for a command, in order, each from the output of
	// the one before (the first from the stored payload).
	Resolvers []PayloadResolver
	// AfterResult are told, in order, about each command that its station accepted or
	// rejected, once that status is stored.
	AfterResult []ResultHook
	// AfterCompletion are told, in order, about each command that reached a final
	// status, once that status is stored; before AfterResult.
	AfterCompletion []CompletionHook
	// AfterSweep run, in order, after Sweep in the sweep job.
	AfterSweep []Sweeper
}

var (
	_ handlers.CallOutcomeObserver = (*Dispatcher)(nil)
	_ SettledConnectivity          = StatusStoreConnectivity{}
)

// kickSettled is Kick for a station that may have connected only a moment ago: a station
// that is not settled yet (see SettledConnectivity) is left alone, so that it can send
// its BootNotification first. The kick after Config.ConnectDispatchDelay, the one after
// its BootNotification, or the next sweep sends its commands then.
func (d *Dispatcher) kickSettled(ctx context.Context, stationKey string) error {
	if settled, ok := d.Connectivity.(SettledConnectivity); ok {
		ready, err := settled.IsSettled(ctx, stationKey, d.Clock.Now(), d.Config.ConnectDispatchDelay)
		if err != nil {
			return fmt.Errorf("connectivity of %s: %w", stationKey, err)
		}
		if !ready {
			return nil
		}
	}
	return d.Kick(ctx, stationKey)
}

func (d *Dispatcher) completer() completer {
	return completer{store: d.Store, tx: d.Tx, events: d.Events, hooks: d.AfterCompletion}
}

// Kick sends the oldest queued command of the station when the station is connected and
// no other command of it waits for an answer. A command whose payload cannot be built
// fails and the next one is tried. When the send fails, the command goes back to the
// queue and the error is returned; the sweep tries again.
func (d *Dispatcher) Kick(ctx context.Context, stationKey string) error {
	connected, err := d.Connectivity.IsConnected(ctx, stationKey)
	if err != nil {
		return fmt.Errorf("connectivity of %s: %w", stationKey, err)
	}
	if !connected {
		return nil
	}

	for {
		cmd, err := d.Store.ClaimNextCommand(ctx, stationKey, d.Clock.Now())
		if err != nil {
			return fmt.Errorf("claiming the next command of %s: %w", stationKey, err)
		}
		if cmd == nil {
			return nil
		}

		payload, err := d.resolve(ctx, cmd)
		if err != nil {
			now := d.Clock.Now()
			description := err.Error()
			_, terr := d.completer().complete(ctx, cmd.Id, []store.CommandStatus{store.CommandStatusSent}, store.CommandUpdate{
				Status:           store.CommandStatusFailed,
				ErrorCode:        ptr(ErrorCodePayloadResolution),
				ErrorDescription: &description,
				SentAt:           cmd.SentAt,
				CompletedAt:      &now,
			}, false)
			if terr != nil {
				return fmt.Errorf("failing command %s whose payload could not be built (%v): %w", cmd.Id, err, terr)
			}
			slog.Warn("command failed: its payload could not be built", "commandId", cmd.Id,
				"stationKey", stationKey, "action", cmd.Action, "err", err)
			continue
		}

		err = d.send(ctx, cmd, payload)
		if err != nil {
			_, rerr := d.Store.TransitionCommand(ctx, cmd.Id, []store.CommandStatus{store.CommandStatusSent},
				store.CommandUpdate{Status: store.CommandStatusQueued})
			if rerr != nil {
				return errors.Join(err, fmt.Errorf("putting command %s back in the queue: %w", cmd.Id, rerr))
			}
			return err
		}
		slog.Info("command sent", "commandId", cmd.Id, "stationKey", stationKey, "action", cmd.Action)
		d.redactSent(ctx, cmd)
		return nil
	}
}

// redactSent replaces the secrets of the payload of a command that has been sent: they
// are no longer needed. When that fails they are replaced when the command completes.
func (d *Dispatcher) redactSent(ctx context.Context, cmd *store.Command) {
	redacted, changed := RedactPayload(cmd.Payload, cmd.RedactPaths)
	if !changed {
		return
	}
	if err := d.Store.RedactCommandPayload(ctx, cmd.Id, redacted); err != nil {
		slog.Error("replacing the secrets of a sent command; they are replaced when it completes",
			"commandId", cmd.Id, "stationKey", cmd.StationKey, "action", cmd.Action, "err", err)
	}
}

// resolve returns the payload to send for cmd.
func (d *Dispatcher) resolve(ctx context.Context, cmd *store.Command) (json.RawMessage, error) {
	payload := cmd.Payload
	for _, resolver := range d.Resolvers {
		resolved := *cmd
		resolved.Payload = payload
		next, err := resolver.Resolve(ctx, &resolved)
		if err != nil {
			return nil, err
		}
		payload = next
	}
	return payload, nil
}

// send sends cmd with the payload, the command id as MessageId.
func (d *Dispatcher) send(ctx context.Context, cmd *store.Command, payload json.RawMessage) error {
	sender, ok := d.Senders[transport.OcppVersion(cmd.OcppVersion)]
	if !ok {
		return fmt.Errorf("sending command %s: no sender for OCPP version %q", cmd.Id, cmd.OcppVersion)
	}
	err := sender.SendRaw(ctx, cmd.StationKey, cmd.Id, cmd.Action, payload)
	if err != nil {
		return fmt.Errorf("sending command %s: %w", cmd.Id, err)
	}
	return nil
}

// OnStationConnected sends the queued commands of a station whose connection opened,
// after Config.ConnectDispatchDelay, so that a rebooting station can send its
// BootNotification first.
func (d *Dispatcher) OnStationConnected(ctx context.Context, stationKey string) {
	ctx = context.WithoutCancel(ctx)
	d.Clock.AfterFunc(d.Config.ConnectDispatchDelay, func() {
		// in a goroutine of its own: a clock may call this while it holds its lock, and
		// Kick reads the clock
		go func() {
			if err := d.Kick(ctx, stationKey); err != nil {
				slog.Warn("sending the queued commands of a connected station", "stationKey", stationKey, "err", err)
			}
		}()
	})
}

// ObserveCallResult records the CallResult of a command: accepted or rejected, as the
// catalogue classifies the response, or failed when the response does not match its
// schema. It returns false, and changes nothing, when the MessageId is not the id of a
// command of this station and action: the CallResult then answered another call.
func (d *Dispatcher) ObserveCallResult(ctx context.Context, chargeStationId string, msg *transport.Message) bool {
	cmd := d.trackedCommand(ctx, chargeStationId, msg)
	if cmd == nil {
		return false
	}

	now := d.Clock.Now()
	upd := store.CommandUpdate{SentAt: cmd.SentAt, CompletedAt: &now}
	spec, known := Lookup(transport.OcppVersion(cmd.OcppVersion), cmd.Action)
	if !known {
		// the action left the catalogue while the command waited: take any answer
		spec = ActionSpec{Version: transport.OcppVersion(cmd.OcppVersion), Action: cmd.Action}
	}
	if code, err := d.checkResponse(spec, msg.ResponsePayload); err != nil {
		description := err.Error()
		upd.Status = store.CommandStatusFailed
		upd.ErrorCode = &code
		upd.ErrorDescription = &description
	} else {
		upd.Status = Classify(spec, msg.ResponsePayload)
		upd.Result = msg.ResponsePayload
	}

	d.finish(ctx, cmd, upd)
	return true
}

// checkResponse checks a CallResult payload against the response schema of the action.
// It returns the error code to record when the payload cannot be taken.
func (d *Dispatcher) checkResponse(spec ActionSpec, response json.RawMessage) (string, error) {
	if spec.ResponseSchema == "" {
		return "", nil
	}
	schema, err := schemas.Compile(d.SchemaFS, spec.ResponseSchema)
	if err != nil {
		slog.Error("loading the response schema of a command", "schema", spec.ResponseSchema, "err", err)
		return ErrorCodeInternalError, fmt.Errorf("response schema %s: %w", spec.ResponseSchema, err)
	}
	if err := schema.Validate(bytes.NewReader(response)); err != nil {
		return ErrorCodeFormatViolation, err
	}
	return "", nil
}

// ObserveCallError records the CallError of a command: failed, with the error code and
// description of the station. It returns false, and changes nothing, when the MessageId
// is not the id of a command of this station and action.
func (d *Dispatcher) ObserveCallError(ctx context.Context, chargeStationId string, msg *transport.Message) bool {
	cmd := d.trackedCommand(ctx, chargeStationId, msg)
	if cmd == nil {
		return false
	}

	now := d.Clock.Now()
	code := string(msg.ErrorCode)
	description := msg.ErrorDescription
	d.finish(ctx, cmd, store.CommandUpdate{
		Status:           store.CommandStatusFailed,
		ErrorCode:        &code,
		ErrorDescription: &description,
		SentAt:           cmd.SentAt,
		CompletedAt:      &now,
	})
	return true
}

// trackedCommand returns the command that msg answers, or nil when msg answers no
// command of this station and action.
func (d *Dispatcher) trackedCommand(ctx context.Context, chargeStationId string, msg *transport.Message) *store.Command {
	if msg.MessageId == "" {
		return nil
	}
	cmd, err := d.Store.GetCommand(ctx, msg.MessageId)
	if err != nil {
		slog.Error("looking up the command of a station's answer", "chargeStationId", chargeStationId,
			"messageId", msg.MessageId, "action", msg.Action, "err", err)
		return nil
	}
	if cmd == nil {
		return nil
	}
	if cmd.StationKey != chargeStationId || cmd.Action != msg.Action {
		slog.Warn("answer with the id of a command of another station or action", "chargeStationId", chargeStationId,
			"messageId", msg.MessageId, "action", msg.Action, "commandStationKey", cmd.StationKey, "commandAction", cmd.Action)
		return nil
	}
	return cmd
}

// finish stores the final status of a command that its station answered, then sends the
// next queued command of the station. An answer to a command that timed out is stored
// too, as late; an answer to a command that is already complete (cancelled, answered
// before) changes nothing.
func (d *Dispatcher) finish(ctx context.Context, cmd *store.Command, upd store.CommandUpdate) {
	log := slog.With("commandId", cmd.Id, "stationKey", cmd.StationKey, "action", cmd.Action)

	late := false
	done, err := d.completer().complete(ctx, cmd.Id, []store.CommandStatus{store.CommandStatusSent}, upd, false)
	if errors.Is(err, store.ErrCommandConflict) {
		late = true
		done, err = d.completer().complete(ctx, cmd.Id, []store.CommandStatus{store.CommandStatusTimeout}, upd, true)
	}
	switch {
	case errors.Is(err, store.ErrCommandConflict):
		log.Warn("answer to a command that is no longer waiting for one", "status", upd.Status)
		return
	case err != nil:
		log.Error("recording the answer to a command", "status", upd.Status, "err", err)
		return
	case done == nil:
		log.Warn("answer to a command that no longer exists", "status", upd.Status)
		return
	}
	log.Info("command completed", "status", done.Status, "late", late)

	if done.Status == store.CommandStatusAccepted || done.Status == store.CommandStatusRejected {
		d.afterResult(ctx, done)
	}

	kickCtx := context.WithoutCancel(ctx)
	go func() {
		if err := d.Kick(kickCtx, cmd.StationKey); err != nil {
			slog.Warn("sending the next queued command", "stationKey", cmd.StationKey, "err", err)
		}
	}()
}

// afterResult tells the result hooks about a command that its station accepted or
// rejected. A hook that panics is logged and the next one is told all the same.
func (d *Dispatcher) afterResult(ctx context.Context, cmd *store.Command) {
	for _, hook := range d.AfterResult {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("a hook on the result of a command panicked", "commandId", cmd.Id,
						"stationKey", cmd.StationKey, "action", cmd.Action, "panic", r)
				}
			}()
			hook.AfterResult(ctx, cmd)
		}()
	}
}

// Sweep times out the queued commands that expired and the sent commands that got no
// answer within Config.ResponseTimeout, then sends the next command of every station
// with queued commands (which catches missed connections and failed sends).
func (d *Dispatcher) Sweep(ctx context.Context) error {
	return errors.Join(
		d.expireQueued(ctx),
		d.timeOutSent(ctx),
		d.kickQueuedStations(ctx),
	)
}

// expireQueued moves the queued commands whose expiry passed to timeout.
func (d *Dispatcher) expireQueued(ctx context.Context) error {
	for page := 0; page < maxSweepPages; page++ {
		now := d.Clock.Now()
		expired, err := d.Store.ListExpiredQueuedCommands(ctx, now, sweepPageSize)
		if err != nil {
			return fmt.Errorf("listing expired queued commands: %w", err)
		}
		var errs []error
		for _, cmd := range expired {
			_, err := d.completer().complete(ctx, cmd.Id, []store.CommandStatus{store.CommandStatusQueued}, store.CommandUpdate{
				Status:           store.CommandStatusTimeout,
				ErrorCode:        ptr(ErrorCodeQueueExpired),
				ErrorDescription: ptr("not sent before it expired at " + cmd.ExpiresAt.UTC().Format(time.RFC3339)),
				SentAt:           cmd.SentAt,
				CompletedAt:      &now,
			}, false)
			if err != nil && !errors.Is(err, store.ErrCommandConflict) {
				errs = append(errs, fmt.Errorf("expiring command %s: %w", cmd.Id, err))
			}
		}
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		if len(expired) < sweepPageSize {
			return nil
		}
	}
	return nil
}

// timeOutSent moves the sent commands that got no answer in time to timeout, and sends
// the next command of their stations.
func (d *Dispatcher) timeOutSent(ctx context.Context) error {
	timeout := d.Config.responseTimeout()
	for page := 0; page < maxSweepPages; page++ {
		now := d.Clock.Now()
		overdue, err := d.Store.ListOverdueSentCommands(ctx, now.Add(-timeout), sweepPageSize)
		if err != nil {
			return fmt.Errorf("listing overdue sent commands: %w", err)
		}
		var errs []error
		for _, cmd := range overdue {
			_, err := d.completer().complete(ctx, cmd.Id, []store.CommandStatus{store.CommandStatusSent}, store.CommandUpdate{
				Status:           store.CommandStatusTimeout,
				ErrorCode:        ptr(ErrorCodeResponseTimeout),
				ErrorDescription: ptr("no answer within " + timeout.String()),
				SentAt:           cmd.SentAt,
				CompletedAt:      &now,
			}, false)
			if errors.Is(err, store.ErrCommandConflict) {
				// answered meanwhile
				continue
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("timing out command %s: %w", cmd.Id, err))
				continue
			}
			slog.Warn("command timed out: no answer from the station", "commandId", cmd.Id,
				"stationKey", cmd.StationKey, "action", cmd.Action, "timeout", timeout)
			if err := d.Kick(ctx, cmd.StationKey); err != nil {
				slog.Warn("sending the next queued command", "stationKey", cmd.StationKey, "err", err)
			}
		}
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		if len(overdue) < sweepPageSize {
			return nil
		}
	}
	return nil
}

// kickQueuedStations sends the next command of every station that has queued commands.
func (d *Dispatcher) kickQueuedStations(ctx context.Context) error {
	after := ""
	for page := 0; page < maxSweepPages; page++ {
		keys, err := d.Store.ListQueuedStationKeys(ctx, d.Clock.Now(), after, stationPageSize)
		if err != nil {
			return fmt.Errorf("listing stations with queued commands: %w", err)
		}
		for _, key := range keys {
			if err := d.kickSettled(ctx, key); err != nil {
				slog.Warn("sending the next queued command", "stationKey", key, "err", err)
			}
		}
		if len(keys) < stationPageSize {
			return nil
		}
		after = keys[len(keys)-1]
	}
	return nil
}

// Purge deletes the commands that completed more than Config.Retention ago.
func (d *Dispatcher) Purge(ctx context.Context) error {
	before := d.Clock.Now().Add(-d.Config.retention())
	total := 0
	for {
		n, err := d.Store.DeleteCompletedCommandsBefore(ctx, before, purgeBatchSize)
		if err != nil {
			return fmt.Errorf("deleting completed commands: %w", err)
		}
		total += n
		if n < purgeBatchSize {
			break
		}
	}
	if total > 0 {
		slog.Info("deleted completed commands", "count", total, "completedBefore", before)
	}
	return nil
}

// SweepJob returns the job that runs Sweep and then each of AfterSweep, every
// Config.SweepInterval. The steps of AfterSweep run also when Sweep fails.
func (d *Dispatcher) SweepJob() jobs.Job {
	return jobs.Job{Name: SweepJobName, Interval: d.Config.sweepInterval(), Run: d.sweepAll}
}

// sweepAll runs Sweep and then each of AfterSweep, and returns all their errors.
func (d *Dispatcher) sweepAll(ctx context.Context) error {
	errs := []error{d.Sweep(ctx)}
	for _, sweeper := range d.AfterSweep {
		errs = append(errs, sweeper.Sweep(ctx))
	}
	return errors.Join(errs...)
}

// PurgeJob returns the job that runs Purge every PurgeInterval.
func (d *Dispatcher) PurgeJob() jobs.Job {
	return jobs.Job{Name: PurgeJobName, Interval: PurgeInterval, Run: d.Purge}
}

func ptr[T any](v T) *T {
	return &v
}
