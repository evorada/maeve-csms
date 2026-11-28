// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

// The OCPP 1.6 actions whose effect the Tracker follows.
const (
	actionRemoteStart        = "RemoteStartTransaction"
	actionRemoteStop         = "RemoteStopTransaction"
	actionChangeAvailability = "ChangeAvailability"
)

const (
	// trackedPageSize is the number of commands that one lookup of the tracker reads.
	trackedPageSize = 50
	// sweepCreatedMargin is added to the longest window when the sweep looks for the
	// commands whose window passed: a command waits in the queue before it is sent, and
	// only the commands created since then are looked at.
	sweepCreatedMargin = time.Hour
)

var _ ResultHook = (*Tracker)(nil)

// Tracker follows the OCPP 1.6 commands that their stations accepted to their effect:
// the transaction that a RemoteStartTransaction began (or that none began in time), the
// end of the transaction that a RemoteStopTransaction asked for, the connector status
// that a ChangeAvailability led to. Each step is stored as the progress of the command
// with its command.progress event, in one store transaction.
//
// It follows the StartTransaction, StopTransaction and StatusNotification messages of
// the stations but never fails or changes their handling: its errors are logged.
type Tracker struct {
	Store store.CommandStore
	// Tx runs each lookup and the progress it writes, with its event, in one transaction.
	Tx store.Transactor
	// Events receives the command.progress events.
	Events events.Emitter
	Clock  clock.PassiveClock
	// Config gives the windows: RemoteStartWindow, RemoteStopWindow, AvailabilityWindow.
	Config Config
}

// AfterResult starts to follow a command that its station accepted: a remote start
// waits for its transaction, a remote stop for the end of the transaction, a change of
// availability for the connector status (or, when the station scheduled it, for the end
// of the running transaction).
func (t *Tracker) AfterResult(ctx context.Context, cmd *store.Command) {
	if cmd.Status != store.CommandStatusAccepted || cmd.OcppVersion != string(transport.OcppVersion16) {
		return
	}
	now := t.Clock.Now()
	var progress Progress
	switch cmd.Action {
	case actionRemoteStart:
		progress = Progress{State: ProgressAwaitingTransaction, Deadline: deadline(now, t.Config.remoteStartWindow())}
	case actionRemoteStop:
		progress = Progress{State: ProgressAwaitingStop, Deadline: deadline(now, t.Config.remoteStopWindow())}
	case actionChangeAvailability:
		switch resultStatus(cmd.Result) {
		case "Scheduled":
			// applies when the running transaction ends, however long that takes
			progress = Progress{State: ProgressScheduled}
		case "Accepted":
			progress = Progress{State: ProgressAwaitingStatus, Deadline: deadline(now, t.Config.availabilityWindow())}
		default:
			return
		}
	default:
		return
	}

	err := t.Tx.RunInTx(ctx, func(ctx context.Context) error {
		return t.setProgress(ctx, cmd.Id, progress, now)
	})
	if err != nil {
		slog.Error("storing the progress of an accepted command", "commandId", cmd.Id, "stationKey", cmd.StationKey,
			"action", cmd.Action, "state", progress.State, "err", err)
	}
}

// OnTransactionStarted links the newest remote start of the station that waits for a
// transaction, for the idTag and for the connector (or for any connector), to the
// transaction that the station started. transactionId is the id the transaction is
// stored with, ocppTransactionId the id the station got.
func (t *Tracker) OnTransactionStarted(ctx context.Context, stationKey string, connectorId int, idTag, transactionId string,
	ocppTransactionId int) {
	payloadFilter, err := json.Marshal(map[string]string{"idTag": idTag})
	if err != nil {
		slog.Error("following the remote starts of a started transaction", "stationKey", stationKey, "err", err)
		return
	}

	now := t.Clock.Now()
	var linked string
	err = t.Tx.RunInTx(ctx, func(ctx context.Context) error {
		linked = ""
		waiting, _, err := t.Store.ListCommands(ctx, store.CommandFilter{
			StationKey:       stationKey,
			Actions:          []string{actionRemoteStart},
			Statuses:         []store.CommandStatus{store.CommandStatusAccepted},
			PayloadContains:  payloadFilter,
			ProgressContains: stateFilter(ProgressAwaitingTransaction),
			Limit:            trackedPageSize,
		})
		if err != nil {
			return err
		}
		for _, cmd := range waiting {
			var payload struct {
				ConnectorId *int `json:"connectorId"`
			}
			if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
				return fmt.Errorf("payload of command %s: %w", cmd.Id, err)
			}
			if payload.ConnectorId != nil && *payload.ConnectorId != connectorId {
				continue
			}
			linked = cmd.Id
			return t.setProgress(ctx, cmd.Id, Progress{
				State:             ProgressTransactionStarted,
				TransactionId:     transactionId,
				OcppTransactionId: &ocppTransactionId,
				ConnectorId:       &connectorId,
			}, now)
		}
		return nil
	})
	if err != nil {
		slog.Error("linking a started transaction to its remote start", "stationKey", stationKey,
			"connectorId", connectorId, "transactionId", transactionId, "err", err)
		return
	}
	if linked != "" {
		slog.Info("remote start began its transaction", "commandId", linked, "stationKey", stationKey,
			"connectorId", connectorId, "transactionId", transactionId)
	}
}

// OnTransactionStopped records the end of the transaction on the newest remote stop of
// the station that waits for it.
func (t *Tracker) OnTransactionStopped(ctx context.Context, stationKey string, ocppTransactionId int, reason string) {
	payloadFilter, err := json.Marshal(map[string]int{"transactionId": ocppTransactionId})
	if err != nil {
		slog.Error("following the remote stops of a stopped transaction", "stationKey", stationKey, "err", err)
		return
	}

	now := t.Clock.Now()
	var linked string
	err = t.Tx.RunInTx(ctx, func(ctx context.Context) error {
		linked = ""
		waiting, _, err := t.Store.ListCommands(ctx, store.CommandFilter{
			StationKey:       stationKey,
			Actions:          []string{actionRemoteStop},
			Statuses:         []store.CommandStatus{store.CommandStatusAccepted},
			PayloadContains:  payloadFilter,
			ProgressContains: stateFilter(ProgressAwaitingStop),
			Limit:            1,
		})
		if err != nil || len(waiting) == 0 {
			return err
		}
		linked = waiting[0].Id
		return t.setProgress(ctx, linked, Progress{
			State:             ProgressTransactionStopped,
			OcppTransactionId: &ocppTransactionId,
			StopReason:        reason,
		}, now)
	})
	if err != nil {
		slog.Error("recording a stopped transaction on its remote stop", "stationKey", stationKey,
			"ocppTransactionId", ocppTransactionId, "err", err)
		return
	}
	if linked != "" {
		slog.Info("remote stop ended its transaction", "commandId", linked, "stationKey", stationKey,
			"ocppTransactionId", ocppTransactionId, "reason", reason)
	}
}

// OnConnectorStatus marks as applied the changes of availability of the station that
// wait for this connector (or are for the whole station, connector 0) and that the
// reported status fulfils: Unavailable for Inoperative, any other status for Operative.
func (t *Tracker) OnConnectorStatus(ctx context.Context, stationKey string, connectorId int, status string) {
	now := t.Clock.Now()
	var applied []string
	err := t.Tx.RunInTx(ctx, func(ctx context.Context) error {
		applied = nil
		for _, state := range []ProgressState{ProgressAwaitingStatus, ProgressScheduled} {
			waiting, _, err := t.Store.ListCommands(ctx, store.CommandFilter{
				StationKey:       stationKey,
				Actions:          []string{actionChangeAvailability},
				Statuses:         []store.CommandStatus{store.CommandStatusAccepted},
				ProgressContains: stateFilter(state),
				Limit:            trackedPageSize,
			})
			if err != nil {
				return err
			}
			for _, cmd := range waiting {
				var payload struct {
					ConnectorId int    `json:"connectorId"`
					Type        string `json:"type"`
				}
				if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
					return fmt.Errorf("payload of command %s: %w", cmd.Id, err)
				}
				if payload.ConnectorId != 0 && payload.ConnectorId != connectorId {
					continue
				}
				if !availabilityReached(payload.Type, status) {
					continue
				}
				err := t.setProgress(ctx, cmd.Id, Progress{
					State:           ProgressApplied,
					ConnectorId:     &connectorId,
					ConnectorStatus: status,
				}, now)
				if err != nil {
					return err
				}
				applied = append(applied, cmd.Id)
			}
		}
		return nil
	})
	if err != nil {
		slog.Error("following the changes of availability of a connector status", "stationKey", stationKey,
			"connectorId", connectorId, "status", status, "err", err)
		return
	}
	for _, id := range applied {
		slog.Info("change of availability applied", "commandId", id, "stationKey", stationKey,
			"connectorId", connectorId, "status", status)
	}
}

// availabilityReached reports whether a connector status is the one that a
// ChangeAvailability of the type asks for.
func availabilityReached(availabilityType, status string) bool {
	switch availabilityType {
	case "Inoperative":
		return status == "Unavailable"
	case "Operative":
		return status != "Unavailable"
	default:
		return false
	}
}

// sweepStep moves the commands that waited in one state past their deadline to the
// state that says the effect did not come.
type sweepStep struct {
	action   string
	from, to ProgressState
}

var sweepSteps = []sweepStep{
	{actionRemoteStart, ProgressAwaitingTransaction, ProgressNoTransaction},
	{actionRemoteStop, ProgressAwaitingStop, ProgressStopNotConfirmed},
	{actionChangeAvailability, ProgressAwaitingStatus, ProgressNotApplied},
}

// Sweep ends the waits whose deadline passed: a remote start without its transaction
// becomes no_transaction, a remote stop without the end of its transaction
// stop_not_confirmed, a change of availability without its status not_applied. Only
// the commands created within the longest window plus an hour are looked at. A scheduled
// change of availability has no deadline and is left alone.
func (t *Tracker) Sweep(ctx context.Context) error {
	now := t.Clock.Now()
	longest := max(t.Config.remoteStartWindow(), t.Config.remoteStopWindow(), t.Config.availabilityWindow())
	createdAfter := now.Add(-longest - sweepCreatedMargin)

	var errs []error
	for _, step := range sweepSteps {
		if err := t.sweep(ctx, step, now, createdAfter); err != nil {
			errs = append(errs, fmt.Errorf("ending the %s commands past their deadline: %w", step.from, err))
		}
	}
	return errors.Join(errs...)
}

// sweep runs one step of Sweep, a page of commands per transaction. The commands that
// it moves leave the list, those whose deadline has not passed are skipped.
func (t *Tracker) sweep(ctx context.Context, step sweepStep, now, createdAfter time.Time) error {
	skipped := 0
	for page := 0; page < maxSweepPages; page++ {
		var listed int
		var ended []string
		err := t.Tx.RunInTx(ctx, func(ctx context.Context) error {
			ended = nil
			waiting, _, err := t.Store.ListCommands(ctx, store.CommandFilter{
				Actions:          []string{step.action},
				Statuses:         []store.CommandStatus{store.CommandStatusAccepted},
				ProgressContains: stateFilter(step.from),
				CreatedAfter:     &createdAfter,
				Limit:            sweepPageSize,
				Offset:           skipped,
			})
			if err != nil {
				return err
			}
			listed = len(waiting)
			for _, cmd := range waiting {
				var progress Progress
				if err := json.Unmarshal(cmd.Progress, &progress); err != nil {
					return fmt.Errorf("progress of command %s: %w", cmd.Id, err)
				}
				if progress.Deadline == nil || !progress.Deadline.Before(now) {
					skipped++
					continue
				}
				progress.State = step.to
				if err := t.setProgress(ctx, cmd.Id, progress, now); err != nil {
					return err
				}
				ended = append(ended, cmd.Id)
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, id := range ended {
			slog.Warn("the effect of an accepted command did not come in time", "commandId", id,
				"action", step.action, "state", step.to)
		}
		if listed < sweepPageSize {
			return nil
		}
	}
	return nil
}

// setProgress replaces the progress of a command and emits command.progress. It must
// run in the transaction of Tx.
func (t *Tracker) setProgress(ctx context.Context, id string, progress Progress, now time.Time) error {
	body, err := json.Marshal(progress)
	if err != nil {
		return fmt.Errorf("progress of command %s: %w", id, err)
	}
	cmd, err := t.Store.SetCommandProgress(ctx, id, body, now, nil)
	if err != nil || cmd == nil {
		return err
	}
	if err := t.Events.Emit(ctx, progressEvent(cmd)); err != nil {
		return fmt.Errorf("emitting %s: %w", events.TypeCommandProgress, err)
	}
	return nil
}

// progressEvent is the command.progress event of a command whose progress has just
// changed. Like command.completed, its actor is whoever created the command.
func progressEvent(cmd *store.Command) events.Event {
	var actor string
	if cmd.Actor != nil {
		actor = *cmd.Actor
	}
	progressAt := cmd.ProgressAt.UTC()
	return events.Event{
		Type:         events.TypeCommandProgress,
		OrgId:        cmd.OrgId,
		ResourceType: events.ResourceCommand,
		ResourceId:   cmd.Id,
		Actor:        actor,
		Time:         progressAt,
		Data: events.CommandProgressData{
			CommandId:  cmd.Id,
			OrgId:      cmd.OrgId,
			StationId:  events.StationId(cmd.StationKey),
			StationKey: cmd.StationKey,
			Action:     cmd.Action,
			Status:     string(cmd.Status),
			Progress:   cmd.Progress,
			ProgressAt: progressAt,
		},
	}
}

// stateFilter selects the commands whose progress is in the state.
func stateFilter(state ProgressState) json.RawMessage {
	return json.RawMessage(`{"state":"` + string(state) + `"}`)
}

// deadline is the end of a wait of the window that starts now.
func deadline(now time.Time, window time.Duration) *time.Time {
	end := now.Add(window).UTC()
	return &end
}

// resultStatus returns the top-level status of a CallResult, "" when it has none.
func resultStatus(result json.RawMessage) string {
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(result, &body); err != nil {
		return ""
	}
	return body.Status
}
