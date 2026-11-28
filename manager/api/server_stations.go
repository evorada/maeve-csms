// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/render"
	"github.com/google/uuid"
	"github.com/thoughtworks/maeve-csms/manager/assets"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
)

const (
	// maxStationTextLength is the most characters of the display name of a charge station
	// and of the reason of its decommissioning.
	maxStationTextLength = 255
	// maxCapacityKw is the largest capacity that the registry keeps (NUMERIC(10,3)).
	maxCapacityKw = 9999999.999
)

// errRegistryNotAvailable answers an operation on the registry of the charge stations of a
// manager whose storage engine does not keep it.
var errRegistryNotAvailable = &ErrResponse{
	HTTPStatusCode: http.StatusNotImplemented,
	StatusText:     http.StatusText(http.StatusNotImplemented),
	ErrorText:      "the storage engine of the manager does not keep the registry of the charge stations",
}

// connectorStatusFilters are the connector statuses that a list of charge stations can be
// narrowed to.
var connectorStatusFilters = []ConnectorStatusFilter{
	ConnectorStatusFilterAvailable,
	ConnectorStatusFilterPreparing,
	ConnectorStatusFilterCharging,
	ConnectorStatusFilterSuspendedEVSE,
	ConnectorStatusFilterSuspendedEV,
	ConnectorStatusFilterFinishing,
	ConnectorStatusFilterReserved,
	ConnectorStatusFilterUnavailable,
	ConnectorStatusFilterFaulted,
	ConnectorStatusFilterOccupied,
}

// renderStationError answers an operation on the registry of the charge stations that
// failed with err.
func renderStationError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotImplemented) {
		_ = render.Render(w, r, errRegistryNotAvailable)
		return
	}
	renderAssetError(w, r, "charge station", err)
}

// stationOrgOf returns the org that owns the charge station of the request, which
// AuthzMiddleware resolved and checked against the scope of the caller. Without it the
// request is answered (500) and false is reported.
func stationOrgOf(w http.ResponseWriter, r *http.Request, csId string) (string, bool) {
	orgId, ok := tenancy.StationOrgFromContext(r.Context())
	if !ok {
		slog.Error("an operation on a charge station without a resolved org", "chargeStationId", csId)
		_ = render.Render(w, r, errInternal)
		return "", false
	}
	return orgId, true
}

// lookupStation returns the registry record of a charge station that exists, and
// otherwise the answer (404) as the error.
func (s *Server) lookupStation(ctx context.Context, stationKey string) (*store.ChargeStation, error) {
	cs, err := s.store.LookupChargeStation(ctx, stationKey)
	if err != nil {
		return nil, fmt.Errorf("look up charge station %s: %w", stationKey, err)
	}
	if cs == nil {
		return nil, answerWith(ErrNotFound)
	}
	return cs, nil
}

// stationOrgIds returns the orgs whose charge stations the caller may list: the orgs in
// its scope and, with the default org, the legacy org, whose charge stations belong to
// the default org (tenancy.StationOrgId). They are also the orgs of what takes its org
// from a charge station, the transactions and the meter values (Server.sessionOrgIds).
func (s *Server) stationOrgIds(ctx context.Context, p *tenancy.Principal) ([]string, error) {
	orgIds, err := s.scoper.OrgIds(ctx, p)
	if err != nil {
		return nil, err
	}
	defaultOrgId := strings.ToLower(s.defaultOrgId())
	if defaultOrgId != store.LegacyOrgId && slices.Contains(orgIds, defaultOrgId) {
		return append(slices.Clone(orgIds), store.LegacyOrgId), nil
	}
	return orgIds, nil
}

// stationOwner returns the org that owns the charge station: the org of its record, or
// the default org for a charge station of no org (tenancy.StationOrgId).
func (s *Server) stationOwner(cs *store.ChargeStation) string {
	if cs.OrgId == "" || cs.OrgId == store.LegacyOrgId {
		return strings.ToLower(s.defaultOrgId())
	}
	return cs.OrgId
}

// apiChargeStation is the charge station of the org as the API shows it, with whether its
// connection is open, without its EVSEs.
func (s *Server) apiChargeStation(ctx context.Context, cs *store.ChargeStation, orgId string) (ChargeStationRecord, error) {
	out := ChargeStationRecord{
		CsId:           cs.StationKey,
		OrgId:          orgId,
		LocationId:     cs.LocationId,
		DisplayName:    cs.DisplayName,
		MaxCapacityKw:  cs.MaxCapacityKw,
		AllowAnyToken:  cs.AllowAnyToken,
		State:          ChargeStationState(cs.State),
		StateChangedAt: cs.StateChangedAt,
		StateReason:    cs.StateReason,
	}
	if cs.State == store.StationStatePending && cs.Onboarding != nil {
		out.Onboarding = apiStationOnboarding(cs.Onboarding)
	}
	if runtime := cs.Runtime; runtime != nil {
		if runtime.OcppVersion != "" {
			ocppVersion := runtime.OcppVersion
			out.OcppVersion = &ocppVersion
		}
		out.Vendor = runtime.Vendor
		out.Model = runtime.Model
		out.SerialNumber = runtime.SerialNumber
		out.FirmwareVersion = runtime.FirmwareVersion
		out.Iccid = runtime.Iccid
		out.Imsi = runtime.Imsi
		out.MeterSerialNumber = runtime.MeterSerialNumber
		out.MeterType = runtime.MeterType
		out.LastBootAt = runtime.LastBootAt
	}

	status, err := s.store.GetChargeStationStatus(ctx, cs.StationKey)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// the charge station has never connected
	case err != nil:
		return ChargeStationRecord{}, fmt.Errorf("status of charge station %s: %w", cs.StationKey, err)
	default:
		out.Connected = status.Connected
		out.LastHeartbeat = status.LastHeartbeat
	}
	return out, nil
}

// pageLimit returns the size of a page of a list: the requested limit, or the default.
// When the limit is out of range the request is answered (400) and false is reported.
func pageLimit(w http.ResponseWriter, r *http.Request, requested *int) (int, bool) {
	if requested == nil {
		return store.DefaultPageLimit, true
	}
	if *requested < 1 || *requested > store.MaxPageLimit {
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("limit must be 1 to %d", store.MaxPageLimit)))
		return 0, false
	}
	return *requested, true
}

// stationFilter returns the filter of a list of charge stations, without its orgs. When a
// parameter has a value that is not known the request is answered (400) and false is
// reported.
func stationFilter(w http.ResponseWriter, r *http.Request, params ListChargeStationsParams) (store.ChargeStationFilter, bool) {
	limit, ok := pageLimit(w, r, params.Limit)
	if !ok {
		return store.ChargeStationFilter{}, false
	}
	filter := store.ChargeStationFilter{
		Vendor:    params.Vendor,
		Model:     params.Model,
		Connected: params.Connected,
		Query:     params.Q,
		Page:      store.PageRequest{Limit: limit},
	}
	if params.Cursor != nil {
		filter.Page.Cursor = *params.Cursor
	}
	if params.LocationId != nil {
		locationId := params.LocationId.String()
		filter.LocationId = &locationId
	}
	if params.State != nil {
		for _, state := range *params.State {
			if !store.StationState(state).Valid() {
				_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("state %q is not known: pending, active or decommissioned", state)))
				return store.ChargeStationFilter{}, false
			}
			filter.States = append(filter.States, store.StationState(state))
		}
	}
	if params.Status != nil {
		for _, status := range *params.Status {
			if !slices.Contains(connectorStatusFilters, status) {
				_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("status %q is not an OCPP status of a connector", status)))
				return store.ChargeStationFilter{}, false
			}
			filter.StatusAnyOf = append(filter.StatusAnyOf, store.ConnectorStatusType(status))
		}
	}
	return filter, true
}

// ListChargeStations answers a page of the charge stations of the orgs that the caller
// may reach.
func (s *Server) ListChargeStations(w http.ResponseWriter, r *http.Request, params ListChargeStationsParams) {
	p, ok := s.caller(w, r)
	if !ok {
		return
	}
	filter, ok := stationFilter(w, r, params)
	if !ok {
		return
	}
	// the operation has no charge station: AuthzMiddleware does not scope it
	orgIds, err := s.stationOrgIds(r.Context(), p)
	if err != nil {
		renderScopeError(w, r, err)
		return
	}
	page := ChargeStationPage{Items: []ChargeStationRecord{}}
	if len(orgIds) == 0 {
		render.JSON(w, r, page)
		return
	}
	filter.OrgIds = orgIds

	found, next, err := s.store.ListChargeStations(r.Context(), filter)
	if errors.Is(err, store.ErrInvalidCursor) {
		_ = render.Render(w, r, ErrInvalidRequest(errors.New("cursor is not the next_cursor of a page")))
		return
	}
	if err != nil {
		renderStationError(w, r, fmt.Errorf("list charge stations: %w", err))
		return
	}
	for _, cs := range found {
		item, err := s.apiChargeStation(r.Context(), cs, s.stationOwner(cs))
		if err != nil {
			renderStationError(w, r, err)
			return
		}
		page.Items = append(page.Items, item)
	}
	if next != "" {
		page.NextCursor = &next
	}
	render.JSON(w, r, page)
}

// GetChargeStation answers the charge station with its EVSEs.
func (s *Server) GetChargeStation(w http.ResponseWriter, r *http.Request, csId string) {
	orgId, ok := stationOrgOf(w, r, csId)
	if !ok {
		return
	}
	cs, err := s.lookupStation(r.Context(), csId)
	if err != nil {
		renderStationError(w, r, err)
		return
	}
	out, err := s.apiChargeStation(r.Context(), cs, orgId)
	if err != nil {
		renderStationError(w, r, err)
		return
	}
	evses, err := s.store.ListAssetEvsesForStation(r.Context(), csId)
	if err != nil {
		renderStationError(w, r, fmt.Errorf("list the EVSEs of charge station %s: %w", csId, err))
		return
	}
	withStatus, err := s.apiAssetEvses(r.Context(), evses)
	if err != nil {
		renderStationError(w, r, err)
		return
	}
	out.Evses = &withStatus
	render.JSON(w, r, out)
}

// patchField is a field of a merge patch: whether the patch has it, and its value (nil
// when the patch clears it).
type patchField[T any] struct {
	set   bool
	value *T
}

// stationPatch is a merge patch of the registry record of a charge station.
type stationPatch struct {
	displayName   patchField[string]
	locationId    patchField[string]
	maxCapacityKw patchField[float64]
	allowAnyToken patchField[bool]
}

// patchValue reads a field of a merge patch: null clears the field. It fails with reason
// when the value is not one of type T.
func patchValue[T any](raw json.RawMessage, reason string) (patchField[T], error) {
	if string(bytes.TrimSpace(raw)) == "null" {
		return patchField[T]{set: true}, nil
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return patchField[T]{}, errors.New(reason)
	}
	return patchField[T]{set: true, value: &v}, nil
}

// parseStationPatch reads the merge patch of a charge station. It returns the fields that
// break a rule as assets.ValidationErrors.
func parseStationPatch(body map[string]json.RawMessage) (stationPatch, error) {
	var patch stationPatch
	var invalid assets.ValidationErrors
	for name, raw := range body {
		var err error
		switch name {
		case "display_name":
			patch.displayName, err = patchValue[string](raw, "must be a string or null")
		case "location_id":
			patch.locationId, err = patchValue[string](raw, "must be the id (a UUID) of a location or null")
		case "max_capacity_kw":
			patch.maxCapacityKw, err = patchValue[float64](raw, "must be a number or null")
		case "allow_any_token":
			patch.allowAnyToken, err = patchValue[bool](raw, "must be true or false")
		default:
			err = errors.New("is not a field of a charge station that can be changed")
		}
		if err != nil {
			invalid = append(invalid, assets.ValidationError{Field: name, Reason: err.Error()})
		}
	}

	if name := patch.displayName.value; name != nil && (strings.TrimSpace(*name) == "" || utf8.RuneCountInString(*name) > maxStationTextLength) {
		invalid = append(invalid, assets.ValidationError{Field: "display_name", Reason: fmt.Sprintf("must be 1 to %d characters, or null to clear it", maxStationTextLength)})
	}
	if id := patch.locationId.value; id != nil {
		if _, err := uuid.Parse(*id); err != nil {
			invalid = append(invalid, assets.ValidationError{Field: "location_id", Reason: "must be the id (a UUID) of a location or null"})
		}
	}
	if capacity := patch.maxCapacityKw.value; capacity != nil && (*capacity <= 0 || *capacity > maxCapacityKw) {
		invalid = append(invalid, assets.ValidationError{Field: "max_capacity_kw", Reason: fmt.Sprintf("must be above 0 and at most %.3f kW, or null to clear it", maxCapacityKw)})
	}
	if patch.allowAnyToken.set && patch.allowAnyToken.value == nil {
		invalid = append(invalid, assets.ValidationError{Field: "allow_any_token", Reason: "must be true or false"})
	}
	// the fields of a JSON object have no order: answer them in a stable one
	slices.SortStableFunc(invalid, func(a, b assets.ValidationError) int { return strings.Compare(a.Field, b.Field) })
	return patch, validation(nil, invalid...)
}

// UpdateChargeStation applies a merge patch to the registry record of the charge station.
// A new location moves the charge station and its EVSEs there.
func (s *Server) UpdateChargeStation(w http.ResponseWriter, r *http.Request, csId string) {
	orgId, ok := stationOrgOf(w, r, csId)
	if !ok {
		return
	}
	var body map[string]json.RawMessage
	if !decodeBody(w, r, &body) {
		return
	}
	if body == nil {
		_ = render.Render(w, r, ErrInvalidRequest(errors.New("the body is not a JSON object")))
		return
	}
	patch, err := parseStationPatch(body)
	if err != nil {
		renderStationError(w, r, err)
		return
	}

	var updated *store.ChargeStation
	err = s.store.RunInTx(r.Context(), func(ctx context.Context) error {
		cs, err := s.lookupStation(ctx, csId)
		if err != nil {
			return err
		}
		if cs.State == store.StationStateDecommissioned {
			return answerWith(ErrConflict(errorStationDecommissioned + ": the charge station is decommissioned"))
		}

		changed := *cs
		if patch.displayName.set {
			changed.DisplayName = patch.displayName.value
		}
		if patch.maxCapacityKw.set {
			changed.MaxCapacityKw = patch.maxCapacityKw.value
		}
		if patch.allowAnyToken.set {
			changed.AllowAnyToken = *patch.allowAnyToken.value
		}
		if patch.locationId.set {
			if err := s.relocateStation(ctx, &changed, orgId, patch.locationId.value); err != nil {
				return err
			}
		}

		if !sameRegistryFields(cs, &changed) {
			if err := s.store.UpdateChargeStation(ctx, &changed); err != nil {
				return fmt.Errorf("update charge station %s: %w", csId, err)
			}
		}
		updated, err = s.lookupStation(ctx, csId)
		return err
	})
	if err != nil {
		renderStationError(w, r, err)
		return
	}
	s.renderStation(w, r, updated, orgId)
}

// relocateStation changes the location of the charge station cs of the org to the
// location, or clears it (nil). A new location must be one of the org: the charge station
// and its EVSEs move there, and location.updated is emitted for every location whose EVSEs
// change. The location can only be cleared while the charge station has no EVSEs.
func (s *Server) relocateStation(ctx context.Context, cs *store.ChargeStation, orgId string, locationId *string) error {
	evses, err := s.store.ListAssetEvsesForStation(ctx, cs.StationKey)
	if err != nil {
		return fmt.Errorf("list the EVSEs of charge station %s: %w", cs.StationKey, err)
	}
	if locationId == nil {
		if len(evses) > 0 {
			return assets.ValidationErrors{{Field: "location_id", Reason: "cannot be cleared while the charge station has EVSEs: move it to another location"}}
		}
		cs.LocationId = nil
		return nil
	}

	target, err := s.store.LookupAssetLocation(ctx, *locationId)
	if err != nil {
		return fmt.Errorf("look up location %s: %w", *locationId, err)
	}
	// a location of another org is answered like one that does not exist
	if target == nil || !strings.EqualFold(target.OrgId, orgId) {
		return assets.ValidationErrors{{Field: "location_id", Reason: "is not a location of the org of the charge station"}}
	}
	if cs.LocationId != nil && *cs.LocationId == target.Id && !evsesElsewhere(evses, target.Id) {
		return nil
	}

	// the locations that lose an EVSE, in the order of the EVSEs, after the one that gains them
	affected := []*store.AssetLocation{target}
	for _, evse := range evses {
		if evse.LocationId == target.Id || slices.ContainsFunc(affected, func(loc *store.AssetLocation) bool { return loc.Id == evse.LocationId }) {
			continue
		}
		source, err := s.locationOf(ctx, evse)
		if err != nil {
			return err
		}
		affected = append(affected, source)
	}
	if err := s.store.MoveChargeStationToLocation(ctx, cs.StationKey, target.Id); err != nil {
		return fmt.Errorf("move charge station %s to location %s: %w", cs.StationKey, target.Id, err)
	}
	cs.LocationId = &target.Id
	if len(affected) == 1 {
		// no EVSE moved: what the locations publish is as it was
		return nil
	}
	for _, loc := range affected {
		if err := s.emitLocationUpdated(ctx, loc, events.LocationChangeUpdated, "", "", false); err != nil {
			return err
		}
	}
	return nil
}

// evsesElsewhere reports whether an EVSE is not at the location.
func evsesElsewhere(evses []*store.AssetEvse, locationId string) bool {
	return slices.ContainsFunc(evses, func(evse *store.AssetEvse) bool { return evse.LocationId != locationId })
}

// sameRegistryFields reports whether the fields that UpdateChargeStation writes are the
// same in both records.
func sameRegistryFields(a, b *store.ChargeStation) bool {
	return equalPtr(a.LocationId, b.LocationId) && equalPtr(a.DisplayName, b.DisplayName) &&
		equalPtr(a.MaxCapacityKw, b.MaxCapacityKw) && a.AllowAnyToken == b.AllowAnyToken
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// decodeOptionalBody reads the JSON body of the request into v, and leaves v as it is
// when there is no body. When the body cannot be read the request is answered (400) and
// false is reported.
func decodeOptionalBody(w http.ResponseWriter, r *http.Request, v any) bool {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("the body cannot be read: %w", err)))
		return false
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return true
	}
	if err := json.Unmarshal(raw, v); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("the body is not a JSON document of the operation: %w", err)))
		return false
	}
	return true
}

// DecommissionChargeStation takes the charge station out of service: its state becomes
// decommissioned and its EVSEs REMOVED. While a transaction is running on it, only with
// force: the charging goes on, and so does the connection of the charge station.
func (s *Server) DecommissionChargeStation(w http.ResponseWriter, r *http.Request, csId string) {
	orgId, ok := stationOrgOf(w, r, csId)
	if !ok {
		return
	}
	in := new(DecommissionRequest)
	if !decodeOptionalBody(w, r, in) {
		return
	}
	if in.Reason != nil && utf8.RuneCountInString(*in.Reason) > maxStationTextLength {
		renderStationError(w, r, assets.ValidationErrors{{Field: "reason", Reason: fmt.Sprintf("must be at most %d characters", maxStationTextLength)}})
		return
	}
	force := in.Force != nil && *in.Force

	var decommissioned *store.ChargeStation
	err := s.store.RunInTx(r.Context(), func(ctx context.Context) error {
		cs, err := s.lookupStation(ctx, csId)
		if err != nil {
			return err
		}
		if cs.State == store.StationStateDecommissioned {
			return answerWith(ErrConflict(errorStationDecommissioned + ": the charge station is decommissioned already"))
		}
		if !force {
			charging, err := s.stationCharging(ctx, csId)
			if err != nil {
				return err
			}
			if charging {
				return answerWith(ErrConflict(errorActiveTransaction + ": a transaction is running on the charge station; force decommissions it all the same"))
			}
		}

		evses, err := s.store.ListAssetEvsesForStation(ctx, csId)
		if err != nil {
			return fmt.Errorf("list the EVSEs of charge station %s: %w", csId, err)
		}
		if err := s.store.DecommissionChargeStation(ctx, csId, in.Reason); err != nil {
			return fmt.Errorf("decommission charge station %s: %w", csId, err)
		}
		if err := s.emitEvsesRemoved(ctx, evses); err != nil {
			return err
		}
		if err := s.emitStationDecommissioned(ctx, cs, orgId, in.Reason); err != nil {
			return err
		}
		decommissioned, err = s.lookupStation(ctx, csId)
		return err
	})
	if err != nil {
		renderStationError(w, r, err)
		return
	}
	s.renderStation(w, r, decommissioned, orgId)
}

// stationCharging reports whether a transaction is running on the charge station: it
// reports a current transaction on a connector, or has a transaction that has not ended.
func (s *Server) stationCharging(ctx context.Context, stationKey string) (bool, error) {
	statuses, err := s.store.ListConnectorStatuses(ctx, stationKey)
	if err != nil {
		return false, fmt.Errorf("connector statuses of %s: %w", stationKey, err)
	}
	for _, status := range statuses {
		if status.CurrentTransactionId != nil && *status.CurrentTransactionId != "" {
			return true, nil
		}
	}
	_, active, err := s.store.ListTransactionsForChargeStation(ctx, stationKey, "active", nil, nil, 1, 0, nil)
	if err != nil {
		return false, fmt.Errorf("active transactions of %s: %w", stationKey, err)
	}
	return active > 0, nil
}

// emitEvsesRemoved emits location.updated for each of the EVSEs that was not REMOVED
// before the charge station was decommissioned.
func (s *Server) emitEvsesRemoved(ctx context.Context, evses []*store.AssetEvse) error {
	locations := map[string]*store.AssetLocation{}
	for _, evse := range evses {
		if evse.Status == store.AssetEvseStatusRemoved {
			continue
		}
		loc := locations[evse.LocationId]
		if loc == nil {
			var err error
			if loc, err = s.locationOf(ctx, evse); err != nil {
				return err
			}
			locations[evse.LocationId] = loc
		}
		if err := s.emitLocationUpdated(ctx, loc, events.LocationChangeUpdated, evse.Id, "", false); err != nil {
			return err
		}
	}
	return nil
}

// emitStationDecommissioned stores the station.decommissioned event of the charge station
// of the org in the store transaction of ctx. Its actor is the caller (ActorMiddleware).
func (s *Server) emitStationDecommissioned(ctx context.Context, cs *store.ChargeStation, orgId string, reason *string) error {
	actor := events.ActorFrom(ctx)
	if actor == "" {
		actor = events.DefaultActor
	}
	err := s.events.Emit(ctx, events.Event{
		Type:         events.TypeStationDecommissioned,
		OrgId:        orgId,
		ResourceType: events.ResourceStation,
		ResourceId:   cs.StationKey,
		Actor:        actor,
		Data: events.StationDecommissionedData{
			StationKey: cs.StationKey,
			StationId:  events.StationId(cs.StationKey),
			LocationId: cs.LocationId,
			Reason:     reason,
			Actor:      actor,
		},
	})
	if err != nil {
		return fmt.Errorf("emit %s: %w", events.TypeStationDecommissioned, err)
	}
	return nil
}

// renderStation answers with the charge station of the org, without its EVSEs.
func (s *Server) renderStation(w http.ResponseWriter, r *http.Request, cs *store.ChargeStation, orgId string) {
	out, err := s.apiChargeStation(r.Context(), cs, orgId)
	if err != nil {
		renderStationError(w, r, err)
		return
	}
	render.JSON(w, r, out)
}
