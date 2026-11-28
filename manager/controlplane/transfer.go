// SPDX-License-Identifier: Apache-2.0

package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/entitlements"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
)

// StationStore is what a transfer reads and writes: the charge stations, their assets,
// their connector statuses and transactions, in one store transaction.
type StationStore interface {
	store.Transactor
	store.ChargeStationRegistryStore
	store.AssetStore
	store.StatusStore
	store.TransactionStore
}

const (
	// resourcesPath is where the actions on a resource are, followed by the URN of the
	// resource, ":" and the action.
	resourcesPath = "/resources/"
	// actionTransfer moves a resource to another org.
	actionTransfer = "transfer"
	// maxTransferRequestBytes is the largest body of a transfer.
	maxTransferRequestBytes = 64 << 10
	// maxTransferReasonLength is the longest reason of a transfer, in characters.
	maxTransferReasonLength = 255
)

// The values of Error.Error of a transfer that cannot be done.
const (
	codeActiveTransaction       = "active_transaction"
	codeLocationShared          = "location_shared"
	codeTargetNotActive         = "target_not_active"
	codeEntitlementExceeded     = "entitlement_exceeded"
	codeInvalidState            = "invalid_state"
	codeUnsupportedResourceType = "unsupported_resource_type"
	codeTargetPartyRequired     = "target_party_required"
)

// The fields of a transfer, as named in the answers.
const (
	fieldURN              = "urn"
	fieldToOrgId          = "to_org_id"
	fieldMoveLocation     = "move_location"
	fieldTargetPartyRef   = "target_party_ref"
	fieldTargetLocationId = "target_location_id"
	fieldReason           = "reason"
)

// cpoRole is the role of the parties of a tenant that publish its locations.
const cpoRole = "CPO"

// TransferResource is the route of the spec: a URN that was percent-encoded as one
// path segment, followed by ":transfer".
func (s *Server) TransferResource(w http.ResponseWriter, r *http.Request, urn string) {
	s.transfer(w, r, urn)
}

// resourceAction serves every other POST below /resources/. The URN of a resource has
// ":" and "/" of its own, which a client may or may not encode, so the path is read here
// and not by the router: what follows the last ":" is the action, what comes before it
// is the URN, percent-encoded.
func (s *Server) resourceAction(w http.ResponseWriter, r *http.Request) {
	path := r.URL.EscapedPath()
	start := strings.Index(path, resourcesPath)
	if start < 0 {
		writeError(w, http.StatusNotFound, codeNotFound)
		return
	}
	resource := path[start+len(resourcesPath):]
	colon := strings.LastIndex(resource, ":")
	if colon < 0 || resource[colon+1:] != actionTransfer {
		writeError(w, http.StatusNotFound, codeNotFound)
		return
	}
	urn, err := url.PathUnescape(resource[:colon])
	if err != nil {
		writeInvalid(w, invalid(fieldURN, "cannot be read"))
		return
	}
	s.transfer(w, r, urn)
}

// transferRequest is a TransferRequest that was checked.
type transferRequest struct {
	toOrgId          string
	moveLocation     TransferRequestMoveLocation
	targetPartyRef   *string
	targetLocationId *string
	force            bool
	reason           *string
	// actor is who transfers the station, for the events
	actor string
}

// refusal is the answer to a transfer that cannot be done, returned as an error from the
// store transaction so that nothing of it is kept.
type refusal struct {
	status  int
	code    string
	field   string
	message string
}

func (e *refusal) Error() string {
	return e.code + ": " + e.message
}

func refuse(status int, code, field, message string) *refusal {
	return &refusal{status: status, code: code, field: field, message: message}
}

// transfer moves a charge station of the org of the URN to the org of the request: the
// station, its EVSEs and their connectors, its charging profiles and, when it is the only
// station there, its location, all in one store transaction with the events of the
// change and the cancellation of the commands that are queued for the station.
func (s *Server) transfer(w http.ResponseWriter, r *http.Request, urnText string) {
	// the guard in front of the handler only lets a verified caller through
	p, ok := tenancy.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthenticated)
		return
	}
	if !p.Has(tenancy.PermStationWrite) {
		slog.Warn("control plane: transfer refused without "+tenancy.PermStationWrite, "sub", p.Subject, "jti", p.Jti)
		writeError(w, http.StatusForbidden, codeForbidden)
		return
	}
	urn, err := ParseURN(urnText)
	if err != nil {
		writeInvalid(w, invalid(fieldURN, err.Error()))
		return
	}
	if urn.Type != events.ResourceStation {
		writeProblem(w, http.StatusUnprocessableEntity, codeUnsupportedResourceType, fieldURN, "only a charge station ("+events.ResourceStation+") can be transferred")
		return
	}
	req, problem := readTransferRequest(w, r)
	if problem != nil {
		if problem.code == codeTooLarge {
			writeError(w, http.StatusRequestEntityTooLarge, codeTooLarge)
			return
		}
		writeInvalid(w, problem)
		return
	}
	// the caller is the actor of the events, also where no middleware put it in ctx
	req.actor = events.ActorFrom(r.Context())
	if req.actor == "" {
		req.actor = p.Subject
	}
	if s.Stations == nil || s.Tenants == nil {
		writeError(w, http.StatusNotImplemented, codeNotImplemented)
		return
	}
	if s.Events == nil {
		slog.Error("control plane: no emitter for the events of a transfer")
		writeError(w, http.StatusInternalServerError, codeInternal)
		return
	}

	var answer *TransferResult
	err = s.Stations.RunInTx(r.Context(), func(ctx context.Context) error {
		var err error
		answer, err = s.transferInTx(ctx, urn, req)
		return err
	})
	var refused *refusal
	switch {
	case errors.As(err, &refused):
		writeProblem(w, refused.status, refused.code, refused.field, refused.message)
	case errors.Is(err, store.ErrNotImplemented):
		writeError(w, http.StatusNotImplemented, codeNotImplemented)
	case err != nil:
		slog.Error("control plane: transfer charge station", "stationKey", urn.Id, "from", urn.OrgId, "to", req.toOrgId, "error", err)
		writeError(w, http.StatusInternalServerError, codeInternal)
	default:
		slog.Info("charge station transferred", "stationKey", urn.Id, "from", answer.FromOrgId, "to", answer.ToOrgId,
			"locationMoved", answer.LocationMoved, "evses", answer.EvsesMoved, "connectors", answer.ConnectorsMoved, "sub", p.Subject)
		writeJSON(w, http.StatusOK, answer)
	}
}

// transferInTx makes the checks and the transfer in the store transaction of ctx.
func (s *Server) transferInTx(ctx context.Context, urn URN, req *transferRequest) (*TransferResult, error) {
	stationKey := urn.Id
	cs, err := s.Stations.LookupChargeStation(ctx, stationKey)
	if err != nil {
		return nil, fmt.Errorf("look up charge station %s: %w", stationKey, err)
	}
	notFound := refuse(http.StatusNotFound, codeNotFound, "", "")
	if cs == nil {
		return nil, notFound
	}
	owner, err := s.stationOwner(ctx, cs)
	if err != nil {
		return nil, err
	}
	// a station of another org is answered like one that does not exist
	if owner != urn.OrgId {
		return nil, notFound
	}
	if cs.State == store.StationStateDecommissioned {
		return nil, refuse(http.StatusConflict, codeInvalidState, "", "the charge station is decommissioned")
	}
	// an approval names a location of the old org and may be sending a password to the
	// station: it is finished (approved or rejected) in the old org first
	if cs.Onboarding != nil && (cs.Onboarding.Approval != nil || cs.Onboarding.Status != store.OnboardingAwaitingApproval) {
		return nil, refuse(http.StatusConflict, codeInvalidState, "", "the approval of the pending charge station is not finished: approve it again or reject it first")
	}
	if req.toOrgId == owner {
		return nil, refuse(http.StatusUnprocessableEntity, codeInvalidField, fieldToOrgId, "must not be the org of the charge station")
	}

	target, err := s.Tenants.GetTenant(ctx, req.toOrgId)
	if err != nil {
		return nil, fmt.Errorf("look up tenant %s: %w", req.toOrgId, err)
	}
	if target == nil || target.Status == store.TenantStatusSuspended || target.Status == store.TenantStatusCancelled {
		return nil, refuse(http.StatusConflict, codeTargetNotActive, fieldToOrgId, "must be a tenant that is active")
	}

	if !req.force {
		charging, err := s.stationCharging(ctx, stationKey)
		if err != nil {
			return nil, err
		}
		if charging {
			return nil, refuse(http.StatusConflict, codeActiveTransaction, "", "a transaction is running on the charge station; force transfers it all the same, and the session keeps the old org")
		}
	}

	plan, err := s.planLocation(ctx, cs, owner, target, req)
	if err != nil {
		return nil, err
	}

	if cs.State == store.StationStateActive {
		connectors, err := s.connectorsToCount(ctx, stationKey, plan.evses)
		if err != nil {
			return nil, err
		}
		var exceeded *entitlements.LimitExceededError
		err = s.Entitlements.CheckConnectors(ctx, req.toOrgId, connectors)
		if errors.As(err, &exceeded) {
			return nil, refuse(http.StatusConflict, codeEntitlementExceeded, fieldToOrgId, exceeded.Error())
		}
		if err != nil {
			return nil, fmt.Errorf("check the connector limit of org %s: %w", req.toOrgId, err)
		}
	}

	params := store.TransferParams{
		StationKey:   stationKey,
		FromOrgId:    cs.OrgId,
		ToOrgId:      req.toOrgId,
		MoveLocation: plan.move,
	}
	if plan.move {
		params.TargetPartyRef = &plan.partyRef
	} else if plan.target != nil {
		params.TargetLocationId = &plan.target.Id
	}
	result, err := s.Stations.TransferChargeStation(ctx, params)
	switch {
	case errors.Is(err, store.ErrAlreadyExists):
		return nil, refuse(http.StatusConflict, codeConflict, fieldTargetPartyRef, "the party already has a location with the OCPI id of the location of the charge station")
	case errors.Is(err, store.ErrInvalidState), errors.Is(err, store.ErrNotFound):
		return nil, refuse(http.StatusConflict, codeInvalidState, "", "the charge station or its location changed at the same time")
	case err != nil:
		return nil, fmt.Errorf("transfer charge station %s: %w", stationKey, err)
	}

	if err := s.emitTransferred(ctx, cs, owner, req, plan, result); err != nil {
		return nil, err
	}
	if err := s.cancelQueuedCommands(ctx, stationKey); err != nil {
		return nil, err
	}

	return &TransferResult{
		Urn:               urn.String(),
		NewUrn:            URN{Product: events.Product, OrgId: req.toOrgId, Type: events.ResourceStation, Id: stationKey}.String(),
		FromOrgId:         owner,
		ToOrgId:           req.toOrgId,
		LocationId:        result.LocationId,
		LocationMoved:     result.LocationMoved,
		EvsesMoved:        result.EvsesMoved,
		ConnectorsMoved:   result.ConnectorsMoved,
		ClearedTariffRefs: result.ClearedTariffRefs,
	}, nil
}

// stationOwner returns the org that owns the charge station, as the administration API
// sees it: a station without an org belongs to the tenant of its org key, or to the
// default org.
func (s *Server) stationOwner(ctx context.Context, cs *store.ChargeStation) (string, error) {
	owner := cs.OrgId
	if s.Resolver != nil {
		var err error
		if owner, err = s.Resolver.OrgIdForStation(ctx, cs.StationKey); err != nil {
			return "", fmt.Errorf("org of charge station %s: %w", cs.StationKey, err)
		}
	}
	if id, err := uuid.Parse(owner); err == nil {
		return id.String(), nil
	}
	return owner, nil
}

// stationCharging reports whether a transaction is running on the charge station: it
// reports a current transaction on a connector, or has a transaction that has not ended.
func (s *Server) stationCharging(ctx context.Context, stationKey string) (bool, error) {
	statuses, err := s.Stations.ListConnectorStatuses(ctx, stationKey)
	if err != nil {
		return false, fmt.Errorf("connector statuses of %s: %w", stationKey, err)
	}
	for _, status := range statuses {
		if status.CurrentTransactionId != nil && *status.CurrentTransactionId != "" {
			return true, nil
		}
	}
	_, active, err := s.Stations.ListTransactionsForChargeStation(ctx, stationKey, "active", nil, nil, 1, 0)
	if err != nil {
		return false, fmt.Errorf("active transactions of %s: %w", stationKey, err)
	}
	return active > 0, nil
}

// connectorsToCount returns how many connectors the charge station brings to the
// connector limit of the org that gets it: the connectors of its EVSEs that are not
// removed, or the connectors that it reports a status of when those are more (the limit
// counts the reported ones).
func (s *Server) connectorsToCount(ctx context.Context, stationKey string, evses []*store.AssetEvse) (int, error) {
	assetConnectors := 0
	for _, evse := range evses {
		if evse.Status != store.AssetEvseStatusRemoved {
			assetConnectors += len(evse.Connectors)
		}
	}
	statuses, err := s.Stations.ListConnectorStatuses(ctx, stationKey)
	if err != nil {
		return 0, fmt.Errorf("connector statuses of %s: %w", stationKey, err)
	}
	reported := 0
	for _, status := range statuses {
		if status.ConnectorId > 0 {
			reported++
		}
	}
	return max(assetConnectors, reported), nil
}

// locationPlan says where a transferred charge station and its EVSEs go.
type locationPlan struct {
	// move is true when the location of the station moves with it, to the party partyRef.
	move     bool
	partyRef string
	// current is the location of the station before the transfer; nil when it has none.
	current *store.AssetLocation
	// target is where the station and its EVSEs are after the transfer: the location
	// that moves, or a location of the target org; nil leaves the station without one.
	target *store.AssetLocation
	// evses are the EVSEs of the station before the transfer.
	evses []*store.AssetEvse
}

// planLocation decides, as move_location says, whether the location of the charge
// station moves with it and where its EVSEs go.
func (s *Server) planLocation(ctx context.Context, cs *store.ChargeStation, owner string, target *store.Tenant, req *transferRequest) (*locationPlan, error) {
	evses, err := s.Stations.ListAssetEvsesForStation(ctx, cs.StationKey)
	if err != nil {
		return nil, fmt.Errorf("list the EVSEs of charge station %s: %w", cs.StationKey, err)
	}
	plan := &locationPlan{evses: evses}
	if cs.LocationId != nil {
		if plan.current, err = s.Stations.LookupAssetLocation(ctx, *cs.LocationId); err != nil {
			return nil, fmt.Errorf("look up location %s: %w", *cs.LocationId, err)
		}
	}
	whollyOwned := false
	if plan.current != nil && strings.EqualFold(plan.current.OrgId, owner) {
		shared, err := s.locationShared(ctx, plan.current, cs.StationKey)
		if err != nil {
			return nil, err
		}
		whollyOwned = !shared
	}

	switch {
	case req.moveLocation == TransferRequestMoveLocationAlways:
		if plan.current == nil {
			return nil, refuse(http.StatusUnprocessableEntity, codeInvalidField, fieldMoveLocation, "cannot be always: the charge station has no location")
		}
		if !whollyOwned {
			return nil, refuse(http.StatusConflict, codeLocationShared, fieldMoveLocation, "the location of the charge station is shared with other charge stations")
		}
		plan.move = true
	case req.targetLocationId != nil:
		loc, err := s.Stations.LookupAssetLocation(ctx, *req.targetLocationId)
		if err != nil {
			return nil, fmt.Errorf("look up location %s: %w", *req.targetLocationId, err)
		}
		if loc == nil || !strings.EqualFold(loc.OrgId, req.toOrgId) {
			return nil, refuse(http.StatusUnprocessableEntity, codeInvalidField, fieldTargetLocationId, "must be a location of the org to_org_id")
		}
		plan.target = loc
	case req.moveLocation == TransferRequestMoveLocationAuto && whollyOwned:
		plan.move = true
	case plan.current == nil && len(evses) == 0:
		// nothing to place: the station has no location before and after
	case req.moveLocation == TransferRequestMoveLocationAuto:
		return nil, refuse(http.StatusUnprocessableEntity, codeInvalidField, fieldTargetLocationId, "is required: the location of the charge station is shared with other charge stations")
	default:
		return nil, refuse(http.StatusUnprocessableEntity, codeInvalidField, fieldTargetLocationId, "is required: the location of the charge station stays with its org")
	}

	if plan.move {
		plan.target = plan.current
		party, problem := targetParty(target, req.targetPartyRef)
		if problem != nil {
			return nil, problem
		}
		plan.partyRef = party
	}
	return plan, nil
}

// locationShared reports whether another charge station, or an EVSE of another charge
// station, is at the location.
func (s *Server) locationShared(ctx context.Context, loc *store.AssetLocation, stationKey string) (bool, error) {
	evses, err := s.Stations.ListAssetEvsesForLocation(ctx, loc.Id)
	if err != nil {
		return false, fmt.Errorf("list the EVSEs of location %s: %w", loc.Id, err)
	}
	if slices.ContainsFunc(evses, func(evse *store.AssetEvse) bool { return evse.StationKey != stationKey }) {
		return true, nil
	}
	stations, _, err := s.Stations.ListChargeStations(ctx, store.ChargeStationFilter{
		OrgIds:     []string{loc.OrgId},
		LocationId: &loc.Id,
		Page:       store.PageRequest{Limit: 2},
	})
	if err != nil {
		return false, fmt.Errorf("list the charge stations at location %s: %w", loc.Id, err)
	}
	return slices.ContainsFunc(stations, func(other *store.ChargeStation) bool { return other.StationKey != stationKey }), nil
}

// targetParty returns the CPO party of the target tenant that a location that moves gets:
// the one that the request names, or the only CPO party of the tenant.
func targetParty(target *store.Tenant, requested *string) (string, *refusal) {
	var cpo []string
	for _, party := range target.Parties {
		if party.Role == cpoRole {
			cpo = append(cpo, party.Id)
		}
	}
	if requested != nil {
		if !slices.Contains(cpo, *requested) {
			return "", refuse(http.StatusUnprocessableEntity, codeInvalidField, fieldTargetPartyRef, "must be a CPO party of the tenant to_org_id")
		}
		return *requested, nil
	}
	if len(cpo) != 1 {
		return "", refuse(http.StatusUnprocessableEntity, codeTargetPartyRequired, fieldTargetPartyRef,
			"is required: the location moves and the tenant to_org_id has no single CPO party")
	}
	return cpo[0], nil
}

// emitTransferred stores the events of a transfer in the store transaction of ctx:
// station.transferred in both orgs; location.updated with the change transferred in both
// orgs for a location that moved, and with the change updated for every location that
// lost or gained EVSEs otherwise.
func (s *Server) emitTransferred(ctx context.Context, cs *store.ChargeStation, owner string, req *transferRequest, plan *locationPlan, result *store.TransferResult) error {
	actor := req.actor
	data := events.StationTransferredData{
		StationKey:        cs.StationKey,
		StationId:         events.StationId(cs.StationKey),
		FromOrgId:         owner,
		ToOrgId:           req.toOrgId,
		LocationId:        result.LocationId,
		LocationMoved:     result.LocationMoved,
		EvsesMoved:        result.EvsesMoved,
		ConnectorsMoved:   result.ConnectorsMoved,
		ClearedTariffRefs: result.ClearedTariffRefs,
		Actor:             actor,
		Reason:            req.reason,
	}
	emitted := make([]events.Event, 0, 4)
	for _, orgId := range []string{owner, req.toOrgId} {
		emitted = append(emitted, events.Event{
			Type:         events.TypeStationTransferred,
			OrgId:        orgId,
			ResourceType: events.ResourceStation,
			ResourceId:   cs.StationKey,
			Actor:        actor,
			Data:         data,
		})
	}

	if plan.move {
		moved := *plan.current
		emitted = append(emitted, locationUpdated(plan.current, owner, events.LocationChangeTransferred, actor))
		moved.PartyRef = plan.partyRef
		emitted = append(emitted, locationUpdated(&moved, req.toOrgId, events.LocationChangeTransferred, actor))
	}
	// the other locations of the old org that lose EVSEs, each once
	lost := map[string]bool{}
	for _, evse := range plan.evses {
		if (plan.move && evse.LocationId == plan.current.Id) || lost[evse.LocationId] {
			continue
		}
		lost[evse.LocationId] = true
		loc, err := s.Stations.LookupAssetLocation(ctx, evse.LocationId)
		if err != nil {
			return fmt.Errorf("look up location %s: %w", evse.LocationId, err)
		}
		if loc != nil {
			emitted = append(emitted, locationUpdated(loc, loc.OrgId, events.LocationChangeUpdated, actor))
		}
	}
	if !plan.move && plan.target != nil && len(plan.evses) > 0 {
		emitted = append(emitted, locationUpdated(plan.target, req.toOrgId, events.LocationChangeUpdated, actor))
	}

	if err := s.Events.Emit(ctx, emitted...); err != nil {
		return fmt.Errorf("emit %s: %w", events.TypeStationTransferred, err)
	}
	return nil
}

// locationUpdated is the location.updated event of the location in the org.
func locationUpdated(loc *store.AssetLocation, orgId, change, actor string) events.Event {
	return events.Event{
		Type:         events.TypeLocationUpdated,
		OrgId:        orgId,
		ResourceType: events.ResourceLocation,
		ResourceId:   loc.Id,
		Actor:        actor,
		Data: events.LocationUpdatedData{
			LocationId: loc.Id,
			OcpiId:     loc.OcpiId,
			PartyRef:   loc.PartyRef,
			Change:     change,
			Publish:    loc.Publish,
		},
	}
}

// cancelQueuedCommands cancels the commands that wait to be sent to the charge station:
// they were asked for by its old org. A command that is sent meanwhile is left to
// finish. Without a command queue there is nothing to cancel.
func (s *Server) cancelQueuedCommands(ctx context.Context, stationKey string) error {
	if s.Commands == nil {
		return nil
	}
	for {
		queued, _, err := s.Commands.List(ctx, store.CommandFilter{
			StationKey: stationKey,
			Statuses:   []store.CommandStatus{store.CommandStatusQueued},
			Limit:      store.MaxCommandListLimit,
		})
		if errors.Is(err, store.ErrNotImplemented) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("list the queued commands of charge station %s: %w", stationKey, err)
		}
		cancelled := 0
		for _, cmd := range queued {
			_, err := s.Commands.Cancel(ctx, cmd.Id)
			if errors.Is(err, commands.ErrNotCancellable) {
				continue
			}
			if err != nil {
				return fmt.Errorf("cancel command %s of charge station %s: %w", cmd.Id, stationKey, err)
			}
			cancelled++
		}
		// a full page may be followed by more; a command that could not be cancelled is
		// no longer queued and is not listed again
		if len(queued) < store.MaxCommandListLimit || cancelled == 0 {
			return nil
		}
	}
}

// readTransferRequest reads and checks the body of a transfer. A body that is too large
// is a problem with the code codeTooLarge.
func readTransferRequest(w http.ResponseWriter, r *http.Request) (*transferRequest, *invalidField) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxTransferRequestBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, &invalidField{code: codeTooLarge}
		}
		return nil, invalid(fieldBody, "cannot be read")
	}
	var doc TransferRequest
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, jsonProblem("", err)
	}

	req := &transferRequest{moveLocation: TransferRequestMoveLocationAuto}
	var ok bool
	if req.toOrgId, ok = canonicalOrgId(doc.ToOrgId); !ok {
		return nil, invalid(fieldToOrgId, "must be a UUID")
	}
	if doc.MoveLocation != nil {
		switch *doc.MoveLocation {
		case TransferRequestMoveLocationAuto, TransferRequestMoveLocationAlways, TransferRequestMoveLocationNever:
			req.moveLocation = *doc.MoveLocation
		default:
			return nil, invalid(fieldMoveLocation, "must be auto, always or never")
		}
	}
	if doc.TargetPartyRef != nil {
		if !validPartyRecordId(*doc.TargetPartyRef) {
			return nil, invalid(fieldTargetPartyRef, fmt.Sprintf("must be 1 to %d characters, without control characters", maxPartyRecordIdLength))
		}
		req.targetPartyRef = doc.TargetPartyRef
	}
	if doc.TargetLocationId != nil {
		id, err := uuid.Parse(*doc.TargetLocationId)
		if err != nil || len(*doc.TargetLocationId) != 36 {
			return nil, invalid(fieldTargetLocationId, "must be a UUID")
		}
		if req.moveLocation == TransferRequestMoveLocationAlways {
			return nil, invalid(fieldTargetLocationId, "must not be given when the location moves (move_location always)")
		}
		location := id.String()
		req.targetLocationId = &location
	}
	if doc.Reason != nil {
		if utf8.RuneCountInString(*doc.Reason) > maxTransferReasonLength {
			return nil, invalid(fieldReason, fmt.Sprintf("must be at most %d characters", maxTransferReasonLength))
		}
		req.reason = doc.Reason
	}
	req.force = doc.Force != nil && *doc.Force
	return req, nil
}
