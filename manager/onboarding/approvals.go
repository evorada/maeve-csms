// SPDX-License-Identifier: Apache-2.0

package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"golang.org/x/exp/slog"
)

// Approvals completes the approvals of pending charge stations: at once when the password
// of the station is set by hand or is the onboarding password, and when the station
// takes its new password when that is sent to it (AfterCompletion, a hook of the command
// queue).
type Approvals struct {
	Store store.Engine
	// Tx runs each change of a station with its events in one transaction.
	Tx store.Transactor
	// Events receives station.approved, station.approval_failed and location.updated.
	Events events.Emitter
	// Commands asks an approved station for a new BootNotification; nil asks nothing.
	Commands *commands.Service
}

var _ commands.CompletionHook = (*Approvals)(nil)

// Complete makes the pending station cs active as the approval says, with auth as its
// authentication details (and sharedCredentials when its password is the onboarding
// password of its tenant), and emits location.updated for each EVSE that it creates and
// station.approved, whose actor is approval.RequestedBy. passwordMode is how the password
// was set (events.PasswordMode*). It must run in the transaction of ctx
// (store.Transactor.RunInTx); the errors of CompleteApproval are returned as they are.
func (a *Approvals) Complete(ctx context.Context, cs *store.ChargeStation, approval *store.PendingApproval,
	auth *store.ChargeStationAuth, sharedCredentials bool, passwordMode string) error {
	if err := a.Store.CompleteApproval(ctx, cs.StationKey, approval, auth, sharedCredentials); err != nil {
		return err
	}
	loc, err := a.Store.LookupAssetLocation(ctx, approval.LocationId)
	if err != nil {
		return fmt.Errorf("look up location %s: %w", approval.LocationId, err)
	}
	if loc == nil {
		return fmt.Errorf("location %s: %w", approval.LocationId, store.ErrNotFound)
	}

	connectors := 0
	approved := make([]events.Event, 0, len(approval.Evses)+1)
	for _, evse := range approval.Evses {
		connectors += len(evse.Connectors)
		approved = append(approved, events.Event{
			Type:         events.TypeLocationUpdated,
			OrgId:        loc.OrgId,
			ResourceType: events.ResourceLocation,
			ResourceId:   loc.Id,
			Actor:        approval.RequestedBy,
			Data: events.LocationUpdatedData{
				LocationId: loc.Id,
				OcpiId:     loc.OcpiId,
				PartyRef:   loc.PartyRef,
				Change:     events.LocationChangeCreated,
				EvseUid:    evse.Id,
				Publish:    loc.Publish,
			},
		})
	}
	approved = append(approved, events.Event{
		Type:         events.TypeStationApproved,
		OrgId:        cs.OrgId,
		StationKey:   cs.StationKey,
		ResourceType: events.ResourceStation,
		ResourceId:   cs.StationKey,
		Actor:        approval.RequestedBy,
		Data: events.StationApprovedData{
			StationKey:        cs.StationKey,
			StationId:         events.StationId(cs.StationKey),
			LocationId:        loc.Id,
			EvseCount:         len(approval.Evses),
			ConnectorCount:    connectors,
			PasswordMode:      passwordMode,
			SharedCredentials: sharedCredentials,
			Actor:             approval.RequestedBy,
		},
	})
	if err := a.Events.Emit(ctx, approved...); err != nil {
		return fmt.Errorf("emit %s: %w", events.TypeStationApproved, err)
	}
	return nil
}

// Rotation is a new password on its way to a pending station.
type Rotation struct {
	// Command sends the password to the station. It is queued (commands.Service.Enqueue)
	// once the transaction of StartRotation is committed.
	Command *store.Command
	// before is the onboarding of the station before the approval, and pendingBefore its
	// pending password hash, which UndoRotation puts back.
	before        *store.StationOnboarding
	pendingBefore *string
}

// StartRotation stores the approval of the pending station cs, of the OCPP version, with
// a new password as its pending password (onboarding status approving), and returns the
// command that sends the password to the station, with actor and jti as its creator. The
// id of the command is in the onboarding before the command exists, so that the answer
// of the station finds the approval however soon it comes. It must run in the
// transaction of ctx; the password is kept only in the payload of the command.
func (a *Approvals) StartRotation(ctx context.Context, cs *store.ChargeStation, approval *store.PendingApproval,
	version transport.OcppVersion, actor, jti *string) (*Rotation, error) {
	password, hash, err := NewPassword()
	if err != nil {
		return nil, err
	}
	action, payload, redactPaths, err := CredentialCommand(version, password)
	if err != nil {
		return nil, err
	}
	auth, err := a.Store.LookupChargeStationAuth(ctx, cs.StationKey)
	if err != nil {
		return nil, fmt.Errorf("look up the auth details of %s: %w", cs.StationKey, err)
	}
	var pendingBefore *string
	if auth != nil && auth.PendingBase64SHA256Password != "" {
		pendingBefore = &auth.PendingBase64SHA256Password
	}
	commandId, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("new command id: %w", err)
	}
	id := commandId.String()

	rotating := store.StationOnboarding{}
	if cs.Onboarding != nil {
		rotating = *cs.Onboarding
	}
	rotating.Status = store.OnboardingApproving
	rotating.Error = nil
	rotating.CommandId = &id
	rotating.Approval = approval
	if err := a.Store.SetOnboarding(ctx, cs.StationKey, &rotating, &hash); err != nil {
		return nil, fmt.Errorf("start the approval of %s: %w", cs.StationKey, err)
	}
	return &Rotation{
		Command: &store.Command{
			Id:          id,
			OrgId:       cs.OrgId,
			StationKey:  cs.StationKey,
			OcppVersion: string(version),
			Action:      action,
			Payload:     payload,
			RedactPaths: redactPaths,
			Actor:       actor,
			Jti:         jti,
		},
		before:        cs.Onboarding,
		pendingBefore: pendingBefore,
	}, nil
}

// UndoRotation puts back the onboarding and the pending password that the station had
// before StartRotation, when the command of the rotation could not be queued. A station
// whose onboarding changed meanwhile is left alone.
func (a *Approvals) UndoRotation(ctx context.Context, r *Rotation) error {
	return a.Tx.RunInTx(ctx, func(ctx context.Context) error {
		cs, err := a.Store.LookupChargeStation(ctx, r.Command.StationKey)
		if err != nil {
			return fmt.Errorf("look up charge station %s: %w", r.Command.StationKey, err)
		}
		if cs == nil || cs.State != store.StationStatePending || cs.Onboarding == nil ||
			cs.Onboarding.CommandId == nil || *cs.Onboarding.CommandId != r.Command.Id || r.before == nil {
			return nil
		}
		return a.Store.SetOnboarding(ctx, r.Command.StationKey, r.before, r.pendingBefore)
	})
}

// errNotTheApproval is the answer of a credential command that no approval waits for
// (any more): the station was approved by hand meanwhile, rejected, or approved again.
var errNotTheApproval = errors.New("no approval waits for the command")

// AfterCompletion follows the command that sends its new password to a pending station.
// When the station took the password, the approval is completed with the pending
// password as the password of the station, and the station is asked for a new
// BootNotification so that it is accepted at once. Otherwise the approval stops at
// credentials_failed with the pending password kept (a station that took the password
// all the same can still connect), and station.approval_failed is emitted; the operator
// approves again. Commands that no approval waits for are left alone.
func (a *Approvals) AfterCompletion(ctx context.Context, cmd *store.Command) {
	if !isCredentialCommand(cmd) {
		return
	}
	log := slog.With("commandId", cmd.Id, "stationKey", cmd.StationKey, "status", cmd.Status)
	accepted, result, detail := credentialOutcome(cmd)

	if accepted {
		err := a.Tx.RunInTx(ctx, func(ctx context.Context) error {
			cs, auth, err := a.waitingApproval(ctx, cmd)
			if err != nil {
				return err
			}
			if auth.PendingBase64SHA256Password == "" {
				return fmt.Errorf("%w: the station has no pending password", store.ErrInvalidState)
			}
			return a.Complete(ctx, cs, cs.Onboarding.Approval, &store.ChargeStationAuth{
				SecurityProfile:        cs.Onboarding.Approval.SecurityProfile,
				Base64SHA256Password:   auth.PendingBase64SHA256Password,
				InvalidUsernameAllowed: true,
			}, false, events.PasswordModeRotate)
		})
		switch {
		case err == nil:
			log.Info("charge station approved: it took its new password")
			a.askForBoot(ctx, cmd)
			return
		case errors.Is(err, errNotTheApproval):
			return
		}
		// the station has its password, but the approval cannot be stored as it is
		log.Error("completing the approval of a charge station that took its new password", "err", err)
		result, detail = ResultNotCompleted, ptr(notCompletedReason(err))
	}

	err := a.Tx.RunInTx(ctx, func(ctx context.Context) error {
		cs, auth, err := a.waitingApproval(ctx, cmd)
		if err != nil {
			return err
		}
		if !accepted && cs.Onboarding.Status != store.OnboardingApproving {
			// failed before: a second failure of the same command changes nothing
			return errNotTheApproval
		}
		return a.fail(ctx, cs, auth, cmd, result, detail)
	})
	switch {
	case err == nil:
		log.Warn("charge station did not take its new password: its approval failed", "result", result)
	case !errors.Is(err, errNotTheApproval):
		log.Error("recording the failed approval of a charge station", "result", result, "err", err)
	}
}

// waitingApproval returns the pending station whose approval waits for the command, and
// its auth details; errNotTheApproval when there is none.
func (a *Approvals) waitingApproval(ctx context.Context, cmd *store.Command) (*store.ChargeStation, *store.ChargeStationAuth, error) {
	cs, err := a.Store.LookupChargeStation(ctx, cmd.StationKey)
	if err != nil {
		return nil, nil, fmt.Errorf("look up charge station %s: %w", cmd.StationKey, err)
	}
	if cs == nil || cs.State != store.StationStatePending || cs.Onboarding == nil || cs.Onboarding.Approval == nil ||
		cs.Onboarding.CommandId == nil || *cs.Onboarding.CommandId != cmd.Id ||
		(cs.Onboarding.Status != store.OnboardingApproving && cs.Onboarding.Status != store.OnboardingCredentialsFailed) {
		return nil, nil, errNotTheApproval
	}
	auth, err := a.Store.LookupChargeStationAuth(ctx, cmd.StationKey)
	if err != nil {
		return nil, nil, fmt.Errorf("look up the auth details of %s: %w", cmd.StationKey, err)
	}
	if auth == nil {
		return nil, nil, errNotTheApproval
	}
	return cs, auth, nil
}

// fail records that the station did not take its new password, and emits
// station.approval_failed.
func (a *Approvals) fail(ctx context.Context, cs *store.ChargeStation, auth *store.ChargeStationAuth, cmd *store.Command, result string, detail *string) error {
	failed := *cs.Onboarding
	failed.Status = store.OnboardingCredentialsFailed
	reason := result
	if detail != nil && *detail != "" {
		reason += ": " + *detail
	}
	failed.Error = &reason
	var pending *string
	if auth.PendingBase64SHA256Password != "" {
		pending = &auth.PendingBase64SHA256Password
	}
	if err := a.Store.SetOnboarding(ctx, cs.StationKey, &failed, pending); err != nil {
		if errors.Is(err, store.ErrInvalidState) || errors.Is(err, store.ErrNotFound) {
			return errNotTheApproval
		}
		return err
	}
	err := a.Events.Emit(ctx, events.Event{
		Type:         events.TypeStationApprovalFailed,
		OrgId:        cs.OrgId,
		StationKey:   cs.StationKey,
		ResourceType: events.ResourceStation,
		ResourceId:   cs.StationKey,
		Actor:        cs.Onboarding.Approval.RequestedBy,
		Data: events.StationApprovalFailedData{
			StationKey: cs.StationKey,
			StationId:  events.StationId(cs.StationKey),
			CommandId:  cmd.Id,
			Result:     result,
			Error:      detail,
		},
	})
	if err != nil {
		return fmt.Errorf("emit %s: %w", events.TypeStationApprovalFailed, err)
	}
	return nil
}

// askForBoot asks a station that has just been approved for a BootNotification, so that
// it is accepted without waiting for the interval of its pending boots. When that fails
// the station is accepted at its next boot all the same.
func (a *Approvals) askForBoot(ctx context.Context, cmd *store.Command) {
	if a.Commands == nil {
		return
	}
	_, err := a.Commands.Enqueue(ctx, &store.Command{
		OrgId:       cmd.OrgId,
		StationKey:  cmd.StationKey,
		OcppVersion: cmd.OcppVersion,
		Action:      "TriggerMessage",
		Payload:     json.RawMessage(`{"requestedMessage":"BootNotification"}`),
		Actor:       cmd.Actor,
	})
	if err != nil {
		slog.Warn("asking an approved charge station for a BootNotification; it is accepted at its next one",
			"stationKey", cmd.StationKey, "ocppVersion", cmd.OcppVersion, "err", err)
	}
}

// notCompletedReason says why an approval could not be completed, without the details of
// the store (they are logged).
func notCompletedReason(err error) string {
	switch {
	case errors.Is(err, store.ErrAlreadyExists):
		return "an EVSE of the approval has an evse_id or an OCPP EVSE id that is taken"
	case errors.Is(err, store.ErrNotFound):
		return "the location of the approval does not exist any more"
	default:
		return "the approval could not be stored"
	}
}
