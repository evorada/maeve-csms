// SPDX-License-Identifier: Apache-2.0

package assets_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/assets"
	"github.com/thoughtworks/maeve-csms/manager/ocpi"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"k8s.io/utils/clock"
)

var renderT0 = time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

func renderAt(minutes int) time.Time {
	return renderT0.Add(time.Duration(minutes) * time.Minute)
}

// partyResolver resolves every party to ES/CCM, or fails with err.
type partyResolver struct {
	err   error
	calls []string
}

func (p *partyResolver) ResolveParty(_ context.Context, orgId, partyRef string) (string, string, error) {
	p.calls = append(p.calls, orgId+"/"+partyRef)
	if p.err != nil {
		return "", "", p.err
	}
	return "ES", "CCM", nil
}

// statusStore holds the connector statuses of the stations and counts the reads.
type statusStore struct {
	store.StatusStore
	statuses map[string][]*store.ConnectorStatus
	reads    map[string]int
	err      error
}

func (s *statusStore) ListConnectorStatuses(_ context.Context, chargeStationId string) ([]*store.ConnectorStatus, error) {
	if s.reads == nil {
		s.reads = map[string]int{}
	}
	s.reads[chargeStationId]++
	if s.err != nil {
		return nil, s.err
	}
	return s.statuses[chargeStationId], nil
}

func connectorStatus(connectorId int, status store.ConnectorStatusType, updatedAt time.Time) *store.ConnectorStatus {
	return &store.ConnectorStatus{ConnectorId: connectorId, Status: status, UpdatedAt: updatedAt}
}

func renderLocation() *store.AssetLocation {
	loc := validLocation()
	loc.Id = "0193b2c5-0000-7000-8000-000000000001"
	loc.Publish = true
	loc.ChargingWhenClosed = ptr(false)
	loc.OpeningTimes.RegularHours = append(loc.OpeningTimes.RegularHours,
		store.RegularHours{Weekday: 6, PeriodBegin: "10:00", PeriodEnd: "14:00"})
	loc.UpdatedAt = renderAt(0)
	return loc
}

func renderEvse(n, ocppEvseId int, status store.AssetEvseStatus, updatedAt time.Time) *store.AssetEvse {
	c := validConnector(1)
	c.Id = fmt.Sprintf("0193b2c5-0000-7000-8000-0000000001%02d", n)
	c.OcpiId = "1"
	c.UpdatedAt = updatedAt
	return &store.AssetEvse{
		Id:         fmt.Sprintf("0193b2c5-0000-7000-8000-0000000000%02d", n),
		StationKey: station16,
		OcppEvseId: ocppEvseId,
		EvseId:     ptr(fmt.Sprintf("ES*CCM*ECP16*%d", ocppEvseId)),
		Status:     status,
		Connectors: []*store.AssetConnector{c},
		UpdatedAt:  updatedAt,
	}
}

func render(t *testing.T, loc *store.AssetLocation, evses []*store.AssetEvse, statuses store.StatusStore, tariffIds assets.TariffIdResolver) *assets.OcpiLocation {
	t.Helper()
	rendered, err := assets.RenderOcpiLocation(context.Background(), loc, evses, statuses, &partyResolver{}, tariffIds)
	require.NoError(t, err)
	return rendered
}

func TestRenderOcpiLocation(t *testing.T) {
	ctx := context.Background()
	loc := renderLocation()
	evses := []*store.AssetEvse{
		renderEvse(1, 1, store.AssetEvseStatusActive, renderAt(1)),
		renderEvse(2, 2, store.AssetEvseStatusActive, renderAt(2)),
	}
	statuses := &statusStore{statuses: map[string][]*store.ConnectorStatus{station16: {
		connectorStatus(0, store.ConnectorStatusAvailable, renderAt(30)), // the station as a whole: not used
		connectorStatus(1, store.ConnectorStatusCharging, renderAt(3)),
		connectorStatus(2, store.ConnectorStatusAvailable, renderAt(4)),
	}}}
	parties := &partyResolver{}

	rendered, err := assets.RenderOcpiLocation(ctx, loc, evses, statuses, parties, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{testOrg + "/party-a"}, parties.calls)
	assert.Equal(t, map[string]int{station16: 1}, statuses.reads, "the statuses of a station are read once")

	document, err := json.Marshal(rendered)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"country_code": "ES",
		"party_id": "CCM",
		"id": "LOC-1",
		"publish": true,
		"name": "Plaza",
		"address": "Calle Mayor 1",
		"city": "Madrid",
		"postal_code": "28013",
		"state": "Madrid",
		"country": "ESP",
		"coordinates": {"latitude": "40.416775", "longitude": "-3.7037902"},
		"time_zone": "Europe/Madrid",
		"parking_type": "ON_STREET",
		"facilities": ["CAFE", "WIFI"],
		"charging_when_closed": false,
		"opening_times": {
			"twentyfourseven": false,
			"regular_hours": [
				{"weekday": 1, "period_begin": "08:00", "period_end": "23:59"},
				{"weekday": 6, "period_begin": "10:00", "period_end": "14:00"}
			],
			"exceptional_closings": [
				{"period_begin": "2026-12-25T00:00:00Z", "period_end": "2026-12-26T00:00:00Z"}
			]
		},
		"evses": [
			{
				"uid": "0193b2c5-0000-7000-8000-000000000001",
				"evse_id": "ES*CCM*ECP16*1",
				"status": "CHARGING",
				"connectors": [{
					"id": "1",
					"standard": "IEC_62196_T2_COMBO",
					"format": "CABLE",
					"power_type": "DC",
					"max_voltage": 920,
					"max_amperage": 200,
					"max_electric_power": 150000,
					"last_updated": "2026-11-27T10:01:00Z"
				}],
				"last_updated": "2026-11-27T10:03:00Z"
			},
			{
				"uid": "0193b2c5-0000-7000-8000-000000000002",
				"evse_id": "ES*CCM*ECP16*2",
				"status": "AVAILABLE",
				"connectors": [{
					"id": "1",
					"standard": "IEC_62196_T2_COMBO",
					"format": "CABLE",
					"power_type": "DC",
					"max_voltage": 920,
					"max_amperage": 200,
					"max_electric_power": 150000,
					"last_updated": "2026-11-27T10:02:00Z"
				}],
				"last_updated": "2026-11-27T10:04:00Z"
			}
		],
		"last_updated": "2026-11-27T10:04:00Z"
	}`, string(document))
}

func TestRenderOcpiLocationEvseStatus(t *testing.T) {
	for status, want := range map[store.ConnectorStatusType]ocpi.EvseStatus{
		store.ConnectorStatusAvailable:     ocpi.EvseStatusAVAILABLE,
		store.ConnectorStatusPreparing:     ocpi.EvseStatusCHARGING,
		store.ConnectorStatusCharging:      ocpi.EvseStatusCHARGING,
		store.ConnectorStatusSuspendedEV:   ocpi.EvseStatusCHARGING,
		store.ConnectorStatusSuspendedEVSE: ocpi.EvseStatusCHARGING,
		store.ConnectorStatusFinishing:     ocpi.EvseStatusCHARGING,
		store.ConnectorStatusOccupied:      ocpi.EvseStatusCHARGING,
		store.ConnectorStatusReserved:      ocpi.EvseStatusRESERVED,
		store.ConnectorStatusFaulted:       ocpi.EvseStatusOUTOFORDER,
		store.ConnectorStatusUnavailable:   ocpi.EvseStatusINOPERATIVE,
		"SomethingNew":                     ocpi.EvseStatusUNKNOWN,
	} {
		t.Run(string(status), func(t *testing.T) {
			statuses := &statusStore{statuses: map[string][]*store.ConnectorStatus{station16: {
				connectorStatus(1, status, renderAt(1)),
			}}}
			rendered := render(t, renderLocation(), []*store.AssetEvse{renderEvse(1, 1, store.AssetEvseStatusActive, renderAt(0))}, statuses, nil)
			assert.Equal(t, want, (*rendered.Evses)[0].Status)
		})
	}

	t.Run("no status", func(t *testing.T) {
		statuses := &statusStore{statuses: map[string][]*store.ConnectorStatus{station16: {
			connectorStatus(2, store.ConnectorStatusAvailable, renderAt(1)),
		}}}
		rendered := render(t, renderLocation(), []*store.AssetEvse{renderEvse(1, 1, store.AssetEvseStatusActive, renderAt(0))}, statuses, nil)
		assert.Equal(t, ocpi.EvseStatusUNKNOWN, (*rendered.Evses)[0].Status)
	})

	t.Run("REMOVED and PLANNED come from the asset", func(t *testing.T) {
		statuses := &statusStore{statuses: map[string][]*store.ConnectorStatus{station16: {
			connectorStatus(1, store.ConnectorStatusAvailable, renderAt(10)),
			connectorStatus(2, store.ConnectorStatusAvailable, renderAt(10)),
		}}}
		rendered := render(t, renderLocation(), []*store.AssetEvse{
			renderEvse(1, 1, store.AssetEvseStatusRemoved, renderAt(1)),
			renderEvse(2, 2, store.AssetEvseStatusPlanned, renderAt(2)),
		}, statuses, nil)
		assert.Equal(t, ocpi.EvseStatusREMOVED, (*rendered.Evses)[0].Status)
		assert.Equal(t, ocpi.EvseStatusPLANNED, (*rendered.Evses)[1].Status)
		assert.Equal(t, "2026-11-27T10:02:00Z", rendered.LastUpdated, "statuses that are not used do not count")
		assert.Empty(t, statuses.reads, "no status is read")
	})

	t.Run("status store error", func(t *testing.T) {
		errStatus := errors.New("status store down")
		_, err := assets.RenderOcpiLocation(context.Background(), renderLocation(),
			[]*store.AssetEvse{renderEvse(1, 1, store.AssetEvseStatusActive, renderAt(0))},
			&statusStore{err: errStatus}, &partyResolver{}, nil)
		assert.ErrorIs(t, err, errStatus)
	})
}

// The statuses of EVSEs are those that the rendered location has.
func TestEvseStatuses(t *testing.T) {
	ctx := context.Background()
	statuses := &statusStore{statuses: map[string][]*store.ConnectorStatus{station16: {
		connectorStatus(1, store.ConnectorStatusCharging, renderAt(1)),
		connectorStatus(3, store.ConnectorStatusAvailable, renderAt(1)),
	}}}
	evses := []*store.AssetEvse{
		renderEvse(1, 1, store.AssetEvseStatusActive, renderAt(0)),
		renderEvse(2, 2, store.AssetEvseStatusActive, renderAt(0)),
		renderEvse(3, 3, store.AssetEvseStatusRemoved, renderAt(0)),
		renderEvse(4, 4, store.AssetEvseStatusPlanned, renderAt(0)),
	}

	got, err := assets.EvseStatuses(ctx, statuses, evses)
	require.NoError(t, err)
	assert.Equal(t, []ocpi.EvseStatus{ocpi.EvseStatusCHARGING, ocpi.EvseStatusUNKNOWN, ocpi.EvseStatusREMOVED, ocpi.EvseStatusPLANNED}, got)
	assert.Equal(t, map[string]int{station16: 1}, statuses.reads, "the statuses of a station are read once")

	rendered := render(t, renderLocation(), evses, statuses, nil)
	for i, evse := range *rendered.Evses {
		assert.Equal(t, evse.Status, got[i])
	}

	none, err := assets.EvseStatuses(ctx, statuses, nil)
	require.NoError(t, err)
	assert.Empty(t, none)

	errStatus := errors.New("status store down")
	_, err = assets.EvseStatuses(ctx, &statusStore{err: errStatus}, evses)
	assert.ErrorIs(t, err, errStatus)
}

func TestRenderOcpiLocationLastUpdated(t *testing.T) {
	statuses := &statusStore{statuses: map[string][]*store.ConnectorStatus{station16: {
		connectorStatus(1, store.ConnectorStatusAvailable, renderAt(5)),
	}}}
	for _, tc := range []struct {
		name                                      string
		location, evse, connector, want, wantEvse int
	}{
		{"location", 9, 1, 2, 9, 5},
		{"EVSE", 1, 9, 2, 9, 9},
		{"connector", 1, 2, 9, 9, 9},
		{"status", 1, 2, 3, 5, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loc := renderLocation()
			loc.UpdatedAt = renderAt(tc.location)
			evse := renderEvse(1, 1, store.AssetEvseStatusActive, renderAt(tc.evse))
			evse.Connectors[0].UpdatedAt = renderAt(tc.connector)
			rendered := render(t, loc, []*store.AssetEvse{evse}, statuses, nil)
			assert.Equal(t, renderAt(tc.want).Format(time.RFC3339), rendered.LastUpdated)
			assert.Equal(t, renderAt(tc.wantEvse).Format(time.RFC3339), (*rendered.Evses)[0].LastUpdated)
		})
	}

	t.Run("in UTC", func(t *testing.T) {
		loc := renderLocation()
		loc.UpdatedAt = time.Date(2026, 11, 27, 12, 0, 0, 0, time.FixedZone("CET", 3600))
		rendered := render(t, loc, nil, statuses, nil)
		assert.Equal(t, "2026-11-27T11:00:00Z", rendered.LastUpdated)
		assert.Nil(t, rendered.Evses, "a location without EVSEs has no evses")
	})
}

func TestRenderOcpiLocationKeepsPublishFalse(t *testing.T) {
	loc := renderLocation()
	loc.Publish = false
	loc.OcpiId = ""
	loc.OpeningTimes = nil
	loc.ChargingWhenClosed = nil
	rendered := render(t, loc, nil, &statusStore{}, nil)

	document, err := json.Marshal(rendered)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(document, &fields))
	assert.Equal(t, false, fields["publish"])
	assert.Equal(t, loc.Id, fields["id"], "the id when the location has no OCPI id")
	assert.NotContains(t, fields, "opening_times")
	assert.NotContains(t, fields, "charging_when_closed")
}

func TestRenderOcpiLocationPartyError(t *testing.T) {
	_, err := assets.RenderOcpiLocation(context.Background(), renderLocation(), nil, &statusStore{},
		&partyResolver{err: store.ErrNotFound}, nil)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestRenderOcpiLocationTariffIds(t *testing.T) {
	evse := renderEvse(1, 1, store.AssetEvseStatusPlanned, renderAt(0))
	evse.Connectors[0].TariffRef = ptr("0193b2c5-0000-7000-8000-000000000050")
	evse.Connectors[0].AdhocTariffRef = ptr("0193b2c5-0000-7000-8000-000000000051")
	withoutTariff := renderEvse(2, 2, store.AssetEvseStatusPlanned, renderAt(0))

	t.Run("left out without a resolver", func(t *testing.T) {
		rendered := render(t, renderLocation(), []*store.AssetEvse{evse}, &statusStore{}, nil)
		document, err := json.Marshal((*rendered.Evses)[0].Connectors[0])
		require.NoError(t, err)
		assert.NotContains(t, string(document), "tariff_ids")
	})

	t.Run("from the resolver", func(t *testing.T) {
		var asked [][]string
		resolver := func(_ context.Context, refs []string) ([]string, error) {
			asked = append(asked, refs)
			if len(refs) == 0 {
				return nil, nil
			}
			return []string{"T-1", "T-ADHOC"}, nil
		}
		rendered := render(t, renderLocation(), []*store.AssetEvse{evse, withoutTariff}, &statusStore{}, resolver)
		assert.Equal(t, &[]string{"T-1", "T-ADHOC"}, (*rendered.Evses)[0].Connectors[0].TariffIds)
		assert.Nil(t, (*rendered.Evses)[1].Connectors[0].TariffIds, "no tariff ids")
		assert.Equal(t, [][]string{{"0193b2c5-0000-7000-8000-000000000050", "0193b2c5-0000-7000-8000-000000000051"}, nil}, asked)
	})

	t.Run("resolver error", func(t *testing.T) {
		errTariffs := errors.New("tariffs down")
		_, err := assets.RenderOcpiLocation(context.Background(), renderLocation(), []*store.AssetEvse{evse}, &statusStore{}, &partyResolver{},
			func(context.Context, []string) ([]string, error) { return nil, errTariffs })
		assert.ErrorIs(t, err, errTariffs)
	})
}

func TestTenantParties(t *testing.T) {
	ctx := context.Background()
	s := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, s.PutTenant(ctx, &store.Tenant{
		OrgId:  testOrg,
		Path:   store.OrgPathLabel(testOrg),
		Slug:   "acme",
		OrgKey: "k7f2",
		Parties: []store.TenantParty{
			{Id: "party-a", Role: "CPO", CountryCode: "ES", PartyId: "CCM"},
			{Id: "party-b", Role: "CPO", CountryCode: "PT", PartyId: "ABC"},
		},
	}))
	parties := assets.TenantParties{Tenants: s}

	countryCode, partyId, err := parties.ResolveParty(ctx, testOrg, "party-b")
	require.NoError(t, err)
	assert.Equal(t, "PT", countryCode)
	assert.Equal(t, "ABC", partyId)

	_, _, err = parties.ResolveParty(ctx, testOrg, "party-c")
	assert.ErrorIs(t, err, store.ErrNotFound, "unknown party")
	_, _, err = parties.ResolveParty(ctx, "0193b2c4-0000-7000-8000-0000000000b2", "party-a")
	assert.ErrorIs(t, err, store.ErrNotFound, "unknown tenant")
}

// The renderer works on what the asset store returns.
func TestRenderOcpiLocationFromTheStore(t *testing.T) {
	ctx := context.Background()
	s := assetStore(t)
	locations, _, err := s.ListAssetLocations(ctx, store.AssetLocationFilter{OrgIds: []string{testOrg}})
	require.NoError(t, err)
	require.Len(t, locations, 1)
	evses, err := s.ListAssetEvsesForLocation(ctx, locations[0].Id)
	require.NoError(t, err)
	require.NoError(t, s.SetConnectorStatus(ctx, station16, 2, &store.ConnectorStatus{ConnectorId: 2, Status: store.ConnectorStatusFaulted}))

	rendered, err := assets.RenderOcpiLocation(ctx, locations[0], evses, s, &partyResolver{}, nil)
	require.NoError(t, err)
	require.Len(t, *rendered.Evses, 3)
	var statuses []ocpi.EvseStatus
	for _, evse := range *rendered.Evses {
		statuses = append(statuses, evse.Status)
		assert.NotEmpty(t, evse.Connectors)
	}
	// ordered by OCPP EVSE id, then station: CP16 EVSE 1, CP16 EVSE 2, CP201 EVSE 2
	assert.Equal(t, []ocpi.EvseStatus{ocpi.EvseStatusUNKNOWN, ocpi.EvseStatusOUTOFORDER, ocpi.EvseStatusUNKNOWN}, statuses)
}
