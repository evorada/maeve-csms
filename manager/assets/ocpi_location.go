// SPDX-License-Identifier: Apache-2.0

package assets

import (
	"context"
	"fmt"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/ocpi"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// PartyResolver finds the OCPI country code and party id of a party of an org.
type PartyResolver interface {
	ResolveParty(ctx context.Context, orgId, partyRef string) (countryCode, partyId string, err error)
}

// TariffIdResolver returns the OCPI tariff ids of the tariffs that a connector refers to.
type TariffIdResolver func(ctx context.Context, tariffRefs []string) ([]string, error)

// TenantParties resolves party references with the parties of the tenants.
type TenantParties struct {
	Tenants store.TenantStore
}

// ResolveParty returns an error wrapping store.ErrNotFound when the org is not a tenant
// or has no party with the id partyRef.
func (p TenantParties) ResolveParty(ctx context.Context, orgId, partyRef string) (string, string, error) {
	tenant, err := p.Tenants.GetTenant(ctx, orgId)
	if err != nil {
		return "", "", fmt.Errorf("resolve party %s of org %s: %w", partyRef, orgId, err)
	}
	if tenant == nil {
		return "", "", fmt.Errorf("resolve party %s: tenant %s: %w", partyRef, orgId, store.ErrNotFound)
	}
	for _, party := range tenant.Parties {
		if party.Id == partyRef {
			return party.CountryCode, party.PartyId, nil
		}
	}
	return "", "", fmt.Errorf("resolve party %s of org %s: %w", partyRef, orgId, store.ErrNotFound)
}

// OcpiLocation is an OCPI Location. The generated ocpi.Location holds the opening times
// with a single regular period and a single exceptional period of each kind, and
// charging_when_closed as a string; the two fields here replace them in the JSON with
// their OCPI 2.2.1 form (lists of periods, a boolean).
type OcpiLocation struct {
	ocpi.Location
	OpeningTimes       *OcpiHours `json:"opening_times,omitempty"`
	ChargingWhenClosed *bool      `json:"charging_when_closed,omitempty"`
}

// OcpiHours are the opening times of a location (OCPI 2.2.1 Hours).
type OcpiHours struct {
	TwentyFourSeven     bool                     `json:"twentyfourseven"`
	RegularHours        []ocpi.RegularHours      `json:"regular_hours,omitempty"`
	ExceptionalOpenings []ocpi.ExceptionalPeriod `json:"exceptional_openings,omitempty"`
	ExceptionalClosings []ocpi.ExceptionalPeriod `json:"exceptional_closings,omitempty"`
}

// RenderOcpiLocation renders a location and its EVSEs as an OCPI Location of the party of
// the location.
//
// An EVSE that is REMOVED or PLANNED has that status. An ACTIVE EVSE has the status of
// its station's connector with the OCPP EVSE id (the 1.6 connector id; 2.0.1 keeps one
// status per EVSE in the same place): Available is AVAILABLE; Preparing, Charging,
// SuspendedEV, SuspendedEVSE, Finishing and Occupied are CHARGING; Reserved is RESERVED;
// Faulted is OUTOFORDER; Unavailable is INOPERATIVE; no status is UNKNOWN.
//
// The tariff ids of a connector come from tariffIds, and are left out when it is nil.
// last_updated of the location is the latest change of the location, its EVSEs, their
// connectors and the statuses used; that of an EVSE the latest of the EVSE, its
// connectors and its status.
func RenderOcpiLocation(ctx context.Context, loc *store.AssetLocation, evses []*store.AssetEvse,
	statuses store.StatusStore, parties PartyResolver, tariffIds TariffIdResolver) (*OcpiLocation, error) {
	countryCode, partyId, err := parties.ResolveParty(ctx, loc.OrgId, loc.PartyRef)
	if err != nil {
		return nil, fmt.Errorf("render location %s: %w", loc.Id, err)
	}
	r := &locationRenderer{statuses: statuses, tariffIds: tariffIds, stationStatuses: map[string][]*store.ConnectorStatus{}}

	lastUpdated := loc.UpdatedAt
	var ocpiEvses []ocpi.Evse
	for _, evse := range evses {
		ocpiEvse, updated, err := r.evse(ctx, evse)
		if err != nil {
			return nil, fmt.Errorf("render location %s: %w", loc.Id, err)
		}
		ocpiEvses = append(ocpiEvses, ocpiEvse)
		lastUpdated = latest(lastUpdated, updated)
	}

	id := loc.OcpiId
	if id == "" {
		id = loc.Id
	}
	out := &OcpiLocation{
		Location: ocpi.Location{
			CountryCode: countryCode,
			PartyId:     partyId,
			Id:          id,
			Publish:     loc.Publish,
			Name:        loc.Name,
			Address:     loc.Address,
			City:        loc.City,
			PostalCode:  loc.PostalCode,
			State:       loc.State,
			Country:     loc.Country,
			Coordinates: ocpi.GeoLocation{Latitude: loc.Latitude, Longitude: loc.Longitude},
			TimeZone:    &loc.TimeZone,
			LastUpdated: ocpiTime(lastUpdated),
		},
		ChargingWhenClosed: loc.ChargingWhenClosed,
	}
	if loc.ParkingType != nil {
		parkingType := ocpi.LocationParkingType(*loc.ParkingType)
		out.ParkingType = &parkingType
	}
	if len(loc.Facilities) > 0 {
		facilities := make([]ocpi.LocationFacilities, len(loc.Facilities))
		for i, facility := range loc.Facilities {
			facilities[i] = ocpi.LocationFacilities(facility)
		}
		out.Facilities = &facilities
	}
	if ocpiEvses != nil {
		out.Evses = &ocpiEvses
	}
	if loc.OpeningTimes != nil {
		out.OpeningTimes = ocpiHours(loc.OpeningTimes)
	}
	return out, nil
}

// locationRenderer renders the EVSEs of a location. It reads the connector statuses of
// each station once.
type locationRenderer struct {
	statuses        store.StatusStore
	tariffIds       TariffIdResolver
	stationStatuses map[string][]*store.ConnectorStatus
}

// evse returns the OCPI EVSE and its last change.
func (r *locationRenderer) evse(ctx context.Context, evse *store.AssetEvse) (ocpi.Evse, time.Time, error) {
	lastUpdated := evse.UpdatedAt
	connectors := make([]ocpi.Connector, len(evse.Connectors))
	for i, c := range evse.Connectors {
		connector, err := r.connector(ctx, c)
		if err != nil {
			return ocpi.Evse{}, time.Time{}, fmt.Errorf("EVSE %s: %w", evse.Id, err)
		}
		connectors[i] = connector
		lastUpdated = latest(lastUpdated, c.UpdatedAt)
	}

	var status ocpi.EvseStatus
	switch evse.Status {
	case store.AssetEvseStatusRemoved:
		status = ocpi.EvseStatusREMOVED
	case store.AssetEvseStatusPlanned:
		status = ocpi.EvseStatusPLANNED
	default:
		connectorStatus, err := r.connectorStatus(ctx, evse.StationKey, evse.OcppEvseId)
		if err != nil {
			return ocpi.Evse{}, time.Time{}, fmt.Errorf("EVSE %s: %w", evse.Id, err)
		}
		status = ocpi.EvseStatusUNKNOWN
		if connectorStatus != nil {
			status = evseStatus(connectorStatus.Status)
			lastUpdated = latest(lastUpdated, connectorStatus.UpdatedAt)
		}
	}

	return ocpi.Evse{
		Uid:               evse.Id,
		EvseId:            evse.EvseId,
		PhysicalReference: evse.PhysicalReference,
		Status:            status,
		Connectors:        connectors,
		LastUpdated:       ocpiTime(lastUpdated),
	}, lastUpdated, nil
}

func (r *locationRenderer) connector(ctx context.Context, c *store.AssetConnector) (ocpi.Connector, error) {
	connector := ocpi.Connector{
		Id:               c.OcpiId,
		Standard:         ocpi.ConnectorStandard(c.Standard),
		Format:           ocpi.ConnectorFormat(c.Format),
		PowerType:        ocpi.ConnectorPowerType(c.PowerType),
		MaxVoltage:       c.MaxVoltage,
		MaxAmperage:      c.MaxAmperage,
		MaxElectricPower: c.MaxElectricPower,
		LastUpdated:      ocpiTime(c.UpdatedAt),
	}
	if r.tariffIds != nil {
		var refs []string
		for _, ref := range []*string{c.TariffRef, c.AdhocTariffRef} {
			if ref != nil {
				refs = append(refs, *ref)
			}
		}
		ids, err := r.tariffIds(ctx, refs)
		if err != nil {
			return ocpi.Connector{}, fmt.Errorf("tariff ids of connector %s: %w", c.Id, err)
		}
		if len(ids) > 0 {
			connector.TariffIds = &ids
		}
	}
	return connector, nil
}

// connectorStatus returns the status of the connector of the station, or nil when the
// station has reported none.
func (r *locationRenderer) connectorStatus(ctx context.Context, stationKey string, connectorId int) (*store.ConnectorStatus, error) {
	statuses, found := r.stationStatuses[stationKey]
	if !found {
		var err error
		if statuses, err = r.statuses.ListConnectorStatuses(ctx, stationKey); err != nil {
			return nil, fmt.Errorf("connector statuses of %s: %w", stationKey, err)
		}
		r.stationStatuses[stationKey] = statuses
	}
	for _, status := range statuses {
		if status.ConnectorId == connectorId {
			return status, nil
		}
	}
	return nil, nil
}

// evseStatus maps the OCPP status of a connector to the OCPI status of an EVSE.
func evseStatus(status store.ConnectorStatusType) ocpi.EvseStatus {
	switch status {
	case store.ConnectorStatusAvailable:
		return ocpi.EvseStatusAVAILABLE
	case store.ConnectorStatusPreparing, store.ConnectorStatusCharging, store.ConnectorStatusSuspendedEV,
		store.ConnectorStatusSuspendedEVSE, store.ConnectorStatusFinishing, store.ConnectorStatusOccupied:
		return ocpi.EvseStatusCHARGING
	case store.ConnectorStatusReserved:
		return ocpi.EvseStatusRESERVED
	case store.ConnectorStatusFaulted:
		return ocpi.EvseStatusOUTOFORDER
	case store.ConnectorStatusUnavailable:
		return ocpi.EvseStatusINOPERATIVE
	default:
		return ocpi.EvseStatusUNKNOWN
	}
}

func ocpiHours(openingTimes *store.OpeningTimes) *OcpiHours {
	hours := &OcpiHours{TwentyFourSeven: openingTimes.TwentyFourSeven}
	for _, regular := range openingTimes.RegularHours {
		hours.RegularHours = append(hours.RegularHours, ocpi.RegularHours{
			Weekday:     int32(regular.Weekday),
			PeriodBegin: regular.PeriodBegin,
			PeriodEnd:   regular.PeriodEnd,
		})
	}
	hours.ExceptionalOpenings = ocpiPeriods(openingTimes.ExceptionalOpenings)
	hours.ExceptionalClosings = ocpiPeriods(openingTimes.ExceptionalClosings)
	return hours
}

func ocpiPeriods(periods []store.ExceptionalPeriod) []ocpi.ExceptionalPeriod {
	var out []ocpi.ExceptionalPeriod
	for _, period := range periods {
		out = append(out, ocpi.ExceptionalPeriod{
			PeriodBegin: ocpiTime(period.PeriodBegin),
			PeriodEnd:   ocpiTime(period.PeriodEnd),
		})
	}
	return out
}

// ocpiTime formats a time as an OCPI DateTime: UTC, to the second.
func ocpiTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
