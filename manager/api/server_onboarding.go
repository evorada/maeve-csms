// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/render"
	"github.com/google/uuid"
	"github.com/thoughtworks/maeve-csms/manager/assets"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/entitlements"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/onboarding"
	"github.com/thoughtworks/maeve-csms/manager/stations"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"golang.org/x/exp/slog"
)

// The errors of the answers to an approval or a rejection that is refused start with
// these codes.
const (
	errorInvalidState        = "invalid_state"
	errorApprovalInProgress  = "approval_in_progress"
	errorEntitlementExceeded = "entitlement_exceeded"
)

// approvalRequest is an approval of a pending charge station as the request asks for it,
// checked against the rules that do not depend on what is stored.
type approvalRequest struct {
	approval *store.PendingApproval
	mode     string
	// profile is the security profile that the request asks for; nil for the default
	profile *store.SecurityProfile
}

// connectors returns how many connectors the approval creates.
func (a approvalRequest) connectors() int {
	n := 0
	for _, evse := range a.approval.Evses {
		n += len(evse.Connectors)
	}
	return n
}

// parseApproval reads the approval of the pending charge station stationKey. It returns
// the fields that break a rule as assets.ValidationErrors.
func parseApproval(in *ApproveRequest, stationKey string) (approvalRequest, error) {
	var invalid []assets.ValidationError
	req := approvalRequest{
		approval: &store.PendingApproval{LocationId: in.LocationId, DisplayName: in.DisplayName, MaxCapacityKw: in.MaxCapacityKw},
		mode:     events.PasswordModeRotate,
	}
	if _, err := uuid.Parse(in.LocationId); err != nil {
		invalid = append(invalid, assets.ValidationError{Field: "location_id", Reason: "must be the id (a UUID) of a location of the org of the charge station"})
	}
	if name := in.DisplayName; name != nil && (strings.TrimSpace(*name) == "" || utf8.RuneCountInString(*name) > maxStationTextLength) {
		invalid = append(invalid, assets.ValidationError{Field: "display_name", Reason: fmt.Sprintf("must be 1 to %d characters", maxStationTextLength)})
	}
	if capacity := in.MaxCapacityKw; capacity != nil && (*capacity <= 0 || *capacity > maxCapacityKw) {
		invalid = append(invalid, assets.ValidationError{Field: "max_capacity_kw", Reason: fmt.Sprintf("must be above 0 and at most %.3f kW", maxCapacityKw)})
	}
	if in.AllowAnyToken != nil {
		req.approval.AllowAnyToken = *in.AllowAnyToken
	}
	if in.SecurityProfile != nil {
		var profile store.SecurityProfile
		switch *in.SecurityProfile {
		case SP1:
			profile = store.UnsecuredTransportWithBasicAuth
		case SP2:
			profile = store.TLSWithBasicAuth
		default:
			invalid = append(invalid, assets.ValidationError{Field: "security_profile", Reason: "must be SP1 or SP2"})
		}
		req.profile = &profile
	}
	if in.PasswordMode != nil {
		switch *in.PasswordMode {
		case Rotate, Manual, KeepOnboarding:
			req.mode = string(*in.PasswordMode)
		default:
			invalid = append(invalid, assets.ValidationError{Field: "password_mode", Reason: "must be rotate, manual or keep_onboarding"})
		}
	}

	if len(in.Evses) == 0 {
		invalid = append(invalid, assets.ValidationError{Field: "evses", Reason: "must have at least one EVSE"})
	}
	ocppEvseIds := map[int]bool{}
	for i, e := range in.Evses {
		prefix := fmt.Sprintf("evses[%d].", i)
		evse := &store.AssetEvse{
			StationKey:        stationKey,
			OcppEvseId:        e.OcppEvseId,
			EvseId:            e.EvseId,
			PhysicalReference: e.PhysicalReference,
		}
		if e.Status != nil {
			evse.Status = store.AssetEvseStatus(*e.Status)
		}
		for j := range e.Connectors {
			evse.Connectors = append(evse.Connectors, storeAssetConnector(&e.Connectors[j]))
		}

		var evseInvalid assets.ValidationErrors
		if err := assets.ValidateEvse(evse); err != nil && !errors.As(err, &evseInvalid) {
			return approvalRequest{}, err
		}
		for _, field := range evseInvalid {
			invalid = append(invalid, assets.ValidationError{Field: prefix + field.Field, Reason: field.Reason})
		}
		if len(evse.Connectors) == 0 {
			invalid = append(invalid, assets.ValidationError{Field: prefix + "connectors", Reason: "must have at least one connector"})
		}
		for j, c := range evse.Connectors {
			invalid = append(invalid, checkConnector(fmt.Sprintf("%sconnectors[%d].", prefix, j), c)...)
		}
		if ocppEvseIds[e.OcppEvseId] {
			invalid = append(invalid, assets.ValidationError{Field: prefix + "ocpp_evse_id", Reason: fmt.Sprintf("%d is used by another EVSE of the charge station", e.OcppEvseId)})
		}
		ocppEvseIds[e.OcppEvseId] = true
		req.approval.Evses = append(req.approval.Evses, evse)
	}
	return req, validation(nil, invalid...)
}

// approved is what an approval stored, for the answer and for what follows it.
type approved struct {
	// password is the new password of the charge station that is given to the caller
	// (manual)
	password string
	// rotation is the new password on its way to the charge station (rotate)
	rotation *onboarding.Rotation
}

// ApproveChargeStation approves a pending charge station: it gets a location, its EVSEs
// and a password, and becomes active (at once, or once it has taken its new password).
func (s *Server) ApproveChargeStation(w http.ResponseWriter, r *http.Request, csId string) {
	orgId, ok := stationOrgOf(w, r, csId)
	if !ok {
		return
	}
	p, ok := s.caller(w, r)
	if !ok {
		return
	}
	in := new(ApproveRequest)
	if !decodeBody(w, r, in) {
		return
	}
	req, err := parseApproval(in, csId)
	if err != nil {
		renderStationError(w, r, err)
		return
	}
	actor := events.ActorFrom(r.Context())
	if actor == "" {
		actor = events.DefaultActor
	}
	req.approval.RequestedBy = actor
	req.approval.RequestedAt = s.clock.Now().UTC()

	var done approved
	err = s.store.RunInTx(r.Context(), func(ctx context.Context) error {
		var err error
		done, err = s.approve(ctx, p, orgId, csId, req)
		if errors.Is(err, store.ErrInvalidState) {
			// approved or rejected at the same time
			return answerWith(ErrConflict(errorInvalidState + ": the charge station is not pending"))
		}
		return err
	})
	if err != nil {
		renderStationError(w, r, err)
		return
	}

	code := http.StatusOK
	if done.rotation != nil {
		code = http.StatusAccepted
		if _, err := s.commands.Enqueue(r.Context(), done.rotation.Command); err != nil {
			if err := s.approvals().UndoRotation(r.Context(), done.rotation); err != nil {
				slog.Error("putting back the onboarding of a charge station whose new password could not be queued",
					"chargeStationId", csId, "err", err)
			}
			renderEnqueueError(w, r, err)
			return
		}
	}

	cs, err := s.lookupStation(r.Context(), csId)
	if err != nil {
		renderStationError(w, r, err)
		return
	}
	record, err := s.apiChargeStation(r.Context(), cs, orgId)
	if err != nil {
		renderStationError(w, r, err)
		return
	}
	result := ApprovalResult{Station: record}
	if req.mode == events.PasswordModeManual {
		// shown once: it is stored only as its hash
		result.Password = &done.password
	}
	render.Status(r, code)
	render.JSON(w, r, result)
}

// approve checks and stores the approval of the pending charge station of the org in the
// transaction of ctx. For rotate it returns the command that sends the new password,
// which the caller queues once the transaction is committed.
func (s *Server) approve(ctx context.Context, p *tenancy.Principal, orgId, csId string, req approvalRequest) (approved, error) {
	cs, err := s.lookupStation(ctx, csId)
	if err != nil {
		return approved{}, err
	}
	if cs.State != store.StationStatePending || cs.Onboarding == nil {
		return approved{}, answerWith(ErrConflict(errorInvalidState + ": the charge station is not pending"))
	}
	if cs.Onboarding.Status == store.OnboardingApproving {
		return approved{}, answerWith(ErrConflict(errorApprovalInProgress + ": the new password of an approval is being sent to the charge station"))
	}
	approval := req.approval

	loc, err := s.store.LookupAssetLocation(ctx, approval.LocationId)
	if err != nil {
		return approved{}, fmt.Errorf("look up location %s: %w", approval.LocationId, err)
	}
	// a location of another org is answered like one that does not exist
	if loc == nil || !strings.EqualFold(loc.OrgId, orgId) {
		return approved{}, assets.ValidationErrors{{Field: "location_id", Reason: "is not a location of the org of the charge station"}}
	}
	approval.LocationId = loc.Id
	for i, evse := range approval.Evses {
		err := s.checkTariffRefs(ctx, orgId, evse.Connectors, func(j int) string { return fmt.Sprintf("evses[%d].connectors[%d].", i, j) })
		if err != nil {
			return approved{}, err
		}
	}
	for _, evse := range approval.Evses {
		if evse.EvseId != nil {
			continue
		}
		evseId, err := s.generatedEvseId(ctx, loc, evse)
		if err != nil {
			return approved{}, err
		}
		evse.EvseId = &evseId
	}

	if req.profile != nil {
		approval.SecurityProfile = *req.profile
	} else if profile, ok := store.OnboardingSecurityProfile(cs.Onboarding.Transport); ok {
		approval.SecurityProfile = profile
	} else {
		approval.SecurityProfile = store.TLSWithBasicAuth
	}

	var onboardingPassword string
	var version transport.OcppVersion
	switch req.mode {
	case events.PasswordModeKeepOnboarding:
		settings, err := stations.LoadOnboardingSettings(ctx, s.tenantConfig, orgId)
		if err != nil {
			return approved{}, fmt.Errorf("onboarding settings of org %s: %w", orgId, err)
		}
		if cs.Onboarding.Credential != store.OnboardingCredentialOnboardingPassword || settings.PasswordSHA256 == "" {
			return approved{}, assets.ValidationErrors{{Field: "password_mode",
				Reason: "keep_onboarding needs a charge station that connected with the onboarding password of its tenant"}}
		}
		onboardingPassword = settings.PasswordSHA256
	case events.PasswordModeRotate:
		if s.commands == nil {
			return approved{}, answerWith(errCommandsNotAvailable)
		}
		if cs.Runtime != nil {
			version, _ = commands.VersionFromRuntime(cs.Runtime.OcppVersion)
		}
		if version == "" {
			return approved{}, answerWith(ErrConflict(errorStationVersionUnknown +
				": the OCPP version of the charge station is not known before it boots; its password can be set by hand (manual)"))
		}
	}

	if err := s.limits.CheckConnectors(ctx, orgId, req.connectors()); err != nil {
		var exceeded *entitlements.LimitExceededError
		if errors.As(err, &exceeded) {
			return approved{}, answerWith(ErrConflict(errorEntitlementExceeded + ": " + err.Error()))
		}
		return approved{}, fmt.Errorf("check the connector limit of org %s: %w", orgId, err)
	}

	approvals := s.approvals()
	switch req.mode {
	case events.PasswordModeKeepOnboarding:
		auth := &store.ChargeStationAuth{SecurityProfile: approval.SecurityProfile, Base64SHA256Password: onboardingPassword, InvalidUsernameAllowed: true}
		return approved{}, approvals.Complete(ctx, cs, approval, auth, true, req.mode)
	case events.PasswordModeManual:
		password, hash, err := onboarding.NewPassword()
		if err != nil {
			return approved{}, err
		}
		auth := &store.ChargeStationAuth{SecurityProfile: approval.SecurityProfile, Base64SHA256Password: hash, InvalidUsernameAllowed: true}
		return approved{password: password}, approvals.Complete(ctx, cs, approval, auth, false, req.mode)
	}
	var jti *string
	if p.Jti != "" {
		jti = &p.Jti
	}
	rotation, err := approvals.StartRotation(ctx, cs, approval, version, commandActor(p), jti)
	return approved{rotation: rotation}, err
}

// approvals completes the approvals of the API.
func (s *Server) approvals() *onboarding.Approvals {
	return &onboarding.Approvals{Store: s.store, Tx: s.store, Events: s.events, Commands: s.commands}
}

// RejectChargeStation refuses a pending charge station: it is decommissioned and can no
// longer connect.
func (s *Server) RejectChargeStation(w http.ResponseWriter, r *http.Request, csId string) {
	orgId, ok := stationOrgOf(w, r, csId)
	if !ok {
		return
	}
	in := new(RejectRequest)
	if !decodeOptionalBody(w, r, in) {
		return
	}
	reason := ""
	if in.Reason != nil {
		if utf8.RuneCountInString(*in.Reason) > maxStationTextLength {
			renderStationError(w, r, assets.ValidationErrors{{Field: "reason", Reason: fmt.Sprintf("must be at most %d characters", maxStationTextLength)}})
			return
		}
		reason = *in.Reason
	}
	actor := events.ActorFrom(r.Context())
	if actor == "" {
		actor = events.DefaultActor
	}

	var rejected *store.ChargeStation
	var queued *string
	err := s.store.RunInTx(r.Context(), func(ctx context.Context) error {
		cs, err := s.lookupStation(ctx, csId)
		if err != nil {
			return err
		}
		if err := s.store.RejectChargeStation(ctx, csId, reason); err != nil {
			if errors.Is(err, store.ErrInvalidState) {
				return answerWith(ErrConflict(errorInvalidState + ": the charge station is not pending"))
			}
			return fmt.Errorf("reject charge station %s: %w", csId, err)
		}
		if cs.Onboarding != nil && cs.Onboarding.Status == store.OnboardingApproving {
			queued = cs.Onboarding.CommandId
		}
		var why *string
		if reason != "" {
			why = &reason
		}
		err = s.events.Emit(ctx, events.Event{
			Type:         events.TypeStationRejected,
			OrgId:        orgId,
			ResourceType: events.ResourceStation,
			ResourceId:   csId,
			Actor:        actor,
			Data: events.StationRejectedData{
				StationKey: csId,
				StationId:  events.StationId(csId),
				Reason:     why,
				Actor:      actor,
			},
		})
		if err != nil {
			return fmt.Errorf("emit %s: %w", events.TypeStationRejected, err)
		}
		rejected, err = s.lookupStation(ctx, csId)
		return err
	})
	if err != nil {
		renderStationError(w, r, err)
		return
	}
	// a new password that has not been sent yet is not sent to a station that is gone
	if queued != nil && s.commands != nil {
		if _, err := s.commands.Cancel(r.Context(), *queued); err != nil && !errors.Is(err, commands.ErrNotCancellable) {
			slog.Warn("cancelling the new password of a rejected charge station", "chargeStationId", csId, "commandId", *queued, "err", err)
		}
	}
	s.renderStation(w, r, rejected, orgId)
}

// apiStationOnboarding is the onboarding of a pending charge station as the API shows it.
func apiStationOnboarding(ob *store.StationOnboarding) *StationOnboarding {
	return &StationOnboarding{
		Status:      StationOnboardingStatus(ob.Status),
		FirstSeenAt: ob.FirstSeenAt,
		LastSeenAt:  ob.LastSeenAt,
		Transport:   StationOnboardingTransport(ob.Transport),
		Credential:  StationOnboardingCredential(ob.Credential),
		Error:       ob.Error,
		CommandId:   ob.CommandId,
	}
}
