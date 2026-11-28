// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/render"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/thoughtworks/maeve-csms/manager/assets"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
)

// The errors of the answers to an asset operation that is refused start with these codes.
const (
	errorAlreadyExists         = "already_exists"
	errorVersionConflict       = "version_conflict"
	errorInUse                 = "in_use"
	errorStationPending        = "station_pending"
	errorStationDecommissioned = "station_decommissioned"
	errorActiveTransaction     = "active_transaction"
	errorLastConnector         = "last_connector"
	errorUnknownParty          = "unknown_party"
)

// errAssetsNotAvailable answers an asset operation of a manager whose storage engine does
// not keep assets.
var errAssetsNotAvailable = &ErrResponse{
	HTTPStatusCode: http.StatusNotImplemented,
	StatusText:     http.StatusText(http.StatusNotImplemented),
	ErrorText:      "the storage engine of the manager does not keep assets",
}

// assetAnswer is the answer that a step of an asset operation decided on. It is returned
// as an error, so that the store transaction of the operation is rolled back, and
// renderAssetError answers with it.
type assetAnswer struct {
	answer render.Renderer
}

func (a *assetAnswer) Error() string {
	if e, ok := a.answer.(*ErrResponse); ok {
		return fmt.Sprintf("answered %d %s", e.HTTPStatusCode, e.ErrorText)
	}
	return "answered"
}

func answerWith(answer render.Renderer) error {
	return &assetAnswer{answer: answer}
}

// validationFailed answers a request with fields that break the rules of the asset model
// (422).
type validationFailed struct {
	ValidationErrors
}

func (v *validationFailed) Render(_ http.ResponseWriter, r *http.Request) error {
	render.Status(r, http.StatusUnprocessableEntity)
	return nil
}

func newValidationFailed(invalid assets.ValidationErrors) *validationFailed {
	errs := make([]ValidationError, len(invalid))
	for i, e := range invalid {
		errs[i] = ValidationError{Field: e.Field, Reason: e.Reason}
	}
	return &validationFailed{ValidationErrors{Status: Invalid, Errors: errs}}
}

// validation returns the fields that break a rule: those of err, which is nil or an
// assets.ValidationErrors, followed by more. It returns nil when there are none, and err
// itself when it is another error.
func validation(err error, more ...assets.ValidationError) error {
	var invalid assets.ValidationErrors
	if err != nil && !errors.As(err, &invalid) {
		return err
	}
	invalid = append(slices.Clone(invalid), more...)
	if len(invalid) == 0 {
		return nil
	}
	return invalid
}

// renderAssetError answers an asset operation that failed with err. what names the asset
// of the operation ("location", "EVSE", "connector").
func renderAssetError(w http.ResponseWriter, r *http.Request, what string, err error) {
	var answer *assetAnswer
	var invalid assets.ValidationErrors
	switch {
	case errors.As(err, &answer):
		_ = render.Render(w, r, answer.answer)
	case errors.As(err, &invalid):
		_ = render.Render(w, r, newValidationFailed(invalid))
	case errors.Is(err, store.ErrVersionConflict):
		// changed by another request between the read and the write of this one
		_ = render.Render(w, r, ErrPreconditionFailed(errorVersionConflict+": the "+what+" was changed at the same time"))
	case errors.Is(err, store.ErrAlreadyExists):
		_ = render.Render(w, r, ErrConflict(errorAlreadyExists))
	case errors.Is(err, store.ErrInUse):
		_ = render.Render(w, r, ErrConflict(errorInUse))
	case errors.Is(err, store.ErrNotFound):
		// deleted by another request between the read and the write of this one
		_ = render.Render(w, r, ErrNotFound)
	case errors.Is(err, store.ErrNotImplemented):
		_ = render.Render(w, r, errAssetsNotAvailable)
	default:
		slog.Error("asset operation on a "+what, "method", r.Method, "path", r.URL.Path, "err", err)
		_ = render.Render(w, r, errInternal)
	}
}

// decodeBody reads the JSON body of the request into v. When it cannot be read the
// request is answered (400) and false is reported.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := render.DecodeJSON(r.Body, v); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("the body is not a JSON document of the operation: %w", err)))
		return false
	}
	return true
}

// ifMatch is what the If-Match header of a request lets a change apply to.
type ifMatch struct {
	any      bool     // no header, or "*"
	versions []string // the strong entity tags of the header, without their quotes
}

// parseIfMatch reads an If-Match header: "*" or a list of entity tags such as "3". A weak
// tag (W/"3") never matches: If-Match compares strongly.
func parseIfMatch(header *string) (ifMatch, error) {
	if header == nil || strings.TrimSpace(*header) == "*" {
		return ifMatch{any: true}, nil
	}
	var m ifMatch
	for _, part := range strings.Split(*header, ",") {
		tag := strings.TrimSpace(part)
		weak := strings.HasPrefix(tag, "W/")
		tag = strings.TrimPrefix(tag, "W/")
		if len(tag) < 2 || tag[0] != '"' || tag[len(tag)-1] != '"' || strings.Contains(tag[1:len(tag)-1], `"`) {
			return ifMatch{}, fmt.Errorf(`If-Match must be * or entity tags such as "3", not %q`, *header)
		}
		if !weak {
			m.versions = append(m.versions, tag[1:len(tag)-1])
		}
	}
	return m, nil
}

// check returns the answer (412) to a change of an asset with the version when the
// If-Match header does not allow it.
func (m ifMatch) check(what string, version int64) error {
	if m.any || slices.Contains(m.versions, strconv.FormatInt(version, 10)) {
		return nil
	}
	return answerWith(ErrPreconditionFailed(fmt.Sprintf("%s: the %s has version %d", errorVersionConflict, what, version)))
}

// parseIfMatchOf reads the If-Match header of a request. When it cannot be read the
// request is answered (400) and false is reported.
func parseIfMatchOf(w http.ResponseWriter, r *http.Request, header *string) (ifMatch, bool) {
	m, err := parseIfMatch(header)
	if err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return ifMatch{}, false
	}
	return m, true
}

// setETag sets the ETag of an answer with an asset to the version of the asset.
func setETag(w http.ResponseWriter, version int64) {
	w.Header().Set("ETag", `"`+strconv.FormatInt(version, 10)+`"`)
}

// visible returns the answer (404) to a request for an asset of an org that the caller
// may not reach: such an asset is answered like one that does not exist.
func (s *Server) visible(ctx context.Context, p *tenancy.Principal, orgId string) error {
	inScope, err := s.scoper.InScope(ctx, p, orgId)
	if err != nil {
		return fmt.Errorf("resolve the scope of the caller: %w", err)
	}
	if !inScope {
		return answerWith(ErrNotFound)
	}
	return nil
}

// writable returns the answer (403 tenant_suspended) to a change of an asset of an org
// whose tenant is suspended. The caller has been checked to reach the org.
func (s *Server) writable(ctx context.Context, orgId string) error {
	if err := s.scoper.CheckWritable(ctx, orgId); err != nil {
		if errors.Is(err, tenancy.ErrTenantSuspended) {
			return answerWith(ErrForbidden(errorTenantSuspended))
		}
		return fmt.Errorf("read the status of the tenant: %w", err)
	}
	return nil
}

// lookupLocation returns the location when it exists and the caller may reach its org,
// and otherwise the answer (404) as the error.
func (s *Server) lookupLocation(ctx context.Context, p *tenancy.Principal, locationId string) (*store.AssetLocation, error) {
	loc, err := s.store.LookupAssetLocation(ctx, locationId)
	if err != nil {
		return nil, fmt.Errorf("look up location %s: %w", locationId, err)
	}
	if loc == nil {
		return nil, answerWith(ErrNotFound)
	}
	if err := s.visible(ctx, p, loc.OrgId); err != nil {
		return nil, err
	}
	return loc, nil
}

// locationOf returns the location of an EVSE that the caller may reach.
func (s *Server) locationOf(ctx context.Context, evse *store.AssetEvse) (*store.AssetLocation, error) {
	loc, err := s.store.LookupAssetLocation(ctx, evse.LocationId)
	if err != nil {
		return nil, fmt.Errorf("look up location %s of EVSE %s: %w", evse.LocationId, evse.Id, err)
	}
	if loc == nil {
		return nil, fmt.Errorf("location %s of EVSE %s: %w", evse.LocationId, evse.Id, store.ErrNotFound)
	}
	return loc, nil
}

// emitLocationUpdated stores the location.updated event of a change of the location, or
// of an EVSE or a connector at it, in the store transaction of ctx. Its actor is the
// caller (ActorMiddleware).
func (s *Server) emitLocationUpdated(ctx context.Context, loc *store.AssetLocation, change, evseUid, connectorUid string, publishChanged bool) error {
	err := s.events.Emit(ctx, events.Event{
		Type:         events.TypeLocationUpdated,
		OrgId:        loc.OrgId,
		ResourceType: events.ResourceLocation,
		ResourceId:   loc.Id,
		Data: events.LocationUpdatedData{
			LocationId:     loc.Id,
			OcpiId:         loc.OcpiId,
			PartyRef:       loc.PartyRef,
			Change:         change,
			EvseUid:        evseUid,
			ConnectorUid:   connectorUid,
			Publish:        loc.Publish,
			PublishChanged: publishChanged,
		},
	})
	if err != nil {
		return fmt.Errorf("emit %s: %w", events.TypeLocationUpdated, err)
	}
	return nil
}

// checkLocation returns the fields of a location that break a rule: those of the asset
// model, a party that is not a party of the tenant of the org and, on a change, an org
// that is not the org of the location.
func (s *Server) checkLocation(ctx context.Context, loc *store.AssetLocation, requestedOrg *openapi_types.UUID) error {
	var more []assets.ValidationError
	if requestedOrg != nil && !strings.EqualFold(requestedOrg.String(), loc.OrgId) {
		more = append(more, assets.ValidationError{Field: "org_id", Reason: "must be the org of the location: a location does not move to another org"})
	}
	if loc.PartyRef != "" {
		known, err := s.isPartyOf(ctx, loc.OrgId, loc.PartyRef)
		if err != nil {
			return err
		}
		if !known {
			more = append(more, assets.ValidationError{Field: "party_ref", Reason: "is not a party of the tenant of the org"})
		}
	}
	return validation(assets.ValidateLocation(loc), more...)
}

// isPartyOf reports whether the party is a party of the tenant of the org. The party of an
// org that is not a tenant here cannot be checked and is taken as it is.
func (s *Server) isPartyOf(ctx context.Context, orgId, partyRef string) (bool, error) {
	tenant, err := s.store.GetTenant(ctx, orgId)
	if err != nil {
		return false, fmt.Errorf("look up the tenant of org %s: %w", orgId, err)
	}
	if tenant == nil {
		return true, nil
	}
	for _, party := range tenant.Parties {
		if party.Id == partyRef {
			return true, nil
		}
	}
	return false, nil
}

// ListAssetLocations answers a page of the locations of the orgs that the caller may
// reach.
func (s *Server) ListAssetLocations(w http.ResponseWriter, r *http.Request, params ListAssetLocationsParams) {
	p, ok := s.caller(w, r)
	if !ok {
		return
	}
	limit := store.DefaultPageLimit
	if params.Limit != nil {
		if *params.Limit < 1 || *params.Limit > store.MaxPageLimit {
			_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("limit must be 1 to %d", store.MaxPageLimit)))
			return
		}
		limit = *params.Limit
	}
	orgIds, err := s.scoper.OrgIds(r.Context(), p)
	if err != nil {
		renderScopeError(w, r, err)
		return
	}

	filter := store.AssetLocationFilter{
		OrgIds:   orgIds,
		PartyRef: params.PartyRef,
		Publish:  params.Publish,
		Query:    params.Q,
		Page:     store.PageRequest{Limit: limit},
	}
	if params.Cursor != nil {
		filter.Page.Cursor = *params.Cursor
	}
	locations, next, err := s.store.ListAssetLocations(r.Context(), filter)
	if errors.Is(err, store.ErrInvalidCursor) {
		_ = render.Render(w, r, ErrInvalidRequest(errors.New("cursor is not the next_cursor of a page")))
		return
	}
	if err != nil {
		renderAssetError(w, r, "location", fmt.Errorf("list locations: %w", err))
		return
	}

	page := AssetLocationPage{Items: make([]AssetLocation, len(locations))}
	for i, loc := range locations {
		page.Items[i] = apiAssetLocation(loc)
	}
	if next != "" {
		page.NextCursor = &next
	}
	render.JSON(w, r, page)
}

// CreateAssetLocation creates a location of the requested org, or of the org of the
// caller.
func (s *Server) CreateAssetLocation(w http.ResponseWriter, r *http.Request) {
	in := new(AssetLocationInput)
	if !decodeBody(w, r, in) {
		return
	}
	orgId, ok := s.targetOrg(w, r, in.OrgId)
	if !ok {
		return
	}

	loc := storeAssetLocation(in)
	loc.OrgId = orgId
	if in.Publish == nil {
		loc.Publish = true
	}
	err := s.store.RunInTx(r.Context(), func(ctx context.Context) error {
		if err := s.checkLocation(ctx, loc, nil); err != nil {
			return err
		}
		if err := s.store.CreateAssetLocation(ctx, loc); err != nil {
			if errors.Is(err, store.ErrAlreadyExists) {
				return answerWith(ErrConflict(errorAlreadyExists + ": the party already has a location with this ocpi_id"))
			}
			return fmt.Errorf("create a location: %w", err)
		}
		return s.emitLocationUpdated(ctx, loc, events.LocationChangeCreated, "", "", false)
	})
	if err != nil {
		renderAssetError(w, r, "location", err)
		return
	}

	w.Header().Set("Location", BasePath+"/locations/"+loc.Id)
	s.renderLocation(w, r, http.StatusCreated, loc, nil)
}

// GetAssetLocation answers the location, with its EVSEs when they are asked for.
func (s *Server) GetAssetLocation(w http.ResponseWriter, r *http.Request, locationId openapi_types.UUID, params GetAssetLocationParams) {
	p, ok := s.caller(w, r)
	if !ok {
		return
	}
	if params.Expand != nil && *params.Expand != Evses {
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("expand %q is not known: only evses", *params.Expand)))
		return
	}
	loc, err := s.lookupLocation(r.Context(), p, locationId.String())
	if err != nil {
		renderAssetError(w, r, "location", err)
		return
	}

	var evses []AssetEvse
	if params.Expand != nil {
		evses, err = s.evsesAt(r.Context(), loc.Id)
		if err != nil {
			renderAssetError(w, r, "location", err)
			return
		}
	}
	s.renderLocation(w, r, http.StatusOK, loc, evses)
}

// UpdateAssetLocation replaces the fields of the location.
func (s *Server) UpdateAssetLocation(w http.ResponseWriter, r *http.Request, locationId openapi_types.UUID, params UpdateAssetLocationParams) {
	p, ok := s.caller(w, r)
	if !ok {
		return
	}
	match, ok := parseIfMatchOf(w, r, params.IfMatch)
	if !ok {
		return
	}
	in := new(AssetLocationInput)
	if !decodeBody(w, r, in) {
		return
	}

	var loc *store.AssetLocation
	err := s.store.RunInTx(r.Context(), func(ctx context.Context) error {
		current, err := s.lookupLocation(ctx, p, locationId.String())
		if err != nil {
			return err
		}
		if err := s.writable(ctx, current.OrgId); err != nil {
			return err
		}
		if err := match.check("location", current.Version); err != nil {
			return err
		}

		loc = storeAssetLocation(in)
		loc.Id, loc.OrgId, loc.Version = current.Id, current.OrgId, current.Version
		// what the manager sets on a create keeps its value when the body leaves it out
		if in.OcpiId == nil {
			loc.OcpiId = current.OcpiId
		}
		if in.Publish == nil {
			loc.Publish = current.Publish
		}
		if err := s.checkLocation(ctx, loc, in.OrgId); err != nil {
			return err
		}
		if err := s.store.UpdateAssetLocation(ctx, loc); err != nil {
			if errors.Is(err, store.ErrAlreadyExists) {
				return answerWith(ErrConflict(errorAlreadyExists + ": the party already has another location with this ocpi_id"))
			}
			return fmt.Errorf("update location %s: %w", loc.Id, err)
		}
		return s.emitLocationUpdated(ctx, loc, events.LocationChangeUpdated, "", "", loc.Publish != current.Publish)
	})
	if err != nil {
		renderAssetError(w, r, "location", err)
		return
	}
	s.renderLocation(w, r, http.StatusOK, loc, nil)
}

// DeleteAssetLocation deletes a location without EVSEs.
func (s *Server) DeleteAssetLocation(w http.ResponseWriter, r *http.Request, locationId openapi_types.UUID) {
	p, ok := s.caller(w, r)
	if !ok {
		return
	}
	err := s.store.RunInTx(r.Context(), func(ctx context.Context) error {
		loc, err := s.lookupLocation(ctx, p, locationId.String())
		if err != nil {
			return err
		}
		if err := s.writable(ctx, loc.OrgId); err != nil {
			return err
		}
		if err := s.store.DeleteAssetLocation(ctx, loc.Id); err != nil {
			if errors.Is(err, store.ErrInUse) {
				return answerWith(ErrConflict(errorInUse + ": EVSEs are at the location"))
			}
			return fmt.Errorf("delete location %s: %w", loc.Id, err)
		}
		return s.emitLocationUpdated(ctx, loc, events.LocationChangeDeleted, "", "", false)
	})
	if err != nil {
		renderAssetError(w, r, "location", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetAssetLocationOcpi answers the OCPI Location that the location is published as.
func (s *Server) GetAssetLocationOcpi(w http.ResponseWriter, r *http.Request, locationId openapi_types.UUID) {
	p, ok := s.caller(w, r)
	if !ok {
		return
	}
	loc, err := s.lookupLocation(r.Context(), p, locationId.String())
	if err != nil {
		renderAssetError(w, r, "location", err)
		return
	}
	evses, err := s.store.ListAssetEvsesForLocation(r.Context(), loc.Id)
	if err != nil {
		renderAssetError(w, r, "location", fmt.Errorf("list the EVSEs of location %s: %w", loc.Id, err))
		return
	}
	rendered, err := assets.RenderOcpiLocation(r.Context(), loc, evses, s.store, assets.TenantParties{Tenants: s.store}, nil)
	if errors.Is(err, store.ErrNotFound) {
		// the party of the location is not (or no longer) a party of its tenant
		_ = render.Render(w, r, ErrConflict(errorUnknownParty+": the party of the location is not a party of its tenant"))
		return
	}
	if err != nil {
		renderAssetError(w, r, "location", err)
		return
	}
	render.JSON(w, r, rendered)
}

// renderLocation answers with the location (and its EVSEs, when not nil) and its ETag.
func (s *Server) renderLocation(w http.ResponseWriter, r *http.Request, code int, loc *store.AssetLocation, evses []AssetEvse) {
	out := apiAssetLocation(loc)
	if evses != nil {
		out.Evses = &evses
	}
	setETag(w, loc.Version)
	render.Status(r, code)
	render.JSON(w, r, out)
}

// storeAssetLocation is the location of a request body, without what the store sets.
func storeAssetLocation(in *AssetLocationInput) *store.AssetLocation {
	loc := &store.AssetLocation{
		PartyRef:           in.PartyRef,
		Name:               in.Name,
		Address:            in.Address,
		City:               in.City,
		PostalCode:         in.PostalCode,
		State:              in.State,
		Country:            in.Country,
		Latitude:           in.Coordinates.Latitude,
		Longitude:          in.Coordinates.Longitude,
		TimeZone:           in.TimeZone,
		ParkingType:        in.ParkingType,
		ChargingWhenClosed: in.ChargingWhenClosed,
		Facilities:         []string{},
		ExternalRefs:       []string{},
	}
	if in.OcpiId != nil {
		loc.OcpiId = *in.OcpiId
	}
	if in.Publish != nil {
		loc.Publish = *in.Publish
	}
	if in.Facilities != nil {
		loc.Facilities = slices.Clone(*in.Facilities)
	}
	if in.ExternalRefs != nil {
		loc.ExternalRefs = slices.Clone(*in.ExternalRefs)
	}
	if in.OpeningTimes != nil {
		loc.OpeningTimes = storeOpeningTimes(in.OpeningTimes)
	}
	return loc
}

func storeOpeningTimes(in *OpeningTimes) *store.OpeningTimes {
	out := &store.OpeningTimes{TwentyFourSeven: in.Twentyfourseven}
	if in.RegularHours != nil {
		for _, hours := range *in.RegularHours {
			out.RegularHours = append(out.RegularHours, store.RegularHours{
				Weekday:     hours.Weekday,
				PeriodBegin: hours.PeriodBegin,
				PeriodEnd:   hours.PeriodEnd,
			})
		}
	}
	out.ExceptionalOpenings = storePeriods(in.ExceptionalOpenings)
	out.ExceptionalClosings = storePeriods(in.ExceptionalClosings)
	return out
}

func storePeriods(in *[]ExceptionalPeriod) []store.ExceptionalPeriod {
	if in == nil {
		return nil
	}
	var out []store.ExceptionalPeriod
	for _, period := range *in {
		out = append(out, store.ExceptionalPeriod{PeriodBegin: period.PeriodBegin, PeriodEnd: period.PeriodEnd})
	}
	return out
}

// apiAssetLocation is the location as the API shows it, without its EVSEs.
func apiAssetLocation(loc *store.AssetLocation) AssetLocation {
	out := AssetLocation{
		Id:                 loc.Id,
		OrgId:              loc.OrgId,
		OcpiId:             loc.OcpiId,
		PartyRef:           loc.PartyRef,
		Name:               loc.Name,
		Address:            loc.Address,
		City:               loc.City,
		PostalCode:         loc.PostalCode,
		State:              loc.State,
		Country:            loc.Country,
		Coordinates:        GeoLocation{Latitude: loc.Latitude, Longitude: loc.Longitude},
		TimeZone:           loc.TimeZone,
		ParkingType:        loc.ParkingType,
		Publish:            loc.Publish,
		Facilities:         nonNil(loc.Facilities),
		ChargingWhenClosed: loc.ChargingWhenClosed,
		ExternalRefs:       nonNil(loc.ExternalRefs),
		Version:            loc.Version,
		CreatedAt:          loc.CreatedAt,
		UpdatedAt:          loc.UpdatedAt,
	}
	if loc.OpeningTimes != nil {
		out.OpeningTimes = apiOpeningTimes(loc.OpeningTimes)
	}
	return out
}

func apiOpeningTimes(in *store.OpeningTimes) *OpeningTimes {
	out := &OpeningTimes{Twentyfourseven: in.TwentyFourSeven}
	if len(in.RegularHours) > 0 {
		hours := make([]RegularHours, len(in.RegularHours))
		for i, h := range in.RegularHours {
			hours[i] = RegularHours{Weekday: h.Weekday, PeriodBegin: h.PeriodBegin, PeriodEnd: h.PeriodEnd}
		}
		out.RegularHours = &hours
	}
	out.ExceptionalOpenings = apiPeriods(in.ExceptionalOpenings)
	out.ExceptionalClosings = apiPeriods(in.ExceptionalClosings)
	return out
}

func apiPeriods(in []store.ExceptionalPeriod) *[]ExceptionalPeriod {
	if len(in) == 0 {
		return nil
	}
	out := make([]ExceptionalPeriod, len(in))
	for i, period := range in {
		out[i] = ExceptionalPeriod{PeriodBegin: period.PeriodBegin, PeriodEnd: period.PeriodEnd}
	}
	return &out
}

// nonNil returns an empty list for nil, so that a list is answered as [] and not null.
func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
