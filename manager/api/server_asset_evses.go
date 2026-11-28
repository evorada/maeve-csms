// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/render"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/thoughtworks/maeve-csms/manager/assets"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// activeTransactionsPage is how many active transactions of a charge station are read at
// a time to find one on an EVSE.
const activeTransactionsPage = 100

// lookupEvse returns the EVSE, with its connectors, when it exists and the caller may
// reach its org, and otherwise the answer (404) as the error.
func (s *Server) lookupEvse(ctx context.Context, p *tenancy.Principal, evseUid string) (*store.AssetEvse, error) {
	evse, err := s.store.LookupAssetEvse(ctx, evseUid)
	if err != nil {
		return nil, fmt.Errorf("look up EVSE %s: %w", evseUid, err)
	}
	if evse == nil {
		return nil, answerWith(ErrNotFound)
	}
	if err := s.visible(ctx, p, evse.OrgId); err != nil {
		return nil, err
	}
	return evse, nil
}

// lookupConnectorOf returns the connector of the EVSE, and otherwise the answer (404) as
// the error: a connector of another EVSE is not one of this EVSE.
func (s *Server) lookupConnectorOf(ctx context.Context, evse *store.AssetEvse, connectorUid string) (*store.AssetConnector, error) {
	c, err := s.store.LookupAssetConnector(ctx, connectorUid)
	if err != nil {
		return nil, fmt.Errorf("look up connector %s: %w", connectorUid, err)
	}
	if c == nil || c.EvseUid != evse.Id {
		return nil, answerWith(ErrNotFound)
	}
	return c, nil
}

// evsesAt returns the EVSEs at the location with their live status.
func (s *Server) evsesAt(ctx context.Context, locationId string) ([]AssetEvse, error) {
	evses, err := s.store.ListAssetEvsesForLocation(ctx, locationId)
	if err != nil {
		return nil, fmt.Errorf("list the EVSEs of location %s: %w", locationId, err)
	}
	return s.apiAssetEvses(ctx, evses)
}

// apiAssetEvses returns the EVSEs as the API shows them, with their live status.
func (s *Server) apiAssetEvses(ctx context.Context, evses []*store.AssetEvse) ([]AssetEvse, error) {
	statuses, err := assets.EvseStatuses(ctx, s.store, evses)
	if err != nil {
		return nil, fmt.Errorf("live status of the EVSEs: %w", err)
	}
	out := make([]AssetEvse, len(evses))
	for i, evse := range evses {
		out[i] = apiAssetEvse(evse, AssetEvseLiveStatus(statuses[i]))
	}
	return out, nil
}

// stationForEvses returns the charge station of the station key when it can have EVSEs of
// the org: an active charge station of the org. A station that does not exist and one of
// another org are the same field error, so that nothing is learnt about another org.
func (s *Server) stationForEvses(ctx context.Context, orgId, stationKey string) (*store.ChargeStation, error) {
	stationOrg, exists, err := s.scoper.StationOrg(ctx, stationKey)
	if err != nil {
		return nil, err
	}
	var cs *store.ChargeStation
	if exists && strings.EqualFold(stationOrg, orgId) {
		if cs, err = s.store.LookupChargeStation(ctx, stationKey); err != nil {
			return nil, fmt.Errorf("look up charge station %s: %w", stationKey, err)
		}
	}
	switch {
	case cs == nil:
		return nil, assets.ValidationErrors{{Field: "cs_id", Reason: "is not a charge station of the org of the location"}}
	case cs.State == store.StationStatePending:
		return nil, answerWith(ErrConflict(errorStationPending + ": the charge station waits for approval"))
	case cs.State == store.StationStateDecommissioned:
		return nil, answerWith(ErrConflict(errorStationDecommissioned + ": the charge station is decommissioned"))
	}
	return cs, nil
}

// generatedEvseId returns the eMI3 id of an EVSE that was created without one, made of
// the party of the location, the station id and the OCPP EVSE id.
func (s *Server) generatedEvseId(ctx context.Context, loc *store.AssetLocation, evse *store.AssetEvse) (string, error) {
	countryCode, partyId, err := assets.TenantParties{Tenants: s.store}.ResolveParty(ctx, loc.OrgId, loc.PartyRef)
	if errors.Is(err, store.ErrNotFound) {
		return "", assets.ValidationErrors{{Field: "evse_id", Reason: "is required: the party of the location is not a party of its tenant"}}
	}
	if err != nil {
		return "", err
	}
	return assets.GenerateEvseId(countryCode, partyId, s.chargeStationId(evse.StationKey), evse.OcppEvseId), nil
}

// transactionOn reports whether a transaction is running on the EVSE: its charge station
// reports a current transaction on it, or has a transaction on it that has not ended.
func (s *Server) transactionOn(ctx context.Context, evse *store.AssetEvse) (bool, error) {
	statuses, err := s.store.ListConnectorStatuses(ctx, evse.StationKey)
	if err != nil {
		return false, fmt.Errorf("connector statuses of %s: %w", evse.StationKey, err)
	}
	for _, status := range statuses {
		if status.ConnectorId == evse.OcppEvseId && status.CurrentTransactionId != nil && *status.CurrentTransactionId != "" {
			return true, nil
		}
	}

	for offset := 0; ; offset += activeTransactionsPage {
		transactions, total, err := s.store.ListTransactionsForChargeStation(ctx, evse.StationKey, "active", nil, nil, activeTransactionsPage, offset)
		if err != nil {
			return false, fmt.Errorf("active transactions of %s: %w", evse.StationKey, err)
		}
		for _, transaction := range transactions {
			// OCPP 1.6 transactions name the connector, which is the EVSE
			evseId := transaction.EvseId
			if evseId == nil {
				evseId = transaction.ConnectorId
			}
			if evseId != nil && *evseId == evse.OcppEvseId {
				return true, nil
			}
		}
		if len(transactions) == 0 || int64(offset+len(transactions)) >= total {
			return false, nil
		}
	}
}

// checkConnector returns the fields of a connector that break a rule. prefix is the JSON
// path of the connector in the request ("" for a connector on its own).
func checkConnector(prefix string, c *store.AssetConnector) []assets.ValidationError {
	var invalid []assets.ValidationError
	for _, ref := range []struct {
		field string
		value *string
	}{{"tariff_ref", c.TariffRef}, {"adhoc_tariff_ref", c.AdhocTariffRef}} {
		if ref.value == nil {
			continue
		}
		if _, err := uuid.Parse(*ref.value); err != nil {
			invalid = append(invalid, assets.ValidationError{Field: prefix + ref.field, Reason: "must be the id (a UUID) of a tariff"})
		}
	}
	return invalid
}

// checkEvse returns the fields of an EVSE, and of the connectors that it is created with,
// that break a rule.
func checkEvse(evse *store.AssetEvse, create bool) error {
	var more []assets.ValidationError
	if evse.StationKey == "" {
		more = append(more, assets.ValidationError{Field: "cs_id", Reason: "is required"})
	}
	if create && len(evse.Connectors) == 0 {
		more = append(more, assets.ValidationError{Field: "connectors", Reason: "must have at least one connector"})
	}
	for i, c := range evse.Connectors {
		more = append(more, checkConnector(fmt.Sprintf("connectors[%d].", i), c)...)
	}
	return validation(assets.ValidateEvse(evse), more...)
}

// ListAssetEvses answers the EVSEs of the location.
func (s *Server) ListAssetEvses(w http.ResponseWriter, r *http.Request, locationId openapi_types.UUID) {
	p, ok := s.caller(w, r)
	if !ok {
		return
	}
	loc, err := s.lookupLocation(r.Context(), p, locationId.String())
	if err != nil {
		renderAssetError(w, r, "location", err)
		return
	}
	evses, err := s.evsesAt(r.Context(), loc.Id)
	if err != nil {
		renderAssetError(w, r, "location", err)
		return
	}
	render.JSON(w, r, evses)
}

// CreateAssetEvse creates an EVSE of an active charge station of the org of the location,
// with its connectors.
func (s *Server) CreateAssetEvse(w http.ResponseWriter, r *http.Request, locationId openapi_types.UUID) {
	p, ok := s.caller(w, r)
	if !ok {
		return
	}
	in := new(AssetEvseInput)
	if !decodeBody(w, r, in) {
		return
	}

	evse := storeAssetEvse(in)
	err := s.store.RunInTx(r.Context(), func(ctx context.Context) error {
		loc, err := s.lookupLocation(ctx, p, locationId.String())
		if err != nil {
			return err
		}
		if err := s.writable(ctx, loc.OrgId); err != nil {
			return err
		}
		evse.OrgId, evse.LocationId = loc.OrgId, loc.Id
		if err := checkEvse(evse, true); err != nil {
			return err
		}
		cs, err := s.stationForEvses(ctx, loc.OrgId, evse.StationKey)
		if err != nil {
			return err
		}
		if evse.EvseId == nil {
			evseId, err := s.generatedEvseId(ctx, loc, evse)
			if err != nil {
				return err
			}
			evse.EvseId = &evseId
		}

		if err := s.store.CreateAssetEvse(ctx, evse); err != nil {
			if errors.Is(err, store.ErrAlreadyExists) {
				return answerWith(ErrConflict(errorAlreadyExists + ": the charge station already has an EVSE with this ocpp_evse_id, or the evse_id is taken"))
			}
			return fmt.Errorf("create an EVSE at location %s: %w", loc.Id, err)
		}
		// a charge station is at the location of its first EVSE
		if cs.LocationId == nil {
			cs.LocationId = &loc.Id
			if err := s.store.UpdateChargeStation(ctx, cs); err != nil {
				return fmt.Errorf("set the location of charge station %s: %w", cs.StationKey, err)
			}
		}
		return s.emitLocationUpdated(ctx, loc, events.LocationChangeCreated, evse.Id, "", false)
	})
	if err != nil {
		renderAssetError(w, r, "EVSE", err)
		return
	}

	w.Header().Set("Location", BasePath+"/evses/"+evse.Id)
	s.renderEvse(w, r, http.StatusCreated, evse)
}

// GetAssetEvse answers the EVSE with its connectors.
func (s *Server) GetAssetEvse(w http.ResponseWriter, r *http.Request, evseUid openapi_types.UUID) {
	p, ok := s.caller(w, r)
	if !ok {
		return
	}
	evse, err := s.lookupEvse(r.Context(), p, evseUid.String())
	if err != nil {
		renderAssetError(w, r, "EVSE", err)
		return
	}
	s.renderEvse(w, r, http.StatusOK, evse)
}

// UpdateAssetEvse replaces the fields of the EVSE; its connectors stay as they are.
func (s *Server) UpdateAssetEvse(w http.ResponseWriter, r *http.Request, evseUid openapi_types.UUID, params UpdateAssetEvseParams) {
	p, ok := s.caller(w, r)
	if !ok {
		return
	}
	match, ok := parseIfMatchOf(w, r, params.IfMatch)
	if !ok {
		return
	}
	in := new(AssetEvseInput)
	if !decodeBody(w, r, in) {
		return
	}
	// the connectors have operations of their own
	in.Connectors = nil

	var evse *store.AssetEvse
	err := s.store.RunInTx(r.Context(), func(ctx context.Context) error {
		current, err := s.lookupEvse(ctx, p, evseUid.String())
		if err != nil {
			return err
		}
		if err := s.writable(ctx, current.OrgId); err != nil {
			return err
		}
		if err := match.check("EVSE", current.Version); err != nil {
			return err
		}

		evse = storeAssetEvse(in)
		evse.Id, evse.OrgId, evse.LocationId, evse.Version = current.Id, current.OrgId, current.LocationId, current.Version
		// what the manager sets on a create keeps its value when the body leaves it out
		if in.EvseId == nil {
			evse.EvseId = current.EvseId
		}
		if in.Status == nil {
			evse.Status = current.Status
		}
		if err := checkEvse(evse, false); err != nil {
			return err
		}
		if evse.StationKey != current.StationKey {
			if _, err := s.stationForEvses(ctx, current.OrgId, evse.StationKey); err != nil {
				return err
			}
		}

		if err := s.store.UpdateAssetEvse(ctx, evse); err != nil {
			if errors.Is(err, store.ErrAlreadyExists) {
				return answerWith(ErrConflict(errorAlreadyExists + ": the charge station already has an EVSE with this ocpp_evse_id, or the evse_id is taken"))
			}
			return fmt.Errorf("update EVSE %s: %w", evse.Id, err)
		}
		loc, err := s.locationOf(ctx, evse)
		if err != nil {
			return err
		}
		return s.emitLocationUpdated(ctx, loc, events.LocationChangeUpdated, evse.Id, "", false)
	})
	if err != nil {
		renderAssetError(w, r, "EVSE", err)
		return
	}
	s.renderEvse(w, r, http.StatusOK, evse)
}

// DeleteAssetEvse deletes the EVSE and its connectors, unless a transaction is running on
// it.
func (s *Server) DeleteAssetEvse(w http.ResponseWriter, r *http.Request, evseUid openapi_types.UUID) {
	p, ok := s.caller(w, r)
	if !ok {
		return
	}
	err := s.store.RunInTx(r.Context(), func(ctx context.Context) error {
		evse, err := s.lookupEvse(ctx, p, evseUid.String())
		if err != nil {
			return err
		}
		if err := s.writable(ctx, evse.OrgId); err != nil {
			return err
		}
		if err := s.refuseWhileCharging(ctx, evse); err != nil {
			return err
		}
		loc, err := s.locationOf(ctx, evse)
		if err != nil {
			return err
		}
		if err := s.store.DeleteAssetEvse(ctx, evse.Id); err != nil {
			return fmt.Errorf("delete EVSE %s: %w", evse.Id, err)
		}
		return s.emitLocationUpdated(ctx, loc, events.LocationChangeDeleted, evse.Id, "", false)
	})
	if err != nil {
		renderAssetError(w, r, "EVSE", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// refuseWhileCharging returns the answer (409 active_transaction) when a transaction is
// running on the EVSE.
func (s *Server) refuseWhileCharging(ctx context.Context, evse *store.AssetEvse) error {
	running, err := s.transactionOn(ctx, evse)
	if err != nil {
		return err
	}
	if running {
		return answerWith(ErrConflict(errorActiveTransaction + ": a transaction is running on the EVSE"))
	}
	return nil
}

// CreateAssetConnector creates a connector of the EVSE.
func (s *Server) CreateAssetConnector(w http.ResponseWriter, r *http.Request, evseUid openapi_types.UUID) {
	p, ok := s.caller(w, r)
	if !ok {
		return
	}
	in := new(AssetConnectorInput)
	if !decodeBody(w, r, in) {
		return
	}

	c := storeAssetConnector(in)
	err := s.store.RunInTx(r.Context(), func(ctx context.Context) error {
		evse, err := s.lookupEvse(ctx, p, evseUid.String())
		if err != nil {
			return err
		}
		if err := s.writable(ctx, evse.OrgId); err != nil {
			return err
		}
		c.EvseUid = evse.Id
		if err := validation(assets.ValidateConnector(c), checkConnector("", c)...); err != nil {
			return err
		}
		if err := s.store.CreateAssetConnector(ctx, c); err != nil {
			if errors.Is(err, store.ErrAlreadyExists) {
				return answerWith(ErrConflict(errorAlreadyExists + ": the EVSE already has a connector with this ocpp_connector_id or ocpi_id"))
			}
			return fmt.Errorf("create a connector of EVSE %s: %w", evse.Id, err)
		}
		loc, err := s.locationOf(ctx, evse)
		if err != nil {
			return err
		}
		return s.emitLocationUpdated(ctx, loc, events.LocationChangeCreated, evse.Id, c.Id, false)
	})
	if err != nil {
		renderAssetError(w, r, "connector", err)
		return
	}
	s.renderConnector(w, r, http.StatusCreated, c)
}

// UpdateAssetConnector replaces the fields of the connector.
func (s *Server) UpdateAssetConnector(w http.ResponseWriter, r *http.Request, evseUid openapi_types.UUID, connectorUid openapi_types.UUID, params UpdateAssetConnectorParams) {
	p, ok := s.caller(w, r)
	if !ok {
		return
	}
	match, ok := parseIfMatchOf(w, r, params.IfMatch)
	if !ok {
		return
	}
	in := new(AssetConnectorInput)
	if !decodeBody(w, r, in) {
		return
	}

	var c *store.AssetConnector
	err := s.store.RunInTx(r.Context(), func(ctx context.Context) error {
		evse, err := s.lookupEvse(ctx, p, evseUid.String())
		if err != nil {
			return err
		}
		current, err := s.lookupConnectorOf(ctx, evse, connectorUid.String())
		if err != nil {
			return err
		}
		if err := s.writable(ctx, evse.OrgId); err != nil {
			return err
		}
		if err := match.check("connector", current.Version); err != nil {
			return err
		}

		c = storeAssetConnector(in)
		c.Id, c.OrgId, c.EvseUid, c.Version = current.Id, current.OrgId, current.EvseUid, current.Version
		// what the manager sets on a create keeps its value when the body leaves it out
		if in.OcpiId == nil {
			c.OcpiId = current.OcpiId
		}
		if err := validation(assets.ValidateConnector(c), checkConnector("", c)...); err != nil {
			return err
		}
		if err := s.store.UpdateAssetConnector(ctx, c); err != nil {
			if errors.Is(err, store.ErrAlreadyExists) {
				return answerWith(ErrConflict(errorAlreadyExists + ": another connector of the EVSE has this ocpp_connector_id or ocpi_id"))
			}
			return fmt.Errorf("update connector %s: %w", c.Id, err)
		}
		loc, err := s.locationOf(ctx, evse)
		if err != nil {
			return err
		}
		return s.emitLocationUpdated(ctx, loc, events.LocationChangeUpdated, evse.Id, c.Id, false)
	})
	if err != nil {
		renderAssetError(w, r, "connector", err)
		return
	}
	s.renderConnector(w, r, http.StatusOK, c)
}

// DeleteAssetConnector deletes a connector of the EVSE, unless a transaction is running
// on the EVSE or the EVSE would be left without connectors.
func (s *Server) DeleteAssetConnector(w http.ResponseWriter, r *http.Request, evseUid openapi_types.UUID, connectorUid openapi_types.UUID) {
	p, ok := s.caller(w, r)
	if !ok {
		return
	}
	err := s.store.RunInTx(r.Context(), func(ctx context.Context) error {
		evse, err := s.lookupEvse(ctx, p, evseUid.String())
		if err != nil {
			return err
		}
		c, err := s.lookupConnectorOf(ctx, evse, connectorUid.String())
		if err != nil {
			return err
		}
		if err := s.writable(ctx, evse.OrgId); err != nil {
			return err
		}
		if err := s.refuseWhileCharging(ctx, evse); err != nil {
			return err
		}
		// an OCPI EVSE has at least one connector
		if len(evse.Connectors) <= 1 {
			return answerWith(ErrConflict(errorLastConnector + ": the EVSE needs a connector; delete the EVSE instead"))
		}
		loc, err := s.locationOf(ctx, evse)
		if err != nil {
			return err
		}
		if err := s.store.DeleteAssetConnector(ctx, c.Id); err != nil {
			return fmt.Errorf("delete connector %s: %w", c.Id, err)
		}
		return s.emitLocationUpdated(ctx, loc, events.LocationChangeDeleted, evse.Id, c.Id, false)
	})
	if err != nil {
		renderAssetError(w, r, "connector", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// renderEvse answers with the EVSE, its live status and its ETag.
func (s *Server) renderEvse(w http.ResponseWriter, r *http.Request, code int, evse *store.AssetEvse) {
	out, err := s.apiAssetEvses(r.Context(), []*store.AssetEvse{evse})
	if err != nil {
		renderAssetError(w, r, "EVSE", err)
		return
	}
	setETag(w, evse.Version)
	render.Status(r, code)
	render.JSON(w, r, out[0])
}

// renderConnector answers with the connector and its ETag.
func (s *Server) renderConnector(w http.ResponseWriter, r *http.Request, code int, c *store.AssetConnector) {
	setETag(w, c.Version)
	render.Status(r, code)
	render.JSON(w, r, apiAssetConnector(c))
}

// storeAssetEvse is the EVSE of a request body, with its connectors, without what the
// store sets.
func storeAssetEvse(in *AssetEvseInput) *store.AssetEvse {
	evse := &store.AssetEvse{
		StationKey:        in.CsId,
		OcppEvseId:        in.OcppEvseId,
		EvseId:            in.EvseId,
		PhysicalReference: in.PhysicalReference,
	}
	if in.Status != nil {
		evse.Status = store.AssetEvseStatus(*in.Status)
	}
	if in.Connectors != nil {
		for i := range *in.Connectors {
			evse.Connectors = append(evse.Connectors, storeAssetConnector(&(*in.Connectors)[i]))
		}
	}
	return evse
}

// storeAssetConnector is the connector of a request body, without what the store sets.
func storeAssetConnector(in *AssetConnectorInput) *store.AssetConnector {
	c := &store.AssetConnector{
		OcppConnectorId:  in.OcppConnectorId,
		Standard:         in.Standard,
		Format:           in.Format,
		PowerType:        in.PowerType,
		MaxVoltage:       in.MaxVoltage,
		MaxAmperage:      in.MaxAmperage,
		MaxElectricPower: in.MaxElectricPower,
		TariffRef:        in.TariffRef,
		AdhocTariffRef:   in.AdhocTariffRef,
	}
	if in.OcpiId != nil {
		c.OcpiId = *in.OcpiId
	}
	return c
}

// apiAssetEvse is the EVSE as the API shows it.
func apiAssetEvse(evse *store.AssetEvse, liveStatus AssetEvseLiveStatus) AssetEvse {
	connectors := make([]AssetConnector, len(evse.Connectors))
	for i, c := range evse.Connectors {
		connectors[i] = apiAssetConnector(c)
	}
	return AssetEvse{
		Uid:               evse.Id,
		LocationId:        evse.LocationId,
		CsId:              evse.StationKey,
		OcppEvseId:        evse.OcppEvseId,
		EvseId:            evse.EvseId,
		PhysicalReference: evse.PhysicalReference,
		Status:            AssetEvseStatus(evse.Status),
		LiveStatus:        liveStatus,
		Connectors:        connectors,
		Version:           evse.Version,
		CreatedAt:         evse.CreatedAt,
		UpdatedAt:         evse.UpdatedAt,
	}
}

// apiAssetConnector is the connector as the API shows it.
func apiAssetConnector(c *store.AssetConnector) AssetConnector {
	return AssetConnector{
		Uid:              c.Id,
		EvseUid:          c.EvseUid,
		OcppConnectorId:  c.OcppConnectorId,
		OcpiId:           c.OcpiId,
		Standard:         c.Standard,
		Format:           c.Format,
		PowerType:        c.PowerType,
		MaxVoltage:       c.MaxVoltage,
		MaxAmperage:      c.MaxAmperage,
		MaxElectricPower: c.MaxElectricPower,
		TariffRef:        c.TariffRef,
		AdhocTariffRef:   c.AdhocTariffRef,
		Version:          c.Version,
		CreatedAt:        c.CreatedAt,
		UpdatedAt:        c.UpdatedAt,
	}
}
